# Archives

Every plan execution writes a JSON archive: `aat run plan`, each plan in `aat run batch`, `aat prompt` when it executes the plan, and the MCP server's execution tool. An archive records each request and response, how every input got its value, every assertion result, cleanup, and the outcome, so you can inspect a run long after it finished. The [web UI](web-ui.md) is the viewer; the files are plain JSON for scripts too.

## Where Archives Go

Archives are written to the directory given by `--output`, else the manifest's `archives:` entry, else `_output/runs` in the working directory. `aat web`, `aat web view`, `aat run show`, `aat import`, `aat run clean`, and `aat run rebuild-summaries` look for them the same way.

## Layout

A single run gets its own directory:

```
_output/runs/
  run-20260910-225958-d819f460/
    archive.json
    attempt-01.json
    summary.json
```

A batch gets a directory holding `batch.json` and one run directory per executed plan (duplicate permutations skipped by [dedup](batch-layers.md#duplicate-detection) appear in `batch.json` but get no directory):

```
_output/runs/
  batch-20260910-225919-0754c0ea/
    batch.json
    run-20260910-225919-5a04ca45/
      archive.json
      summary.json
    run-20260910-225921-80a38128/
      archive.json
      summary.json
    ...
```

Directory names are `run-` or `batch-`, the local date and time (`YYYYMMDD-HHMMSS`), and eight random hex characters.

| File | Contents |
|------|----------|
| `archive.json` | The full record of the run. With `--retries`, the final attempt |
| `attempt-NN.json` | An earlier attempt that `--retries` retried (`attempt-01.json`, `attempt-02.json`, …), in the same format as `archive.json`. Present only when the run was retried |
| `summary.json` | A small summary the web UI reads for its lists: run ID, timestamp, outcome, step counts, duration, plan name, attempt numbers, layers, and `issues` counts. Derived from `archive.json`; see [Rebuilding Summaries](#rebuilding-summaries-aat-run-rebuild-summaries) |
| `batch.json` | Batch metadata (batch ID, source directory, layers, layer groups), one entry per run including skipped duplicates (plan name, run ID, outcome, counts, layers, permutation, `duplicateOf`), and the aggregate result |

## What an Archive Contains

`archive.json` has these top-level keys:

| Key | Contents |
|-----|----------|
| `metadata` | Run ID, timestamp, environment name, graph version, AAT version, the plan as loaded (`plan`), the plan after graph defaults and layers were merged in (`instantiatedPlan`), the layers applied, and `attempt`/`totalAttempts` for retried runs |
| `steps` | One record per main and verification step (see below) |
| `cleanup` | Cleanup step records in the same format; absent when no cleanup ran |
| `cleanupSkipped` | Registered cleanups that did not run because they were no longer needed: `node`, `cleanupFor`, `reason` (`released` or `when`), and `releasedBy` or `when`. See [API Graphs: Cleanup](graphs.md#cleanup). Absent when none were skipped |
| `result` | `outcome` (`passed`, `failed`, `error`, `aborted`, or `stopped`), `error`, and `durationMs`, the run's wall-clock time (archives written before it was recorded omit it, and the web UI then sums the step durations) |

Each step record holds:

| Field | Contents |
|-------|----------|
| `stepId`, `node`, `startTime`, `durationMs` | Which step ran, and when: a retried step's start and duration cover every attempt and the waits between them. Archives written before 0.1.0 name the duration `duration_ms`; AAT reads both |
| `inputs` | The resolved input values |
| `request` | Method, full URL, headers, and body. When an override routed the step elsewhere, `originalUrl` holds the URL it would have used |
| `response` | Status, headers, and body |
| `outputs`, `displayOutputs` | Extracted outputs (after any [Lua transform](lua-transforms.md), whose script is in `transformScript`) and the plan's display outputs |
| `resolutions` | How each input got its value: `source` (such as `plan_default`, `graph_default`, `layer`, `expression`, `plan_from`, `select_edge`, or `fallback_pool`), the `layer` that set the value, the raw and final value, the step and output it came from, and whether a `constraint` passed |
| `selections` | For inputs picked from an array: source step and field, array size, filter and how many elements passed it, strategy, and the selected index |
| `validation` | Each assertion's type, pass or fail, and message |
| `expectFailure` | For negative steps: expected statuses, actual status, pass or fail |
| `oasValidation` | Request and response checks against the OpenAPI spec |
| `errorClassification`, `error`, `retryCount`, `retriedOn` | Error category and detail for a failed step, and the category of each step-level retry |
| `cleanupFor` | On a cleanup step: the step whose resource it releases, or the cleanup step before it in a [cleanup chain](graphs.md#cleanup). Cleanup step IDs are unique within a run (`deleteCart`, then `deleteCart_2`) |
| `whenError` | On a cleanup step: why its pairing's `when` condition couldn't be evaluated, such as an output the step didn't return. The cleanup ran anyway |

## What Is Redacted, and What Is Not

Archives redact the credentials AAT knows about, not every piece of sensitive data, so an archive, an exported `.aar`/`.aab`, or a CI artifact is not automatically safe to commit or share. Before you attach one to a ticket, check what it holds.

Redacted:

- **Credential headers, by name.** In requests and responses, the values of `Authorization`, `Proxy-Authorization`, `X-API-Key`, `X-Auth-Token`, `Cookie`, and `Set-Cookie` (any capitalization) are replaced with `[REDACTED]`. The header name stays.
- **Known secrets, everywhere.** AAT collects the resolved secret credentials of every `auth` block that can apply to the run — the environment's, its host overrides' (such as the shop's payments key), the plan's, and those of overlays and their overrides — plus the LLM API key and, in `aat run plan` and `aat run batch`, the access token the run authenticated with. A secret credential is any credential except the oauth2 `username` and `clientId`, which identify an account rather than prove it. Each secret is redacted from every string in the archive: URLs and query parameters, headers under any name, request and response bodies, inputs and resolved values, outputs and display outputs, error and assertion messages, and the archived plan. In a JSON body only string values change; keys, numbers, and layout are kept. `batch.json` is redacted the same way.
- **How a secret matches.** A string that equals a secret is always replaced. A secret of at least eight characters is also replaced inside longer strings, including its URL-escaped forms. A shorter secret is not, because it is an ordinary word in data too: with the shop sandbox's `demo` password, `"password": "demo"` is redacted but `demo@example.com` is kept.
- **Plan credentials.** In `metadata.plan` and `metadata.instantiatedPlan`, the value of every `source: literal` credential in the plan's `auth` block and the plan's credential headers are replaced with `[REDACTED]`, whatever their length. `source: env` references keep their variable names.

Stored as-is:

- **Tokens AAT does not know as secrets** anywhere other than a credential header, such as a session token a login step returns, or the access token of a run started by `aat prompt` or the MCP server's `execute_plan`, echoed in a body.
- **Other sensitive data** an API returns or a plan sends, such as personal data, and a secret written as a literal step value, which is not a credential AAT knows about.
- **A short secret inside a longer string**, as described above.

Terminal output and the `--json` summary are not redacted: they are the live view of the run, like the requests themselves.

`summary.json` holds no request data. The live-state dump written by `--dump-state` redacts nothing at all; see [Checkpoints: Security](checkpoints.md#security).

## Viewing Archives

`aat web` serves the archive directory; `aat web view` opens one run or batch in the browser:

```
aat web view                                    # the run list
aat web view latest                             # the newest run or batch
aat web view run-20260910-225958-d819f460       # a run
aat web view batch-20260910-225919-0754c0ea     # a batch
```

A reference is a directory name in the archive directory, saved names included; AAT treats it as a batch when the directory holds `batch.json`. `latest` opens whichever run or batch in the archive directory has the newest timestamp. If a server is already listening on the port (`--port`, default `9119`), `aat web view` opens the page there; otherwise it starts a temporary server that runs until you press Ctrl+C.

### Viewing a File

The reference can also be a file: an exported `.aar` or `.aab`, or a bare `archive.json` or `batch.json`, for example from a downloaded CI artifact. AAT loads it into memory and serves it from a temporary server, with no project, manifest, or archive directory needed, and writes nothing to disk:

```
aat web view exported-run.aar
aat web view nightly.aab
aat web view downloaded-artifacts/run-20260910-225958-d819f460/archive.json
```

Next to an `archive.json`, sibling `attempt-NN.json` files are loaded so the attempt selector works; next to a `batch.json`, the member run directories are loaded. A file is read-only: save, rename, export, and import are unavailable.

Known issue: `aat web view <file>` always starts its own server, so it fails with `bind: address already in use` while `aat web` (or another `aat web view`) holds the port. Pass a different `--port`.

## Inspecting a Run from the CLI

`aat run show` prints what an archive recorded, for a terminal, a script, or an AI coding assistant that cannot open the web UI. It only reads: it writes nothing, not even `summary.json`.

```
aat run show latest                                     # the steps
aat run show latest --step checkout                     # one step
aat run show latest --step checkout --response --shape  # the structure of its response
aat run show latest --step checkout --response --path orderId
```

The run is one of:

- `latest`: the newest run, runs inside batches included, even those of a batch that is still running
- a run ID, such as `run-20260910-225958-d819f460`, looked up at the top of the archive directory and then inside each batch
- a batch ID and a run ID joined by a slash, such as `batch-20260910-225919-0754c0ea/run-20260910-225919-5a04ca45`
- a path to a run directory, an `archive.json` or `attempt-NN.json`, or an exported `.aar` file
- a batch ID, such as `batch-20260910-225919-0754c0ea`, or a path to a batch directory or its `batch.json`, which shows the batch instead of a run (see [A Batch](#a-batch))

IDs are looked up in the archive directory, as [Where Archives Go](#where-archives-go) describes.

Without `--step`, it lists the steps: ID, node, HTTP status, pass or fail, duration, and output names, then the verification steps and the cleanup steps, each with the step it releases. After the shop's `smoke` recipe:

```
run-20260912-224235-0842d2b8  PASSED  2ms
plan: Buy one in-stock product and pay by card
archive: /path/to/shop/_output/runs/run-20260912-224235-0842d2b8/archive.json

  #  STEP           NODE           STATUS  RESULT     TIME  OUTPUTS
  1  listProducts   listProducts      200  pass        0ms  currency, products
  2  createCart     createCart        201  pass        0ms  cartId, status
  3  addItem        addItem           201  pass        0ms  cartId, lineCount, subtotal
  4  checkout       checkoutCart      201  pass        0ms  currency, discount, orderId, receiptNumber, shipping, status, subtotal, tax, ta…
  5  paymentCharge  paymentCharge     201  pass        0ms  amountDisplay, orderStatus, paymentId, status

cleanup:
  #  STEP         NODE         STATUS  RESULT     TIME  FOR
  1  deleteOrder  deleteOrder     204  pass        0ms  checkout
  2  deleteCart   deleteCart      204  pass        0ms  createCart
```

`--step` takes a step ID, or a node name when that node ran only once. On its own, it prints:
- the step's method and URL, status, result, duration and retries, and error
- the inputs, each with where its value came from
- the outputs, with arrays and objects by their size
- assertion counts, with each failure
- warnings, such as a [selection tie](value-flow.md#selection-strategies)
- the sizes of the request and response bodies

A step that failed before its request was sent still shows the inputs resolved up to the failure, and the one that failed.

```
step checkout (node checkoutCart)
POST http://localhost:8765/us/v1/carts/cart_0001/checkout
status 201  pass  0ms
inputs:
  cartId         "cart_0001"  plan_from createCart.cartId
  customerEmail  -            optional_skip
  deliveryDate   -            optional_skip
  notes          -            optional_skip
  postalCode     "78701"      expression {{env.postalCode}}
  shippingTier   "standard"   graph_default
outputs:
  currency       "USD"
  orderId        "ord_0001"
  status         "created"
  total          10340
  ...
assertions: 1 passed, 0 failed
request body: 48 bytes
response body: 518 bytes
```

These flags print one part of the step instead:

| Flag | Prints |
|------|--------|
| `--request`, `--response` | The request or response body, as indented JSON |
| `--inputs`, `--outputs` | The resolved inputs or the extracted outputs, as JSON |
| `--resolutions` | How each input got its value, as JSON. Each entry is the input's resolution record (`source`, `fromStep` and `fromOutput`, `expression`, the pool pick, a `constraint`) with the `selection` that picked the value, and an `error` for an input that couldn't be resolved |
| `--path PATH` | Only what a [gjson path](https://github.com/tidwall/gjson/blob/master/SYNTAX.md) selects: `lines.0.sku`, or `lines.#.sku` for every element. The `$.lines[0].sku` form works too. Without a part flag, it reads the response body |
| `--shape` | The part's structure instead of its values. Without a part flag, the response body's |
| `--max-bytes N` | Cut a printed part after `N` bytes, 65536 by default, with a note on stderr; `0` prints everything |
| `--json` | The step list or the step as JSON with `snake_case` keys, or the shape as a JSON array. A step's JSON includes `resolutions`, `warnings`, and `validation`, its assertion results as `archive.json` names them (`archive.json` itself uses `camelCase` keys) |
| `--compact` | JSON on one line, for a script or `jq`: a part, with or without `--path`, and with `--json` the step list, the step, or the shape. With `--shape`, or without a part, it needs `--json` |

Without `--step`, a part flag or `--path` prints that part of every step that has it, cleanup steps included, one line each. One command then answers a question about the whole run:

```
$ aat run show latest --response --path error.code
STEP                  NODE                  VALUE
captureBeforeConfirm  capturePaymentIntent  "payment_intent_unexpected_state"
refundTwice           createRefund          "charge_already_refunded"
```

With `--json` or `--compact`, it prints a JSON array of `step_id`, `node`, and `value`. `--shape` still needs `--step`.

`--shape` is the way to learn a large response. It prints one line per path: the path's type, an array's item count, how many objects hold a key when not all of them do, and a sample value. The elements of an array are merged, so a key that only some elements hold, or a value that is sometimes `null` (`string|null`), shows up. Each path works as an extract rule in a [template](templates.md) and as `--path`:

```
$ aat run show latest --step checkout --response --shape
orderId            string  "ord_0001"
receiptNumber      string  "RCPT-US-0001"
status             string  "created"
...
total              number  10340
totalDisplay       string  "$103.40"
lines              array   1 item
lines.#            object
lines.#.sku        string  "SKU-1001"
lines.#.quantity   number  1
lines.#.lineTotal  number  8999
createdAt          string  "2026-09-13T03:42:35Z"
```

Archives several hundred megabytes in size still list their steps in under a second, and so does the shape of a response tens of megabytes long.

A path that matches nothing, an unknown step, or an unknown run exits with code `2`, and the message says what does exist: the top-level keys, the step IDs, or the forms a run reference takes. Archives are redacted when they are written, so `aat run show` prints only what the archive holds; see [What Is Redacted, and What Is Not](#what-is-redacted-and-what-is-not). The MCP server's `get_sample_response` takes the same `path` and `shape`; see [MCP Server](mcp-server.md).

### A Batch

A batch ID, or a path to a batch directory or its `batch.json`, shows the batch: its totals from `batch.json`, one row per permutation, and what cleanup did across its runs.

```
$ aat run show batch-20260913-160341-4a6502e8
batch-20260913-160341-4a6502e8  PASSED  4m30s
batch: _output/runs/batch-20260913-160341-4a6502e8/batch.json
runs: 220, 94 passed, 0 failed, 0 errors, 126 skipped as duplicates

  #  RUN                           PLAN         LAYERS                  RESULT  STEPS     TIME
  1  run-20260913-160341-5a04ca45  full-refund  card-visa,currency-usd  pass      9/9     1.2s
  2  -                             full-refund  card-visa,currency-eur  skip        -        -  duplicate of full-refund [card-visa,currency-usd]
  ...

cleanup:
  NODE                    RAN  FAILED  RELEASED  WHEN
  cancelPaymentIntent       8       0         0    24
  deleteCustomer           93       0         1     0
  reconcilePaymentIntent   32       0        61     0
```

- **RAN** and **FAILED** count cleanup steps across the runs.
- **RELEASED** and **WHEN** count the pairings skipped: because a step released the resource, or because the pairing's `when` was false.
- **Cleanup failures:** a `cleanup failures:` list names each failed cleanup step by run, with its status or error.
- **`--json`** prints the same as a document with `snake_case` keys.
- **Steps:** a batch has no steps of its own, so `--step` and the part flags need a run: `batch-ID/run-ID`.

## Exporting and Importing

A run or batch can travel as a single zip file.

### Export

Export is a web UI feature; there is no CLI command for it. In the run list, each row has a download arrow (**Export run** or **Export batch**), and run and batch detail pages have an **Export** button.

- A run exports as `.aar`, a zip of its directory's JSON files: `archive.json`, `summary.json`, and any `attempt-NN.json`.
- A batch exports as `.aab`: `batch.json` plus every member run's directory.

The file is named after the run or batch ID, or its saved name (`run-20260910-225958-d819f460.aar`, `checkout-baseline.aar`). The content is the archive as written, so the redaction limits above apply to exports too. The same downloads are available at `GET /api/runs/{id}/export` and `GET /api/batches/{id}/export`.

### Import

- **Web UI**: the **Import** button on the run list accepts `.aar` and `.aab` files, adds them to the served archive directory, and opens the imported run or batch.
- **CLI**: `aat import FILE` extracts into the archive directory.
- **API**: `POST /api/import` with a multipart form whose `file` field holds the archive (100 MB limit). The response is `{"ref": …, "name": …, "type": "run"|"batch"}`; a name that already exists returns `409`.

```
$ aat import "nightly run #3.aar"
Imported run "nightly-run-3" → /path/to/shop/_output/runs/nightly-run-3
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name` | string | derived from the file name | Directory name for the imported archive |
| `--output` | path | manifest `archives`, else `_output/runs` | Archive directory to import into |
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |

Without `--name`, the web UI, the API, and the CLI derive the directory name from the file name: the `.aar`/`.aab` extension is dropped, every character other than letters, digits, `.`, `_`, and `-` becomes `-`, repeated dashes collapse, and leading and trailing dashes go. A result that starts with `run-` or `batch-` gets a `!` prefix, so `run-20260910-225958-d819f460.aar` imports as `!run-20260910-225958-d819f460`. An archive imported without `--name` is therefore always a [named run](#naming-and-saving-runs) that `aat run clean` never deletes.

`--name` must be a single directory name, without path separators, and a name that starts with `run-` or `batch-` gets the same `!` prefix.

An import never overwrites or merges. If the target directory exists, it fails (exit code `2` from the CLI):

```
aat: import: directory "nightly-run-3" already exists
```

The file must be a zip with `archive.json` (a run) or `batch.json` (a batch) at its root. Symlinks, absolute paths, and `..` entries are rejected, as are zips with more than 10,000 entries or more than 500 MB uncompressed.

## Naming and Saving Runs

A directory whose name starts with `run-` or `batch-` is auto-generated and a candidate for `aat run clean`. Any other name, or a name starting with `!`, marks a *named* (saved) run or batch: it is never deleted by `aat run clean` and shows under the **Saved** filter in the web UI's run list.

On a run or batch detail page in the web UI:

- **Save** opens a name field, prefilled with the plan name (for a batch, its source). **Save** in that form renames the directory to the name you typed; **Save as-is** keeps the ID and adds the `!` prefix (`!run-20260910-225829-a4c05ee5`). A typed name that starts with `run-` or `batch-` also gets the `!` prefix.
- A saved run shows its name, a pencil (**Edit name**) to rename it again, and **Unsave**, which restores the original ID from the archive's metadata.

A name cannot contain `/` or `\` or be `.` or `..`, and renaming fails if a directory with the new name exists. Naming works only when serving a directory, not a [file](#viewing-a-file).

The same operations are `PUT /api/runs/{id}/name` with a body such as `{"name": "run-keep"}` (an empty name saves as-is) and `DELETE /api/runs/{id}/name` to unsave; batches use `/api/batches/{id}/name`. The response carries the new directory name to use in URLs:

```json
{"ref":"!run-keep","name":"run-keep"}
```

Because only the directory name matters, renaming a directory by hand works too. Quote a `!` name in the shell, for example `'!run-20260910-225829-a4c05ee5'`.

## Pruning Old Runs (`aat run clean`)

The archive directory grows by one directory per run or batch. `aat run clean` deletes auto-generated run and batch directories older than a cutoff:

```
aat run clean                # delete runs older than 7 days
aat run clean --days 30      # keep a month
aat run clean --dry-run      # list what would be deleted, remove nothing
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--days` | int | `7` | Delete runs older than this many days |
| `--dry-run` | bool | `false` | Preview without deleting |
| `--output` | path | manifest `archives`, else `_output/runs` | Archive directory to clean |
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |

Only top-level directories whose names start with `run-` or `batch-` are candidates, and their age comes from the timestamp in the name, not from file times. Deleting a batch directory deletes its member runs with it. Named directories are skipped and counted:

```
$ aat run clean --dry-run --days 0
aat: dry run — previewing cleanup in _output/runs (older than 0 days)...
  would delete: batch-20260910-225858-2e1dcfc5 (age: < 1 day)
  would delete: batch-20260910-225919-0754c0ea (age: < 1 day)
  would delete: run-20260910-225742-06f1a24d (age: < 1 day)
  ...
  would delete: run-20260910-230834-1a340655 (age: < 1 day)
  warning: run-garbage: cannot parse timestamp: unrecognized format: run-garbage
aat: would delete 16 run(s), skipped 2 saved, 1 errors
```

Known issue: a `run-` or `batch-` directory whose name carries no parseable timestamp, like `run-garbage` above, is reported as an error and left alone, but the command still exits `0`. In scripts, check the summary line for `errors`.

## Rebuilding Summaries (`aat run rebuild-summaries`)

The web UI builds its lists from each run's `summary.json`. After an AAT upgrade adds summary fields, older summaries lack them. Rebuild every summary from the full archives without re-running anything:

```
$ aat run rebuild-summaries
aat: rebuilding summaries in /path/to/shop/_output/runs...
aat: rebuilt 3 summaries
```

It rewrites `summary.json` for every top-level run directory with an `archive.json` and for every run inside a batch directory (auto-generated or named), using `--output` or the manifest's `archives` like the other commands. The next web UI listing picks the new summaries up. A run with no `summary.json` at all does not need this: the web UI computes and writes the summary the first time it lists the run.

Known issue: `batch.json` is not recomputed, so batch-level counts stay as they were written.

---

*Source: `archive/types.go`, `archive/writer.go`, `archive/redact.go`, `archive/transfer.go`, `archive/naming.go`, `archive/clean.go`, `archive/rebuild.go`, `engine/archive.go`, `cmd/aat/run_shared.go`, `cmd/aat/import_cmd.go`, `cmd/aat/run_clean_cmd.go`, `cmd/aat/run_rebuild_cmd.go`, `cmd/aat/web_cmd.go`, `server/handlers.go`, `server/service.go`, `server/web/src/routes/*.svelte`.*
