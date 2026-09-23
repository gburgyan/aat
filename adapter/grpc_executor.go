package adapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gburgyan/aat/internal/grpcstatus"
	"github.com/gburgyan/aat/internal/protoreg"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/dynamicpb"
)

// GRPCExecutor sends adapter-built requests as unary gRPC calls.
//
// It owns no connection of its own: the pool does, so every executor routed to
// the same target shares one. Closing an executor is therefore a no-op, and
// the run closes the pool.
type GRPCExecutor struct {
	target string
	pool   *ConnPool
	// ownsPool is set when the executor was given no pool and made one.
	ownsPool bool
	reg      *protoreg.Registry
	secure   bool
	tls      TLSConfig
	// timeout bounds one call, as http.Client.Timeout bounds one request. It
	// is DefaultRequestTimeout; tests shorten it.
	timeout time.Duration
}

var _ Executor = (*GRPCExecutor)(nil)

// NewGRPCExecutor creates an executor for a gRPC target, such as
// "grpc://localhost:9090" or "grpcs://api.example.com:443". The registry
// supplies the descriptors the messages are built and read with.
func NewGRPCExecutor(target string, pool *ConnPool, reg *protoreg.Registry, tlsCfg TLSConfig) (*GRPCExecutor, error) {
	if reg == nil {
		return nil, fmt.Errorf("gRPC target %s needs a descriptor set: name one with proto: in the project manifest or the graph", target)
	}
	_, secure, err := ParseGRPCTarget(target)
	if err != nil {
		return nil, err
	}
	ownsPool := pool == nil
	if ownsPool {
		pool = NewConnPool()
	}
	return &GRPCExecutor{target: target, pool: pool, ownsPool: ownsPool, reg: reg, secure: secure, tls: tlsCfg, timeout: DefaultRequestTimeout}, nil
}

// Protocol names the wire protocol: gRPC.
func (e *GRPCExecutor) Protocol() string { return ProtocolGRPC }

// Target returns the target requests are sent to.
func (e *GRPCExecutor) Target() string { return e.target }

// Close releases nothing when the executor was given a pool: the pool owns the
// connections, and the run closes it. An executor made without one made its
// own, which nothing else can reach, and closes that.
func (e *GRPCExecutor) Close() error {
	if e.ownsPool {
		return e.pool.Close()
	}
	return nil
}

// Execute sends req as a unary call and returns the response.
//
// The response always carries a body, and it is always JSON. A call that
// succeeded gives the reply message; one that failed gives a status envelope,
// so extract rules, assertions, and error-detection rules read a failure the
// same way they read a success.
func (e *GRPCExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	if req.Protocol != ProtocolGRPC {
		return nil, fmt.Errorf("executing %s: a gRPC executor sends gRPC requests", req.Path)
	}
	service, method, ok := protoreg.SplitFullMethod(req.Path)
	if !ok {
		return nil, fmt.Errorf("executing %q: it must name a service and a method", req.Path)
	}

	md, err := e.reg.Method(service, method)
	if err != nil {
		return nil, fmt.Errorf("executing %s: %w", req.Path, err)
	}
	if md.IsStreamingClient() || md.IsStreamingServer() {
		return nil, fmt.Errorf("executing %s: aat runs unary methods, where one request has one response", req.Path)
	}

	body := req.Body
	if len(body) == 0 {
		body = []byte("{}")
	}
	in, err := e.reg.JSONToMessage(md.Input(), body)
	if err != nil {
		return nil, &NotSentError{Err: fmt.Errorf("building the request for %s: %w", req.Path, err)}
	}

	conn, err := e.pool.Get(e.target, e.secure, e.tls)
	if err != nil {
		return nil, err
	}

	if len(req.Headers) > 0 {
		pairs := make([]string, 0, len(req.Headers)*2)
		for k, v := range req.Headers {
			// Metadata keys are lowercase on the wire; grpc-go rejects
			// anything else rather than folding it.
			key := strings.ToLower(k)
			pairs = append(pairs, key, outgoingMetadataValue(key, v))
		}
		ctx = metadata.AppendToOutgoingContext(ctx, pairs...)
	}

	// The call gets the same deadline an HTTP request gets from the client's
	// Timeout. grpc.NewClient does not connect, so the first RPC dials too,
	// and without this a wedged server or an unreachable host would hang the
	// step for as long as the run lasts.
	callCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	out := dynamicpb.NewMessage(md.Output())
	var header, trailer metadata.MD
	start := time.Now()
	invokeErr := conn.Invoke(callCtx, fullMethod(service, method), in, out,
		grpc.Header(&header), grpc.Trailer(&trailer))

	if invokeErr != nil {
		// A run that was interrupted, or whose own deadline passed, did not
		// get an answer, and grpc-go's CANCELLED or DEADLINE_EXCEEDED for it is
		// not something the server said. Returning it as a response would have
		// the step's assertions run against it, and the archive record an
		// exchange that never finished. It is an error, as it is over HTTP.
		if err := callerErr(ctx); err != nil {
			return nil, fmt.Errorf("executing %s: %w", req.Path, err)
		}
		// A DEADLINE_EXCEEDED the caller never asked for reads as a server
		// behaviour rather than aat's limit, so the limit says so itself. A
		// cancelled run, or a deadline the server hit on its own terms before
		// ours, is left alone.
		if callTimedOut(ctx, invokeErr, start, e.timeout) {
			return nil, fmt.Errorf("executing %s: no response within aat's %s request timeout", req.Path, e.timeout)
		}
		return e.errorResponse(invokeErr, header, trailer)
	}

	respBody, err := e.reg.MessageToJSON(out)
	if err != nil {
		return nil, fmt.Errorf("reading the response of %s: %w", req.Path, err)
	}
	return &Response{
		Protocol:   ProtocolGRPC,
		StatusCode: grpcstatus.HTTPStatus(grpcstatus.OK),
		Headers:    metadataHeader(header),
		Trailers:   metadataHeader(trailer),
		Body:       respBody,
		GRPC:       &GRPCStatus{Code: grpcstatus.OK, Name: grpcstatus.Name(grpcstatus.OK)},
	}, nil
}

// callerErr is ctx.Err, except that a deadline already past counts before its
// timer fires. The server gets the caller's deadline too, and its
// DEADLINE_EXCEEDED for it can arrive first, while ctx.Err is still nil; that
// answer is to the caller's deadline, not something the server decided.
func callerErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// callTimedOut reports whether the call ran out of aat's request timeout
// rather than the caller's own context: it ended in DEADLINE_EXCEEDED, the
// whole limit had elapsed, and the run itself was still live. The status and
// the clock decide it, not the call context's timer: the server gets the same
// deadline, and its DEADLINE_EXCEEDED for it can arrive before that timer fires.
func callTimedOut(ctx context.Context, invokeErr error, start time.Time, limit time.Duration) bool {
	return callerErr(ctx) == nil &&
		status.Code(invokeErr) == codes.DeadlineExceeded &&
		time.Since(start) >= limit
}

// errorResponse turns a failed call into a response.
//
// Nearly everything that goes wrong in gRPC carries a status, an unreachable
// host included: grpc-go reports that as UNAVAILABLE, which maps to 503 and so
// classifies as transient and is retried, exactly as it should be. Returning
// it as an error instead would be worse, because the engine recognises a
// network failure by unwrapping a net.OpError, which a status error is not.
//
// Only something with no status at all — which should not happen — is returned
// as an error.
func (e *GRPCExecutor) errorResponse(invokeErr error, header, trailer metadata.MD) (*Response, error) {
	st, ok := status.FromError(invokeErr)
	if !ok {
		return nil, fmt.Errorf("executing gRPC request: %w", invokeErr)
	}
	// A handshake that failed is not the service being unavailable. grpc-go
	// reports it as UNAVAILABLE, which is transient and so retried, but a
	// certificate that is not trusted, or a client certificate the server
	// wanted and did not get, fails the same way every time: it is the
	// environment's grpc.tls block that is wrong. It is an error, which is not
	// retried, and it says where to look.
	if st.Code() == codes.Unavailable && isTLSFailure(st.Message()) {
		return nil, fmt.Errorf("connecting to %s: the TLS handshake failed: %s (check the scheme, and the environment's grpc.tls settings: caFile, certFile and keyFile, serverName)", e.target, tlsFailureDetail(st.Message()))
	}

	code := uint32(st.Code())
	if code > grpcstatus.MaxCode {
		return nil, fmt.Errorf("executing gRPC request: %w", invokeErr)
	}

	gs := &GRPCStatus{
		Code:    code,
		Name:    grpcstatus.Name(code),
		Message: st.Message(),
	}
	for _, detail := range st.Proto().GetDetails() {
		encoded, err := e.reg.MessageToJSON(detail)
		if err != nil {
			// A detail whose type the descriptors do not name must not lose
			// the status it came with.
			encoded = []byte(fmt.Sprintf(`{"@type":%q}`, detail.GetTypeUrl()))
		}
		gs.Details = append(gs.Details, encoded)
	}

	return &Response{
		Protocol:   ProtocolGRPC,
		StatusCode: grpcstatus.HTTPStatus(code),
		Headers:    metadataHeader(header),
		Trailers:   metadataHeader(trailer),
		Body:       gs.envelope(),
		GRPC:       gs,
	}, nil
}

// envelope renders a failed call's status as the JSON body of the response.
// It mirrors the error envelope an API of AAT's own examples returns, so the
// same assertions and errorDetection rules read both.
func (s *GRPCStatus) envelope() []byte {
	type envelope struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Details []json.RawMessage `json:"details,omitempty"`
	}
	body, err := json.Marshal(envelope{Code: s.Name, Message: s.Message, Details: s.Details})
	if err != nil {
		return []byte(`{"code":"UNKNOWN","message":"the status could not be encoded"}`)
	}
	return body
}

// fullMethod renders the path gRPC puts on the wire.
func fullMethod(service, method string) string { return "/" + service + "/" + method }

// metadataHeader renders gRPC metadata as a header set, keeping every value of
// a repeated key. It returns nil for empty metadata, so a response that carries
// no trailers records none.
//
// Keys are stored as the wire sends them, which for gRPC metadata is
// lowercase. http.Header.Add would canonicalize them to Content-Type, a
// spelling no gRPC server ever sent and a grpcurl command copied from the UI
// would not reproduce, so the map is built directly. Every reader of these
// maps matches names case-insensitively.
func metadataHeader(md metadata.MD) http.Header {
	if len(md) == 0 {
		return nil
	}
	out := make(http.Header, len(md))
	for k, values := range md {
		for _, v := range values {
			out[k] = append(out[k], incomingMetadataValue(k, v))
		}
	}
	return out
}

// binarySuffix marks a metadata key whose value is bytes rather than text.
// gRPC base64-encodes such a value on the wire, and grpc-go does that
// encoding and decoding itself, so what it hands over and takes is the bytes.
const binarySuffix = "-bin"

// outgoingMetadataValue gives grpc-go the value to send. A binary key's value
// is written in a template as base64, which is the only way YAML can hold
// arbitrary bytes and is what grpcurl takes; it is decoded here so that
// grpc-go's own encoding does not encode it twice. A value that is not base64
// is sent as its bytes, as grpcurl does.
func outgoingMetadataValue(key, value string) string {
	if !strings.HasSuffix(key, binarySuffix) {
		return value
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if raw, err := enc.DecodeString(value); err == nil {
			return string(raw)
		}
	}
	return value
}

// incomingMetadataValue is the form a value is recorded in. A binary value is
// base64 again, as it was on the wire: its bytes are rarely text, and an
// archive is JSON, which would replace what it cannot encode.
func incomingMetadataValue(key, value string) string {
	if !strings.HasSuffix(key, binarySuffix) {
		return value
	}
	return base64.StdEncoding.EncodeToString([]byte(value))
}

// isTLSFailure reports whether a connection error came from the TLS handshake
// or from the peer's TLS alert. grpc-go gives these no code of their own, so
// they are told apart by Go's own error prefixes, which crypto/tls and
// crypto/x509 put on everything they return.
func isTLSFailure(message string) bool {
	return strings.Contains(message, "tls: ") || strings.Contains(message, "x509: ")
}

// tlsFailureDetail drops grpc-go's wrapping from a handshake error, leaving
// what crypto/tls said.
func tlsFailureDetail(message string) string {
	for _, marker := range []string{"x509: ", "tls: "} {
		if i := strings.Index(message, marker); i >= 0 {
			return strings.TrimRight(message[i:], `"`)
		}
	}
	return message
}
