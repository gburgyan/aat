package mcp

import (
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
