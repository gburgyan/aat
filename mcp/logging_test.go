package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggingHooks_ToolCall(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	hooks := NewLoggingHooks(logger)

	id := "req-1"
	req := &mcp.CallToolRequest{}
	req.Params.Name = "list_nodes"

	// Simulate before → after sequence.
	hooks.OnBeforeCallTool[0](context.Background(), id, req)
	hooks.OnAfterCallTool[0](context.Background(), id, req, &mcp.CallToolResult{})

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	assert.Equal(t, "INFO", entry["level"])
	assert.Equal(t, "tool_call", entry["msg"])
	assert.Equal(t, "list_nodes", entry["tool"])
	assert.Equal(t, false, entry["is_error"])
	assert.Contains(t, entry, "duration_ms")
}

func TestLoggingHooks_ToolCallError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	hooks := NewLoggingHooks(logger)

	id := "req-2"
	req := &mcp.CallToolRequest{}
	req.Params.Name = "get_node"

	hooks.OnBeforeCallTool[0](context.Background(), id, req)
	hooks.OnAfterCallTool[0](context.Background(), id, req, &mcp.CallToolResult{IsError: true})

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	assert.Equal(t, "tool_call", entry["msg"])
	assert.Equal(t, "get_node", entry["tool"])
	assert.Equal(t, true, entry["is_error"])
}

func TestLoggingHooks_OnError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	hooks := NewLoggingHooks(logger)

	hooks.OnError[0](context.Background(), nil, mcp.MethodToolsCall, nil, assert.AnError)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	assert.Equal(t, "ERROR", entry["level"])
	assert.Equal(t, "mcp_error", entry["msg"])
	assert.Equal(t, "tools/call", entry["method"])
	assert.Equal(t, assert.AnError.Error(), entry["error"])
}

func TestSlogMCPLogger_Infof(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	adapter := slogMCPLogger{logger}

	adapter.Infof("hello %s", "world")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "INFO", entry["level"])
	assert.Equal(t, "hello world", entry["msg"])
}

func TestSlogMCPLogger_Errorf(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	adapter := slogMCPLogger{logger}

	adapter.Errorf("fail: %d", 42)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "ERROR", entry["level"])
	assert.Equal(t, "fail: 42", entry["msg"])
}

func TestNewServer_WithLogger_NoHooksWithoutOption(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
	}

	srv := NewServer(ctx)
	assert.NotNil(t, srv)
	assert.Nil(t, srv.logger)
}

func TestNewServer_WithLogger_SetsLogger(t *testing.T) {
	ctx := &ServerContext{
		Graph:    &graph.Graph{Nodes: map[string]*graph.Node{}},
		Registry: adapter.NewRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
	}

	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	srv := NewServer(ctx, WithLogger(logger))
	assert.NotNil(t, srv)
	assert.Equal(t, logger, srv.logger)
}
