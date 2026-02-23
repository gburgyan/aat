# Web UI and Archives

Every AAT run produces a JSON archive that captures the full execution trace — requests, responses, timing, value resolutions, selections, and assertions. The web UI provides a visual interface for browsing and debugging these archives.

## Archives

### What's in an Archive

| Content | Description |
|---------|-------------|
| Metadata | Plan name, graph version, start/end timestamps, outcome, layers applied |
| Steps | Per-step records: HTTP method, URL, request headers/body, response headers/body, status code, duration |
| Value resolutions | How each input was resolved — source (literal, reference, pool, expression), value used, constraint pass/fail |
| Selection decisions | Array element selection: source size, filter applied, strategy used, selected index, LLM call details |
| Assertions | Per-step assertion results: type, path, expected value, actual value, pass/fail |
| Expect-failure records | Negative test outcomes: expected status codes, actual status, pass/fail |
| Extractions | Output values extracted from responses via gjson paths |
| Cleanup | Cleanup step results with the same detail as main steps |

### Directory Structure

**Single run:**

```
_output/runs/
  run-20260223-143052-a1b2c3d4/
    archive.json
```

**Batch run:**

```
_output/runs/
  batch-20260223-150000-e5f6g7h8/
    batch.json
    run-20260223-150001-i9j0k1l2/
      archive.json
    run-20260223-150003-m3n4o5p6/
      archive.json
```

**Retries:**

When a plan is retried (via `--retries`), failed attempts are preserved alongside the final result:

```
run-20260223-143052-a1b2c3d4/
  attempt-01.json
  attempt-02.json
  archive.json        # final attempt
```

### Header Redaction

Sensitive headers (`Authorization`, API keys, bearer tokens) are automatically redacted in archives. The header name is preserved but the value is replaced with `[REDACTED]`. Archives are safe to store as CI artifacts, share with teammates, or commit to repositories.

### Export and Import

The web UI provides an export button that downloads a run or batch as a single file. Use `aat web`'s import endpoint to restore archives into a different output directory.

## Starting the Web UI

```
aat web
```

Opens a web server on port 9119 serving the archive viewer.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--port` | int | `9119` | Listen port |
| `--open` | bool | `false` | Open browser automatically after starting |
| `--dev` | bool | `false` | Development mode (request logging) |
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |
| `--output` | path | `_output/runs` | Archive directory to serve |

The server runs until interrupted (Ctrl-C).

## Viewing Runs

```
aat web view [ref]
```

Opens a specific run in the browser. If a server is already running on the configured port, AAT opens the URL directly. If not, it starts an ephemeral server in the background.

Without a `ref` argument, opens the most recent run.

```
# Open a specific run
aat web view run-20260223-143052-a1b2c3d4

# Open the most recent run
aat web view

# Open a batch
aat web view batch-20260223-150000-e5f6g7h8
```

AAT auto-detects whether a reference is a single run or a batch by checking for a `batch.json` file.

## Viewing Traces

```
aat web viewtrace [id]
```

Opens the trace viewer for a specific planning trace, or lists all traces if no ID is given. Traces are produced by `aat prompt --trace`.

```
# Open a specific trace
aat web viewtrace trace-20260223-141000-f1g2h3i4

# Browse all traces
aat web viewtrace
```

See [LLM-Assisted Planning: Debugging with Traces](prompt.md#debugging-with-traces) for what traces contain.

## Web UI Features

### Run and Batch List

The landing page shows all runs and batches in reverse chronological order. Each entry shows the run ID, timestamp, outcome badge (passed/failed/error), duration, and step count. Batches show aggregate counts.

### Run Detail

Clicking a run opens the detail view:

- **Step timeline** — a Gantt chart showing step durations and execution order, making parallelism and bottlenecks visible at a glance
- **Metadata** — plan name, outcome, timing, layers applied, graph version
- **Attempt selector** — when a run has retries, switch between attempt archives to compare what changed

### Step Detail

Clicking a step in the timeline opens the step detail with tabbed panels:

| Tab | Contents |
|-----|----------|
| Request | HTTP method, URL, headers, request body (formatted JSON) |
| Response | Status code, response headers, response body (formatted JSON with expand/collapse) |
| Assertions | Per-assertion results: type, path, expected vs actual, pass/fail |
| Resolutions | Per-input value resolution chain: source, strategy, value, constraint satisfaction |
| Selections | Array element selection details: source array size, filter, strategy, selected index |
| Extractions | Output values extracted from the response |
| Error | Error classification, message, and details (when step failed) |

### Batch Detail

Batch detail shows an aggregate view with outcome counts and a per-plan list. Click any plan to drill down to its individual run detail.

### Trace Viewer

The trace viewer shows the LLM planning pipeline step by step:

- Workflow selection call — prompts sent, raw response, token counts, timing
- Skeleton composition — the composed plan scaffold
- Value fill call — prompts, response, tokens, timing
- Post-processing snapshots
- Validation results or errors

## Debugging Patterns

### Failed Steps

Start with the **Response** tab to see the status code and response body. Common patterns:

- `400` — bad request, check the **Request** tab for malformed input
- `401`/`403` — authentication issue, check your environment config
- `404` — resource not found, check the **Resolutions** tab for incorrect references
- `500` — server error, the response body usually contains diagnostic details

### Value Resolution Issues

Open the **Resolutions** tab to see how each input was resolved:

- **Source** — where the value came from (literal, from-reference, pool, expression, LLM)
- **Value** — the resolved value
- **Constraint** — whether constraints were satisfied, relaxed, or failed

If a value looks wrong, trace it back through its source. A `from` reference points to an earlier step's output — check that step's **Extractions** tab to see what was actually extracted.

### Selection Problems

The **Selections** tab shows:

- **Source array size** — how many elements were available
- **Filter** — what predicate was applied (and how many elements passed)
- **Strategy** — which selection strategy was used (first, min, match, etc.)
- **Selected index** — which element was picked

Common issues: an empty source array (the search returned no results), a filter that eliminates all elements, or a sort field that doesn't differentiate elements well.

### Assertion Failures

The **Assertions** tab shows each assertion with its expected and actual values. For predicate assertions, the full expression and evaluation result are shown. Compare expected vs actual to understand what diverged.

## API Routes

The web server exposes a REST API that you can use programmatically.

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/api/runs` | List all runs |
| `GET` | `/api/runs/latest` | Get the latest run |
| `GET` | `/api/runs/{id}` | Get run detail |
| `PUT` | `/api/runs/{id}/name` | Rename a run |
| `DELETE` | `/api/runs/{id}/name` | Remove custom name |
| `GET` | `/api/runs/{id}/steps/{stepId}` | Get step detail |
| `GET` | `/api/runs/{id}/attempts/{attempt}` | Get a retry attempt |
| `GET` | `/api/runs/{id}/attempts/{attempt}/steps/{stepId}` | Get step from a specific attempt |
| `GET` | `/api/runs/{id}/export` | Export run as JSON |
| `GET` | `/api/batches` | List all batches |
| `GET` | `/api/batches/{id}` | Get batch detail |
| `PUT` | `/api/batches/{id}/name` | Rename a batch |
| `DELETE` | `/api/batches/{id}/name` | Remove custom name |
| `GET` | `/api/batches/{id}/export` | Export batch as JSON |
| `POST` | `/api/import` | Import an archive file |
| `GET` | `/api/traces` | List plan traces |
| `GET` | `/api/traces/{id}` | Get trace detail |

## Development Mode

For frontend development, run the Vite dev server alongside AAT's Go server:

```bash
# Terminal 1: Vite dev server with hot reload
cd server/web && npm run dev

# Terminal 2: Go server proxying to Vite
aat web --dev
```

In `--dev` mode, the Go server proxies frontend requests to Vite on port 5173 and enables request logging. The API routes (`/api/*`) are served directly by the Go server. This gives you hot reload for frontend changes while using the real API backend.

For production, `make build` compiles the Svelte frontend and embeds it into the Go binary via `//go:embed`.

---

*Source: `cmd/aat/web_cmd.go`, `server/server.go`, `server/types.go`, `archive/types.go`.*
