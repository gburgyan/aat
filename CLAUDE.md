# AAT — Adaptive API Testing

AAT is a Go CLI tool that uses LLM-assisted planning and execution to test API workflows end-to-end. Vision and implementation details live in `vision/adaptive-api-testing-6-pager.md` and `vision/implementation-plan.md`.

## Module

`github.com/gburgyan/aat` — Go 1.23+

## Package Structure

| Package | Responsibility |
|---------|---------------|
| `cmd/aat/` | CLI binary — thin wrapper, wires packages together |
| `graph/` | API graph model, YAML parsing, traversal, backward chaining, diffing |
| `adapter/` | Adapter interface, HTTP executor, Tier 1/2/3 loaders |
| `domain/` | Domain knowledge: concepts, types, value pools |
| `plan/` | Plan model, expression evaluator, validation, persistence |
| `intent/` | LLM-powered prompt → plan transformation |
| `engine/` | Execution engine: scheduling, value resolution, retry, adaptation |
| `validate/` | Mechanical, semantic, and intent validation |
| `archive/` | Run archives: capture, inspection, diffing, reports |
| `llm/` | Provider-agnostic LLM client |
| `config/` | Configuration, environments, local storage |
| `server/` | Local web API server, WebSocket, embedded frontend |
| `gateway/` | LLM gateway proxy logic |
| `internal/testutil/` | Shared test helpers and fixtures |
| `internal/version/` | Build version info |

## Dependency Rules

Dependencies flow in one direction. No cycles. No lateral imports within a tier.

**Leaf packages** (zero aat imports): `config`, `graph`, `domain`, `llm`
**Mid-tier**: `adapter` → config; `plan` → graph; `archive` → plan; `validate` → llm
**Orchestrators**: `engine` → graph, adapter, plan, domain, llm, validate, archive, config
**Entry points**: `intent` → graph, domain, plan, llm; `server` → engine, archive, plan, config
**Binaries**: `cmd/aat` → engine, server, intent, archive, config

Data flows down, decisions flow up. No business logic in `cmd/`.

## go-ctxdep Patterns

All cross-package dependencies are resolved from `context.Context` via `go-ctxdep`. No constructor injection, no global mutable state.

```go
// Production: register real implementation on context
ctx := context.Background()
ctx = ctxdep.RegisterValue(ctx, realHTTPClient)

// Usage: resolve from context at call site
func RunStep(ctx context.Context, step plan.Step) error {
    client, err := ctxdep.Get[*http.Client](ctx)
    if err != nil { return err }
    // use client
}

// Tests: build context with test implementations
func TestRunStep(t *testing.T) {
    ctx := context.Background()
    ctx = ctxdep.RegisterValue(ctx, &fakeHTTPClient{})
    err := RunStep(ctx, testStep)
    // assert
}
```

## Testing Philosophy

- Each package has its own `_test.go` files; tests run with `go test ./...`
- Use `github.com/stretchr/testify` — `assert` for non-fatal checks, `require` for fatal preconditions
- Tests build a `context.Context` with test implementations via go-ctxdep
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

## Task planning

If a task seems too aggressive to do in one operation, push back and offer to break it down into sub-tasks. When doing this, update the implementation plan with the new information so we can have clearly defined work items.

## Current Stage

**Stage 1: Foundation** — Project scaffolded. Starting Task 1 (YAML graph schema).
See `docs/internal/progress.md` for detailed status.
