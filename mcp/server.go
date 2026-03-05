package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gburgyan/aat/internal/version"
	"github.com/mark3labs/mcp-go/server"
)

// ServerPersona selects which tools, resources, and prompts to register.
type ServerPersona string

const (
	// PersonaAll registers all tools for backward compatibility.
	PersonaAll ServerPersona = ""
	// PersonaIntegration registers API knowledge tools for integration developers.
	PersonaIntegration ServerPersona = "api"
	// PersonaTest registers test lifecycle tools for test developers.
	PersonaTest ServerPersona = "test"
	// PersonaRemoteIntegration is like Integration but excludes tools that
	// leak raw response bodies (e.g. get_sample_response). Used for HTTP serving.
	PersonaRemoteIntegration ServerPersona = "remote-api"
)

// Server wraps the mcp-go MCPServer with AAT project context.
type Server struct {
	mcp     *server.MCPServer
	ctx     *ServerContext
	persona ServerPersona
}

// NewServer creates an MCP server backed by the given project context.
// Registers all tools, resources, and prompts for backward compatibility.
func NewServer(ctx *ServerContext) *Server {
	return newServer(ctx, PersonaAll)
}

// NewIntegrationServer creates an MCP server with API knowledge tools
// for integration developers.
func NewIntegrationServer(ctx *ServerContext) *Server {
	return newServer(ctx, PersonaIntegration)
}

// NewRemoteIntegrationServer creates an MCP server with API knowledge tools
// for remote HTTP serving. Same as Integration but excludes get_sample_response
// (which leaks raw archive response bodies).
func NewRemoteIntegrationServer(ctx *ServerContext) *Server {
	return newServer(ctx, PersonaRemoteIntegration)
}

// NewTestServer creates an MCP server with test lifecycle tools
// for test developers.
func NewTestServer(ctx *ServerContext) *Server {
	return newServer(ctx, PersonaTest)
}

func newServer(ctx *ServerContext, persona ServerPersona) *Server {
	name := "aat"
	if ctx.Manifest != nil && ctx.Manifest.Name != "" {
		name = "aat:" + ctx.Manifest.Name
	}
	if persona != PersonaAll {
		name += ":" + string(persona)
	}

	mcpServer := server.NewMCPServer(name, version.Version)

	s := &Server{
		mcp:     mcpServer,
		ctx:     ctx,
		persona: persona,
	}

	switch persona {
	case PersonaIntegration:
		s.registerIntegrationServer()
	case PersonaRemoteIntegration:
		s.registerRemoteIntegrationServer()
	case PersonaTest:
		s.registerTestServer()
	default:
		s.registerAllServer()
	}

	return s
}

// registerIntegrationServer registers tools/resources/prompts for the API persona.
func (s *Server) registerIntegrationServer() {
	s.registerIntegrationGraphTools()
	s.registerIntegrationTemplateTools()
	s.registerIntegrationDomainTools()
	if len(s.ctx.OASSpecs) > 0 {
		s.registerOASTools()
	}
	s.registerIntegrationDocsTools()
	s.registerIntegrationWorkflowTools()
	s.registerSampleResponseTool()
	s.registerIntegrationResources()
	s.registerIntegrationPrompts()
}

// registerRemoteIntegrationServer registers tools for the remote Integration persona.
// Same as registerIntegrationServer but excludes registerSampleResponseTool
// which leaks raw archive response bodies.
func (s *Server) registerRemoteIntegrationServer() {
	s.registerIntegrationGraphTools()
	s.registerIntegrationTemplateTools()
	s.registerIntegrationDomainTools()
	if len(s.ctx.OASSpecs) > 0 {
		s.registerOASTools()
	}
	s.registerIntegrationDocsTools()
	s.registerIntegrationWorkflowTools()
	// Note: registerSampleResponseTool deliberately excluded — leaks raw response bodies.
	s.registerIntegrationResources()
	s.registerIntegrationPrompts()
}

// registerTestServer registers tools/resources/prompts for the test persona.
func (s *Server) registerTestServer() {
	s.registerTestGraphTools()
	s.registerTestTemplateTools()
	s.registerTestDomainTools()
	s.registerTestDocsTools()
	s.registerPlanTools()
	s.registerTestWorkflowTools()
	s.registerExecTools()
	s.registerArchiveTools()
	s.registerTestResources()
	s.registerTestPrompts()
}

// registerAllServer registers everything for backward compatibility.
func (s *Server) registerAllServer() {
	s.registerGraphTools()
	s.registerTemplateTools()
	s.registerDomainTools()
	if len(s.ctx.OASSpecs) > 0 {
		s.registerOASTools()
	}
	s.registerDocsTools()
	s.registerPlanTools()
	s.registerWorkflowTools()
	s.registerExecTools()
	s.registerArchiveTools()
	s.registerSampleResponseTool()
	s.registerResources()
	s.registerPrompts()
	s.registerWorkflowPrompts()
}

// MCPServer returns the underlying mcp-go MCPServer for external option wiring.
func (s *Server) MCPServer() *server.MCPServer {
	return s.mcp
}

// Serve starts the MCP server on stdio and blocks until the connection closes.
func (s *Server) Serve() error {
	return server.ServeStdio(s.mcp)
}

// ServeHTTP starts the MCP server over Streamable HTTP and blocks until the
// context is cancelled. Uses the StreamableHTTPServer as an http.Handler on a
// managed http.Server for graceful shutdown support.
func (s *Server) ServeHTTP(ctx context.Context, addr string, opts ...server.StreamableHTTPOption) error {
	handler := server.NewStreamableHTTPServer(s.mcp, opts...)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}
