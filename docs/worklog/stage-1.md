# Stage 1 Worklog

## 2026-02-07 — Project Scaffolding

**What:** Initialized the Go module, created all 14 package directories with doc.go placeholders, set up CLAUDE.md, progress tracking, and worklog structure.

**Decisions:**

- **Module path:** `github.com/gburgyan/aat` — matches the user's GitHub org and keeps import paths short.
- **Go version:** 1.23 — stable release with all language features we need (range-over-func, etc.). No reason to chase newer versions yet.
- **Progress tracking:** Separate `docs/internal/progress.md` with checkbox list, cross-referencing task numbers from the implementation plan. This avoids loading the ~1900-line implementation plan just to check status. Stages 3-4 collapsed to ranges, expanded when we get there.
- **Package placeholders:** Each package gets a `doc.go` with package declaration and one-line doc comment. This makes `go build ./...` work from day one and makes package boundaries visible in the file tree. `cmd/aat/` gets a `main.go` with a placeholder print instead.
- **go-ctxdep:** Added as a dependency from the start to establish the pattern. All cross-package dependencies will be resolved from `context.Context` via `ctxdep.Get[T]`.
- **Deferred:** Makefile, CI config, LICENSE — will decide deliberately when needed rather than adding boilerplate prematurely.

**Open questions:**

- Which real API to use for the first graph (Task 2)? The implementation plan suggests a travel booking flow (search → select → book → add traveler). Need to identify a suitable public API or mock target.

## 2026-02-07 — YAML Graph Schema (Task 1)

**What:** Implemented the core graph data model, YAML parsing, semver versioning, and structural validation with cycle detection in the `graph/` package. 10 source/test files, 12 YAML test fixtures, 38 tests all passing.

**Decisions:**

- **Raw type strings in structs, parsed on demand:** `ParseFieldType(raw)` is a standalone function rather than storing parsed types on the structs. This avoids lossy round-tripping and keeps the YAML types as pure containers. Downstream code calls `ParseFieldType` when it needs the parsed representation.
- **`Nodes` as `map[string]*Node`:** Map key = node name, matching YAML structure naturally. `Node.Name` is populated from the map key during parsing. O(1) lookup during validation.
- **`Default` field as `any`:** YAML default values can be string, number, or bool depending on input type. Using `any` lets yaml.v3 unmarshal naturally. Downstream code type-asserts as needed.
- **Multi-error validation:** `ValidationError` collects all errors in a `[]string` slice rather than failing fast. This gives authors all problems at once for good UX. Errors are formatted as a bulleted list.
- **Semver: custom ~30-line implementation:** `X.Y.Z` parsing with `fmt.Sscanf` plus round-trip validation. No pre-release/build metadata — not needed for graph versioning. Doesn't justify a third-party dependency.
- **Cycle detection via DFS three-color marking:** Operates on node-level adjacency built from edges. Reports cycle path in error messages (e.g., "cycle detected: A → B → C → A").
- **`source` on Input:** Format is validated during parsing but not cross-checked against edges. Source is an authoring hint; edges are the canonical data flow.
- **Condition `when`: raw string, not parsed.** Expression parsing is deferred to Task 6a.

**Open questions:**

- None — Task 1 is self-contained. Task 2 (author a real graph) can proceed immediately.

## 2026-02-07 — Travelport Booking Graph (Task 2)

**What:** Authored a real API graph for the Travelport+ JSON API air booking workflow. 7 nodes, 10 edges, comprehensive parse and validation tests. File: `graph/testdata/valid/travelport_booking.yaml`.

**Decisions:**

- **7 nodes, not 4:** The implementation plan summary says "search → select → book → add traveler" but the real Travelport API requires 6 API calls plus a cleanup operation. Modeled all 7 faithfully: searchFlights, priceOffer, createWorkbench, addOffer, addTraveler, commitBooking, ignoreWorkbench.
- **Ordering via data edges:** `commitBooking` logically requires both `addOffer` and `addTraveler` to complete first. Since the graph schema has no explicit `dependsOn` mechanism, we express ordering through data edges — `commitBooking` accepts `offerStatus` (from addOffer) and `travelerId` (from addTraveler) as inputs. The adapter may ignore these values in the actual API call, but the edges encode the ordering constraint. This keeps the schema simple while being expressive enough.
- **Single selection point:** The user selects one offering from search results via a `select: true` edge from `searchFlights.catalogOfferings` to `priceOffer.offeringId`. The confirmed offering ID then flows forward to `addOffer` as a scalar — no second selection needed.
- **Parallel-capable topology:** `searchFlights` and `createWorkbench` have no dependencies between them and can execute in parallel. `addTraveler` only depends on `createWorkbench`, so it can run in parallel with `priceOffer`/`addOffer`.
- **Cleanup on createWorkbench:** `createWorkbench` declares `cleanup: ignoreWorkbench` to ensure workbench teardown on failure. The cleanup node receives `workbenchId` via a regular edge.
- **Adapter naming convention:** Used dotted names (`travelport.searchFlights`) to namespace adapters by provider. This will map naturally to adapter loader paths in Task 3+.

**Open questions:**

- None — the graph exercises all current schema features (arrays with elementFields, select edges, optional inputs with defaults, enum types, cleanup). Ready for Task 3 (adapter interface).

## 2026-02-07 — Adapter Interface and HTTPExecutor (Task 3)

**What:** Implemented the `adapter` package — Adapter interface, Registry, HTTPExecutor, Request/Response types, EnvironmentConfig, and ValidationResult. 8 source files, 2 test files, 23 tests all passing.

**Decisions:**

- **Request.Body / Response.Body as `[]byte`:** Idiomatic Go; `json.Unmarshal` works directly on `[]byte`, supports binary payloads for Tier 3 adapters.
- **Request.Headers as `map[string]string`, Response.Headers as `http.Header`:** Simpler single-value map for adapter authors building requests; multi-value `http.Header` for responses to preserve Set-Cookie and similar headers.
- **BaseURL owned by HTTPExecutor, not adapters:** Adapters produce relative paths. `url.ResolveReference` handles path joining correctly including query params and edge cases.
- **Minimal EnvironmentConfig in adapter package:** Just BaseURL, Headers, Values — enough for BuildRequest. Full config expansion is Task 10.
- **ValidationResult semantics:** `nil` means "no validation performed" (opt-out), `Valid: true` means "checked and passed", `Valid: false` with Errors means "checked and failed". This three-state design lets adapters opt out of validation entirely.
- **Registry with error returns:** `Register` returns error on duplicate (fail-fast), `Get` returns error on not-found. No silent overwrites.
- **No body reader pooling in HTTPExecutor:** Uses `strings.NewReader` for simplicity. The body is already in memory as `[]byte`; no benefit from pooling at this stage.

**Open questions:**

- None — ready for Task 4 (Tier 1 template adapter loader).
