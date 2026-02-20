# AAT — Adaptive API Testing

AAT is a Go CLI tool that uses LLM-assisted planning and execution to test API workflows end-to-end. Vision and implementation details live in `vision/adaptive-api-testing-6-pager.md` and `vision/implementation-plan.md`.

## Module

`github.com/gburgyan/aat` — Go 1.24+

## Package Structure

| Package | Responsibility |
|---------|---------------|
| `cmd/aat/` | CLI binary — thin wrapper, wires packages together |
| `graph/` | API graph model, YAML parsing, traversal, backward chaining, diffing |
| `adapter/` | Adapter interface, HTTP executor, Tier 1/3 loaders |
| `domain/` | Domain knowledge: concepts, types, value pools |
| `plan/` | Plan model, expression evaluator, validation, persistence |
| `intent/` | LLM-powered prompt → plan transformation |
| `engine/` | Execution engine: scheduling, value resolution, retry, adaptation |
| `validate/` | Mechanical, semantic, and intent validation |
| `archive/` | Run archives: capture, inspection, diffing, reports |
| `llm/` | Provider-agnostic LLM client |
| `config/` | Configuration, environments, local storage |
| `server/` | Local web API server, WebSocket, embedded frontend |
| `mcp/` | MCP server: API lifecycle platform for IDE-based AI tools |
| `gateway/` | LLM gateway proxy logic |
| `internal/testutil/` | Shared test helpers and fixtures |
| `internal/version/` | Build version info |

## Dependency Rules

Dependencies flow in one direction. No cycles. No lateral imports within a tier.

**Leaf packages** (zero aat imports): `config`, `graph`, `domain`, `llm`
**Mid-tier**: `adapter` → config; `plan` → graph, config; `archive` → plan; `validate` → llm
**Orchestrators**: `engine` → graph, adapter, plan, domain, llm, validate, archive, config
**Entry points**: `intent` → graph, domain, plan, llm; `mcp` → all packages; `server` → engine, archive, plan, config
**Binaries**: `cmd/aat` → engine, server, intent, mcp, archive, config

Data flows down, decisions flow up. No business logic in `cmd/`.

## Dependency Injection Patterns

Cross-package dependencies use direct struct fields and function parameters — explicit, no framework. No global mutable state.

```go
// Production: construct with NewEngine + builder methods
router := engine.NewExecutorRouter(executor, envConfig)
eng := engine.NewEngine(g, registry, router).
    WithMode(config.ModeLean).
    WithLLM(llmClient).
    WithDomain(kb)

// Usage: collaborators are struct fields or function params
func (e *Engine) Run(ctx context.Context, p *plan.Plan) *RunResult {
    // use e.graph, e.LLMClient, etc.
}

// Tests: construct with test implementations
func TestRun(t *testing.T) {
    router := engine.NewExecutorRouter(executor, envConfig)
    eng := engine.NewEngine(g, &fakeRegistry{}, router).
        WithMode(config.ModeStrict)
    result := eng.Run(ctx, testPlan)
    // assert
}
```

## Testing Philosophy

- Each package has its own `_test.go` files; tests run with `go test ./...`
- Use `github.com/stretchr/testify` — `assert` for non-fatal checks, `require` for fatal preconditions
- Tests construct types with test implementations directly (no DI framework)
- Table-driven tests preferred for systematic coverage
- Closed-loop: tests verify observable behavior, not internal state
- `internal/testutil/` for shared helpers only — no test logic there

## Go Conventions

- `context.Context` as first parameter to functions that do I/O or cross package boundaries
- Errors as values; wrap with `fmt.Errorf("...: %w", err)` for context
- No `init()` functions; no global mutable state
- Exported types and functions get doc comments
- Use `errors.Is` / `errors.As` for error inspection
- Prefer returning concrete types; accept interfaces

## Documentation

- **`docs/internal/`** — progress tracker, architecture notes (for contributors)
- **`docs/worklog/`** — decision log entries per stage (date, decisions, rationale)
- **`docs/user/`** — user-facing docs (created when there's something to document)

Update `docs/internal/progress.md` as tasks complete. Add worklog entries for non-trivial decisions.

## Worklogs

When making design decisions or completing stage milestones, add entries to `docs/worklog/stage-N.md`. Format:

```
## YYYY-MM-DD — Summary

**What:** What was done
**Decisions:** Key choices and their rationale
**Open questions:** Anything deferred
```

## Running AAT (Travelport Example)

Build the binary and use the travelport config files in `travelport/`:

```bash
# Build (injects version/commit/date automatically)
make build

# Or without make:
# go build -o aat ./cmd/aat/

# LLM-generated plan from natural language prompt
./aat prompt \
  --env travelport/env.yaml \
  --graph travelport/graph.yaml \
  --templates travelport/templates/ \
  --domain travelport/domain.yaml \
  "book a flight from rome to new york"

# Optional prompt flags:
#   --yes              skip interactive confirmation (auto-execute)
#   --save FILE        save generated plan to a YAML file
#   --trace            capture planning pipeline trace for debugging
#   --trace-dir DIR    trace output directory (default: traces/)
#   --output DIR       archive output directory (default: runs/)

# Execute a pre-written plan
./aat run \
  --plan travelport/plans/roundtrip-booking.yaml \
  --env travelport/env.yaml \
  --graph travelport/graph.yaml \
  --templates travelport/templates/ \
  --domain travelport/domain.yaml

# Optional run flags:
#   --mode MODE        strict (no LLM), lean (LLM fallback), adaptive (lean + relaxation)
#   --output DIR       archive output directory (default: runs/)
#   --json             machine-readable JSON summary to stdout
#   --quiet            suppress progress, show final line only
#   --override NODE=URL  route a node to a different URL (repeatable)
#   --env-overlay FILE   path to overlay YAML with additional overrides
```

**Travelport config files:**
- `travelport/env.yaml` — environment config (auth, LLM endpoint)
- `travelport/graph.yaml` — API graph (59 nodes)
- `travelport/templates/` — 56 request templates
- `travelport/domain.yaml` — domain knowledge (concepts, types, value pools)
- `travelport/plans/` — 27 pre-written plan files for various scenarios

LLM config (endpoint, API key, model) comes from the `llm:` section in the env YAML. The API key resolves from an OS environment variable via `SecretRef`.

## Observability & Debugging

AAT has two layers of observability: **run archives** capture execution, **plan traces** capture planning.

### Run Archives (`archive/`)

Every execution writes a JSON archive to the output directory (default `runs/`). Archives contain per-step request/response pairs, timing, status codes, and overall outcome. Sensitive headers are redacted.

```bash
aat run --plan plan.yaml --env env.yaml --output runs/
# produces: runs/run-YYYYMMDD-HHMMSS-XXXXXXXX/archive.json
```

Key types: `archive.Archive`, `archive.Write`, `archive.Read`, `archive.GenerateRunID`.

### Plan Traces (`intent/`)

When `--trace` is passed to `aat prompt`, the `intent.Interpret()` pipeline captures every intermediate step as a JSON trace file. This is the primary tool for debugging LLM prompt engineering and plan generation issues.

```bash
aat prompt --trace --trace-dir traces/ --env env.yaml --graph graph.yaml --templates tpl/ "book a flight"
# produces: traces/trace-YYYYMMDD-HHMMSS-XXXXXXXX/plan-trace.json
```

The trace captures:
- **Goal call**: full system/user prompts, raw LLM response, token counts, timing, whether heuristic fallback was used
- **Backward chaining**: nodes, edges, decisions, timing
- **Skeleton**: the deterministic plan scaffold + YAML sent to the LLM, unfed inputs list
- **Plan call**: full prompts, raw LLM response, token counts, timing
- **Merge/post-process**: snapshots of the plan after merge and after post-processing
- **Validation**: any validation errors
- **Partial traces on error**: if the pipeline fails mid-way, whatever was captured so far is still written

Opt-in via `InterpretRequest.EnableTrace = true`. Zero overhead when disabled. Key types: `intent.PlanTrace`, `intent.WritePlanTrace`.

## Task planning

If a task seems too aggressive to do in one operation, push back and offer to break it down into sub-tasks. When doing this, update the implementation plan with the new information so we can have clearly defined work items.

## Current Stage

**Stage 2: Intelligence** — Foundation complete. Working through LLM-assisted planning and execution.
See `docs/internal/progress.md` for detailed status.
