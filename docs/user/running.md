# Running Tests

AAT has two run commands: `aat run plan` executes a single plan, and `aat run batch` executes all plans in a directory. Both produce JSON archives that capture every request, response, and assertion for later inspection.

## Running a Single Plan

```
aat run plan <name-or-path>
```

The positional argument is either a file path or a plan name. AAT resolves names by searching the plan directories declared in your [manifest](project-setup.md), then falling back to the literal file path. A `.yaml` or `.yml` extension is optional when using names.

```
$ aat run plan smoke-test
  [1/3] listProducts            200    45ms
  [2/3] createOrder             201   312ms
  [3/3] getOrderStatus          200    28ms

PASSED (3/3 steps, 385ms)
```

Each line shows the step index, node name, HTTP status code, and duration. Display outputs defined in the plan appear indented below their step.

## Checkpoints: Stopping Early and Handing Off State

Sometimes you want AAT to drive a system into a particular state and then hand off to a different, specialized test harness — for example, run a booking plan only as far as the step that creates a workbench, then let an external script take over with that live workbench and its auth token.

Two single-plan flags support this:

| Flag | Description |
|------|-------------|
| `--stop-after STEP` | Stop execution after the step whose ID equals `STEP` completes. Cleanup is **skipped**, so resources created up to that point stay alive for the handoff. |
| `--dump-state FILE` | Write the accumulated run state to `FILE`. Use `-` for stdout instead of a file. Usable with or without `--stop-after`. |

```
aat run plan roundtrip-booking \
  --stop-after createWorkbench \
  --dump-state workbench-state.json
```

The run reports a `stopped` outcome (exit code `0`), and the regular archive is still written. The dump file is JSON:

```json
{
  "version": "1",
  "outcome": "stopped",
  "stoppedAt": "createWorkbench",
  "baseUrl": "https://api.pp.example.com",
  "auth": { "headers": { "Authorization": "Bearer ..." } },
  "steps": [ { "stepId": "createWorkbench", "node": "createWorkbench", "outputs": { "workbenchId": "wb-42" } } ],
  "values": { "createWorkbench.workbenchId": "wb-42" }
}
```

- `baseUrl` and `auth.headers` come from the last request issued, capturing the live session.
- `values` flattens every completed step's outputs as `stepId.outputName` for easy lookup.

### Dumping to stdout (no file)

Pass `-` as the path to emit the state on stdout instead of writing a file. Combined with `--json`, the state is nested under a top-level `state` key in the summary, so a wrapping harness can capture everything from a single JSON object on stdout:

```
aat run plan roundtrip-booking --stop-after createWorkbench --json --dump-state -
```

```json
{
  "outcome": "stopped",
  "steps": [ ... ],
  "summary": { ... },
  "state": { "baseUrl": "...", "auth": { "headers": { ... } }, "values": { ... } }
}
```

Without `--json`, `--dump-state -` prints the state object on its own (pair it with `--quiet` to suppress progress).

> **Security:** unlike run archives, auth headers in the dump are **not redacted** — that is the point, so the external harness can replay calls. To a file it is written with mode `0600`; to stdout it lands in your terminal/pipe. Either way, treat it as a secret: do not commit, log, or share it.

`STEP` is matched against the step's ID (its `id:` if set, otherwise the node name). An unknown step name fails the run with a clear error rather than silently running to completion.

## Running Batches

```
aat run batch [directory]
```

Without arguments, AAT discovers all `.yaml` and `.yml` files in the plan directories declared in your manifest. With a directory argument, it scopes discovery to that subdirectory.

### Subdirectory Filtering

A relative path filters within the configured plan directories. An absolute path is used as a standalone directory.

```
# Run only plans under plans/orders/
aat run batch orders/

# Run plans from an absolute path
aat run batch /tmp/smoke-tests/
```

### Parallel Execution

By default, plans run sequentially. Use `--parallel` to run multiple plans concurrently.

```
aat run batch --parallel 4
```

In parallel mode, AAT displays a compact progress renderer that tracks all active plans. Sequential mode shows step-by-step output for each plan.

### Layer Expansion

Layers provide alternate test data for the same plan structure. The `--layer` flag adds a layer to every plan in the batch. The `--layer-group` flag creates a cartesian product — each plan runs once per combination of layer groups.

```
# Every plan runs with the "premium" layer applied
aat run batch --layer premium

# 2 plans x 2 groups = 4 runs
aat run batch --layer-group "premium,standard" --layer-group "us,eu"
```

With two plans (`smoke-test.yaml`, `full-checkout.yaml`) and two layer groups of two values each, AAT runs eight total executions: each plan with each combination of (`premium`/`standard`) x (`us`/`eu`).

See [Plans: Layers](plans.md#layers) for how layers are defined and how they override step values.

## Shared Flags

These flags apply to both `run plan` and `run batch`.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |
| `--env` | path | from manifest | Environment config file |
| `--env-name` | string | from manifest | Environment name (for multi-environment files) |
| `--graph` | path | from manifest | API graph file |
| `--templates` | path | from manifest | Templates directory |
| `--domain` | path | from manifest | Domain knowledge file |
| `--output` | path | `_output/runs` | Archive output directory |
| `--override` | `NODE=URL` | — | Route a node to a different URL (repeatable) |
| `--env-overlay` | path | — | Overlay YAML with additional environment overrides |
| `--retries` | int | `0` | Max plan-level retries on failure |
| `--layer` | string | — | Data layer to apply (repeatable) |
| `--no-auto-overrides` | bool | `false` | Disable auto-discovery of `.aat-overrides.yaml` |
| `--no-mutations` | bool | `false` | Skip mutation-expanded sibling steps; run only the happy path (smoke-test mode) |
| `--verbose-auth` | bool | `false` | Log auth request/response details to stderr for debugging |
| `--json` | bool | `false` | Machine-readable JSON summary to stdout |
| `--quiet` | bool | `false` | Suppress progress, show final line only |

The `run batch` command adds:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--parallel` | int | `1` | Concurrency limit (1 = sequential) |
| `--layer-group` | string | — | Comma-separated layer names for permutations (repeatable) |

When a manifest is discoverable, `--env`, `--graph`, `--templates`, and `--domain` are optional. Explicit flags always override manifest paths. See [Project Setup: Auto-Discovery](project-setup.md#auto-discovery) for how manifest resolution works.

AAT also auto-discovers a `.aat-overrides.yaml` dotfile for personal, per-project routing overrides. This is especially useful for [local development](local-dev.md) — drop the file once and every run picks it up without extra flags. Use `--no-auto-overrides` to disable this for CI or clean runs.

## Output Modes

### Default (Progress)

Without flags, AAT prints step-by-step progress as each step completes.

```
$ aat run plan full-checkout
  [1/5] listProducts            200    52ms
  [2/5] createCart               201    98ms
  [3/5] addToCart                200    45ms
  [4/5] createOrder             201   287ms
  [5/5] getOrderStatus          200    31ms

  cleanup:
    cancelOrder                  200    64ms

PASSED (5/5 steps, 513ms)
```

Assertion failures append a marker to the step line. Display outputs appear indented below their step.

### Quiet (`--quiet`)

Suppresses all progress output. Prints a single summary line when the run finishes.

```
$ aat run plan smoke-test --quiet
PASSED (3/3 steps, 385ms)
```

For batches, each plan gets one summary line:

```
$ aat run batch --quiet
smoke-test          PASSED  3 steps   385ms
full-checkout       PASSED  5 steps   513ms
return-flow         FAILED  4 steps   892ms

FAILED (2/3 passed)
```

### JSON (`--json`)

Writes a machine-readable JSON summary to stdout. Implies `--quiet` — no progress output is mixed with the JSON. See [CI/CD Integration](ci-cd.md) for the full JSON schema and pipeline integration patterns.

```
$ aat run plan smoke-test --json
{"outcome":"passed","steps":[...],"summary":{"total_steps":3,...},...}
```

## Exit Codes

| Code | Meaning | When |
|------|---------|------|
| `0` | Passed / Stopped | All steps succeeded, or a `--stop-after` checkpoint was reached |
| `1` | Failed | One or more steps or assertions failed |
| `2` | Error | Infrastructure or setup error (bad config, network failure, invalid plan) |

These codes are deterministic and designed for CI/CD pipelines. See [CI/CD Integration: Exit Codes](ci-cd.md#exit-codes) for detailed scenarios.

## What Happens During Execution

When you run a plan, AAT performs these steps in order:

1. **Load and validate** — parse the plan YAML, validate it against the graph
2. **Authenticate** — obtain credentials using the environment's auth config
3. **Resolve and execute** — for each step in topological order: resolve input values, execute the HTTP request, extract outputs, run assertions
4. **Cleanup** — run cleanup steps in reverse order, even if main steps failed
5. **Archive** — write the full execution trace to the output directory

### Step Execution Order

Steps run in topological order based on `dependsOn` declarations. Steps with no dependencies run first. Steps that depend on earlier steps wait until their dependencies complete. Within a dependency level, steps run in plan declaration order.

### Value Resolution at Runtime

Each step input is resolved through a priority chain: plan-provided values, then references to earlier step outputs, then domain value pools, then graph defaults. Expressions like `{{today + 7 days}}` and environment variable references like `{{env.API_REGION}}` are evaluated at resolution time.

See [Value Resolution](value-flow.md) for the full priority chain and resolution strategies.

### Cleanup

Steps with `cleanup: true` run after all main steps complete, regardless of whether main steps passed or failed. Cleanup steps run in reverse declaration order. A failed cleanup step does not affect the run outcome — the outcome is determined by the main steps.

## Retries

The `--retries` flag sets the maximum number of plan-level retries on failure. When a plan fails and retries remain, AAT re-executes the entire plan from scratch.

```
aat run plan flaky-test --retries 2
```

Each failed attempt is saved as `attempt-01.json`, `attempt-02.json`, etc. in the run directory. The final attempt (whether it passed or not) is saved as `archive.json`. Setup errors (invalid plan, missing config) are not retried.

A short delay separates retry attempts to avoid hammering the API.

## Archives

Every execution produces a JSON archive in the output directory.

### Single Run

```
_output/runs/
  run-20260223-143052-a1b2c3d4/
    archive.json
```

### Batch

```
_output/runs/
  batch-20260223-150000-e5f6g7h8/
    batch.json
    run-20260223-150001-i9j0k1l2/
      archive.json
    run-20260223-150003-m3n4o5p6/
      archive.json
```

The batch directory contains a `batch.json` with aggregate results and a subdirectory per plan with its individual archive.

### What Archives Contain

Archives capture the full execution trace: per-step request/response pairs (method, URL, headers, body), HTTP status codes, timing, extracted outputs, value resolution decisions, selection decisions, assertion results, and error classifications. Sensitive headers (`Authorization`, API keys) are automatically redacted.

Archives are safe to store as CI artifacts or share with teammates. See [Web UI and Archives](web-ui.md) for browsing and debugging with the archive viewer.

## Debugging Authentication

The `--verbose-auth` flag prints the full authentication exchange to stderr, so you can see exactly what AAT sends and receives when obtaining tokens.

```
$ aat run plan smoke-test --verbose-auth
[auth] authenticating with type=oauth2
[auth] POST https://auth.example.com/oauth/token
[auth]   client_id = IeWY...
[auth]   client_secret = IzlP...
[auth]   grant_type = password
[auth]   password = RVfN...
[auth]   username = testuser
[auth] response status: 200
[auth] response body: {"access_token":"eyJhb...","token_type":"Bearer","expires_in":86400}
[auth] token type=Bearer expires_in=86400 access_token=eyJhbGci...
```

Key details:

- **Output goes to stderr** — it won't interfere with `--json` output on stdout.
- **Passwords and client secrets are truncated** to the first 4 characters for readability (you already have access to the full values in your config).
- **All auth types are covered** — OAuth2 shows the full token exchange; API key and bearer show the resolved credential values.
- **Works with overlay auth** — if an overlay file overrides auth, the verbose output reflects the effective auth being used.

This flag is available on both `run plan` and `run batch`.

## Building AAT

Build the AAT binary with version information embedded:

```
make build
```

This compiles the Go binary with version, commit hash, and build date injected via linker flags, and builds the embedded web UI frontend.

For a quick build without the frontend:

```
go build -o aat ./cmd/aat/
```

The resulting binary is self-contained — no runtime dependencies, no external files needed beyond your project's YAML configuration.

---

*Source: `cmd/aat/run_plan_cmd.go`, `cmd/aat/run_batch_cmd.go`, `cmd/aat/run_shared.go`, `cmd/aat/progress.go`.*
