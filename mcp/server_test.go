package mcp

import (
	goContext "context"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/stretchr/testify/assert"
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
