package adapter

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gburgyan/aat/internal/grpcstatus"
	"github.com/gburgyan/aat/internal/protoreg"
	"github.com/gburgyan/aat/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// handler answers one unary call. It is given the decoded request and returns
// the reply message, or an error.
type handler func(ctx context.Context, in *dynamicpb.Message) (proto.Message, error)

// startShopServer runs a gRPC server serving shop.v1.Carts/CreateCart with the
// given handler, and returns the executor that talks to it.
func startShopServer(t *testing.T, h handler) *GRPCExecutor {
	t.Helper()
	addr, reg := serveShop(t, h)
	return shopExecutor(t, "grpc://"+addr, reg, TLSConfig{})
}

// shopExecutor returns an executor for target with a pool the test closes.
func shopExecutor(t *testing.T, target string, reg *protoreg.Registry, tlsCfg TLSConfig) *GRPCExecutor {
	t.Helper()
	pool := NewConnPool()
	t.Cleanup(func() { _ = pool.Close() })
	exec, err := NewGRPCExecutor(target, pool, reg, tlsCfg)
	require.NoError(t, err)
	return exec
}

// serveShop runs the server, plaintext or with the given server options, and
// returns the address it listens on and the descriptors it serves.
func serveShop(t *testing.T, h handler, opts ...grpc.ServerOption) (string, *protoreg.Registry) {
	t.Helper()

	path := testutil.WriteDescriptorSet(t, "shop.protoset", testutil.ShopFile())
	reg, err := protoreg.LoadDescriptorSets(path)
	require.NoError(t, err)

	md, err := reg.Method("shop.v1.Carts", "CreateCart")
	require.NoError(t, err)

	// A codec-free service description: the server decodes into a dynamic
	// message of the method's own input type.
	desc := &grpc.ServiceDesc{
		ServiceName: "shop.v1.Carts",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "CreateCart",
			Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := dynamicpb.NewMessage(md.Input())
				if err := dec(in); err != nil {
					return nil, err
				}
				return h(ctx, in)
			},
		}},
		Metadata: "shop/v1/carts.proto",
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer(opts...)
	srv.RegisterService(desc, struct{}{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String(), reg
}

// cartReply builds a shop.v1.Cart from JSON.
func cartReply(t *testing.T, reg *protoreg.Registry, out protoreflect.MessageDescriptor, jsonBody string) proto.Message {
	t.Helper()
	msg, err := reg.JSONToMessage(out, []byte(jsonBody))
	require.NoError(t, err)
	return msg
}

func grpcRequest(body string) *Request {
	return &Request{Protocol: ProtocolGRPC, Path: "shop.v1.Carts/CreateCart", Body: []byte(body)}
}

func TestGRPCExecutor_UnaryCall(t *testing.T) {
	var got *dynamicpb.Message
	exec := startShopServer(t, func(_ context.Context, in *dynamicpb.Message) (proto.Message, error) {
		got = in
		out, _ := exec_reg(t).Method("shop.v1.Carts", "CreateCart")
		return cartReply(t, exec_reg(t), out.Output(), `{"cartId":"c-1","subtotal":"4200","status":"OPEN"}`), nil
	})

	resp, err := exec.Execute(context.Background(), grpcRequest(`{"customerId":"cust-9","currency":"USD"}`))
	require.NoError(t, err)

	// The request reached the server decoded.
	require.NotNil(t, got)
	assert.Equal(t, "cust-9", got.Get(got.Descriptor().Fields().ByJSONName("customerId")).String())

	// OK maps to 200, so every `StatusCode < 400` check in the engine holds.
	assert.Equal(t, 200, resp.StatusCode)
	require.NotNil(t, resp.GRPC)
	assert.Equal(t, grpcstatus.OK, resp.GRPC.Code)
	assert.Equal(t, "OK", resp.GRPC.Name)
	assert.Equal(t, ProtocolGRPC, resp.Protocol)

	// The body is JSON, so extraction needs no notion of protobuf.
	assert.JSONEq(t, `{"cartId":"c-1","subtotal":"4200","status":"OPEN","items":[]}`, string(resp.Body))
}

// exec_reg loads the shop registry for a test that needs descriptors of its own.
func exec_reg(t *testing.T) *protoreg.Registry {
	t.Helper()
	path := testutil.WriteDescriptorSet(t, "shop.protoset", testutil.ShopFile())
	reg, err := protoreg.LoadDescriptorSets(path)
	require.NoError(t, err)
	return reg
}

func TestGRPCExecutor_StatusCodesMapToHTTP(t *testing.T) {
	tests := []struct {
		code     codes.Code
		wantHTTP int
		wantName string
	}{
		{codes.NotFound, 404, "NOT_FOUND"},
		{codes.InvalidArgument, 400, "INVALID_ARGUMENT"},
		{codes.PermissionDenied, 403, "PERMISSION_DENIED"},
		{codes.Unauthenticated, 401, "UNAUTHENTICATED"},
		{codes.ResourceExhausted, 429, "RESOURCE_EXHAUSTED"},
		{codes.Unavailable, 503, "UNAVAILABLE"},
		{codes.Internal, 500, "INTERNAL"},
		{codes.AlreadyExists, 409, "ALREADY_EXISTS"},
	}
	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
				return nil, status.Error(tt.code, "no")
			})
			resp, err := exec.Execute(context.Background(), grpcRequest(`{}`))
			require.NoError(t, err, "a status is a response, not a transport error")

			assert.Equal(t, tt.wantHTTP, resp.StatusCode)
			assert.GreaterOrEqual(t, resp.StatusCode, 400, "a failure must read as one to the engine")
			require.NotNil(t, resp.GRPC)
			assert.Equal(t, tt.wantName, resp.GRPC.Name)
		})
	}
}

func TestGRPCExecutor_FailedCallStillHasAJSONBody(t *testing.T) {
	// Assertions and errorDetection rules read a failure the same way they
	// read a success, so a failed call cannot come back with an empty body.
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		return nil, status.Error(codes.NotFound, "cart 7 not found")
	})

	resp, err := exec.Execute(context.Background(), grpcRequest(`{}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"code":"NOT_FOUND","message":"cart 7 not found"}`, string(resp.Body))
}

func TestGRPCExecutor_MetadataAndTrailers(t *testing.T) {
	exec := startShopServer(t, func(ctx context.Context, _ *dynamicpb.Message) (proto.Message, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		require.Equal(t, []string{"acme"}, md.Get("x-tenant"))
		require.Equal(t, []string{"Bearer tok"}, md.Get("authorization"))

		require.NoError(t, grpc.SetHeader(ctx, metadata.Pairs("x-request-id", "req-1")))
		require.NoError(t, grpc.SetTrailer(ctx, metadata.Pairs("x-cost", "3")))

		out, _ := exec_reg(t).Method("shop.v1.Carts", "CreateCart")
		return cartReply(t, exec_reg(t), out.Output(), `{"cartId":"c-1"}`), nil
	})

	req := grpcRequest(`{}`)
	// Mixed case on the way out: metadata keys are lowercase on the wire.
	req.Headers = map[string]string{"X-Tenant": "acme", "Authorization": "Bearer tok"}

	resp, err := exec.Execute(context.Background(), req)
	require.NoError(t, err)
	// Keys are stored as the wire sent them: lowercase, not canonicalized to
	// X-Request-Id, which is the spelling an archive and a copied grpcurl
	// command show.
	assert.Contains(t, headerKeys(resp.Headers), "x-request-id", "header metadata is wire-spelled")
	assert.NotContains(t, headerKeys(resp.Headers), "X-Request-Id", "no canonicalized spelling")
	assert.Equal(t, []string{"x-cost"}, headerKeys(resp.Trailers), "a trailer is kept apart from a header")
	assert.Nil(t, resp.Headers.Values("x-cost"), "a trailer is not a header")

	// A template reads a value wherever the server chose to put it, in any case.
	assert.Equal(t, []string{"req-1"}, resp.HeaderValues("X-Request-Id"))
	assert.Equal(t, []string{"3"}, resp.HeaderValues("X-Cost"))
	assert.Nil(t, resp.HeaderValues("x-absent"))
}

func TestGRPCExecutor_UnknownRequestFieldFailsBeforeTheWire(t *testing.T) {
	called := false
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		called = true
		return nil, nil
	})

	_, err := exec.Execute(context.Background(), grpcRequest(`{"customerIdd":"c-1"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customerIdd")
	assert.False(t, called, "a misspelled field never reaches the server")
}

// TestGRPCExecutor_MetadataGRPCRefusesIsNotSent checks that metadata grpc-go
// would refuse before dialing is a NotSentError, not the INTERNAL status
// grpc-go would make of it.
func TestGRPCExecutor_MetadataGRPCRefusesIsNotSent(t *testing.T) {
	called := false
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		called = true
		return nil, nil
	})
	for _, md := range []map[string]string{
		{"x-hint": "Zoë"},
		{"x-hint": "a\x00b"},
		{"x hint": "a"},
	} {
		req := grpcRequest(`{}`)
		req.Headers = md
		_, err := exec.Execute(context.Background(), req)
		var notSent *NotSentError
		assert.ErrorAs(t, err, &notSent, "%v", md)
	}
	assert.False(t, called)

	assert.NoError(t, metadataError("x-trace-bin", "\x00\xff"), "a -bin value is bytes")
	assert.NoError(t, metadataError("x-hint", "plain ~ text"))
}

func TestGRPCExecutor_StreamingMethodIsRefused(t *testing.T) {
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		return nil, nil
	})
	req := &Request{Protocol: ProtocolGRPC, Path: "shop.v1.Carts/WatchCart", Body: []byte(`{}`)}
	_, err := exec.Execute(context.Background(), req)
	require.ErrorContains(t, err, "unary methods")
}

func TestGRPCExecutor_UnknownMethod(t *testing.T) {
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		return nil, nil
	})
	req := &Request{Protocol: ProtocolGRPC, Path: "shop.v1.Carts/Nope", Body: []byte(`{}`)}
	_, err := exec.Execute(context.Background(), req)
	require.ErrorContains(t, err, `has no method "Nope"`)
}

func TestGRPCExecutor_UnreachableHostIsUnavailable(t *testing.T) {
	// grpc-go reports an unreachable host as UNAVAILABLE, which maps to 503
	// and so classifies as transient and is retried. The reason survives in
	// the status message, which the archive and run show report.
	reg := exec_reg(t)
	pool := NewConnPool()
	t.Cleanup(func() { _ = pool.Close() })

	exec, err := NewGRPCExecutor("grpc://127.0.0.1:1", pool, reg, TLSConfig{})
	require.NoError(t, err)

	resp, err := exec.Execute(context.Background(), grpcRequest(`{}`))
	require.NoError(t, err)
	assert.Equal(t, 503, resp.StatusCode)
	require.NotNil(t, resp.GRPC)
	assert.Equal(t, "UNAVAILABLE", resp.GRPC.Name)
	assert.Contains(t, resp.GRPC.Message, "connection refused")
}

func TestGRPCExecutor_Interface(t *testing.T) {
	reg := exec_reg(t)
	exec, err := NewGRPCExecutor("grpcs://api.example.com:443", nil, reg, TLSConfig{})
	require.NoError(t, err)

	var e Executor = exec
	assert.Equal(t, ProtocolGRPC, e.Protocol())
	assert.Equal(t, "grpcs://api.example.com:443", e.Target())
	assert.NoError(t, e.Close(), "the pool owns the connections")
}

func TestNewGRPCExecutor_Errors(t *testing.T) {
	t.Run("without descriptors", func(t *testing.T) {
		_, err := NewGRPCExecutor("grpc://localhost:9090", nil, nil, TLSConfig{})
		require.ErrorContains(t, err, "needs a descriptor set")
	})

	t.Run("with an HTTP target", func(t *testing.T) {
		_, err := NewGRPCExecutor("https://api.example.com", nil, exec_reg(t), TLSConfig{})
		require.ErrorContains(t, err, "is not a gRPC target")
	})
}

func TestGRPCExecutor_RefusesAnHTTPRequest(t *testing.T) {
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		return nil, nil
	})
	_, err := exec.Execute(context.Background(), &Request{Method: "GET", Path: "/carts"})
	require.ErrorContains(t, err, "a gRPC executor sends gRPC requests")
}

func TestConnPool_ReusesAndCloses(t *testing.T) {
	pool := NewConnPool()
	target := "grpc://127.0.0.1:9091"

	first, err := pool.Get(target, false, TLSConfig{})
	require.NoError(t, err)
	second, err := pool.Get(target, false, TLSConfig{})
	require.NoError(t, err)
	assert.Same(t, first, second, "one connection serves every step on a target")

	other, err := pool.Get("grpc://127.0.0.1:9092", false, TLSConfig{})
	require.NoError(t, err)
	assert.NotSame(t, first, other)

	require.NoError(t, pool.Close())
	require.NoError(t, pool.Close(), "Close is idempotent")

	_, err = pool.Get(target, false, TLSConfig{})
	require.ErrorContains(t, err, "closed")
}

func TestConnPool_SeparatesConnectionsByTLSSettings(t *testing.T) {
	pool := NewConnPool()
	t.Cleanup(func() { _ = pool.Close() })

	plain, err := pool.Get("grpc://127.0.0.1:9091", false, TLSConfig{})
	require.NoError(t, err)
	secure, err := pool.Get("grpc://127.0.0.1:9091", true, TLSConfig{})
	require.NoError(t, err)
	assert.NotSame(t, plain, secure)

	named, err := pool.Get("grpc://127.0.0.1:9091", true, TLSConfig{ServerName: "other"})
	require.NoError(t, err)
	assert.NotSame(t, secure, named)
}

func TestParseGRPCTarget(t *testing.T) {
	tests := []struct {
		in         string
		dial       string
		secure     bool
		wantErrStr string
	}{
		{in: "grpc://localhost:9090", dial: "localhost:9090"},
		{in: "grpcs://api.example.com:443", dial: "api.example.com:443", secure: true},
		{in: "grpc://localhost:9090/", dial: "localhost:9090"},
		{in: "grpc://dns:///api.example.com:443", dial: "dns:///api.example.com:443"},
		{in: "https://api.example.com", wantErrStr: "is not a gRPC target"},
		{in: "localhost:9090", wantErrStr: "is not a gRPC target"},
		{in: "grpc://", wantErrStr: "names no host"},
		{in: "grpc://unix:///run/api.sock", dial: "unix:///run/api.sock"},
		{in: "grpc://[::1]:9090", dial: "[::1]:9090"},
		// grpc-go would default a missing port to 443 either way. For grpcs://
		// that is the right port, and it is written out; for grpc:// it is
		// plaintext sent to a TLS port.
		{in: "grpcs://api.example.com", dial: "api.example.com:443", secure: true},
		{in: "grpcs://[2001:db8::1]", dial: "[2001:db8::1]:443", secure: true},
		{in: "grpc://localhost", wantErrStr: "names no port"},
		{in: "grpc://host/shop.v1.Carts", wantErrStr: "has a path"},
		// A port has a colon too, and must not pass for a resolver's.
		{in: "grpc://localhost:8767/shop.v1.Payments", wantErrStr: "has a path"},
		{in: "grpcs://api.example.com:443/shop.v1.Payments/Charge", wantErrStr: "has a path"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			dial, secure, err := ParseGRPCTarget(tt.in)
			if tt.wantErrStr != "" {
				require.ErrorContains(t, err, tt.wantErrStr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.dial, dial)
			assert.Equal(t, tt.secure, secure)
		})
	}
}

func TestIsGRPCTarget(t *testing.T) {
	assert.True(t, IsGRPCTarget("grpc://localhost:9090"))
	assert.True(t, IsGRPCTarget("grpcs://api.example.com:443"))
	assert.False(t, IsGRPCTarget("https://api.example.com"))
	assert.False(t, IsGRPCTarget(""))
}

func TestGRPCStatus_EnvelopeIsStable(t *testing.T) {
	s := &GRPCStatus{Code: grpcstatus.NotFound, Name: "NOT_FOUND", Message: "gone"}
	assert.JSONEq(t, `{"code":"NOT_FOUND","message":"gone"}`, string(s.envelope()))
	assert.Equal(t, string(s.envelope()), string(s.envelope()))
}

func TestTLSConfig_Build(t *testing.T) {
	t.Run("the zero value verifies against the system roots", func(t *testing.T) {
		cfg, err := TLSConfig{}.build()
		require.NoError(t, err)
		assert.Nil(t, cfg.RootCAs)
		assert.False(t, cfg.InsecureSkipVerify)
		assert.Empty(t, cfg.Certificates)
	})

	t.Run("a missing CA bundle names the file", func(t *testing.T) {
		_, err := TLSConfig{CAFile: "/no/such/ca.pem"}.build()
		require.ErrorContains(t, err, "reading CA bundle")
	})

	t.Run("a CA bundle with no certificates says so", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.pem")
		require.NoError(t, os.WriteFile(path, []byte("not a certificate"), 0o600))
		_, err := TLSConfig{CAFile: path}.build()
		require.ErrorContains(t, err, "no certificates found")
	})

	t.Run("half a client certificate is an error", func(t *testing.T) {
		_, err := TLSConfig{CertFile: "client.pem"}.build()
		require.ErrorContains(t, err, "needs both cert and key")
		_, err = TLSConfig{KeyFile: "client-key.pem"}.build()
		require.ErrorContains(t, err, "needs both cert and key")
	})

	t.Run("settings distinguish pooled connections", func(t *testing.T) {
		assert.NotEqual(t, TLSConfig{}.key(), TLSConfig{ServerName: "a"}.key())
		assert.NotEqual(t, TLSConfig{CAFile: "a"}.key(), TLSConfig{CAFile: "b"}.key())
		assert.Equal(t, TLSConfig{CAFile: "a"}.key(), TLSConfig{CAFile: "a"}.key())
	})
}

func TestResponse_HeaderValuesSpansTrailers(t *testing.T) {
	resp := &Response{
		Headers:  http.Header{"X-One": []string{"a"}, "Shared": []string{"from-header"}},
		Trailers: http.Header{"X-Two": []string{"b"}, "Shared": []string{"from-trailer"}},
	}
	assert.Equal(t, []string{"a"}, resp.HeaderValues("x-one"))
	assert.Equal(t, []string{"b"}, resp.HeaderValues("X-TWO"))
	assert.Equal(t, []string{"from-header"}, resp.HeaderValues("shared"), "a header wins over a trailer of the same name")

	merged := resp.mergedHeaders()
	assert.Equal(t, []string{"from-header", "from-trailer"}, merged.Values("Shared"), "the merged view keeps both")
}

func TestResponse_HeaderValuesWithoutTrailers(t *testing.T) {
	resp := &Response{Headers: http.Header{"Content-Type": []string{"application/json"}}}
	assert.Equal(t, []string{"application/json"}, resp.HeaderValues("content-type"))
	assert.Nil(t, resp.Trailers, "an HTTP response carries no trailers")
}

func TestNewGRPCExecutor_DefaultTimeout(t *testing.T) {
	exec, err := NewGRPCExecutor("grpc://localhost:9090", nil, exec_reg(t), TLSConfig{})
	require.NoError(t, err)
	assert.Equal(t, DefaultRequestTimeout, exec.timeout, "a gRPC call is bounded like an HTTP request")
}

// A server that accepts the call and never replies must not hang the step.
func TestGRPCExecutor_TimeoutNamesTheLimit(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	exec := startShopServer(t, func(ctx context.Context, _ *dynamicpb.Message) (proto.Message, error) {
		select {
		case <-block:
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	})
	exec.timeout = 100 * time.Millisecond

	_, err := exec.Execute(context.Background(), grpcRequest(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no response within aat's 100ms request timeout")
}

// A run the user interrupted got no answer. grpc-go reports it as CANCELLED,
// which is not something the server said, so it is an error, as it is over
// HTTP, and is not blamed on the timeout either.
func TestGRPCExecutor_CancelledRunIsAnErrorNotAResponse(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	exec := startShopServer(t, func(ctx context.Context, _ *dynamicpb.Message) (proto.Message, error) {
		select {
		case <-block:
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	})
	exec.timeout = time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	resp, err := exec.Execute(ctx, grpcRequest(`{}`))
	require.Error(t, err)
	assert.Nil(t, resp, "nothing to assert on, and nothing to archive as an exchange")
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "request timeout")
}

// The same for a deadline the caller set, such as the budget an aborted run
// gives its cleanup: it is aat's deadline, not the server's DEADLINE_EXCEEDED.
func TestGRPCExecutor_CallersDeadlineIsAnErrorNotAResponse(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	exec := startShopServer(t, func(ctx context.Context, _ *dynamicpb.Message) (proto.Message, error) {
		select {
		case <-block:
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	})
	exec.timeout = time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	resp, err := exec.Execute(ctx, grpcRequest(`{}`))
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// expiredCtx is a context whose deadline has passed but whose timer has not
// fired yet: Err is still nil. It is the instant a caller's deadline races
// the server's DEADLINE_EXCEEDED for it, held still so a test can land in it.
type expiredCtx struct{ context.Context }

func (expiredCtx) Deadline() (time.Time, bool) { return time.Now().Add(-time.Millisecond), true }

// The server's answer to the caller's own deadline can arrive before the
// caller's timer fires. It is still the caller's deadline, not a response.
func TestGRPCExecutor_CallersDeadlineBeatenByTheServerIsStillAnError(t *testing.T) {
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		return nil, status.Error(codes.DeadlineExceeded, "context deadline exceeded")
	})

	resp, err := exec.Execute(expiredCtx{context.Background()}, grpcRequest(`{}`))
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// A deadline the server reports on its own account is still a response.
func TestGRPCExecutor_ServersDeadlineExceededIsAResponse(t *testing.T) {
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		return nil, status.Error(codes.DeadlineExceeded, "the inventory service did not answer")
	})

	resp, err := exec.Execute(context.Background(), grpcRequest(`{}`))
	require.NoError(t, err)
	require.NotNil(t, resp.GRPC)
	assert.Equal(t, "DEADLINE_EXCEEDED", resp.GRPC.Name)
}

// google.rpc's error details are what a server says was wrong with a request,
// and a project's descriptor set rarely includes them. They are read anyway.
func TestGRPCExecutor_StandardErrorDetailsAreRead(t *testing.T) {
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		st, err := status.New(codes.InvalidArgument, "the cart is not valid").WithDetails(
			&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{
				{Field: "currency", Description: "must be an ISO 4217 code"},
			}},
			&errdetails.ErrorInfo{Reason: "CURRENCY_UNKNOWN", Domain: "shop.example.com"},
		)
		require.NoError(t, err)
		return nil, st.Err()
	})

	resp, err := exec.Execute(context.Background(), grpcRequest(`{}`))
	require.NoError(t, err)
	require.Len(t, resp.GRPC.Details, 2)
	first := gjson.ParseBytes(resp.GRPC.Details[0])
	assert.Equal(t, "type.googleapis.com/google.rpc.BadRequest", first.Get("@type").String())
	assert.Equal(t, "currency", first.Get("fieldViolations.0.field").String(), "the detail itself, not its type URL alone")
	assert.Equal(t, "CURRENCY_UNKNOWN", gjson.GetBytes(resp.Body, "details.1.reason").String(),
		"and an assertion reads them from the body")
}

// grpc-go refuses a reply over 4 MiB unless told otherwise, as
// RESOURCE_EXHAUSTED: a transient status, retried to no end. An HTTP response
// of any size is read, and so is this.
func TestGRPCExecutor_ReadsAReplyOverFourMiB(t *testing.T) {
	var reply proto.Message
	exec := startShopServer(t, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		return reply, nil
	})
	md, err := exec.reg.Method("shop.v1.Carts", "CreateCart")
	require.NoError(t, err)
	big := strings.Repeat("x", 5<<20)
	reply = cartReply(t, exec.reg, md.Output(), `{"cartId":"`+big+`"}`)

	resp, err := exec.Execute(context.Background(), grpcRequest(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "OK", resp.GRPC.Name)
	assert.Greater(t, len(resp.Body), 5<<20)
}

// An executor given no pool makes one nothing else can reach, so closing the
// executor has to close it.
func TestGRPCExecutor_ClosesAPoolItMade(t *testing.T) {
	reg := exec_reg(t)

	owned, err := NewGRPCExecutor("grpc://localhost:9090", nil, reg, TLSConfig{})
	require.NoError(t, err)
	require.NoError(t, owned.Close())
	_, err = owned.pool.Get("grpc://localhost:9090", false, TLSConfig{})
	require.Error(t, err, "the pool it made is closed")

	pool := NewConnPool()
	shared, err := NewGRPCExecutor("grpc://localhost:9090", pool, reg, TLSConfig{})
	require.NoError(t, err)
	require.NoError(t, shared.Close())
	_, err = pool.Get("grpc://localhost:9090", false, TLSConfig{})
	require.NoError(t, err, "a pool it was given is the run's to close")
	require.NoError(t, pool.Close())
}

// A key ending in -bin carries bytes. A template writes them as base64 and an
// archive records them as base64, which is how they travel; between the two
// grpc-go does the wire encoding, so aat must neither encode twice on the way
// out nor write raw bytes into JSON on the way back.
func TestGRPCExecutor_BinaryMetadata(t *testing.T) {
	sent := []byte{0x00, 0xff, 0x10, 0x80}
	reply := []byte{0xde, 0xad, 0xbe, 0xef}

	var got []string
	exec := startShopServer(t, func(ctx context.Context, _ *dynamicpb.Message) (proto.Message, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		got = md.Get("x-trace-bin")
		require.NoError(t, grpc.SetTrailer(ctx, metadata.Pairs("x-signature-bin", string(reply))))
		return nil, status.Error(codes.NotFound, "no such cart")
	})

	req := grpcRequest(`{}`)
	req.Headers = map[string]string{"X-Trace-Bin": base64.StdEncoding.EncodeToString(sent)}
	resp, err := exec.Execute(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, sent, []byte(got[0]), "the server reads the bytes the template's base64 named")
	// Keys are stored as the wire spells them, so the map is read as a map.
	trailers := map[string][]string(resp.Trailers)
	assert.Equal(t, []string{base64.StdEncoding.EncodeToString(reply)}, trailers["x-signature-bin"],
		"a binary value is recorded as base64, not as bytes JSON cannot hold")
}

func TestOutgoingMetadataValue(t *testing.T) {
	assert.Equal(t, "aGVsbG8=", outgoingMetadataValue("x-note", "aGVsbG8="), "a text key is sent as written")
	assert.Equal(t, "hello", outgoingMetadataValue("x-note-bin", "aGVsbG8="))
	assert.Equal(t, "hello", outgoingMetadataValue("x-note-bin", "aGVsbG8"), "unpadded base64 is base64 too")
	assert.Equal(t, "not base64!", outgoingMetadataValue("x-note-bin", "not base64!"), "anything else is sent as its bytes")
}

// headerKeys returns a header map's keys as stored, sorted, for asserting the
// spelling rather than the lookup.
func headerKeys(h http.Header) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
