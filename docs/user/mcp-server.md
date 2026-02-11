# MCP Server

AAT includes a built-in [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that exposes your API project's graph, templates, domain knowledge, OAS specs, and documentation to IDE-based AI tools like Claude Code. This gives the AI rich, structured context about your API workflows without manually copy-pasting files.

## Quick Start

1. Create an `aat-project.yaml` manifest in your project root:

```yaml
name: my-api
description: "My API test project"
graph: graph.yaml
templates: templates/
domain: domain.yaml        # optional
docs: docs/                 # optional — per-node Markdown docs
environment: env.yaml       # optional
plans: plans/               # optional
archives: runs/             # optional
```

2. Start the MCP server:

```bash
aat mcp serve
```

The server communicates over stdio using the MCP protocol. Configure your IDE to connect to it as an MCP server.

### Claude Code Configuration

Add to your `.mcp.json`:

```json
{
  "mcpServers": {
    "aat": {
      "command": "aat",
      "args": ["mcp", "serve"]
    }
  }
}
```

## Project Manifest

The `aat-project.yaml` file tells the MCP server where to find project artifacts. The server searches for it starting from the current directory and walking up to parent directories.

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Project name (shown in MCP server identity) |
| `description` | no | Project description |
| `tags` | no | Tags for categorization |
| `graph` | yes | Path to the graph YAML file |
| `templates` | yes | Path to the templates directory |
| `domain` | no | Path to domain knowledge YAML |
| `docs` | no | Directory containing per-node Markdown docs |
| `environment` | no | Path to environment config |
| `plans` | no | Directory containing plan files |
| `archives` | no | Directory containing run archives |

All paths are resolved relative to the manifest file's directory.

## Tools

The MCP server exposes tools organized by category. Each tool returns Markdown-formatted text.

### Graph Browsing

| Tool | Parameters | Description |
|------|-----------|-------------|
| `list_nodes` | — | List all graph nodes with descriptions and I/O counts |
| `describe_node` | `node` (required) | Full node detail: inputs, outputs, edges, adapter, OAS ref |
| `trace_workflow` | `goal` (required) | Backward-chain trace showing dependency order and data flow |
| `find_workflows` | `query` (required) | Case-insensitive keyword search across node names, descriptions, and I/O names |

### Templates and Domain

| Tool | Parameters | Description |
|------|-----------|-------------|
| `inspect_template` | `adapter` (required) | Show HTTP template details: method, path, headers, body, extraction |
| `list_domain_concepts` | — | List all domain concepts with descriptions |
| `get_domain_concept` | `concept` (required) | Detail for a concept: description, applies_to, relationships |
| `get_value_pool` | `pool` (required) | Show available values for a domain type |

### OAS (OpenAPI Spec)

These tools require nodes to have `oas.operationId` references and the spec to be loadable.

| Tool | Parameters | Description |
|------|-----------|-------------|
| `get_oas_operation` | `node` (required) | OAS operation detail: method, path, params, request/response schemas |
| `get_oas_schema` | `schema` (required), `depth` (optional) | Resolve a component schema with allOf inheritance |
| `search_oas_schemas` | `pattern` (required) | Regex search across component schema names |
| `validate_oas_request` | `node` (required), `payload` (required) | Validate a JSON payload against the node's request body schema |
| `build_oas_example` | `node` (required), `scenario` (optional) | Generate example request JSON (minimal/typical/full) |

### Documentation

These tools work with per-node Markdown documentation stored in the `docs` directory configured in the manifest.

| Tool | Parameters | Description |
|------|-----------|-------------|
| `get_node_documentation` | `node` (required) | Merged view: graph metadata + user-written docs + OAS summary |
| `get_workflow_documentation` | `goal` (required) | Merged docs for every node in a backward-chain trace |
| `generate_doc_stub` | `node` (required) | Generate a Markdown skeleton for a node (returns text, does not write to disk) |
| `list_undocumented_nodes` | — | List nodes without a corresponding `.md` file |

The `get_node_documentation` tool works even without a docs directory — it falls back to graph metadata only.

## Resources

Resources provide static context that IDE AI tools can load at conversation start.

| Resource | URI | Description |
|----------|-----|-------------|
| Graph overview | `aat://graph` | Full graph formatted as text (nodes, edges, conditions) |
| Templates | `aat://templates` | Summary of all registered adapter templates |
| Domain knowledge | `aat://domain` | Domain concepts, types, and value pools |
| Project metadata | `aat://metadata` | Project name, description, tags, graph stats |
| Node detail | `aat://node/{name}` | Full detail for a specific node |
| Template detail | `aat://template/{adapter}` | Full detail for a specific template |

## Prompts

Prompts are reusable templates that assemble rich context for specific tasks.

| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `explain_workflow` | `goal` (required) | Assemble context to explain how a workflow achieves a goal |
| `generate_client_code` | `node` (required), `language` (optional, default: go) | Assemble context for generating client code for an endpoint |
| `enrich_documentation` | `node` (required) | Assemble context for enriching or creating node documentation |

The `enrich_documentation` prompt is especially useful for bootstrapping docs. It includes the node's graph metadata, any existing documentation, OAS operation details, domain knowledge, and connected node context — giving the LLM everything it needs to write comprehensive docs.

## Per-Node Documentation

AAT supports per-node Markdown documentation files stored in a configured directory. Each file is named `<NodeName>.md` (case-sensitive, matching the graph node name exactly).

### Setup

1. Set the `docs` field in `aat-project.yaml`:

```yaml
docs: docs/nodes/
```

2. Create Markdown files for nodes:

```
docs/nodes/
  searchFlights.md
  createWorkbench.md
  commitBooking.md
```

### Generating Stubs

Use the `generate_doc_stub` tool to create a starting-point skeleton:

```
> generate_doc_stub node=searchFlights

# searchFlights

Search for available flights

## Inputs

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| origin | airportCode | yes | | Departure airport |
| destination | airportCode | yes | | Arrival airport |
| departureDate | date | yes | | Outbound date |

## Outputs

| Name | Type | Description |
|------|------|-------------|
| offerings | offering[] | Available flight offerings |

## Overview

_TODO: Describe what this node does and when to use it._

## Usage Notes

_TODO: Add any important usage notes, constraints, or prerequisites._

## Examples

_TODO: Add example inputs and expected outputs._
```

Save this output to `docs/nodes/searchFlights.md` and fill in the TODO sections.

### Enriching with AI

The `enrich_documentation` prompt assembles comprehensive context for the LLM:

1. Graph metadata (inputs, outputs, edges, adapter)
2. Existing documentation (if any)
3. OAS operation details (if linked)
4. Domain knowledge (concepts, types)
5. Connected nodes context

This lets the AI write detailed documentation without you having to manually gather context.

### Tracking Coverage

Use `list_undocumented_nodes` to see which nodes still need docs:

```
> list_undocumented_nodes

3 of 7 nodes are undocumented:

- **addOffer** — Add an offer to the workbench
- **addTraveler** — Add traveler details
- **commitBooking** — Finalize the booking

Use `generate_doc_stub` to create a documentation skeleton for any of these nodes.
```

### How Documentation Merges

When you request node documentation (via `get_node_documentation` or `get_workflow_documentation`), the server merges three sources:

1. **Graph metadata** — Always present. Node description, inputs, outputs, edges.
2. **User documentation** — From the `<NodeName>.md` file if it exists.
3. **OAS summary** — Brief operation summary if the node has an OAS reference.

The merged output appears as a single Markdown document with clearly labeled sections.
