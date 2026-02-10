package mcp

import (
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go MCPServer with AAT project context.
type Server struct {
	mcp *server.MCPServer
	ctx *ServerContext
}

// NewServer creates an MCP server backed by the given project context.
// Tools, resources, and prompts will be registered in later work items.
func NewServer(ctx *ServerContext) *Server {
	name := "aat"
	if ctx.Manifest != nil && ctx.Manifest.Name != "" {
		name = "aat:" + ctx.Manifest.Name
	}

	mcpServer := server.NewMCPServer(name, "0.1.0",
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(false, false),
		server.WithPromptCapabilities(false),
	)

	s := &Server{
		mcp: mcpServer,
		ctx: ctx,
	}
	s.registerGraphTools()
	s.registerTemplateTools()
	s.registerDomainTools()
	return s
}

// Serve starts the MCP server on stdio and blocks until the connection closes.
func (s *Server) Serve() error {
	return server.ServeStdio(s.mcp)
}
