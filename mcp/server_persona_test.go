package mcp

import (
	"context"
	"sort"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildPersonaTestGraph builds a small graph suitable for persona/tool tests.
func buildPersonaTestGraph() *graph.Graph {
	g := &graph.Graph{
		Version: "1.0",
		Title:   "Test API",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:      "search",
				Adapter:   "searchTmpl",
				Satisfies: []string{"results"},
				Inputs: []graph.Input{
					{Name: "origin", Type: "string"},
					{Name: "destination", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "results", Type: "flight[]", ElementFields: []graph.Field{
						{Name: "id", Type: "string"},
						{Name: "carrier", Type: "string"},
					}},
				},
			},
			"book": {
				Name:      "book",
				Adapter:   "bookTmpl",
				Requires:  []string{"results"},
				Satisfies: []string{"workbenchId"},
				Inputs: []graph.Input{
					{Name: "results", Type: "flight[]"},
					{Name: "flightId", Type: "string"},
				},
				Outputs: []graph.Output{
					{Name: "workbenchId", Type: "string"},
					{Name: "confirmationCode", Type: "string"},
				},
			},
		},
		Workflows: []graph.Workflow{
			{Name: "basic-booking", Description: "Basic booking flow"},
		},
	}
	g.BuildSatisfierIndex()
	return g
}

func buildPersonaTestRegistry() *adapter.Registry {
	reg := adapter.NewRegistry()
	_ = reg.Register("searchTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "searchTmpl", Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/search"},
		Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{"results": {Path: "$.data"}}},
	}))
	_ = reg.Register("bookTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "bookTmpl", Protocol: "http",
		Request:  adapter.TemplateRequest{Method: "POST", Path: "/api/book"},
		Response: adapter.TemplateResponse{Extract: map[string]adapter.ExtractRule{"workbenchId": {Path: "$.workbench_id"}}},
	}))
	return reg
}

func buildPersonaTestContext() *ServerContext {
	return &ServerContext{
		Graph:    buildPersonaTestGraph(),
		Registry: buildPersonaTestRegistry(),
		Manifest: &ProjectManifest{Name: "test"},
	}
}

// --- Persona tool registration tests ---

func TestNewServer_AllPersona_RegistersAllTools(t *testing.T) {
	srv := NewServer(buildPersonaTestContext())
	tools := srv.mcp.ListTools()
	// Without OAS specs: all original tools minus 7 OAS tools (28+)
	assert.GreaterOrEqual(t, len(tools), 28, "all-persona (no OAS) should have at least 28 tools, got %d", len(tools))
}

func TestNewServer_AllPersona_WithOAS_RegistersOASTools(t *testing.T) {
	ctx := buildPersonaTestContext()
	ctx.OASSpecs = map[string]*v3high.Document{"test.yaml": {}}
	srv := NewServer(ctx)
	tools := srv.mcp.ListTools()
	// With OAS specs: all tools including 7 OAS tools (35+)
	assert.GreaterOrEqual(t, len(tools), 35, "all-persona (with OAS) should have at least 35 tools, got %d", len(tools))
	toolNames := collectToolNames(tools)
	assert.Contains(t, toolNames, "list_oas_operations")
	assert.Contains(t, toolNames, "get_oas_operation")
	assert.Contains(t, toolNames, "get_oas_schema")
	assert.Contains(t, toolNames, "search_oas_schemas")
	assert.Contains(t, toolNames, "validate_oas_request")
	assert.Contains(t, toolNames, "build_oas_example")
	assert.Contains(t, toolNames, "list_oas_subtypes")
}

func TestNewIntegrationServer_ToolNames(t *testing.T) {
	srv := NewIntegrationServer(buildPersonaTestContext())
	tools := srv.mcp.ListTools()
	toolNames := collectToolNames(tools)

	// API-vocabulary tools should be present.
	assert.Contains(t, toolNames, "list_api_operations")
	assert.Contains(t, toolNames, "describe_operation")
	assert.Contains(t, toolNames, "trace_dependency_chain")
	assert.Contains(t, toolNames, "search_api")
	assert.Contains(t, toolNames, "inspect_request_template")
	assert.Contains(t, toolNames, "get_data_flow")
	assert.Contains(t, toolNames, "get_response_shape")
	assert.Contains(t, toolNames, "explain_field")
	assert.Contains(t, toolNames, "list_integration_flows")
	assert.Contains(t, toolNames, "get_integration_flow")
	assert.Contains(t, toolNames, "get_sample_response")

	// OAS tools should NOT be present (no specs loaded).
	assert.NotContains(t, toolNames, "list_oas_operations")
	assert.NotContains(t, toolNames, "list_oas_subtypes")
	assert.NotContains(t, toolNames, "get_oas_operation")
	assert.NotContains(t, toolNames, "get_oas_schema")

	// Domain tools present.
	assert.Contains(t, toolNames, "list_concepts")
	assert.Contains(t, toolNames, "list_value_pools")

	// Test-only tools should NOT be present.
	assert.NotContains(t, toolNames, "list_nodes")
	assert.NotContains(t, toolNames, "describe_node")
	assert.NotContains(t, toolNames, "list_adapters")
	assert.NotContains(t, toolNames, "generate_plan")
	assert.NotContains(t, toolNames, "execute_plan")
	assert.NotContains(t, toolNames, "list_archives")
	assert.NotContains(t, toolNames, "instantiate_workflow")
}

func TestNewIntegrationServer_WithOAS_ToolNames(t *testing.T) {
	ctx := buildPersonaTestContext()
	ctx.OASSpecs = map[string]*v3high.Document{"test.yaml": {}}
	srv := NewIntegrationServer(ctx)
	tools := srv.mcp.ListTools()
	toolNames := collectToolNames(tools)

	// OAS tools should be present when specs are loaded.
	assert.Contains(t, toolNames, "list_oas_operations")
	assert.Contains(t, toolNames, "list_oas_subtypes")
	assert.Contains(t, toolNames, "get_oas_operation")
	assert.Contains(t, toolNames, "get_oas_schema")
	assert.Contains(t, toolNames, "search_oas_schemas")
	assert.Contains(t, toolNames, "validate_oas_request")
	assert.Contains(t, toolNames, "build_oas_example")
}

func TestNewTestServer_ToolNames(t *testing.T) {
	srv := NewTestServer(buildPersonaTestContext())
	tools := srv.mcp.ListTools()
	toolNames := collectToolNames(tools)

	// Test-vocabulary tools should be present.
	assert.Contains(t, toolNames, "list_nodes")
	assert.Contains(t, toolNames, "describe_node")
	assert.Contains(t, toolNames, "trace_workflow")
	assert.Contains(t, toolNames, "find_workflows")
	assert.Contains(t, toolNames, "list_adapters")
	assert.Contains(t, toolNames, "inspect_template")
	assert.Contains(t, toolNames, "generate_plan")
	assert.Contains(t, toolNames, "validate_plan")
	assert.Contains(t, toolNames, "execute_plan")
	assert.Contains(t, toolNames, "list_archives")
	assert.Contains(t, toolNames, "list_recent_failures")
	assert.Contains(t, toolNames, "list_workflows")
	assert.Contains(t, toolNames, "get_workflow_detail")
	assert.Contains(t, toolNames, "instantiate_workflow")

	// API-only tools should NOT be present.
	assert.NotContains(t, toolNames, "list_api_operations")
	assert.NotContains(t, toolNames, "describe_operation")
	assert.NotContains(t, toolNames, "list_oas_operations")
	assert.NotContains(t, toolNames, "list_oas_subtypes")
	assert.NotContains(t, toolNames, "get_oas_operation")
	assert.NotContains(t, toolNames, "get_data_flow")
	assert.NotContains(t, toolNames, "get_response_shape")
	assert.NotContains(t, toolNames, "list_value_pools")
}

func TestPersona_ServerName(t *testing.T) {
	ctx := buildPersonaTestContext()

	all := NewServer(ctx)
	assert.Equal(t, PersonaAll, all.persona)

	api := NewIntegrationServer(ctx)
	assert.Equal(t, PersonaIntegration, api.persona)

	test := NewTestServer(ctx)
	assert.Equal(t, PersonaTest, test.persona)
}

func TestNewIntegrationServer_ToolCount(t *testing.T) {
	srv := NewIntegrationServer(buildPersonaTestContext())
	tools := srv.mcp.ListTools()
	// 24 base - 7 OAS (no specs) = 17 tools
	assert.Equal(t, 17, len(tools), "integration server (no OAS) should have 17 tools, got %d: %v",
		len(tools), collectToolNames(tools))
}

func TestNewIntegrationServer_WithOAS_ToolCount(t *testing.T) {
	ctx := buildPersonaTestContext()
	ctx.OASSpecs = map[string]*v3high.Document{"test.yaml": {}}
	srv := NewIntegrationServer(ctx)
	tools := srv.mcp.ListTools()
	// 17 base + 7 OAS = 24 tools
	assert.Equal(t, 24, len(tools), "integration server (with OAS) should have 24 tools, got %d: %v",
		len(tools), collectToolNames(tools))
}

func TestNewTestServer_ToolCount(t *testing.T) {
	srv := NewTestServer(buildPersonaTestContext())
	tools := srv.mcp.ListTools()
	// 25 base + 1 new = 26 tools
	assert.Equal(t, 26, len(tools), "test server should have 26 tools, got %d: %v",
		len(tools), collectToolNames(tools))
}

// --- Compact overview resource tests ---

func TestHandleAPIOverviewResource(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewIntegrationServer(ctx)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "aat://api/overview"
	contents, err := srv.handleAPIOverviewResource(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, contents, 1)

	text := contents[0].(mcp.TextResourceContents).Text
	assert.Contains(t, text, "API Operations (2)")
	assert.Contains(t, text, "**search** POST `/api/search`")
	assert.Contains(t, text, "**book** POST `/api/book`")
}

func TestHandleGraphOverviewResource(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewTestServer(ctx)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "aat://graph/overview"
	contents, err := srv.handleGraphOverviewResource(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, contents, 1)

	text := contents[0].(mcp.TextResourceContents).Text
	assert.Contains(t, text, "Graph Nodes (2)")
	assert.Contains(t, text, "**search** [searchTmpl]")
	assert.Contains(t, text, "**book** [bookTmpl]")
	assert.Contains(t, text, "2 in")
}

// --- New tool handler tests ---

func TestHandleGetDataFlow_Valid(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetDataFlow, map[string]any{
		"node_from": "search",
		"node_to":   "book",
	})
	require.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "Data Flow: search → book")
	assert.Contains(t, text, "results")
}

func TestHandleGetDataFlow_NoConnection(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetDataFlow, map[string]any{
		"node_from": "book",
		"node_to":   "search",
	})
	require.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "No direct data flow")
}

func TestHandleGetDataFlow_UnknownNode(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetDataFlow, map[string]any{
		"node_from": "nonexistent",
		"node_to":   "book",
	})
	assert.True(t, result.IsError)
}

func TestHandleGetDataFlow_ElementFields(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetDataFlow, map[string]any{
		"node_from": "search",
		"node_to":   "book",
	})
	text := resultText(t, result)
	// Should show element fields for array output
	assert.Contains(t, text, "element fields")
	assert.Contains(t, text, "id")
	assert.Contains(t, text, "carrier")
}

func TestHandleGetResponseShape_Valid(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetResponseShape, map[string]any{"node": "search"})
	require.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "Response Shape: search")
	assert.Contains(t, text, "results")
	assert.Contains(t, text, "flight[]")
	assert.Contains(t, text, "`$.data`")
	assert.Contains(t, text, "id")
	assert.Contains(t, text, "carrier")
	assert.Contains(t, text, "book")
}

func TestHandleGetResponseShape_NoOutputs(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"noout": {Name: "noout"},
		},
	}
	g.BuildSatisfierIndex()
	srv := newTestServer(g)

	result := callTool(t, srv.handleGetResponseShape, map[string]any{"node": "noout"})
	assert.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "no outputs")
}

func TestHandleGetResponseShape_UnknownNode(t *testing.T) {
	srv := NewServer(buildPersonaTestContext())
	result := callTool(t, srv.handleGetResponseShape, map[string]any{"node": "nonexistent"})
	assert.True(t, result.IsError)
}

func TestHandleGetResponseShape_WithTransform(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "searchTmpl",
				Outputs: []graph.Output{{Name: "results", Type: "result[]"}},
			},
		},
	}
	g.BuildSatisfierIndex()
	reg := adapter.NewRegistry()
	_ = reg.Register("searchTmpl", adapter.NewTemplateAdapter(adapter.Template{
		Adapter: "searchTmpl", Protocol: "http",
		Request: adapter.TemplateRequest{Method: "POST", Path: "/search"},
		Response: adapter.TemplateResponse{
			Extract:   map[string]adapter.ExtractRule{"results": {Path: "$.data"}},
			Transform: "-- Flatten nested arrays\nlocal r = {}\nreturn r\n",
		},
	}))
	ctx := &ServerContext{
		Graph:    g,
		Registry: reg,
		Manifest: &ProjectManifest{Name: "test"},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetResponseShape, map[string]any{"node": "search"})
	require.False(t, result.IsError)
	text := resultText(t, result)
	assert.Contains(t, text, "Lua transform")
	assert.Contains(t, text, "Flatten nested arrays")
}

func TestHandleGetResponseShape_WithoutTransform(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleGetResponseShape, map[string]any{"node": "search"})
	require.False(t, result.IsError)
	text := resultText(t, result)
	assert.NotContains(t, text, "transform")
	assert.NotContains(t, text, "Transform")
}

func TestHandleExplainField_Input(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExplainField, map[string]any{
		"node":  "search",
		"field": "origin",
	})
	require.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "Field: search.origin")
	assert.Contains(t, text, "**Kind:** input")
	assert.Contains(t, text, "**Type:** string")
}

func TestHandleExplainField_Output(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExplainField, map[string]any{
		"node":  "search",
		"field": "results",
	})
	require.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "Field: search.results")
	assert.Contains(t, text, "**Kind:** output")
	assert.Contains(t, text, "**Type:** flight[]")
	assert.Contains(t, text, "Element Fields")
	assert.Contains(t, text, "id")
	assert.Contains(t, text, "Consumed By")
	assert.Contains(t, text, "book")
}

func TestHandleExplainField_OutputWithExtractPath(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExplainField, map[string]any{
		"node":  "book",
		"field": "workbenchId",
	})
	require.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "**Kind:** output")
	assert.Contains(t, text, "**Extract Path:** `$.workbench_id`")
}

func TestHandleExplainField_ElementField(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExplainField, map[string]any{
		"node":  "search",
		"field": "id",
	})
	require.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "**Kind:** elementField")
	assert.Contains(t, text, "**Parent Output:** results")
}

func TestHandleExplainField_WithDomainKnowledge(t *testing.T) {
	ctx := buildPersonaTestContext()
	ctx.KB = &domain.KnowledgeBase{
		Types: map[string]*domain.TypeDef{
			"origin": {Description: "Airport code", Format: "string", Pool: "airports"},
		},
		ValuePools: map[string]*domain.ValuePool{
			"airports": {Description: "Airport codes", Type: "string", Values: []string{"JFK", "LAX", "ORD"}},
		},
	}
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExplainField, map[string]any{
		"node":  "search",
		"field": "origin",
	})
	require.False(t, result.IsError)
	text := resultText(t, result)

	assert.Contains(t, text, "Domain Type: origin")
	assert.Contains(t, text, "Airport code")
	assert.Contains(t, text, "JFK")
}

func TestHandleExplainField_NotFound(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExplainField, map[string]any{
		"node":  "search",
		"field": "nonexistent",
	})
	assert.True(t, result.IsError)
}

func TestHandleExplainField_UnknownNode(t *testing.T) {
	ctx := buildPersonaTestContext()
	srv := NewServer(ctx)

	result := callTool(t, srv.handleExplainField, map[string]any{
		"node":  "nonexistent",
		"field": "origin",
	})
	assert.True(t, result.IsError)
}

// --- Workflow detail summary mode test ---

func TestFormatWorkflowDetail_SummaryMode(t *testing.T) {
	g := buildPersonaTestGraph()
	reg := buildPersonaTestRegistry()
	wf := graph.Workflow{Name: "test-wf", Description: "Test workflow"}

	p := &plan.Plan{
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{
					"origin": {Default: "DEN"},
				}},
			},
		},
	}

	// Summary=true should NOT have the values table.
	summaryOutput := formatWorkflowDetail(wf, p, g, reg, true)
	assert.NotContains(t, summaryOutput, "| origin | literal | DEN |")
	assert.Contains(t, summaryOutput, "### 1. search")

	// Summary=false should have the values table.
	detailOutput := formatWorkflowDetail(wf, p, g, reg, false)
	assert.Contains(t, detailOutput, "| origin | literal | DEN |")
}

// --- Helper ---

func collectToolNames(tools map[string]*server.ServerTool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
