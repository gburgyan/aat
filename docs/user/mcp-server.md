# MCP Server

AAT exposes a Model Context Protocol (MCP) server that integrates your API project into IDE-based AI tools like Claude Code, Cursor, and VS Code Copilot. The server provides tools, resources, and prompts that let AI assistants browse your API graph, author plans, run tests, and debug failures — all through the stdio transport.

## Quick Start

```
aat mcp serve
```

The server auto-discovers your project manifest and loads the graph, templates, domain knowledge, and environment. All output goes to stderr; stdout is reserved for the MCP protocol.

```
aat mcp serve --manifest path/to/aat-project.yaml
```

Use `--manifest` to specify a project explicitly when auto-discovery doesn't apply.

## Personas

The MCP server supports **personas** that tailor the tool set for different workflows. Instead of exposing all tools to every user, personas filter to what's relevant — reducing context window usage and keeping the AI focused.

```
aat mcp serve                   # all tools (backward compatible)
aat mcp serve --persona api     # API knowledge tools (~22 tools)
aat mcp serve --persona test    # test lifecycle tools (~26 tools)
```

| Persona | Target User | Focus |
|---------|------------|-------|
| `api` | Integration developer | Understanding endpoints, data shapes, schemas, domain rules |
| `test` | Test developer | Creating plans, running tests, debugging failures |
| *(omitted)* | Both | All tools registered (backward compatible) |

### Choosing a Persona

**Use `api`** when building client code against the API that AAT describes. The API persona renames tools to match integration vocabulary (e.g., `list_api_operations` instead of `list_nodes`) and includes OAS schema tools, domain value pools, and data flow tracing.

**Use `test`** when creating, running, or debugging AAT test plans. The test persona includes plan management, execution, archive inspection, and failure analysis tools.

**Omit the flag** for backward compatibility or when you need both sets of capabilities.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |
| `--persona` | string | *(all)* | Server persona: `api`, `test`, or omit for all tools |

## IDE Configuration

### Claude Code

Add AAT as an MCP server in your project's `.mcp.json`. You can configure one or both personas:

```json
{
  "mcpServers": {
    "aat-api": {
      "command": "/path/to/aat",
      "args": ["mcp", "serve", "--persona", "api"]
    },
    "aat-test": {
      "command": "/path/to/aat",
      "args": ["mcp", "serve", "--persona", "test"]
    }
  }
}
```

Or use a single server with all tools:

```json
{
  "mcpServers": {
    "aat": {
      "command": "/path/to/aat",
      "args": ["mcp", "serve"]
    }
  }
}
```

If the manifest is discoverable from the project directory, omit the `--manifest` argument.

### Cursor / VS Code

Add an MCP server entry in your IDE settings. The configuration varies by IDE, but the pattern is the same — point to the AAT binary with `mcp serve` as the command:

```json
{
  "mcp.servers": {
    "aat-api": {
      "command": "/path/to/aat",
      "args": ["mcp", "serve", "--persona", "api"]
    }
  }
}
```

Consult your IDE's MCP documentation for the exact settings location.

## Tools — API Persona

The API persona registers 22 tools focused on understanding and integrating with the API.

### API Operations (7 tools)

| Tool | Description |
|------|-------------|
| `list_api_operations` | List all API operations with descriptions and input/output counts |
| `describe_operation` | Show full details for an API operation: inputs, outputs, dependencies, OAS reference, and Lua transform indicator |
| `trace_dependency_chain` | Trace the dependency chain for a target operation using backward chaining |
| `search_api` | Search for API operations by keyword across names, descriptions, and input/output names |
| `get_data_flow` | Show how data flows between two API operations: which outputs map to which inputs |
| `get_response_shape` | Show the output fields for an API operation with extraction paths, downstream consumers, and transform notes |
| `explain_field` | Show everything known about a specific field: type, domain concept, value pool, constraints, and which operations produce/consume it |

### Request Templates (1 tool)

| Tool | Description |
|------|-------------|
| `inspect_request_template` | Show the HTTP request template for an API operation: method, path, headers, body, response extraction rules, and Lua transforms |

### Domain Knowledge (4 tools)

| Tool | Description |
|------|-------------|
| `list_concepts` | List all domain concepts with descriptions and applicable fields |
| `list_types` | List all domain type definitions with format and field info |
| `list_value_pools` | List all value pools with type, sample values, and total count |
| `explain_concept` | Show full detail for a concept: description, constraints, examples, and related types/pools |

### OpenAPI (7 tools)

| Tool | Description |
|------|-------------|
| `list_oas_operations` | List operations from loaded OAS specs with optional filters (tag, keyword, HTTP method) |
| `list_oas_subtypes` | Show polymorphic subtypes for a schema (discriminator mappings, oneOf/anyOf, allOf) |
| `get_oas_operation` | Show OAS operation details for a graph node: HTTP method, path, parameters, request/response schemas |
| `get_oas_schema` | Resolve and display a component schema by name, including allOf inheritance and validation constraints |
| `search_oas_schemas` | Search for component schema names matching a regex pattern across all loaded OAS specs |
| `validate_oas_request` | Validate a JSON payload against the request body schema for a node's OAS operation |
| `build_oas_example` | Generate an example JSON request payload for a node's OAS operation (minimal, typical, or full) |

### Documentation (2 tools)

| Tool | Description |
|------|-------------|
| `get_node_documentation` | Show merged documentation for an API operation: graph metadata, user-written docs, and OAS summary |
| `get_workflow_documentation` | Show merged documentation for every operation in a dependency chain traced backward from goal |

### Integration Flows (2 tools)

| Tool | Description |
|------|-------------|
| `list_integration_flows` | List all integration flows with decision points (slots) and optional extensions (addons) |
| `get_integration_flow` | Show an enriched step-by-step recipe for an integration flow: HTTP methods, data flow, selections, outputs, and operation name mapping |

### Sample Responses (1 tool)

| Tool | Description |
|------|-------------|
| `get_sample_response` | Get a sample API response for an operation from run archives, showing response body, status code, and extracted outputs |

## Tools — Test Persona

The test persona registers 26 tools focused on test plan lifecycle, execution, and debugging.

### Graph Exploration (4 tools)

| Tool | Description |
|------|-------------|
| `list_nodes` | List all nodes in the API graph with descriptions and input/output counts |
| `describe_node` | Show full details for a node: inputs, outputs, edges, adapter, and OAS reference |
| `trace_workflow` | Trace the dependency chain for a goal node using backward chaining |
| `find_workflows` | Search for nodes by keyword across names, descriptions, and input/output names |

### Templates (2 tools)

| Tool | Description |
|------|-------------|
| `list_adapters` | List all registered adapter names |
| `inspect_template` | Show the HTTP template for an adapter: method, path, headers, body, and response extraction rules |

### Domain Knowledge (3 tools)

| Tool | Description |
|------|-------------|
| `list_concepts` | List all domain concepts with descriptions and applicable fields |
| `list_types` | List all domain type definitions with format and field info |
| `explain_concept` | Show full detail for a concept: description, constraints, examples, and related types/pools |

### Documentation (3 tools)

| Tool | Description |
|------|-------------|
| `get_node_documentation` | Show merged documentation for a graph node: graph metadata, user-written docs, and OAS summary |
| `generate_doc_stub` | Generate a Markdown documentation skeleton for a node with input/output tables and placeholders |
| `list_undocumented_nodes` | List graph nodes that do not have a corresponding Markdown doc file |

### Workflows (3 tools)

| Tool | Description |
|------|-------------|
| `list_workflows` | List all named workflows in the graph, including addons and composed workflows |
| `get_workflow_detail` | Show an enriched step-by-step recipe for a workflow: HTTP methods, data flow, selections, outputs |
| `instantiate_workflow` | Load and compose a workflow template with optional slot choices and addons |

### Plans (5 tools)

| Tool | Description |
|------|-------------|
| `generate_plan` | Generate an execution plan from a natural-language prompt using the LLM pipeline |
| `validate_plan` | Parse and validate a plan YAML string against the API graph |
| `list_saved_plans` | List saved test plans from the plans directory with name, goal, and step count |
| `load_plan` | Load a saved plan and return its YAML and narrative |
| `save_plan` | Validate and save a plan YAML string to the plans directory |

### Execution (1 tool)

| Tool | Description |
|------|-------------|
| `execute_plan` | Execute a saved test plan: authenticate, run engine, write archive, return summary |

### Archives (5 tools)

| Tool | Description |
|------|-------------|
| `list_archives` | List recent run archives showing run ID, timestamp, outcome, and duration |
| `inspect_archive` | Show a detailed Markdown view of a run archive including per-step request/response data |
| `analyze_failure` | Analyze a failed run archive and provide failure-focused diagnostics with suggested next steps |
| `diff_archives` | Side-by-side comparison of two run archives: outcome, status, duration, and output differences |
| `list_recent_failures` | List recent failed run archives, skipping passed runs |

## Resources

Each persona registers a compact overview resource instead of a full graph dump, reducing context window usage from 50-100KB+ to under 10KB.

### API Persona Resources

**Static:**

| URI | Name | Description |
|-----|------|-------------|
| `aat://api/overview` | API Overview | Compact one-liner-per-operation summary with HTTP method and path |
| `aat://domain` | Domain Knowledge | Domain concepts, types, and value pools |
| `aat://metadata` | Project Metadata | Project manifest and graph statistics |
| `aat://readme` | README | Project README.md from the graph directory (when present) |

**Dynamic:**

| URI Template | Name | Description |
|--------------|------|-------------|
| `aat://operation/{name}` | Operation Detail | Detailed view of a specific API operation |
| `aat://template/{adapter}` | Request Template | HTTP request template for a specific adapter |
| `aat://flow/{name}` | Integration Flow | Enriched step-by-step recipe for an integration flow |

### Test Persona Resources

**Static:**

| URI | Name | Description |
|-----|------|-------------|
| `aat://graph/overview` | Graph Overview | Compact one-liner-per-node summary with adapter and I/O counts |
| `aat://domain` | Domain Knowledge | Domain concepts, types, and value pools |
| `aat://metadata` | Project Metadata | Project manifest and graph statistics |
| `aat://readme` | README | Project README.md from the graph directory (when present) |

**Dynamic:**

| URI Template | Name | Description |
|--------------|------|-------------|
| `aat://node/{name}` | Node Detail | Detailed view of a specific graph node |
| `aat://template/{adapter}` | Template Detail | HTTP template detail for a specific adapter |
| `aat://workflow/{name}` | Workflow Detail | Enriched step-by-step recipe for a workflow |

### Legacy Resources (no persona)

When no persona is specified, the server registers the original resources including the full `aat://graph` dump:

| URI | Name | Description |
|-----|------|-------------|
| `aat://graph` | API Graph | Full graph showing all nodes, edges, and conditions |
| `aat://templates` | Templates | HTTP templates for all registered adapters |
| `aat://domain` | Domain Knowledge | Domain concepts, types, and value pools |
| `aat://metadata` | Project Metadata | Project manifest and graph statistics |
| `aat://readme` | README | Project README.md from the graph directory (when present) |

Dynamic resources: `aat://node/{name}`, `aat://template/{adapter}`, `aat://workflow/{name}`.

## Prompts

### API Persona Prompts

| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `explain_integration_flow` | `goal` (required) | Explain how an API integration flow achieves a goal, with full chain trace and domain context |
| `generate_client_code` | `node` (required), `language` (optional, default: go) | Generate client code for calling a specific API endpoint |
| `integration_guide` | `goal` (required) | Comprehensive guide for integrating with a workflow: chain trace, templates, OAS, domain |

### Test Persona Prompts

| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `test_workflow` | `description` (required) | Guide the full test lifecycle: understand goal, generate plan, validate, execute, inspect |
| `debug_failing_test` | `run_id` (required) | Load a failed run archive and diagnose root causes with comprehensive failure context |
| `enrich_documentation` | `node` (required) | Create or enrich documentation for a node using graph metadata, existing docs, OAS, and domain |

### Legacy Prompts (no persona)

All 6 prompts are registered: `explain_workflow`, `generate_client_code`, `integration_guide`, `test_workflow`, `debug_failing_test`, `enrich_documentation`.

## Per-Node Documentation

AAT supports per-node Markdown documentation files that enrich the AI's understanding of individual API operations. These files live in a `docs/` directory relative to the graph file, named `<NodeName>.md`.

```
my-ecommerce-api/
  graph.yaml
  docs/
    listProducts.md
    createOrder.md
    cancelOrder.md
```

Each doc file can contain whatever context is useful: business rules, edge cases, error codes, example payloads, or integration notes.

The `generate_doc_stub` tool (test persona) creates starter documentation files with input/output tables and placeholders pre-filled from the graph metadata.

The `list_undocumented_nodes` tool (test persona) shows which nodes don't have doc files yet — useful for tracking documentation coverage.

When the AI uses `get_node_documentation`, AAT merges three sources:

1. **Graph metadata** — inputs, outputs, types, descriptions from the graph YAML
2. **Per-node docs** — the Markdown file for that node
3. **OAS details** — operation description, parameters, and schemas from the OpenAPI spec

This merged view gives the AI rich context for authoring plans, generating client code, or explaining API behavior.

## UX Features

The MCP tools include several features designed to reduce friction when AI assistants use them.

### Step IDs vs Operation Names

Workflow templates assign step IDs that may differ from the underlying graph node (operation) name. For example, a round-trip booking workflow might use step IDs like `searchNextLeg2` while the graph node is `searchFlightsNextLeg`.

- **`get_integration_flow` / `get_workflow_detail`** shows the operation name under each step when the step ID differs: `**Operation:** searchFlightsNextLeg`. This makes the mapping visible so you know which name to use in other tools.
- **Error messages** across `describe_operation`, `get_data_flow`, `get_response_shape`, `explain_field`, `inspect_request_template`, and `get_sample_response` suggest checking the workflow detail if the name looks like a step ID.

### Lua Transform Indicators

Only some templates have Lua transforms, but they contain critical post-processing logic. Rather than requiring a separate `inspect_request_template` call to discover this:

- **`describe_operation` / `describe_node`** shows a `**Transform:** Lua (summary)` line when the node's template has a Lua transform script. The summary is extracted from the leading comment block.
- **`get_response_shape`** adds a note when outputs are post-processed by a transform, since extract paths alone don't tell the full story.
- **`inspect_request_template`** shows the full Lua script with syntax highlighting and a comment summary.

### Data Flow Guidance

When `get_data_flow` finds no direct graph connection between two operations, it suggests using `get_integration_flow` or `get_workflow_detail` to see step-level data flow within workflows — since many connections are established through workflow composition rather than direct graph edges.

## Server Context

When the MCP server starts, it loads and caches the project context:

| Artifact | Required | Source |
|----------|----------|--------|
| Graph | yes | `graph` field in manifest |
| Templates | yes | `templates` field in manifest |
| Domain knowledge | no | `domain` field in manifest |
| OAS specs | no | `oas` field in graph and per-node `oas` references |
| Environment | no | `environment` field in manifest |
| Node docs | no | `docs/` directory relative to graph file |
| Workflows | no | `workflows` field in manifest |
| Saved plans | no | `plans` field in manifest |
| Archives | no | `archives` field in manifest |
| README | no | `README.md` in the graph file's directory |

The graph and templates are required. Everything else is optional and adds capabilities — domain knowledge improves plan generation, OAS specs enable schema validation tools, node docs enrich documentation queries.

See [Project Setup](project-setup.md) for how to configure the manifest.

---

*Source: `cmd/aat/mcp_cmd.go`, `mcp/server.go`, `mcp/context.go`, `mcp/resources.go`, `mcp/prompts.go`, `mcp/prompts_workflow.go`, `mcp/tools_*.go`.*
