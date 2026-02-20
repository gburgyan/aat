# AAT Web Server — Implementation Plan (Milestone 1: Archive Viewer)

## Context

AAT is a power-user CLI tool. The web interface makes archive browsing accessible to a wider audience without replacing the CLI. Vision: `vision/web-server.md`. This plan covers foundational decisions and Milestone 1 broken into manageable sessions.

---

## Decisions

### Frontend: Svelte

Svelte with Vite as the build tool. TypeScript enabled. Single-file `.svelte` components with scoped CSS. Tiny compiled output (~20-30KB for M1). Requires Node.js + npm in the build pipeline.

**Project location:** `server/web/` contains the Svelte project. Build output to `server/web/dist/`. `go:embed` on `server/web/dist`.

**Alternatives considered:**
- **Preact + HTM (no build step):** Zero tooling, ~4KB, React-like API via browser ES modules. Rejected: no TypeScript, less polished DX than Svelte.
- **HTMX + Go templates:** Server-rendered, minimal JS. Rejected: breaks API-first principle (vision says web UI is a JSON API client), couples server to presentation, painful for complex step detail view (8+ sub-sections), struggles with WebSocket live progress (M3).
- **React:** Largest ecosystem. Rejected: 40-130KB gzipped, requires Node.js + bundler, overkill for a data dashboard.

### Router: chi

`github.com/go-chi/chi/v5`. Provides middleware chaining, route grouping, and idiomatic patterns. Small dependency. Chosen over stdlib `net/http` (Go 1.22+) for ergonomics — route grouping and middleware chaining are cleaner.

### Build Pipeline

```makefile
frontend:
	cd server/web && npm install && npm run build

build: frontend
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)
```

**Dev workflow:** `cd server/web && npm run dev` runs Vite dev server with HMR on port 5173. Go server in `--dev` mode proxies non-API requests to Vite. This gives hot reload for frontend changes without rebuilding Go.

### Data Service Layer

In `server/` package, file-separated. Service types don't import `net/http`. Handlers are thin HTTP wrappers that call service methods and serialize JSON. View model types (not raw `archive.Archive`) returned from service methods.

---

## File Layout

```
server/
  doc.go              # existing stub
  types.go            # View model types (RunListEntry, RunDetail, StepDetail, etc.)
  service.go          # ArchiveService — reads archives, returns view models
  service_test.go     # Unit tests with temp dir fixtures
  handlers.go         # HTTP handlers (parse request → call service → JSON response)
  handlers_test.go    # httptest-based handler tests
  server.go           # Server struct, chi router setup, lifecycle
  server_test.go      # Integration tests
  embed.go            # go:embed web/dist + static file serving
  web/                # Svelte project
    package.json
    vite.config.ts
    svelte.config.js
    tsconfig.json
    src/
      App.svelte      # Root component + router
      lib/
        api.ts        # API client (fetch wrapper)
        types.ts      # TypeScript types matching server view models
        format.ts     # Duration formatting, time ago, etc.
      routes/
        RunList.svelte
        RunDetail.svelte
        StepDetail.svelte
      components/
        OutcomeBadge.svelte
        StepTimeline.svelte
        JsonViewer.svelte
        AssertionsTable.svelte
        ResolutionsTable.svelte
        SelectionsTable.svelte
        LlmCallPanel.svelte
        ErrorPanel.svelte
        Nav.svelte
      styles/
        global.css
    dist/             # build output (gitignored)

cmd/aat/
  web_cmd.go          # `aat web` command
  web_cmd_test.go
```

---

## Session Breakdown (8 sessions)

### Session 1: Data Service — Types + Archive Scanning

**Delivers:** View model types and the ability to list/scan archive directories.

**Files created:**
- `server/types.go` — `RunListEntry`, `RunDetail`, `StepSummary`, `StepDetail` and sub-types
- `server/service.go` — `ArchiveService` struct with `archiveDir` field, `ListRuns(limit int) ([]RunListEntry, error)`, `LatestRunID() (string, error)`
- `server/service_test.go` — Tests with temp dirs containing sample archive JSON

**Key design:**
- `RunListEntry`: RunID, Timestamp, Outcome, StepCount, PassedCount, FailedCount, DurationMs, PlanName
- Scan dir for `run-*` entries, read each `archive.json`, build summary
- Sort by directory name descending (newest first — run IDs contain timestamps)
- Reference: `mcp/tools_archive.go` lines ~82-126 for the existing scanning pattern
- Error handling: skip unreadable archives with warning, don't fail the whole list

**Test:** Create temp dir with 3 sample archives → `ListRuns(10)` returns 3 entries sorted newest-first.

---

### Session 2: Data Service — Run + Step Detail

**Delivers:** Ability to load a full run and individual step details.

**Files modified:**
- `server/service.go` — Add `GetRun(id string) (*RunDetail, error)`, `GetStep(runID, stepID string) (*StepDetail, error)`
- `server/types.go` — Flesh out `StepDetail` sub-types: `RequestDetail`, `ResponseDetail`, `AssertionDetail`, `ResolutionDetail`, `SelectionDetail`, `LLMCallDetail`, `ErrorDetail`, `ExpectFailureDetail`
- `server/service_test.go` — Tests for run loading and step lookup

**Key design:**
- `GetRun` reads `archive.json`, transforms to `RunDetail` with `StepSummary` entries (not full detail)
- `GetStep` reads archive, finds step by ID, returns full `StepDetail` with all sub-types
- View model types mirror archive types but:
  - JSON tags use camelCase
  - Omit empty fields (`omitempty`)
  - Pre-format where useful (e.g., `DurationFormatted string` alongside `DurationMs`)
  - Headers as `[]HeaderEntry{Name, Value}` instead of `map[string]string` (preserves order in UI)
- Error: `ErrRunNotFound`, `ErrStepNotFound` sentinel errors

**Test:** Load a sample archive → `GetRun` returns correct summary. `GetStep("searchFlights")` returns full detail with request/response/assertions.

---

### Session 3: HTTP Handlers + Server + chi Routing

**Delivers:** Working JSON API testable with `curl`. No frontend yet.

**Files created:**
- `server/handlers.go` — Handler methods: `handleListRuns`, `handleGetRun`, `handleGetStep`, `handleLatestRun`
- `server/server.go` — `Server` struct, `NewServer(opts)`, `ListenAndServe()`, chi router setup
- `server/handlers_test.go` — `httptest` handler tests
- `server/server_test.go` — Integration test (start server, make HTTP requests)

**Key design:**
- chi router with route groups:
  ```go
  r.Route("/api", func(r chi.Router) {
      r.Get("/runs", h.handleListRuns)
      r.Get("/runs/latest", h.handleLatestRun)
      r.Get("/runs/{id}", h.handleGetRun)
      r.Get("/runs/{id}/steps/{stepId}", h.handleGetStep)
  })
  ```
- `handleLatestRun`: 302 redirect to `/api/runs/{latestId}`
- JSON error responses: `{"error": "run not found", "code": "not_found"}`
- `?limit=N` query param on `handleListRuns` (default 50)
- Middleware: request logging (dev mode only), recovery (panic → 500)
- `ServerOptions` struct: Port, ArchiveDir, DevMode

**Test:** `httptest.NewServer` → GET `/api/runs` returns JSON array. GET `/api/runs/nonexistent` returns 404 JSON error.

---

### Session 4: CLI Command + Server Lifecycle

**Delivers:** `aat web` starts server. `aat web view latest` opens browser.

**Files created:**
- `cmd/aat/web_cmd.go` — Cobra `web` command + `view` subcommand

**Key design:**
- Flags: `--port` (default 9119), `--no-open`, `--dev`, `--manifest`, `--output` (archive dir)
- Project context resolution: reuse pattern from `cmd/aat/mcp_cmd.go`
- Browser open: `os/exec` with `open` (macOS), `xdg-open` (Linux), `cmd /c start` (Windows)
- `aat web view <ref>`: resolve `latest` / run ID / file path → URL, start server if needed, open browser
- Graceful shutdown: `signal.NotifyContext` with SIGINT/SIGTERM → `server.Shutdown(ctx)`
- Print `Listening on http://localhost:9119` to stderr

**Test:** Flag parsing tests. URL construction tests. No need to test browser open (platform-specific).

---

### Session 5: Svelte Project Scaffold + Build Pipeline

**Delivers:** Svelte project compiles, `go:embed` works, browser shows "hello world" page served from embedded assets.

**Files created:**
- `server/web/package.json` — Svelte + Vite dependencies
- `server/web/vite.config.ts` — Build config (output to `dist/`)
- `server/web/svelte.config.js`
- `server/web/tsconfig.json`
- `server/web/src/App.svelte` — Minimal root component
- `server/web/src/app.css` — Global styles (reset, system fonts, CSS custom properties for colors)
- `server/web/src/main.ts` — Entry point
- `server/web/index.html` — SPA shell
- `server/embed.go` — `//go:embed web/dist` + file server handler
- Update `server/server.go` — Wire embedded file serving for non-API routes, dev proxy to Vite
- Update `Makefile` — `frontend` target, update `build` to depend on it
- `.gitignore` — Add `server/web/dist/`, `server/web/node_modules/`

**Key design:**
- SPA fallback: non-`/api/` GET requests serve `index.html` (for client-side routing)
- `--dev` mode: reverse proxy to `http://localhost:5173` (Vite dev server) instead of serving embedded
- Vite config: `base: '/'`, output to `dist/`
- Commit `server/web/dist/` placeholder (empty `index.html`) so `go:embed` doesn't fail on fresh clone before `npm run build`

**Test:** `make build` succeeds. Start server, navigate to `localhost:9119/` → see rendered page.

---

### Session 6: Run List View

**Delivers:** Browser shows table of runs with real data from API.

**Files created/modified:**
- `server/web/src/lib/api.ts` — `fetchRuns()`, `fetchRun(id)`, `fetchStep(runId, stepId)` — typed fetch wrappers
- `server/web/src/lib/types.ts` — TypeScript interfaces matching Go view models
- `server/web/src/lib/format.ts` — `formatDuration(ms)`, `timeAgo(date)`, `formatTimestamp(date)`
- `server/web/src/routes/RunList.svelte` — Fetch `/api/runs`, render sortable table
- `server/web/src/components/OutcomeBadge.svelte` — Color-coded pass/fail/error badge
- `server/web/src/components/Nav.svelte` — Header with breadcrumb
- Update `server/web/src/App.svelte` — Client-side routing (hash or path-based via lightweight Svelte router)
- Update `server/web/src/app.css` — Table styles, badge colors, layout

**Key design:**
- Table columns: Run ID (truncated, clickable), Time (relative "2 min ago"), Outcome (badge), Steps (passed/total), Duration, Plan
- Click row → navigate to `/#/runs/{id}`
- Loading spinner while fetching
- Empty state: "No runs found" message
- Auto-refresh consideration: not for M1, but design API client to support it later

**Test:** Navigate to root → see table. Click a row → URL changes to run detail route.

---

### Session 7: Run Detail + Step Timeline

**Delivers:** Click a run → see header with outcome + step timeline.

**Files created/modified:**
- `server/web/src/routes/RunDetail.svelte` — Fetch run, render header + timeline
- `server/web/src/components/StepTimeline.svelte` — Vertical list of steps
- `server/web/src/components/OutcomeBadge.svelte` — Reuse from session 6
- Update `server/web/src/lib/format.ts` — HTTP status formatting, step duration formatting

**Key design:**
- Header: large outcome badge, plan name/goal, total duration, timestamp, environment
- Step timeline: vertical list, each step shows:
  - Step name + node name
  - HTTP status code (color-coded: 2xx green, 4xx orange, 5xx red)
  - Duration
  - Pass/fail indicator (assertion summary)
  - Display outputs (e.g., PNR locator) inline
- Cleanup steps: separate section below main steps, visually distinct
- Click step → expand inline or navigate to step detail
- Breadcrumb: Runs → {run ID}
- Loading + error states

**Test:** Click run in list → see timeline. Steps show status codes and durations. Cleanup section appears when present.

---

### Session 8: Step Detail — Full Audit Trail

**Delivers:** Expand/click a step → see all detail tabs: request, response, assertions, resolutions, selections, LLM calls, errors.

**Files created/modified:**
- `server/web/src/routes/StepDetail.svelte` — Tabbed detail view (or this could be an inline expansion in RunDetail)
- `server/web/src/components/JsonViewer.svelte` — JSON syntax highlighting + collapsible sections
- `server/web/src/components/AssertionsTable.svelte` — Validation results table
- `server/web/src/components/ResolutionsTable.svelte` — Input resolution audit trail
- `server/web/src/components/SelectionsTable.svelte` — Array selection details
- `server/web/src/components/LlmCallPanel.svelte` — LLM prompt/response/metrics
- `server/web/src/components/ErrorPanel.svelte` — Error classification + retry info + expect-failure

**Tabs:**
1. **Request** — Method + URL header, headers as key-value table, body in `JsonViewer`
2. **Response** — Status badge, headers table, body in `JsonViewer`
3. **Assertions** — Table: type, passed/failed icon, message, path, expression. Summary count at top.
4. **Resolutions** — Table: input name, source (edge/plan/expression/pool/llm), from step, value, constraint, relaxed flag
5. **Selections** — Table: input, strategy, source size → filtered size → selected, filter expression, filter relaxed flag
6. **LLM** — Collapsible prompt/response text, model, tokens in/out, duration (only shown when LLM calls present)
7. **Errors** — Error classification card, retry attempts, expect-failure result (only shown when relevant)

**Key design:**
- Tabs only shown when they have data (e.g., no LLM tab if no LLM calls, no Errors tab if no errors)
- `JsonViewer`: syntax highlighting via Svelte component (tokenize JSON, apply CSS classes). Collapsible nested objects for large payloads. Copy-to-clipboard button.
- Large response bodies: collapse by default, show first N lines with "show more"

**Test:** Expand a step → see Request tab with highlighted JSON. Switch to Assertions → see pass/fail table. LLM tab only appears for steps that used LLM.

---

## Critical Files (existing code to reference)

| File | Why |
|------|-----|
| `archive/types.go` | Source data structures — every field here maps to a view model field |
| `archive/reader.go` | `archive.Read()` — the service layer's primary data source |
| `mcp/tools_archive.go` | Archive dir scanning pattern (lines ~82-126), run listing logic |
| `cmd/aat/mcp_cmd.go` | Manifest-based project context resolution (reuse in `web_cmd.go`) |
| `config/resolver.go` | `ResolveProjectPaths()` for finding archive directory |
| `engine/archive.go` | `ToArchive()` — shows how engine results become archive records |
| `server/doc.go` | Existing stub to build on |

## Verification

After all 8 sessions:
1. `make build` produces binary with embedded frontend (no manual frontend build step)
2. `aat web --no-open` starts server, `curl /api/runs` returns JSON
3. Browser at `localhost:9119` → run list → click run → step timeline → expand step → tabbed detail
4. `aat web view latest` opens browser to most recent run
5. `go test ./server/...` passes
6. `--dev` mode proxies to Vite for frontend hot reload
