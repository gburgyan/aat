package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mark3labs/mcp-go/util"
)

// NewLoggingHooks creates mcp-go Hooks that log tool calls and errors using
// the provided slog.Logger. One INFO line per tool call with tool name,
// duration, and error status. MCP-level errors are logged at ERROR.
func NewLoggingHooks(logger *slog.Logger) *server.Hooks {
	var mu sync.Mutex
	starts := make(map[any]time.Time)

	hooks := &server.Hooks{}

	hooks.AddBeforeCallTool(func(_ context.Context, id any, message *mcp.CallToolRequest) {
		mu.Lock()
		starts[id] = time.Now()
		mu.Unlock()
	})

	hooks.AddAfterCallTool(func(_ context.Context, id any, message *mcp.CallToolRequest, result *mcp.CallToolResult) {
		mu.Lock()
		start, ok := starts[id]
		if ok {
			delete(starts, id)
		}
		mu.Unlock()

		attrs := []slog.Attr{
			slog.String("tool", message.Params.Name),
			slog.Bool("is_error", result.IsError),
		}
		if ok {
			attrs = append(attrs, slog.Int64("duration_ms", time.Since(start).Milliseconds()))
		}

		logger.LogAttrs(context.Background(), slog.LevelInfo, "tool_call", attrs...)
	})

	hooks.AddOnError(func(_ context.Context, _ any, method mcp.MCPMethod, _ any, err error) {
		logger.LogAttrs(context.Background(), slog.LevelError, "mcp_error",
			slog.String("method", string(method)),
			slog.String("error", err.Error()),
		)
	})

	return hooks
}

// slogMCPLogger adapts *slog.Logger to the util.Logger interface used by
// mcp-go's StreamableHTTPServer for transport-level logging.
type slogMCPLogger struct {
	logger *slog.Logger
}

var _ util.Logger = slogMCPLogger{}

func (l slogMCPLogger) Infof(format string, v ...any) {
	l.logger.Info(fmt.Sprintf(format, v...))
}

func (l slogMCPLogger) Errorf(format string, v ...any) {
	l.logger.Error(fmt.Sprintf(format, v...))
}
