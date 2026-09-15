# Running Tests

AAT has two run commands: `aat run plan` executes a single plan, and `aat run batch` executes all plans in a directory. Both write [archives](archives.md) that capture every request, response, and assertion for later inspection. To install AAT, see [Install](install.md).

## Running a Single Plan

```
aat run plan <name-or-path>
```

The positional argument is a file path or a plan name. A path that exists is used as is; otherwise AAT searches the plan directories declared in your [manifest](project-setup.md), where the `.yaml` or `.yml` extension is optional.

Running the shop example's `smoke` recipe against `aat-sandbox serve`:

```
$ aat run plan smoke
aat: loading environment...
aat: loaded environment "us"
aat: loaded graph (18 nodes)
aat: loaded domain knowledge
aat: loaded 18 templates
aat: loaded 1 OAS spec(s) for runtime validation
aat: reconstituting recipe "Quick Purchase"...
aat: authenticated via oauth2
aat: override: payment*
aat: executing plan (5 steps)...

  [1/5] listProducts         200  0ms
  [2/5] createCart           201  0ms
  [3/5] addItem              201  0ms
  [4/5] checkout             201  0ms
        Order: ord_0001
        Receipt: RCPT-US-0001
        Tax: Sales tax 8.25%
        Total: $103.40
  [5/5] paymentCharge        201  351ms
        Charged: $103.40

  cleanup:
    deleteOrder            204  0ms
    deleteCart             204  0ms

PASSED (5/5 steps, 354ms)
Archive: /path/to/shop/_output/runs/run-20260910-231357-eca8e0b3/archive.json
```

Each step line shows the step index, the step ID, the HTTP status code, and the duration. The step ID is what `--stop-after` and `dependsOn` take; when it differs from the node and the column has room, the node follows in parentheses (`addProduct (addItem)`), and on a narrow terminal the ID stands alone (`checkout`, whose node is `checkoutCart`). A step's duration runs from its first attempt to the end of its last, so retry waits count, as does any wait for [request pacing](environments.md#request-pacing), and the outcome line reports the run's wall-clock time. Display outputs defined in the plan appear indented below their step, and cleanup steps follow the main steps.

## Checkpoints

`aat run plan --stop-after STEP` stops after a step passes and skips cleanup, so the resources created so far stay alive, and `--dump-state FILE` writes the session (base URLs, headers, and step inputs and outputs) for another tool to pick up, with credentials redacted unless `--dump-state-secrets` asks for them. The outcome is `stopped` with exit code `0`. See [Checkpoints](checkpoints.md) for the dump format, stdout mode, security, and a pytest handoff example.

## Inspecting a Run

`aat run show latest` lists the newest run's steps with their HTTP status, result, and outputs. `--step ID` shows one step, and `--response --shape` prints the structure of its response, which is the quickest way to learn what an API returned, from a terminal or from an AI coding assistant. See [Archives: Inspecting a Run from the CLI](archives.md#inspecting-a-run-from-the-cli).

## Running Batches

```
aat run batch [directory]
```

Without arguments, AAT runs every `.yaml` and `.yml` plan in the plan directories declared in your manifest. With an argument, it runs some of them.

### Selecting Plans

A relative argument names plans within the configured plan directories: a directory selects every plan under it, and a plan name selects that plan, with or without its extension. Names are compared a whole path segment at a time, so `orders` selects `orders.yaml` and every plan under `orders/`, but not `orders-legacy/` or `orders-archive.yaml`. An absolute path is used as a standalone plan directory.

```
# Run only plans under plans/orders/
aat run batch orders

# Run one plan
aat run batch orders/refund

# Run plans from an absolute path
aat run batch /tmp/smoke-tests/
```

A batch that finds no plans, because the filter matches none or the directories hold none, is an error (exit code `2`) rather than an empty pass, so a mistyped filter fails the job.

### Parallel Execution

By default, plans run sequentially. Use `--parallel` to run multiple plans concurrently.

```
aat run batch --parallel 4
```

In parallel mode, AAT displays a compact progress renderer that tracks all active plans. Sequential mode shows step-by-step output for each plan.

Parallel plans share the environment's [request pacing](environments.md#request-pacing): with `settings.minRequestInterval: 250ms`, `--parallel 4` still starts at most four requests a second.

### Layer Expansion

Layers provide alternate test data for the same plan structure. Both flags take layer names. The `--layer` flag adds a layer to every plan in the batch. The `--layer-group` flag creates a cartesian product: each plan runs once per combination of the groups, and every group also includes a "none" choice.

```
# Every plan runs with the "premium" layer applied
aat run batch --layer premium

# 2 plans x (2+1) x (2+1) permutations = 18 runs
aat run batch --layer-group "premium,standard" --layer-group "us,eu"
```

With two plans and two layer groups of two layers each, each group offers three choices (`premium`, `standard`, or neither; `us`, `eu`, or neither), so there are 3 × 3 = 9 permutations and 18 runs, from the base run with no group layers up to `premium, us`. Permutations that produce an identical plan are skipped as duplicates: on the shop example, `smoke` and `full-lifecycle` with `--layer-group shipping-standard,shipping-express --layer-group basket-gear,basket-apparel` report `Batch: 12/18 PASSED, 6 SKIPPED`, because the `shipping-standard` layer sets the shipping tier the plans already use by default.

See [Plans: Layers](plans.md#layers) for how layers are defined and how they override step values, and [Matrix Testing](batch-layers.md) for the permutation formula and duplicate detection.

## Shared Flags

These flags apply to both `run plan` and `run batch`.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |
| `--env-config` | path | from manifest | Environment config file |
| `--env` | string | `AAT_ENV_NAME`, else an overlay's `environment:`, else the manifest's `defaultEnvironment` | Environment name for multi-environment files (see [Environments: Environment Name Resolution](environments.md#environment-name-resolution)) |
| `--graph` | path | from manifest | API graph file |
| `--templates` | path | from manifest | Templates directory |
| `--domain` | path | from manifest | Domain knowledge file |
| `--output` | path | manifest `archives`, else `_output/runs` | Archive output directory (see [Archives](archives.md)) |
| `--override` | `NODE=URL` | — | Route a node to a different URL (repeatable); keeps the environment headers and auth |
| `--overlay` | path | — | Overlay YAML with additional environment overrides |
| `--var` | `KEY=VALUE` | — | Set a var of a multi-environment file (repeatable; wins over the file's vars) — see [Environments: Setting vars from the command line](environments.md#setting-vars-from-the-command-line) |
| `--retries` | int | `0` | Max plan-level retries on failure |
| `--layer` | string | — | Layer name to apply (repeatable) |
| `--no-auto-overrides` | bool | `false` | Disable auto-discovery of `.aat-overrides.yaml` |
| `--oas-validate` | string | `auto` | OAS validation mode: `auto`, `strict`, or `off` (see [OAS Validation](#oas-validation)) |
| `--no-mutations` | bool | `false` | Skip mutation-expanded sibling steps; run only the happy path (smoke-test mode) |
| `--verbose-auth` | bool | `false` | Log auth request/response details to stderr for debugging |
| `--json` | bool | `false` | Machine-readable JSON summary to stdout |
| `--quiet` | bool | `false` | Suppress progress; print only the outcome and archive path |

The `run plan` command adds:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--stop-after` | string | — | Stop after the step with this ID passes and skip cleanup (see [Checkpoints](checkpoints.md)) |
| `--dump-state` | path | — | Write the run state to a file with mode `0600`, with credentials redacted (`-` for stdout, which then carries only the state) |
| `--dump-state-secrets` | bool | `false` | Keep live credentials in the `--dump-state` output, for a harness that sends requests as the run's session (see [Checkpoints: Security](checkpoints.md#security)) |

The `run batch` command adds:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--parallel` | int | `1` | Concurrency limit (1 = sequential) |
| `--layer-group` | string | — | Comma-separated layer names for permutations (repeatable) |
| `--no-dedup` | bool | `false` | Disable duplicate plan detection across permutations |
| `--shuffle` | bool | `false` | Randomize plan execution order |
| `--seed` | int | `0` | Random seed for `--shuffle` (`0` = current time; the seed used is logged) |

See [Matrix Testing: Controlling Behavior](batch-layers.md#controlling-behavior) for how dedup, shuffle, and seed interact with layer groups.

When a manifest is discoverable, `--env-config`, `--graph`, `--templates`, and `--domain` are optional. Explicit flags always override manifest paths. See [Project Setup: Auto-Discovery](project-setup.md#auto-discovery) for how manifest resolution works.

AAT also auto-discovers a `.aat-overrides.yaml` dotfile for personal, per-project routing overrides. This is especially useful for [local development](local-dev.md) — drop the file once and every run picks it up without extra flags. Use `--no-auto-overrides` to disable this for CI or clean runs.

## Output Modes

### Default (Progress)

Without flags, AAT prints the loading messages, then one line per step as it completes, then cleanup, the outcome line, and the archive path. The shop's `full-lifecycle` plan, after the loading messages:

```
aat: executing plan (15 steps)...

  [ 1/15] listProducts         200  0ms
  [ 2/15] checkInventory       200  609ms  retried 1x: response_error
  [ 3/15] createCart           201  0ms
  [ 4/15] addProduct (addItem) 201  0ms
  [ 5/15] addSocks (addItem)   201  0ms
  [ 6/15] getCart              200  0ms
  [ 7/15] applyCoupon          200  0ms
  [ 8/15] checkout             201  0ms
          Order: ord_0001
          Receipt: RCPT-US-0001
          Tax: Sales tax 8.25%
          Total: $130.66
  [ 9/15] paymentCharge        201  351ms
          Charged: $130.66
  [10/15] shipOrder            201  601ms
          Tracking: 1Z298498081
  [11/15] getShipment          200  1.3s  retried 2x: transient
  [12/15] deliverShipment      200  0ms
  [13/15] createReturn         201  0ms
          RMA: RMA-US-0001
  [14/15] paymentRefund        201  0ms
          Refunded: $130.66
  [15/15] verify_getOrder      200  0ms

  cleanup:
    deleteOrder            204  0ms
    deleteCart             204  0ms

PASSED (15/15 steps, 2.9s)
Archive: /path/to/shop/_output/runs/run-20260910-231537-19c3c1c5/archive.json
```

Notes after the duration mark step-level retries (`retried 2x: transient`; the duration includes the waits between attempts), failed assertions (`ASSERTIONS FAILED`, with each failed assertion's message indented below), and OpenAPI violations (`OAS: 2 warning(s)`). Display outputs appear indented below their step.

### Quiet (`--quiet`)

Suppresses all progress output. Prints the outcome and the archive path when the run finishes.

```
$ aat run plan smoke --quiet
PASSED (5/5 steps)
Archive: /path/to/shop/_output/runs/run-20260910-231358-a09d855d/archive.json
```

A failed or errored run prints its error instead of the step counts, for example `FAILED: step "getShipment" returned status 404`.

For batches, each run gets one line, followed by the totals and the batch directory. Here three shop plans failed because `--override` sent `getShipment` to the wrong region:

```
$ aat run batch --quiet --override getShipment=http://localhost:8765/eu/v1
full-lifecycle: FAILED: step "getShipment" returned status 404
giftcard-express: FAILED: step "getShipment" returned status 404
negative/add-item-mutations: PASSED
negative/state-machine: PASSED
registered-paypal-coupon: PASSED
resilience: FAILED: step "getShipment" returned status 404
smoke: PASSED
Batch: 4/7 PASSED, 3 FAILED
Archive: /path/to/shop/_output/runs/batch-20260910-231405-dfb83c02
```

With `--layer-group`, each line names its permutation (`smoke [basket-gear, shipping-express]: PASSED`), and skipped duplicates say which run they duplicate (`smoke [shipping-standard]: SKIPPED (duplicate of smoke [(base)])`).

### JSON (`--json`)

Writes a machine-readable JSON summary to stdout. Implies `--quiet` — no progress output is mixed with the JSON. See [CI/CD Integration](ci-cd.md) for the full JSON schema and pipeline integration patterns.

```
$ aat run plan smoke --json
{
  "outcome": "passed",
  "steps": [ ... ],
  "cleanup": [ ... ],
  "summary": {
    "total_steps": 5,
    "passed_steps": 5,
    "failed_steps": 0,
    "duration_ms": 353
  },
  "archive_path": "/path/to/shop/_output/runs/run-20260910-225512-5d4da602/archive.json"
}
```

## Exit Codes

Every `aat` command uses the same codes:

| Code | Meaning | When |
|------|---------|------|
| `0` | Passed / Stopped | All steps succeeded, or a `--stop-after` checkpoint was reached |
| `1` | Failed | A test ran and failed: one or more steps or assertions failed. For `aat validate`, validation found a problem |
| `2` | Error | AAT could not do what was asked: an unknown flag, argument, or subcommand; a manifest, environment, or `--var` it cannot use; an invalid plan; a network or authentication failure |
| `130` | Aborted | The run was interrupted by Ctrl+C or `SIGTERM` (see [Interrupting a Run](#interrupting-a-run-ctrlc)) |

For batches, the exit code reflects the worst outcome across all plans: any aborted plan gives `130`, otherwise any error gives `2`, otherwise any failure gives `1`.

By command:

- `aat run plan`, `aat run batch`, and `aat prompt` exit with the outcome of the run. With `--json`, an error that stops `aat run plan` or `aat run batch` before a plan runs still prints a JSON document, with `"outcome": "error"` and the reason in `error`. `aat run batch` exits `2` when it finds no plans to run.
- `aat validate` and its subcommands exit `1` when validation finds a problem, including a manifest that fails to load, and `2` when there is nothing to validate: no manifest found, or no `--graph` for `aat validate graph`, `plan`, or `workflow`.
- `aat env list` and `aat plan list` exit `0` when they can list, even when an entry fails to load (they print its error), and `2` when they cannot.
- `aat mcp serve` and `aat web` exit `2` with the reason on stderr when they cannot start.
- `aat generate`, `aat docs generate`, `aat import`, `aat run clean`, and `aat run rebuild-summaries` exit `2` on an error.

These codes are deterministic and designed for CI/CD pipelines. See [CI/CD Integration: Exit Codes](ci-cd.md#exit-codes) for detailed scenarios.

## Interrupting a Run (Ctrl+C)

Pressing Ctrl+C (or sending `SIGTERM`) during a run does not simply kill the process. AAT cancels the request in progress, or the wait before a retry, and issues no new ones; it then runs cleanup for the resources created so far and writes a partial archive. Interrupting the shop's `full-lifecycle` plan while `shipOrder` is in flight:

```
$ aat run plan full-lifecycle
...
  [ 9/15] paymentCharge        201  352ms
          Charged: $130.66
  [10/15] shipOrder            ERROR: executing HTTP request: Post "http://localhost:8765/us/v1/orders/ord_0003/ship": interrupt signal received

  cleanup:
    deleteOrder            204  1ms
    deleteCart             204  0ms

ABORTED (10/15 steps, 1.4s)
Archive: /path/to/shop/_output/runs/run-20260911-123328-b294e061/archive.json
aat: interrupted, writing partial results...
```

The archive records the outcome as `aborted` with the steps that ran, the interrupted one included, and the process exits with code `130`. Cleanup for an aborted run executes under its own 30-second budget so a hung API cannot keep the process alive indefinitely. In a batch, the plan that was running is marked `aborted`; plans that had not yet started still get an entry, but each stops before issuing a request and is recorded as `aborted` too. The batch outcome is `aborted` and the process exits `130`.

## What Happens During Execution

When you run a plan, AAT performs these steps in order:

1. **Load and validate** — parse the plan YAML, validate it against the graph
2. **Authenticate** — obtain credentials using the environment's auth config
3. **Resolve and execute** — for each step in topological order: resolve input values, execute the HTTP request, extract outputs, run assertions
4. **Cleanup** — run plan-level cleanup steps, then graph-level cleanup pairings (once per resource, newest first), even if main steps failed
5. **Archive** — write the full execution trace to the output directory

### Step Execution Order

Steps run in topological order based on `dependsOn` declarations. Steps with no dependencies run first. Steps that depend on earlier steps wait until their dependencies complete. Within a dependency level, steps run in plan declaration order.

### Value Resolution at Runtime

Each step input is resolved through a priority chain: references to a named selection or to other inputs, then `from` references to earlier step outputs, then the step's literal value, expression, or `pool`, where graph defaults and layers have already been copied in. Domain files are not read at run time. Expressions like `{{today + 7 days}}` and environment variable references like `{{env.API_REGION}}` are evaluated at resolution time.

See [Value Resolution](value-flow.md) for the full priority chain and resolution strategies.

### Cleanup

Cleanup runs after the main steps finish — whether the plan passed, failed, errored, or was interrupted with Ctrl+C. The only time cleanup is skipped is a `--stop-after` [checkpoint](checkpoints.md), where the whole point is to leave resources alive.

Two sources of cleanup work combine, in this order:

1. **Plan-level cleanup steps** (`execution.cleanup:` in the plan) for nodes that are not a graph pairing of a step in the plan run first, in declaration order. Each step's `runOn` (`always`, `success`, `failure`; empty means `always`) is checked against the outcome — `success` runs only when the plan passed, `failure` runs when it failed, errored, or was aborted.
2. **Graph-level cleanup pairings** (`cleanup: deleteX` on a node) run next from a stack: every main step whose node declares a cleanup partner pushes that partner when the step succeeds, and the stack unwinds last-in-first-out, so the most recently created resource is torn down first. Two steps on the same node push two entries, one per resource. A plan-level cleanup step that names a paired node does not run separately; its `runOn` decides whether that node's entries run. A cleanup step that succeeds is followed at once by its own node's cleanup pairing, if it has one (a [cleanup chain](graphs.md#cleanup)), so a release that takes two calls finishes before the next resource is torn down.

A plan composed from a workflow (a recipe, or a plan from `aat prompt`) lists each graph pairing in its `cleanup:` section. The order and the number of calls still come from the stack, so a recipe deletes the order before the cart, and sends nothing for a cart it never created.

Cleanup steps do not carry `values:`. Their inputs are filled by matching input names against the outputs of earlier steps — the step that registered the cleanup is consulted first, then the most recent step with an output of that name. A `deleteOrder` cleanup with an `orderId` input picks up `orderId` from the `createOrder` step that created it.

A graph pairing is skipped when it's no longer needed. Either a later main step on the cleanup node, or on a node the pairing's `releasedBy` lists, already released the resource with the same inputs, or the pairing's `when` condition is false for the outputs of the step that registered it. The console prints `skipped:` and the reason in place of a status, as in `cancelOrder  skipped: released by cancelOrder (for createOrder)`, and the archive records the skip in `cleanupSkipped`.

Cleanup results are recorded in the archive (and in the `cleanup` array of `--json` output) with the same detail as main steps, but a failed cleanup step never changes the run outcome — the outcome is determined by the main steps alone.

See [Plans: Cleanup Steps](plans.md#cleanup-steps) and [API Graphs: Cleanup](graphs.md#cleanup) for how each kind is declared.

## Retries

The `--retries` flag sets the maximum number of plan-level retries on failure. When a plan fails and retries remain, AAT re-executes the entire plan from scratch.

```
aat run plan flaky-test --retries 2
```

Each failed attempt is saved as `attempt-01.json`, `attempt-02.json`, etc. in the run directory, and the final attempt (whether it passed or not) as `archive.json` (see [Archives: Layout](archives.md#layout)). Setup errors (invalid plan, missing config, failed authentication) are not retried, and neither is a run that stopped at a checkpoint.

A two-second delay separates attempts to avoid hammering the API. Plan-level attempts do not read a response's `Retry-After`; a step's own `retry:` does (see [Plans: Retry](plans.md#retry)).

## Archives

Every run writes `archive.json` and `summary.json` into its own `run-…` directory under the archive directory (`--output`, else the manifest's `archives`, else `_output/runs`); a batch writes `batch.json` and one run directory per plan into a `batch-…` directory. Archives redact credential headers and every configured secret credential's value, bodies and URLs included, but not tokens the API issues at run time or data it returns, so review them before sharing.

See [Archives](archives.md) for the layout, what is and is not redacted, exporting and importing `.aar`/`.aab` files, naming runs, `aat run clean`, and `aat run rebuild-summaries`, and [Web UI](web-ui.md) for browsing them.

## OAS Validation

When the graph references an OpenAPI spec (a graph-level `oas:` or per-node `oas` references — see [API Graphs: OAS Integration](graphs.md#oas-integration)), AAT validates each step's request body, when it is JSON or form-encoded, and its response body against the spec as it runs, cleanup steps included. Bracketed form keys such as `items[0][sku]` and `tags[]` are read as nested objects and arrays. A request body of another type, or a schema the validator can't compile, is marked as not validated (`OAS: request not validated` on the step line, `skipped` in the archive) and never fails a step. Violations show up in three places: an `OAS: N warning(s)` marker on the step line and a total after the summary, an `issues` map (`{"oas": N}`) in the `--json` summary and archive summary, and the per-step detail in the archive.

A clean run records its result too. Whenever the graph references a spec, the archive's `metadata.oasValidation` holds the mode, and the run's `summary.json` holds `oas`: the `mode`, `validatedRequests` and `validatedResponses`, the request and response bodies checked, cleanup steps included, and `violations`. `aat run show` prints it under the run's header, and a batch's totals across its runs; see [Archives: A Batch](archives.md#a-batch).

The `--oas-validate` flag controls the mode:

| Mode | Behavior |
|------|----------|
| `auto` | Default. Validate when specs are present; report violations as warnings. A spec that fails to load is a warning on stderr, shown even with `--quiet` and `--json`, and the steps that use it are not validated |
| `strict` | Like `auto`, but a request or response that violates the spec fails the step (outcome `failed`, cleanup still runs). A cleanup step's exchange is checked too: a violation fails that cleanup step, and the rest of its cleanup chain still runs. Skipped validations and schema compilation warnings never fail a step; `expectFailure` steps are exempt. A spec that fails to load stops the run before the first request, with exit code `2`. Use a `schema` assertion instead when only specific steps should be strict (see [Plans: Assertions](plans.md#assertions)) |
| `off` | Do not load specs or validate |

Each spec loads once per command, and the validator is built only for the operations the graph's nodes name, so a spec with hundreds of operations adds little to startup.

The default comes from `settings.oasValidation` in the environment file; the flag overrides it for one run. Set `strict` there once, and every run validates strictly without the flag. See [Environments: Runtime Settings](environments.md#runtime-settings).

## Debugging Authentication

The `--verbose-auth` flag prints the authentication exchange to stderr, so you can see what AAT sends and receives when it obtains credentials. On the shop example, with its OAuth2 password grant for the shop API and an API key for the payments host:

```
$ aat run plan smoke --verbose-auth
aat: loading environment...
...
aat: reconstituting recipe "Quick Purchase"...
[auth] authenticating with type=oauth2
[auth] POST http://localhost:8765/oauth/token
[auth]   client_id = aat-shop
[auth]   client_secret = aat-sho...
[auth]   grant_type = password
[auth]   password = de...
[auth]   username = demo
[auth] response status: 200
[auth] response body: {"access_token":"shop-000...","expires_in":3600,"scope":"shop","token_type":"Bearer"}
[auth] token type=Bearer expires_in=3600 access_token=shop-000...
aat: authenticated via oauth2
[auth] authenticating with type=apikey
[auth] apikey resolved (12 chars)
aat: override: payment*
aat: executing plan (5 steps)...
```

What it shows:

- **Output goes to stderr**, so it does not interfere with `--json` on stdout.
- **OAuth2** shows the token request (URL and form fields) and the response. `client_id`, `username`, `grant_type`, and extra parameters appear in full. `password` and `client_secret` show at most their first eight characters, and never more than half, followed by `...`.
- **Tokens** in the response body (`access_token`, `refresh_token`, `id_token`) and on the `token` line show at most their first eight characters, and never more than half of the token.
- **API key and bearer** auth print only the credential's length.
- **Overlay and override auth** are covered: each credential AAT resolves is logged, including an overlay's auth and a routed host's own auth (the `apikey` lines above).

This flag is available on both `run plan` and `run batch`.

---

*Source: `cmd/aat/run_plan_cmd.go`, `cmd/aat/run_batch_cmd.go`, `cmd/aat/run_shared.go`, `cmd/aat/progress.go`, `graph/permute.go`, `config/auth.go`.*
