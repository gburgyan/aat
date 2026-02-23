# MCP Server

AAT exposes a Model Context Protocol (MCP) server that integrates your API project into IDE-based AI tools like Claude Code, Cursor, and VS Code Copilot. The server provides 32 tools, 8 resources, and 6 prompts that let AI assistants browse your API graph, author plans, run tests, and debug failures — all through the stdio transport.

## Quick Start

```
aat mcp serve
```

The server auto-discovers your project manifest and loads the graph, templates, domain knowledge, and environment. All output goes to stderr; stdout is reserved for the MCP protocol.

```
aat mcp serve --manifest path/to/aat-project.yaml
```

Use `--manifest` to specify a project explicitly when auto-discovery doesn't apply.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |

## IDE Configuration

### Claude Code

Add AAT as an MCP server in your project's `.mcp.json`:

```json
{
  "mcpServers": {
    "aat": {
      "command": "/path/to/aat",
      "args": ["mcp", "serve", "--manifest", "/path/to/aat-project.yaml"]
    }
  }
}
```

If the manifest is discoverable from the project directory, omit the `--manifest` argument:

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

### Cursor / VS Code

Add an MCP server entry in your IDE settings. The configuration varies by IDE, but the pattern is the same — point to the AAT binary with `mcp serve` as the command:

```json
{
  "mcp.servers": {
    "aat": {
      "command": "/path/to/aat",
      "args": ["mcp", "serve"]
    }
  }
}
```

Consult your IDE's MCP documentation for the exact settings location.

## Tools

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

### Domain Knowledge (4 tools)

| Tool | Description |
|------|-------------|
| `list_concepts` | List all domain concepts with descriptions and applicable fields |
| `list_types` | List all domain type definitions with format and field info |
| `list_value_pools` | List all value pools with type, sample values, and total count |
| `explain_concept` | Show full detail for a concept: description, constraints, examples, and related types/pools |

### OpenAPI (5 tools)

| Tool | Description |
|------|-------------|
| `get_oas_operation` | Show OAS operation details for a node: HTTP method, path, parameters, request/response schemas |
| `get_oas_schema` | Resolve and display a component schema by name, including allOf inheritance and validation constraints |
| `search_oas_schemas` | Search for component schema names matching a regex pattern across all loaded OAS specs |
| `validate_oas_request` | Validate a JSON payload against the request body schema for a node's OAS operation |
| `build_oas_example` | Generate an example JSON request payload for a node's OAS operation (minimal, typical, or full) |

### Documentation (4 tools)

| Tool | Description |
|------|-------------|
| `get_node_documentation` | Show merged documentation for a node: graph metadata, user-written docs, and OAS summary |
| `get_workflow_documentation` | Show merged documentation for every node in a workflow chain traced backward from goal |
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

### Archives (4 tools)

| Tool | Description |
|------|-------------|
| `list_archives` | List recent run archives showing run ID, timestamp, outcome, and duration |
| `inspect_archive` | Show a detailed Markdown view of a run archive including per-step request/response data |
| `analyze_failure` | Analyze a failed run archive and provide failure-focused diagnostics with suggested next steps |
| `diff_archives` | Side-by-side comparison of two run archives: outcome, status, duration, and output differences |

## Resources

### Static Resources

| URI | Name | Description |
|-----|------|-------------|
| `aat://graph` | API Graph | Full graph showing all nodes, edges, and conditions |
| `aat://templates` | Templates | HTTP templates for all registered adapters |
| `aat://domain` | Domain Knowledge | Domain concepts, types, and value pools |
| `aat://metadata` | Project Metadata | Project manifest and graph statistics |
| `aat://readme` | README | Project README.md from the graph directory (when present) |

### Dynamic Resources

| URI Template | Name | Description |
|--------------|------|-------------|
| `aat://node/{name}` | Node Detail | Detailed view of a specific node: inputs, outputs, edges |
| `aat://template/{adapter}` | Template Detail | HTTP template detail for a specific adapter |
| `aat://workflow/{name}` | Workflow Detail | Enriched step-by-step recipe for a workflow |

Dynamic resources use URI templates. Replace `{name}` or `{adapter}` with the actual identifier.

## Prompts

| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `explain_workflow` | `goal` (required) | Explain how a workflow achieves a goal, with full chain trace and domain context |
| `generate_client_code` | `node` (required), `language` (optional, default: go) | Generate client code for calling a specific API endpoint |
| `enrich_documentation` | `node` (required) | Create or enrich documentation for a node using graph metadata, existing docs, OAS, and domain |
| `integration_guide` | `goal` (required) | Comprehensive guide for integrating with a workflow: chain trace, templates, OAS, domain |
| `test_workflow` | `description` (required) | Guide the full test lifecycle: understand goal, generate plan, validate, execute, inspect |
| `debug_failing_test` | `run_id` (required) | Load a failed run archive and diagnose root causes with comprehensive failure context |

Prompts are pre-built prompt templates that IDE AI tools can invoke. They compose context from multiple sources (graph, domain, OAS, archives) into a focused prompt for the AI to work with.

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

The `generate_doc_stub` tool creates starter documentation files with input/output tables and placeholders pre-filled from the graph metadata.

The `list_undocumented_nodes` tool shows which nodes don't have doc files yet — useful for tracking documentation coverage.

When the AI uses `get_node_documentation`, AAT merges three sources:

1. **Graph metadata** — inputs, outputs, types, descriptions from the graph YAML
2. **Per-node docs** — the Markdown file for that node
3. **OAS details** — operation description, parameters, and schemas from the OpenAPI spec

This merged view gives the AI rich context for authoring plans, generating client code, or explaining API behavior.

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
