# Batch Plan Execution

## 2026-02-22 — Design: Batch Plan Execution

**What:** Design for running multiple plans as a correlated batch, with CLI restructure and nested archive storage.

**Decisions:**
- **CLI restructure**: `aat run` becomes parent command with `plan` and `batch` subcommands. Clean break — no backward compat for `--plan` flag.
- **Batch failure mode**: Always run all plans, report aggregate results. No `--fail-fast` in v1.
- **Shared infra loading**: Load env/graph/templates/domain once per batch, reuse across plans.
- **Archive structure**: Batch runs stored in `batch-YYYYMMDD-HHMMSS-XXXXXXXX/` directory with `batch.json` index and standard `run-*/archive.json` per member.
- **Web viewer**: Planned out but deferred to follow-on task.
- **Exit codes**: 0=all pass, 1=any fail, 2=any error (worst wins).

**Open questions:**
- Batch concurrency (sequential-only in v1, `--concurrency` possible later)
- Whether to refactor `prompt.go`'s `executePlan()` to share `runContext` (deferred)

---

## Design Details

### CLI Structure

```
aat run plan <name-or-path>     # single plan (positional arg)
aat run batch [directory]        # all plans, or filtered by directory
```

Shared flags on `runCmd.PersistentFlags()`: `--manifest`, `--env`, `--graph`, `--templates`, `--domain`, `--output`, `--mode`, `--override`, `--env-overlay`

Per-subcommand flags: `--json`, `--quiet`

### Batch Discovery

- No args → `config.ListPlans(planDirs)` (all plans)
- Relative path → filter by prefix within plan dirs
- Absolute path → treat as standalone plan dir

### Batch Archive Structure

```
runs/
  batch-20260222-143000-a1b2c3d4/
    batch.json                        # aggregate metadata + plan→runID mapping
    run-20260222-143001-aaaaaaaa/     # member run (complex-card)
      archive.json
    run-20260222-143005-bbbbbbbb/     # member run (round-trip)
      archive.json
```

### Code Organization

| New File | Content |
|----------|---------|
| `cmd/aat/run_cmd.go` | Parent command, PersistentFlags, init wiring (renamed from main.go) |
| `cmd/aat/run_plan_cmd.go` | Single plan subcommand |
| `cmd/aat/run_batch_cmd.go` | Batch subcommand + batch types |
| `cmd/aat/run_shared.go` | Shared types: runContext, runArgs, runResult, helpers |

### Shared Infrastructure (`runContext`)

```go
type runContext struct {
    Env       *config.Environment
    Graph     *graph.Graph
    Registry  *adapter.Registry
    Router    *engine.ExecutorRouter
    KB        *domain.KnowledgeBase
    LLMClient llm.Client
    Mode      config.ExecutionMode
    Secrets   map[string]bool
    GraphDir  string
}
```

Loaded once via `loadRunContext()`, then `executePlanWithContext()` called per plan.

### Web Viewer (follow-on)

- `ListRuns()` scans both top-level `run-*` and nested `batch-*/run-*` dirs
- New endpoints: `GET /api/batches`, `GET /api/batches/{id}`
- `RunListEntry` gains optional `batchId` field
- Frontend: collapsible batch groups in RunList, new BatchDetail component
- `aat web view batch-*` routes to `/batches/{id}`
