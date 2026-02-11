package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// registerOASTools adds the OAS browsing and validation tools to the MCP server.
func (s *Server) registerOASTools() {
	s.mcp.AddTool(
		mcp.NewTool("get_oas_operation",
			mcp.WithDescription("Show OAS operation details for a graph node: HTTP method, path, parameters, request body schema, and response schemas. Use describe_node first to see if a node has an OAS reference. Use get_oas_schema to drill into schema names shown in the output. Use validate_oas_request or build_oas_example for the same node to work with payloads."),
			mcp.WithString("node",
				mcp.Description("Graph node name (must have an OAS reference — use describe_node to check)"),
				mcp.Required(),
			),
		),
		s.handleGetOASOperation,
	)

	s.mcp.AddTool(
		mcp.NewTool("get_oas_schema",
			mcp.WithDescription("Resolve and display a component schema by name, including allOf inheritance, nested types, and validation constraints. Schema names appear in get_oas_operation output. Use search_oas_schemas to find schemas by pattern."),
			mcp.WithString("schema",
				mcp.Description("Schema name (e.g. Pet, CreatePetRequest) — visible in get_oas_operation output or via search_oas_schemas"),
				mcp.Required(),
			),
			mcp.WithNumber("depth",
				mcp.Description("Resolution depth (default 2, max 5). Higher values resolve nested schemas deeper."),
			),
		),
		s.handleGetOASSchema,
	)

	s.mcp.AddTool(
		mcp.NewTool("search_oas_schemas",
			mcp.WithDescription("Search for component schema names matching a regex pattern across all loaded OAS specs. Use get_oas_schema on results to see full schema details."),
			mcp.WithString("pattern",
				mcp.Description("Regex pattern to match schema names (case-insensitive)"),
				mcp.Required(),
			),
		),
		s.handleSearchOASSchemas,
	)

	s.mcp.AddTool(
		mcp.NewTool("validate_oas_request",
			mcp.WithDescription("Validate a JSON payload against the request body schema for a graph node's OAS operation. Checks required fields, types, enums, and constraints. Use build_oas_example first to see the expected shape."),
			mcp.WithString("node",
				mcp.Description("Graph node name (must have an OAS reference with a request body — use get_oas_operation to check)"),
				mcp.Required(),
			),
			mcp.WithString("payload",
				mcp.Description("JSON string to validate"),
				mcp.Required(),
			),
		),
		s.handleValidateOASRequest,
	)

	s.mcp.AddTool(
		mcp.NewTool("build_oas_example",
			mcp.WithDescription("Generate an example JSON request payload for a graph node's OAS operation. Use validate_oas_request to check a modified payload against the schema. Use get_oas_operation to see the full operation context."),
			mcp.WithString("node",
				mcp.Description("Graph node name (must have an OAS reference with a request body — use get_oas_operation to check)"),
				mcp.Required(),
			),
			mcp.WithString("scenario",
				mcp.Description("Detail level: minimal (required fields only), typical (common fields), or full (all fields). Default: typical"),
			),
		),
		s.handleBuildOASExample,
	)
}

// handleGetOASOperation resolves a node to its OAS operation and formats the details.
func (s *Server) handleGetOASOperation(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node"), nil
	}

	op, method, path, doc, errMsg := s.resolveNodeOperation(nodeName)
	if errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}

	return mcp.NewToolResultText(formatOperationDetail(method, path, op, doc)), nil
}

// handleGetOASSchema resolves a component schema by name.
func (s *Server) handleGetOASSchema(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("schema")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: schema"), nil
	}

	if len(s.ctx.OASSpecs) == 0 {
		return mcp.NewToolResultError("No OAS specs loaded. Configure an oas field in your graph."), nil
	}

	depth := req.GetInt("depth", 2)
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}

	schema, resolvedName, _ := s.findSchemaByName(name)
	if schema == nil {
		suggestions := s.suggestSchemaNames(name)
		msg := fmt.Sprintf("Schema %q not found.", name)
		if len(suggestions) > 0 {
			msg += fmt.Sprintf(" Did you mean: %s?", strings.Join(suggestions, ", "))
		}
		return mcp.NewToolResultError(msg), nil
	}

	rs := resolveSchema(schema, resolvedName, depth, make(map[*base.Schema]bool))
	return mcp.NewToolResultText(formatResolvedSchema(rs)), nil
}

// handleSearchOASSchemas searches all specs for schema names matching a pattern.
func (s *Server) handleSearchOASSchemas(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pattern, err := req.RequireString("pattern")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: pattern"), nil
	}

	if len(s.ctx.OASSpecs) == 0 {
		return mcp.NewToolResultError("No OAS specs loaded. Configure an oas field in your graph."), nil
	}

	results, err := s.searchSchemaNames(pattern)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid pattern: %v", err)), nil
	}

	// Count total matches
	total := 0
	for _, names := range results {
		total += len(names)
	}

	if total == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No schemas matching %q.", pattern)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d schema(s) matching %q:\n\n", total, pattern)

	specPaths := sortedMapKeys(results)
	for _, specPath := range specPaths {
		names := results[specPath]
		fmt.Fprintf(&b, "**%s:**\n", specPath)
		for _, name := range names {
			fmt.Fprintf(&b, "- %s\n", name)
		}
		b.WriteString("\n")
	}
	return mcp.NewToolResultText(b.String()), nil
}

// handleValidateOASRequest validates a JSON payload against the node's request body schema.
func (s *Server) handleValidateOASRequest(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node"), nil
	}

	payloadStr, err := req.RequireString("payload")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: payload"), nil
	}

	op, _, _, _, errMsg := s.resolveNodeOperation(nodeName)
	if errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}

	schema := getRequestBodySchema(op)
	if schema == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Node %q operation has no request body schema.", nodeName)), nil
	}

	var payload any
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid JSON: %v", err)), nil
	}

	errs := validatePayload(payload, schema, "$", 5)
	if len(errs) == 0 {
		return mcp.NewToolResultText("Payload is valid."), nil
	}

	return mcp.NewToolResultText(formatValidationErrors(errs)), nil
}

// handleBuildOASExample generates an example request payload for a node's operation.
func (s *Server) handleBuildOASExample(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node"), nil
	}

	scenario := req.GetString("scenario", "typical")

	validScenarios := map[string]bool{"minimal": true, "typical": true, "full": true}
	if !validScenarios[scenario] {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid scenario %q. Use: minimal, typical, or full.", scenario)), nil
	}

	op, _, _, _, errMsg := s.resolveNodeOperation(nodeName)
	if errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}

	schema := getRequestBodySchema(op)
	if schema == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Node %q operation has no request body schema.", nodeName)), nil
	}

	example := buildExample(schema, scenario, 5, make(map[*base.Schema]bool))

	data, err := json.MarshalIndent(example, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal example: %v", err)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Example request (%s):\n\n", scenario)
	fmt.Fprintf(&b, "```json\n%s\n```\n", string(data))

	// List which fields are required
	reqFields := collectRequiredFields(schema)
	if len(reqFields) > 0 {
		sort.Strings(reqFields)
		fmt.Fprintf(&b, "\n**Required fields:** %s\n", strings.Join(reqFields, ", "))
	}

	return mcp.NewToolResultText(b.String()), nil
}

// getRequestBodySchema extracts the JSON request body schema from an operation.
func getRequestBodySchema(op *v3high.Operation) *base.Schema {
	if op.RequestBody == nil || op.RequestBody.Content == nil {
		return nil
	}
	jsonContent := op.RequestBody.Content.GetOrZero("application/json")
	if jsonContent == nil || jsonContent.Schema == nil {
		return nil
	}
	return jsonContent.Schema.Schema()
}

// collectRequiredFields gathers required field names from a schema, including allOf members.
func collectRequiredFields(schema *base.Schema) []string {
	if schema == nil {
		return nil
	}
	var required []string
	required = append(required, schema.Required...)
	for _, memberProxy := range schema.AllOf {
		if memberProxy == nil {
			continue
		}
		member := memberProxy.Schema()
		if member != nil {
			required = append(required, member.Required...)
		}
	}
	return uniqueSorted(required)
}
