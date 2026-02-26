package mcp

import (
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

	mcpServer := server.NewMCPServer(name, "0.1.0")

	s := &Server{
		mcp:     mcpServer,
		ctx:     ctx,
		persona: persona,
	}

	switch persona {
	case PersonaIntegration:
		s.registerIntegrationServer()
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

// Serve starts the MCP server on stdio and blocks until the connection closes.
func (s *Server) Serve() error {
	return server.ServeStdio(s.mcp)
}
