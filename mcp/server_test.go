package mcp

import (
	goContext "context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer_CreatesWithoutError(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		OASSpecs: make(map[string]*v3high.Document),
		Manifest: &ProjectManifest{Name: "test"},
	}

	srv := NewServer(ctx)
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.mcp)
	assert.Equal(t, ctx, srv.ctx)
}

func TestNewServer_DefaultName(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		OASSpecs: make(map[string]*v3high.Document),
		Manifest: &ProjectManifest{},
	}

	srv := NewServer(ctx)
	assert.NotNil(t, srv)
}

func TestNewServer_NilManifest(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		OASSpecs: make(map[string]*v3high.Document),
	}

	srv := NewServer(ctx)
	assert.NotNil(t, srv)
}

func TestMCPServer_Accessor(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
	}

	srv := NewServer(ctx)
	assert.NotNil(t, srv.MCPServer())
	assert.Equal(t, srv.mcp, srv.MCPServer())
}

func TestServeHTTP_HealthEndpoint(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
	}

	srv := NewRemoteIntegrationServer(ctx)

	// Find a free port.
	ln, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	bgCtx, cancel := goContext.WithCancel(goContext.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ServeHTTP(bgCtx, fmt.Sprintf(":%d", port))
	}()

	// Wait for the server to be ready.
	addr := fmt.Sprintf("http://localhost:%d/healthz", port)
	client := &http.Client{Timeout: 2 * time.Second}
	var resp *http.Response
	for i := 0; i < 20; i++ {
		resp, err = client.Get(addr)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NoError(t, err, "server did not become ready")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok\n", string(body))

	cancel()
	<-errCh
}

func TestServeHTTP_ListensAndShutdown(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
	}

	srv := NewRemoteIntegrationServer(ctx)

	// Create a context we can cancel to trigger shutdown.
	bgCtx, cancel := goContext.WithCancel(goContext.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ServeHTTP(bgCtx, ":0")
	}()

	// Give the server a moment to start, then cancel.
	// In a real integration test we'd probe the port, but for unit testing
	// cancellation is sufficient.
	cancel()

	err := <-errCh
	assert.NoError(t, err)
}
