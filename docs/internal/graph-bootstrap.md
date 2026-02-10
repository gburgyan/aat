# Graph Bootstrap Architecture

Internal notes on the graph authoring pipeline: how the pieces fit together, design rationale, and extension points.

## Pipeline Overview

```
OAS spec ──→ aat generate ──→ scaffold graph + templates
                                    │
                                    ▼
                              manual/AI refinement
                              (nodes, types, edges)
                                    │
                                    ▼
                         aat graph validate ──→ feedback loop
                                    │
                                    ▼
                              write plan ──→ aat run
```

The pipeline is designed around two principles:

1. **Scaffold, don't generate** — `aat generate` gives you ~70% of the graph (correct nodes, inputs, outputs, HTTP shapes). The remaining 30% (edges, domain types, cleanup, descriptions) requires human or AI judgment. This is intentional: generating edges would require understanding API semantics that aren't in the OAS spec.

2. **Validate continuously** — `aat graph validate` provides structural feedback at every step. The graph is the source of truth; OAS validation surfaces drift but doesn't block.

## Component Responsibilities

### `graph/oas/generate.go` — Scaffold Generator

**Input:** Parsed OAS v3 model + spec filename.
**Output:** `GenerateResult{Graph, Templates, Warnings}`.

Key design decisions:

- **ScaffoldTemplate is a local type** — mirrors `adapter.Template` YAML structure but lives in `graph/oas/` to avoid importing `adapter/` (which is mid-tier). YAML tags are carefully matched so output is compatible with `adapter.ParseTemplate`.

- **Adapter name = operationId** — simplest possible mapping. No namespace prefixes, no transformation. The user can rename later.

- **No edges** — We cannot infer data flow from OAS alone. Two operations might share a parameter name (`id`) but refer to completely different resources. Edges require semantic understanding of the workflow.

- **Array detection** — If a 2xx response is `type: array` with `items.$ref`, we create a single `object[]` output with `elementFields` from the item schema. `deriveArrayOutputName` strips common prefixes (`list`, `search`, `find`, etc.) for readability.

- **$ref resolution** — libopenapi resolves `$ref` transparently through schema proxies. `resolveSchemaProperties` handles both inline schemas and referenced ones.

- **Operations without operationId** — Warned and skipped. No operationId = no stable identifier = no way to link back for validation.

### `graph/oas/validator.go` — OAS Alignment Checker

**Input:** Parsed graph + loaded OAS specs.
**Output:** `SpecValidationResult{Issues}` with error/warning severity.

The validator runs 7 rules comparing graph nodes against their linked OAS operations. This is the feedback loop that makes iterative refinement practical:

| # | Severity | Rule |
|---|----------|------|
| 1 | error | operationId must not be empty |
| 2 | error | Must have a resolvable spec path |
| 3 | error | Spec must be in loaded specs map |
| 4 | error | operationId must exist in spec |
| 5 | warning | Graph input should exist in OAS params/body |
| 6 | warning | OAS required param should exist in graph inputs |
| 7 | warning | Graph output should exist in OAS response schema |

Rules 5-7 are warnings because the graph is intentionally a curated subset of the full OAS surface. A user who omits an optional OAS parameter shouldn't see an error.

### `cmd/aat/generate_cmd.go` — CLI Wiring

Thin: loads spec, calls `Generate()`, marshals output, writes files. The `--output-graph -` flag writes to stdout for pipeline composition (e.g., piping into `yq` for further transformation).

### `cmd/aat/graph_cmd.go` — Validation CLI

Resolves spec paths relative to the graph file's directory. Supports `--oas` override for cases where the spec path in the graph YAML isn't correct for the current filesystem layout. `--strict` promotes warnings to errors.

## AI-Assisted Edge Authoring

The graph scaffold + validation loop is designed to work well with AI authoring tools (Claude Code, Cursor, etc.). The workflow:

1. **User runs `aat generate`** — produces the scaffold
2. **AI reads the graph + API documentation** — understands node semantics
3. **AI adds edges** — based on which outputs feed which inputs
4. **User runs `aat graph validate`** — catches structural errors
5. **AI or user fixes issues** — repeat until clean

This works because:
- The graph YAML is simple, well-structured, and readable by LLMs
- Validation errors are actionable (exact node + field references)
- Edges are the only missing piece — nodes/inputs/outputs are already correct from the scaffold
- API documentation (Swagger/OpenAPI descriptions, README, etc.) provides the semantic context needed to infer data flow

### MCP Integration (Task 60)

When the MCP server is implemented, it will expose graph structure as tools/resources, making the AI authoring loop tighter:

- **`graph/read`** — Current graph state as structured data
- **`graph/validate`** — Run validation and return results
- **`oas/operation`** — Look up OAS operation details (params, schemas, examples)
- **`template/read`** — Current template content

This turns the manual `aat graph validate` step into an in-editor feedback loop.

## Extension Points

### Adding edge inference heuristics

If we ever want to suggest edges (not auto-generate), the heuristics would live in `graph/oas/` as a separate function:

```go
func SuggestEdges(g *graph.Graph, model *v3high.Document) []SuggestedEdge
```

Possible heuristics:
- Name matching: if output X.foo matches input Y.foo by name + type → suggest edge
- Path parameter matching: if a path has `{resourceId}` and another operation outputs `resourceId` → suggest edge
- OAS links: OpenAPI 3.1 `links` object explicitly describes response→request connections

This is not implemented because name matching has a high false-positive rate and OAS links are rarely populated in real specs. The AI-assisted approach is more reliable.

### Supporting multiple spec files

The graph already supports per-node spec overrides via `oas.spec`. The validator handles multiple loaded specs. The generator currently takes a single spec — extending to multiple specs would be straightforward but hasn't been needed yet.

### Generating from non-OAS sources

The `Generate()` function takes a parsed `v3high.Document`. To support other formats (Postman collections, HAR files, gRPC protos), you'd write a converter to the same OAS model or define a parallel generator with the same `GenerateResult` output type.

## File Inventory

| File | Lines | Purpose |
|------|-------|---------|
| `graph/oas/oas.go` | 70 | LoadSpec, FindOperation, ResolveNodeSpec |
| `graph/oas/validator.go` | 249 | Validator: CollectSpecPaths, LoadSpec, Validate |
| `graph/oas/generate.go` | 290 | Generate, ScaffoldTemplate, helpers |
| `graph/oas/oas_test.go` | 107 | LoadSpec, FindOperation tests |
| `graph/oas/validator_test.go` | 320 | 20 validation rule tests |
| `graph/oas/generate_test.go` | 290 | 18 generation tests |
| `graph/spec.go` | 88 | SpecValidator interface, result types |
| `cmd/aat/generate_cmd.go` | 85 | CLI: aat generate |
| `cmd/aat/graph_cmd.go` | 110 | CLI: aat graph validate |
