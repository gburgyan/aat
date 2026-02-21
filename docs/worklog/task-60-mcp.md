# Task 60: MCP Server — Implementation Plan

## 2026-02-10 — Plan Created

**What:** Detailed implementation plan for Task 60 (`aat mcp serve` — API lifecycle platform).
**Scope:** 60a (core + API knowledge + OAS tools), 60b (documentation), 60c (testing lifecycle + developer workflow). 60d (CI/CD + monitoring) deferred to Stage 3a+ with high-level outline.
**Reference:** Travelport MCP server (`tpmcp`) informed OAS tool design and architecture patterns.

---

## Context

AAT's graph encodes rich API knowledge: nodes, edges, inputs, outputs, constraints, adapters (with request/response shapes), domain knowledge, and OAS references. Task 60 exposes all of this through an MCP server so that IDE-based AI tools (primarily Claude Code) can assist with API integration development, testing, documentation, and monitoring.

**Key design decisions:**
- OAS tools included (schema browsing, validation, examples — similar to tpmcp reference)
- 60c targets the broader developer workflow (code → test → run → debug), not just plan CRUD
- 60d (CI/CD + monitoring) is high-level outline only
- gRPC extensibility: avoid one-way doors that make future gRPC support harder

---

## Architecture

### Package Structure

Flat `mcp/` package with logical file grouping (same pattern as `engine/`). No subpackages for 60a-60c — the tool count (~25) is manageable.

```
mcp/
  doc.go                -- package doc
  manifest.go           -- ProjectManifest type + LoadManifest()
  manifest_test.go
  context.go            -- ServerContext: shared state for all handlers
  context_test.go
  server.go             -- Server struct, NewServer(), Serve(), registration
  server_test.go
  format.go             -- shared Markdown formatting helpers
  format_test.go

  # 60a: Graph + template + domain tools
  tools_graph.go        -- list_nodes, describe_node, trace_workflow, find_workflows
  tools_graph_test.go
  tools_template.go     -- inspect_template, list_adapters
  tools_template_test.go
  tools_domain.go       -- list_concepts, list_types, list_value_pools, explain_concept
  tools_domain_test.go

  # 60a: OAS tools
  tools_oas.go          -- get_oas_operation, get_oas_schema, search_oas_schemas, validate_oas_request, build_oas_example
  tools_oas_test.go
  oas_resolve.go        -- schema resolution + example generation logic
  oas_resolve_test.go

  # 60a: Resources + prompts
  resources.go          -- aat:// resources (graph, templates, domain, metadata, node, template)
  resources_test.go
  prompts.go            -- explain_workflow, generate_client_code
  prompts_test.go

  # 60b: Documentation tools
  docs.go               -- doc file loading, graph-doc merging
  docs_test.go
  tools_docs.go         -- get_node_documentation, get_workflow_documentation, generate_doc_stub, list_undocumented_nodes
  tools_docs_test.go

  # 60c: Testing lifecycle tools
  tools_plan.go         -- generate_plan, validate_plan, list_saved_plans, load_plan, save_plan
  tools_plan_test.go
  tools_exec.go         -- execute_plan, get_execution_status
  tools_exec_test.go
  tools_archive.go      -- list_archives, inspect_archive, analyze_failure, diff_archives
  tools_archive_test.go

  # 60c: Developer workflow prompts
  prompts_workflow.go   -- test_workflow_from_description, debug_failing_test, integration_guide
  prompts_workflow_test.go
```

### Key Types

```go
// ProjectManifest describes where project artifacts live.
type ProjectManifest struct {
    Name          string   `yaml:"name"`
    Description   string   `yaml:"description,omitempty"`
    Tags          []string `yaml:"tags,omitempty"`
    GraphPath     string   `yaml:"graph"`
    TemplatesPath string   `yaml:"templates"`
    DomainPath    string   `yaml:"domain,omitempty"`
    DocsDir       string   `yaml:"docs,omitempty"`
    WorkflowsDir  string   `yaml:"workflows,omitempty"`
    ArchiveDir    string   `yaml:"archives,omitempty"`
    EnvPath       string   `yaml:"environment,omitempty"`
}

// ServerContext holds pre-loaded project state shared by all tool handlers.
type ServerContext struct {
    // Core (always available)
    Graph       *graph.Graph
    Registry    *adapter.Registry
    KB          *domain.KnowledgeBase        // may be nil
    OASSpecs    map[string]*v3high.Document  // spec path -> loaded doc

    // Docs (60b, may be nil)
    DocsDir     string
    NodeDocs    map[string]string  // node name -> Markdown content

    // Testing lifecycle (60c, may be nil)
    WorkflowsDir string
    ArchiveDir  string
    Environment *config.Environment

    // Metadata
    Manifest    *ProjectManifest
    GraphDir    string  // base dir for resolving relative paths
}

// Server wraps the mcp-go MCPServer with AAT context.
type Server struct {
    mcp  *server.MCPServer
    ctx  *ServerContext
}
```

### Dependency Flow

```
cmd/aat/mcp_cmd.go
  -> mcp.LoadManifest(path)
  -> mcp.BuildServerContext(manifest)
       -> graph.ParseFile()
       -> adapter.LoadTemplates()
       -> domain.ParseFile()          (if configured)
       -> graph.LoadOASSpec()         (for each spec in graph)
       -> loadNodeDocs()              (if docs dir configured)
       -> config.LoadEnvironment()    (if configured)
  -> mcp.NewServer(ctx)
       -> register all tools/resources/prompts based on what's available in ctx
  -> srv.Serve()                      (stdio transport, blocks)
```

### SDK: mark3labs/mcp-go

Registration pattern:
```go
s.mcp.AddTool(
    mcp.NewTool("describe_node",
        mcp.WithDescription("..."),
        mcp.WithString("node", mcp.Required(), mcp.Description("Node name")),
    ),
    s.handleDescribeNode,
)
```

Handler pattern:
```go
func (s *Server) handleDescribeNode(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    name := req.GetString("node", "")
    // ... lookup, format ...
    return mcp.NewToolResultText(markdown), nil
}
```

Error convention: `mcp.NewToolResultError("message")` for user-facing errors, `(nil, err)` only for transport issues.

### Prerequisites (Changes to Existing Packages)

**adapter/template.go** — Add template inspection:
```go
// InspectTemplate returns the underlying Template for inspection.
func (a *TemplateAdapter) InspectTemplate() *Template { return &a.tmpl }
```

**adapter/adapter.go** — Add Registry method:
```go
// GetTemplate returns the Template for a named adapter if it is template-based.
func (r *Registry) GetTemplate(name string) *Template { ... }
```

No other existing package changes required.

### Protocol Extensibility (gRPC-readiness)

The current implementation is HTTP/REST-centric (templates, OAS). To avoid one-way doors for future gRPC support:

1. **Tool naming**: OAS-specific tools use `oas_` prefix (`get_oas_operation`, `get_oas_schema`, `validate_oas_request`, `build_oas_example`). Future gRPC tools would use `proto_` prefix and coexist. Graph-level tools (`list_nodes`, `describe_node`, etc.) are protocol-agnostic.

2. **`describe_node` output**: Shows the adapter name and protocol (from `Template.Protocol`). Does not hardcode HTTP assumptions — if an adapter isn't template-based, shows "adapter: <name> (non-template)". Future gRPC adapters get appropriate display.

3. **`inspect_template` stays template-specific**: It inspects HTTP template adapters. A future `inspect_grpc_adapter` or similar would inspect gRPC adapters. The tool name is honest about what it does.

4. **Format helpers are protocol-aware**: `formatRequestShape(tmpl)` checks `tmpl.Protocol` and formats accordingly. For HTTP templates this shows method/path/headers/body. The same function can be extended for other protocols.

5. **`ServerContext` is extensible**: `OASSpecs` field holds REST specs. Future: add `ProtoFiles map[string]*ProtoDescriptor` or similar for gRPC service definitions. Tools check what's available.

6. **Graph model is already protocol-agnostic**: Nodes reference adapters by name. Inputs/outputs are logical, not HTTP-specific. Edges are about data flow, not protocol. This is the right foundation.

7. **Resources are protocol-agnostic**: `aat://node/{name}` works regardless of the node's protocol. The content adapts based on what's available (OAS, proto, or just graph metadata).

**No changes needed to the graph or adapter interfaces.** The extensibility comes from: (a) clear naming boundaries between protocol-specific and protocol-agnostic tools, and (b) format helpers that dispatch on protocol rather than assuming HTTP.

---

## Work Items

### WI-1: Core Skeleton + CLI (60a foundation)

**Goal:** Empty MCP server starts on stdio with `aat mcp serve`.

**Files:** `mcp/doc.go`, `mcp/manifest.go`, `mcp/manifest_test.go`, `mcp/context.go`, `mcp/context_test.go`, `mcp/server.go`, `mcp/server_test.go`, `cmd/aat/main.go` (add mcp subcommand)

**Scope:**
1. Add `github.com/mark3labs/mcp-go` dependency
2. `ProjectManifest` type + `LoadManifest(path)` with YAML parsing, path resolution relative to manifest dir
3. `ServerContext` type + `BuildServerContext(manifest)` — loads graph, templates, domain, OAS specs
4. `Server` struct + `NewServer(ctx)` (zero tools) + `Serve()` (stdio transport)
5. Wire `aat mcp serve [--manifest <path>]` CLI command
6. Auto-detection: search for `aat-project.yaml` in cwd, then parent dirs

**Tests:**
- Manifest parsing: valid, missing fields, relative paths, defaults
- Context building: loads graph and templates, handles missing optional paths
- Server creation: starts without error

**Acceptance:** `aat mcp serve --manifest aat-project.yaml` starts and responds to MCP `initialize`.

---

### WI-2: Graph Browsing Tools (60a)

**Goal:** Claude Code can explore the API graph structure.

**Files:** `mcp/tools_graph.go`, `mcp/tools_graph_test.go`, `mcp/format.go`, `mcp/format_test.go`

**Tools:**
| Tool | Params | Description |
|------|--------|-------------|
| `list_nodes` | — | All nodes with descriptions, input/output counts |
| `describe_node` | `node` (required) | Full node detail: inputs (name, type, optional), outputs (name, type, path), edges (from, conditions), adapter, cleanup, OAS ref |
| `trace_workflow` | `goal` (required) | Backward chain from goal — ordered nodes, data flow, entry points |
| `find_workflows` | `query` (required) | Keyword search across node names, descriptions, input/output names |

**Reuse:**
- `graph.BackwardChain()` for `trace_workflow`
- Pattern from `intent.FormatGraph()` for Markdown formatting (but richer, per-tool)

**Format helpers** (`format.go`):
- `formatNodeSummary(node)` — one-line summary for lists
- `formatNodeDetail(node, graph, kb)` — full Markdown with inputs table, outputs table, edges, adapter
- `formatChainTrace(chainResult, graph)` — ordered workflow trace
- `formatInputTable(inputs)`, `formatOutputTable(outputs)` — reusable table formatters

**Tests:** Table-driven per tool. Test graphs: empty, single node, multi-node with edges, cycles with cycle-breaker, keyword matches.

---

### WI-3: Template + Domain Tools (60a)

**Goal:** Claude Code can inspect request/response shapes and domain knowledge.

**Files:** `mcp/tools_template.go`, `mcp/tools_template_test.go`, `mcp/tools_domain.go`, `mcp/tools_domain_test.go`, `adapter/template.go` (add InspectTemplate), `adapter/adapter.go` (add GetTemplate)

**Prerequisite:** Add `TemplateAdapter.InspectTemplate()` and `Registry.GetTemplate(name)`.

**Tools:**
| Tool | Params | Description |
|------|--------|-------------|
| `list_adapters` | — | All adapter names |
| `inspect_template` | `adapter` (required) | Method, path, headers, body template with placeholders, extraction rules, validation schema |
| `list_concepts` | — | All domain concepts with descriptions |
| `list_types` | — | All domain type definitions with fields |
| `list_value_pools` | — | All value pools with sample values (first 5 + count) |
| `explain_concept` | `name` (required) | Full concept: description, appliesTo, constraint, examples, related types/pools |

**Graceful degradation:** Domain tools return "domain knowledge not configured" if KB is nil.

**Tests:** With and without KB. Template inspection with real template fixtures.

---

### WI-4: OAS Tools (60a)

**Goal:** Claude Code can browse OAS schemas, validate payloads, and generate request examples — similar to tpmcp but wired through AAT's graph nodes.

**Files:** `mcp/tools_oas.go`, `mcp/tools_oas_test.go`, `mcp/oas_resolve.go`, `mcp/oas_resolve_test.go`

**Tools:**
| Tool | Params | Description |
|------|--------|-------------|
| `get_oas_operation` | `node` (required) | Resolve node's OAS operationId to full operation detail: method, path, parameters, request body schema, response schemas |
| `get_oas_schema` | `schema` (required), `depth` (optional, default 2) | Resolve a schema name from any loaded OAS spec. Show properties, types, required fields, nested schemas up to depth |
| `search_oas_schemas` | `pattern` (required) | Regex search across all schema names in loaded specs |
| `validate_oas_request` | `node` (required), `payload` (required, JSON string) | Validate a JSON payload against the node's OAS request body schema |
| `build_oas_example` | `node` (required), `scenario` (optional: minimal/typical/full) | Generate example request JSON for a node based on its OAS schema |

**New capability** (`oas_resolve.go`):
- `resolveSchema(doc, schemaName, depth)` — resolve `$ref` and `allOf`, merge properties, track required fields. LRU cache for resolved schemas.
- `validatePayload(doc, operationId, payload)` — check required fields, types, enum values
- `generateExample(doc, operationId, scenario)` — build example JSON with realistic placeholder values
- These build on AAT's existing `graph.LoadOASSpec()` and `graph.FindOperation()` but add schema-level capabilities

**Graceful degradation:** If a node has no OAS reference, OAS tools return "node has no OAS reference; use inspect_template for request shape or describe_node for graph-level detail". This leaves room for future protocol-specific tools (e.g., proto inspection) to handle non-OAS nodes.

**Tests:** Use a small OAS spec fixture (not the full 1.2MB Travelport spec). Test schema resolution, validation errors, example generation with nested objects.

---

### WI-5: Resources + Prompts (60a complete)

**Goal:** MCP resources and prompts provide static context and reusable workflows.

**Files:** `mcp/resources.go`, `mcp/resources_test.go`, `mcp/prompts.go`, `mcp/prompts_test.go`

**Resources:**
| URI | Description |
|-----|-------------|
| `aat://graph` | Full graph as Markdown (reuses pattern from `intent.FormatGraph`) |
| `aat://templates` | Summary of all adapters with method/path |
| `aat://domain` | Full domain KB (reuses `kb.FormatForPrompt()`) |
| `aat://metadata` | Project name, description, tags, graph version, node/edge counts |
| `aat://node/{name}` | Dynamic: detailed node doc (same as describe_node output) |
| `aat://template/{adapter}` | Dynamic: full template detail |

**Prompts:**
| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `explain_workflow` | `goal` (required) | System + user prompt to explain how a workflow achieves a goal. Includes backward chain context + node descriptions. |
| `generate_client_code` | `node` (required), `language` (optional, default "go") | Prompt to generate API client code for calling a node. Includes template, OAS schema, domain types. |

**Tests:** Resource content contains expected sections. Prompt messages have correct roles and include relevant context.

---

### WI-6: Documentation Integration (60b)

**Goal:** Per-node Markdown docs enriched and accessible via MCP.

**Files:** `mcp/docs.go`, `mcp/docs_test.go`, `mcp/tools_docs.go`, `mcp/tools_docs_test.go`

**Doc loading** (`docs.go`):
- `loadNodeDocs(docsDir) -> (map[string]string, error)` — reads `<docsDir>/<NodeName>.md` files
- Called during `BuildServerContext` if `DocsDir` is configured
- `mergeNodeDoc(node, graphDescription, docContent)` — doc takes priority, graph description as fallback

**Tools:**
| Tool | Params | Description |
|------|--------|-------------|
| `get_node_documentation` | `node` (required) | Merged Markdown: graph metadata + per-node doc (if exists) + OAS operation summary |
| `get_workflow_documentation` | `goal` (required) | Assembled workflow doc: backward chain + per-node docs for each step |
| `generate_doc_stub` | `node` (required) | Generate Markdown skeleton from graph node metadata (inputs, outputs, description, OAS if available) |
| `list_undocumented_nodes` | — | Nodes missing doc files in docs dir |

**Prompt:**
| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `enrich_documentation` | `node` (required) | Given node + OAS + existing doc, suggest enriched documentation |

**Relationship to Task 59:** Task 59 (`aat docs generate`) generates docs; 60b exposes and enriches them via MCP. They share the same docs directory convention (`docs/api/`). 60b can work independently (generates stubs, loads existing docs).

**Tests:** With/without docs dir. Partial coverage (some nodes documented, some not). Stub generation includes expected sections.

---

### WI-7: Plan CRUD + Generation (60c part 1)

**Goal:** Claude Code can create, validate, save, and load test plans.

**Files:** `mcp/tools_plan.go`, `mcp/tools_plan_test.go`

**Tools:**
| Tool | Params | Description |
|------|--------|-------------|
| `generate_plan` | `prompt` (required), `save_as` (optional) | Delegate to `intent.Interpret()`. Return plan YAML + narrative. Optionally save. |
| `validate_plan` | `yaml` (required, string) | Parse YAML, validate against graph, return errors/warnings or "valid" |
| `list_saved_plans` | — | Scan workflows dir, show name + goal + step count for each |
| `load_plan` | `name` (required) | Load plan, return YAML + narrative (via `plan.FormatNarrative`) |
| `save_plan` | `name` (required), `yaml` (required) | Validate + write to workflows dir |

**Dependencies:** `intent.Interpret` requires LLM client. `generate_plan` checks that `Environment.LLM` is configured.

**Tests:** Validation with invalid plans. Save/load round-trip using temp dir. Generate with stub LLM client.

---

### WI-8: Execution + Archive Inspection (60c part 2)

**Goal:** Claude Code can run plans and inspect results.

**Files:** `mcp/tools_exec.go`, `mcp/tools_exec_test.go`, `mcp/tools_archive.go`, `mcp/tools_archive_test.go`

**Tools:**
| Tool | Params | Description |
|------|--------|-------------|
| `execute_plan` | `name` (required), `mode` (optional: strict/lean/adaptive) | Load plan, build engine (same pattern as `cmd/aat/runCommand`), execute synchronously, write archive, return summary with outcome + per-step status |
| `list_archives` | `limit` (optional, default 10) | Scan archive dir, return recent archives with run ID, date, outcome, duration |
| `inspect_archive` | `run_id` (required) | Load archive, format detailed Markdown: per-step request/response, assertions, selections, value resolutions |
| `analyze_failure` | `run_id` (required) | Focused on failed steps: error classification, relaxations attempted, HTTP status, response body excerpt, suggested next steps |
| `diff_archives` | `run_id_1` + `run_id_2` (required) | Compare two runs: step-by-step diff of statuses, durations, value differences, new/removed steps |

**Execution model:** Synchronous. The engine already respects `ctx.Done()` for cancellation. For long-running plans, the MCP client (Claude Code) can cancel the tool call. Async execution with polling is a 60d enhancement.

**Tests:** Execution with stub adapters + registry. Archive inspection with fixture JSON. Diff with two fixture archives.

---

### WI-9: Developer Workflow Prompts (60c part 3)

**Goal:** Prompts that guide Claude Code through end-to-end developer workflows: understand API, write integration code, generate tests, run, debug.

**Files:** `mcp/prompts_workflow.go`, `mcp/prompts_workflow_test.go`

**Prompts:**
| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `integration_guide` | `goal` (required) | Comprehensive prompt: "I want to integrate with [goal]". Includes workflow trace, per-node details, OAS schemas, domain types, template shapes. Guides Claude through understanding prerequisites, writing client code, handling errors. |
| `test_workflow` | `description` (required) | Prompt to guide: describe what to test -> generate plan -> confirm -> execute -> inspect. Includes graph context + domain knowledge for plan generation. |
| `debug_failing_test` | `run_id` (required) | Load failed archive, format failure context + surrounding successful steps + value resolutions + error classification. Prompt Claude to diagnose root cause and suggest fixes. |

**Design note:** These prompts combine data from multiple sources (graph, templates, OAS, domain, archives) into rich context that enables Claude Code to assist with the full integration lifecycle. They are the "glue" that makes the individual tools coherent as a workflow.

**Tests:** Prompt messages contain expected context sections. Missing data handled gracefully (no archive -> clear message).

---

## 60d: CI/CD + Production Monitoring (Stage 3a+ — High-Level Outline)

**Deferred.** Will be detailed when Stage 3a begins.

**CI/CD Tools (planned):**
- `generate_ci_config` — GitHub Actions / GitLab CI YAML from graph
- `generate_regression_suite` — multi-plan test suite covering all workflows
- `detect_graph_drift` — diff current graph vs. published version, flag breaking changes

**Production Monitoring (planned):**
- `define_monitor` — synthetic monitor = plan + schedule + alert conditions
- `list_monitors` / `enable_monitor` / `disable_monitor`
- `get_monitor_health` — success rate, duration trends, cost tracking
- `aat monitor daemon` CLI command for scheduled execution

**Prerequisites from 60a-60c:** Archive inspection, plan execution, graph diffing capabilities.

---

## Implementation Order & Dependencies

```
WI-1 (skeleton + CLI)
 ├── WI-2 (graph tools)
 ├── WI-3 (template + domain tools)  [also needs adapter/ change]
 └── WI-4 (OAS tools)
      └── WI-5 (resources + prompts)  [needs WI-2,3,4 for content]
           └── WI-6 (docs, 60b)
                └── WI-7 (plan CRUD, 60c-1)
                     └── WI-8 (execution + archives, 60c-2)
                          └── WI-9 (workflow prompts, 60c-3)
```

WI-2, WI-3, and WI-4 can be parallelized after WI-1. Each is independent.
WI-5 depends on WI-2/3/4 being complete (resources format data from all tool categories).
WI-6 through WI-9 are sequential.

## Session Boundaries

Each WI is designed as one implementation session. Natural break points:

- **Session 1:** WI-1 (skeleton). Establishes the package, manifest, context, and CLI. Everything else builds on this.
- **Sessions 2-4:** WI-2, WI-3, WI-4 (can be done in any order or parallelized across sessions). These are independent after WI-1.
- **Session 5:** WI-5 (resources + prompts). Wraps up 60a. Good checkpoint — test end-to-end with Claude Code.
- **Session 6:** WI-6 (docs, 60b). Self-contained.
- **Session 7:** WI-7 (plan CRUD). Starts 60c.
- **Session 8:** WI-8 (execution + archives). Heaviest WI — may split if needed.
- **Session 9:** WI-9 (workflow prompts). Completes 60c.

## Testing Strategy

- **Unit tests per tool handler:** Construct `ServerContext` with test data, call handler directly, assert Markdown output contains expected content. No MCP transport needed.
- **Format function tests:** Table-driven tests for `format.go` helpers.
- **OAS tests:** Small fixture spec (not the full Travelport spec). Test schema resolution, validation, examples.
- **Integration test (optional per WI):** Use mcp-go's `HandleMessage` for full protocol round-trip on key tools.
- **Manual validation:** After each WI, test with Claude Code against the Travelport booking graph.

## Verification

After all WIs complete:
1. `go test ./mcp/...` — all tests pass
2. Create `aat-project.yaml` for Travelport booking graph
3. `aat mcp serve --manifest aat-project.yaml` — starts successfully
4. Connect Claude Code, verify:
   - `list_nodes` returns 7 Travelport nodes
   - `describe_node` shows full detail for SearchFlights
   - `trace_workflow` traces booking workflow
   - `inspect_template` shows request shape
   - OAS tools resolve schemas (if OAS spec configured)
   - Domain tools show travel concepts
   - Plan generation works (requires LLM config)
   - Plan execution works (requires environment config)
   - Archives are inspectable

---

## 2026-02-10 — WI-1: Core Skeleton + CLI Complete

**What:** Implemented WI-1 — the foundational `mcp/` package with manifest, context, server, and CLI.

**Files created:**
- `mcp/doc.go` — package documentation
- `mcp/manifest.go` — `ProjectManifest`, `LoadManifest()`, `FindManifest()` with path resolution
- `mcp/manifest_test.go` — 10 tests (valid parse, missing fields, absolute paths, file search)
- `mcp/context.go` — `ServerContext`, `BuildServerContext()` with graph/templates/domain/OAS loading
- `mcp/context_test.go` — 11 tests (build with/without optional resources, spec path collection)
- `mcp/server.go` — `Server`, `NewServer()`, `Serve()` (stdio transport via mcp-go)
- `mcp/server_test.go` — 3 tests (creation with various context configurations)
- `cmd/aat/mcp_cmd.go` — `mcpMain()`, `mcpServeMain()` with `--manifest` flag

**Files modified:**
- `cmd/aat/main.go` — added `case "mcp":` to command switch, updated usage strings
- `go.mod` / `go.sum` — added `github.com/mark3labs/mcp-go v0.43.2`

**Decisions:**
- Server name uses `"aat:<project-name>"` format to distinguish multiple project servers
- Capabilities (tool/resource/prompt) all registered with `false` for listChanged — will enable as tools are added in later WIs
- `FindManifest()` walks up from cwd to filesystem root (same pattern as git repo detection)
- OAS spec loading happens eagerly during context build (specs are small; avoids lazy-load complexity)
- All paths in manifest resolved relative to manifest file directory (not cwd)
- Server version hardcoded to "0.1.0" until we wire internal/version

**Test count:** 24 tests, all passing
