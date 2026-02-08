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

## 2026-02-07 — Tier 1 Template Adapter Loader (Task 4)

**What:** Implemented the template adapter system that turns YAML files into live `Adapter` implementations via placeholder substitution and GJSON extraction. 1 source file (`template.go`), 1 test file (`template_test.go`), 6 YAML fixture files, 50 total adapter tests all passing.

**Decisions:**

- **GJSON for JSON extraction:** Added `github.com/tidwall/gjson` dependency. Template authors write JSONPath-style expressions (`$.data.flights`); `normalizeJSONPath` converts to GJSON syntax by stripping `$.` prefix and converting `[N]` bracket notation to `.N` dot notation.
- **Placeholder regex `\{\{\s*([^}]+?)\s*\}\}`:** Matches `{{key}}` with optional internal whitespace. Resolution order: inputs first, then `config.Values`. Collects ALL unresolved placeholders into one error rather than fail-fast — better UX for template authors.
- **Header merging:** Config headers are applied first, then template headers overlay. Template wins on conflicts. This lets environments provide default auth headers while templates can override Content-Type etc.
- **ValidateInputs/ValidateResponse return nil:** Template adapters defer input validation to graph-level type checking and response validation to Task 9 (mechanical validation). The `validate.schema` path is parsed and stored but not acted on yet.
- **LoadTemplates fail-fast:** Stops on first error and includes the filename in the error message. This is simpler than collecting all errors and gives clear diagnostics.
- **Value conversion via `fmt.Sprintf("%v")`:** All Tier 1 inputs are scalar, so this is sufficient. Tier 2/3 adapters that need complex serialization will use Go code or Lua.

**Open questions:**

- None — ready for Task 5 (write real Travelport template adapters).

## 2026-02-07 — Travelport Template Adapters (Task 5)

**What:** Created 7 YAML template adapter files in `adapter/testdata/templates/travelport/` mapping each Travelport booking graph node to its real API endpoint. Added `template_travelport_test.go` with 21 tests covering loading, request building, and output extraction. All 65 adapter tests pass.

**Decisions:**

- **One template per graph node:** 7 files matching the 7 nodes in `travelport_booking.yaml`. Each template maps graph inputs to API request structure and extracts graph outputs from response JSON using GJSON paths.
- **Real Travelport API structures:** Request bodies use actual `@type` discriminators and schema structures from the Travelport+ JSON API spec: `CatalogProductOfferingsQueryRequest`, `OfferQueryBuildFromCatalogProductOfferings`, `ReservationID`, `Traveler` with `PersonName`, `ReservationQueryCommitReservation`. Extract paths follow the real response envelope structure (`CatalogProductOfferingsResponse`, `OfferListResponse`, `ReservationResponse`, `TravelerResponse`).
- **Path parameters via placeholder substitution:** Templates like `addOffer`, `addTraveler`, `commitBooking`, and `ignoreWorkbench` embed `{{workbenchId}}` in the URL path. The existing `substitutePlaceholders` function handles this naturally — no code changes needed.
- **Content-Type only on templates with bodies:** The DELETE template (`ignoreWorkbench`) omits the `Content-Type` header since it has no request body. Other common headers (Authorization, XAUTH_TRAVELPORT_ACCESSGROUP, TraceId, etc.) come from `EnvironmentConfig.Headers` at runtime.
- **One-way search only:** The search template omits optional inputs (`returnDate`, `cabinPreference`). Tier 1 templates don't support conditional body sections, so round-trip and cabin filtering are deferred to Tier 2/3.
- **Ordering inputs not in body:** `commitBooking` accepts `offerStatus` and `travelerId` as graph inputs for ordering but doesn't use them in the request body — the workbench already holds that state.
- **Known Tier 1 gap documented:** `ignoreWorkbench` (DELETE) has no response body to extract `acknowledged: boolean` from. Documented in the template file as a gap for the engine (Task 6) to handle by inferring success from HTTP status.
- **Separate test directory:** Templates live in `adapter/testdata/templates/travelport/` alongside the existing `valid/` and `invalid/` fixture dirs. The existing 3 parser test fixtures are untouched.

**Open questions:**

- None — ready for Task 6 (sequential plan runner with dependency-aware scheduler).

## 2026-02-07 — Sequential Plan Runner (Task 6)

**What:** Implemented the plan model, YAML parsing, sequential execution engine, value resolution, topological sort, auth/config loading, and cleanup stack. Four sub-tasks (6.1-6.4): plan package (24 tests), engine package (28 tests), config package (11 tests). Total: 63 new tests, all passing.

**Decisions:**

- **Task decomposed into 4 sub-tasks:** 6.1 (plan model), 6.2 (engine runner), 6.3 (auth/config), 6.4 (cleanup stack). 6.1 and 6.3 were independent and built in parallel; 6.2 depended on 6.1; 6.4 depended on 6.2.
- **StepValue custom UnmarshalYAML:** Bare YAML scalars (e.g., `origin: "DEN"`) set only the `Default` field. Mapping nodes unmarshal into the full StepValue struct. Uses `yaml.Node.Kind == yaml.ScalarNode` check with a type alias to avoid infinite recursion.
- **TopologicalSort via Kahn's algorithm:** Dependencies from two sources: explicit `dependsOn` and graph edges between plan steps. BFS produces valid ordering and detects cycles. Extensible to parallel execution later (dequeue all zero-in-degree nodes at once).
- **ResolveInputs priority chain:** graph edge → SELECT edge → plan StepValue.Default → graph node Input.Default → optional skip → error. This ensures edges always win over plan values, which in turn win over graph defaults.
- **SELECT edge: first-only for Task 6:** Extracts first array element, optionally using gjson field path. Full selection strategies (min, max, match, etc.) deferred to Task 7.
- **Constructor injection for Engine:** `NewEngine(graph, registry, executor, config)`. No go-ctxdep yet — introduced at CLI wiring (Task 12).
- **Config package stays leaf:** `LoadSettings` and `Authenticate` are standalone. EnvironmentConfig assembly happens at the call site (engine setup or cmd).
- **OAuth2 ROPC flow:** `Authenticate(ctx, settings)` posts form-encoded credentials to `settings.AuthURL`. Returns `OAuthToken` with `access_token`.
- **Cleanup stack FILO:** Last pushed, first executed. Cleanup errors are recorded in StepResult but don't stop subsequent cleanup or propagate to the run outcome.
- **Fail on non-2xx:** Simplistic for Task 6 — any status >= 400 stops forward execution and triggers cleanup. Error taxonomy (transient vs client vs server) deferred to Task 8.

**Open questions:**

- The gjson field path for SELECT edge extraction (`Identifier.value`) may need adjustment after observing real Travelport API responses. The test fixture uses this path but it hasn't been validated against the actual API yet.
- None blocking — ready for Task 6a (predicate expression parser).

## 2026-02-07 — Predicate Expression Parser (Task 6a)

**What:** Implemented a self-contained predicate expression parser and evaluator in `plan/predicate.go`. Recursive descent parser with tokenizer, AST, and evaluator. Exposed as `EvalPredicate(expr, context)` and `ValidatePredicate(expr)`. Integrated syntax validation into `Validate()` for filter, constraint, and predicate assertion expressions. 2 new files, 1 modified file, ~55 new tests, all passing.

**Decisions:**

- **12 token types:** `tokenNumber`, `tokenString`, `tokenBool`, `tokenIdent`, `tokenOperator`, `tokenLParen`, `tokenRParen`, `tokenLBracket`, `tokenRBracket`, `tokenComma`, `tokenIn`, `tokenEOF`. Identifiers include dots (e.g., `price.amount` is one token). `true`/`false` emit `tokenBool`, `in` emits `tokenIn`.
- **5 AST node types:** `literalNode` (float64/string/bool), `identNode` (field reference), `arrayLiteralNode`, `unaryNode` (!), `binaryNode` (==, !=, <, >, <=, >=, &&, ||, in). All implement a `node` interface with a marker method.
- **Precedence via grammar structure (low→high):** logicalOr → logicalAnd → comparison → inExpr → unary → primary. Comparison is non-associative (single operator only). Boolean ops are left-associative.
- **Short-circuit evaluation for && and ||:** `false && missing` returns false without evaluating the right side. `true || missing` returns true. This is essential for practical use where not all fields may be present.
- **Int → float64 coercion:** YAML unmarshals integers as `int`, JSON as `float64`. The evaluator normalizes int/int32/int64 to float64 before comparisons. This handles the common YAML/JSON interop case.
- **`in` operator:** LHS is scalar, RHS must evaluate to `[]any`. Incompatible element types in the array are skipped (not errors) — pragmatic for mixed-type arrays.
- **Field resolution via dot splitting:** `resolveField("a.b.c", ctx)` splits on "." and traverses nested `map[string]any`. Errors on missing keys or non-map intermediates.
- **ValidatePredicate:** Parse-only (tokenize + parse + check EOF), no evaluation. Used by `Validate()` to catch syntax errors at plan load time.
- **Validation integration points:** Three places checked — `SelectionConfig.Filter`, `StepValue.Constraint`, and `MechanicalAssertion` with `Type == "predicate"`.

**Open questions:**

- None — ready for Task 7 (array selection strategies).

## 2026-02-07 — Array Selection Strategies (Task 7)

**What:** Replaced the hardcoded `arr[0]` array selection with a full strategy dispatch supporting 7 deterministic strategies (first, last, index, random, min, max, match), optional filter expressions, selection decision logging, and deduplication for multiple fields from the same selected element. 2 new files, 5 modified files, ~30 new tests, all passing.

**Decisions:**

- **7 strategies:** `first` (default), `last`, `index`, `random`, `min`, `max`, `match`. All are deterministic except `random` (uses `math/rand/v2`). Empty strategy defaults to `first`.
- **`match` vs filter:** `match` strategy uses `SelectionConfig.Filter` as its predicate and returns the first matching element — no pre-filtering step. Non-match strategies with a filter apply the filter as a pre-filter (narrowing the array) then apply the strategy on the filtered result.
- **`SortField` for min/max:** Added to `SelectionConfig`. When both `Field` and `SortField` are set, `SortField` is the comparison key and `Field` is the extraction path. When only `Field` is set for min/max, it serves as both.
- **`SelectionDecision` struct:** Records `InputName`, `SourceNode`, `SourceField`, `SourceSize`, `FilterExpr`, `FilteredSize`, `Strategy`, `SelectedIndex` — enough for debugging and logging without being heavyweight.
- **`ResolveInputs` signature change:** Returns `(map[string]any, []SelectionDecision, error)` instead of `(map[string]any, error)`. All callers updated.
- **Dedup cache in `ResolveInputs`:** Keyed by `from|strategy|filter|index`. When two inputs share the same source and selection config (differing only in `Field`/`SortField`), the selection is performed once and different fields are extracted from the same element. This ensures consistency when e.g. both `itemId` and `itemName` reference the same random selection.
- **Validation additions:** Unknown strategies, min/max without field, match without filter, negative index — all caught at plan validation time.
- **Existing test fix:** Pre-existing test used `Strategy: "filter"` which was never a valid strategy name. Updated to `Strategy: "first"` with a filter.

**Open questions:**

- None — ready for Task 8 (error taxonomy and failure handling).

## 2026-02-07 — Error Taxonomy and Failure Handling (Task 8)

**What:** Added error classification and retry logic to the engine. Every step failure is now categorized into one of 7 error categories (transient, client, auth, server, adapter, network, timeout). Steps with a `RetryConfig` are retried with exponential backoff and jitter. 4 new files, 2 modified files. ~40 new tests, all passing.

**Decisions:**

- **7 error categories:** `transient` (429, 502-504, connection refused/reset), `client` (400, 404, 405, 409, 422, other 4xx), `auth` (401, 403), `server` (500, 501), `adapter` (template/extraction errors), `network` (DNS, TLS, no route), `timeout` (context deadline, net timeout). Category names are lowercase strings matching `RetryConfig.On`/`FailOn` YAML values.
- **`errors.Is`/`errors.As` for classification:** Traverses Go error wrapping chains rather than string matching. Handles `context.DeadlineExceeded`, `net.Error.Timeout()`, `*net.DNSError`, `*net.OpError` with connection refused/reset detection.
- **`shouldRetry` policy:** `nil` config → never retry (backward compatible). `FailOn` overrides everything. Explicit `On` list → only listed categories. Empty `On` → default retryable set (transient, timeout, server).
- **`executeStepWithRetry` wraps `executeStep`:** Loop handles both `result.Error` and `result.StatusCode >= 400` as failure conditions. Only the final result (after retry exhaustion or success) is returned to `Run()`. `Run()` change is a single-line substitution.
- **Exponential backoff with jitter:** Base 500ms, 2x per attempt, capped at 10s, ±25% jitter. Respects `ctx.Done()` during backoff wait.
- **`ErrorClassification` on `StepResult`:** `nil` on success. Contains category, detail string, action ("retried"/"failed"/"failed_fast"), and retry attempt number. `RetryCount` field tracks total retries performed.
- **No retry without config:** Existing behavior is fully preserved — steps without `RetryConfig` never retry. Classification is still attached for reporting.

**Open questions:**

- None — ready for Task 9 (mechanical validation).

## 2026-02-07 — Mechanical Validation (Task 9)

**What:** Implemented the `validate` package with mechanical assertion types and integrated validation into the engine execution loop. 4 new files, 2 modified files. ~43 new tests (37 validate, 6 engine integration), all passing.

**Decisions:**

- **5 assertion types:** `status`, `schema` (stub), `fieldExists`, `fieldEquals`, `predicate`. Each has a dedicated checker function. Unknown types produce a failed result with a descriptive message rather than panicking.
- **Dependency boundary respected:** `validate` cannot import `plan`, `engine`, or `graph`. The predicate evaluator is injected as a `PredicateEvalFunc` function type. `engine` bridges `plan.MechanicalAssertion` → `validate.MechanicalAssertion` via a `convertAssertions` helper doing field-by-field copy.
- **`NormalizeJSONPath` duplicated from adapter:** Small function (~10 lines). Extracting a shared package would add a dependency and complexity disproportionate to the code savings. Both copies are tested independently.
- **Schema validation stubbed:** `checkSchema` always returns `passed: true` with "not yet implemented" message. JSON Schema and OAS-based validation deferred to a future task.
- **Cleanup pushed before assertion check:** When a step's HTTP call succeeds (2xx) but assertions fail, the cleanup node is still pushed. The step executed and may have created resources that need teardown. This differs from the error/4xx case where cleanup was already pushed for prior successful steps.
- **`ContinueOnAssertionFailure` on Engine:** Boolean field controls whether assertion failure aborts execution. Default is false (abort + cleanup). When true, the outcome is set to Failed but subsequent steps still execute — useful for diagnostic runs.
- **Numeric coercion for status expects:** YAML unmarshals `expect: 200` as `int`, but `float64(200)` also works. The `toInt` helper handles int, int64, float64 (if whole number), and json.Number.
- **`valuesEqual` uses gjson.Result:** Direct comparison against the gjson result value rather than re-parsing. Handles string, bool, float64, and int with coercion.

**Open questions:**

- JSON Schema validation (`type: schema`) and OAS-based response validation are deferred. These will be important for comprehensive API testing — see implementation plan for future task details.
- None blocking — ready for Task 10 (config/environment layer).

## 2026-02-07 — Config/Environment Layer (Task 10)

**What:** Replaced the JSON-based `Settings`/`LoadSettings` system with a unified YAML-based `Environment` configuration. New types: `Environment`, `AuthConfig`, `LLMConfig`, `RuntimeSettings`, `SecretRef`, `Duration`, `APIConfig`. `Authenticate` now takes `AuthConfig` and supports oauth2, apikey, bearer, and none auth types. `BuildAPIConfig` bridges environment config to adapter layer. 3 new files, 1 rewritten file, 3 deleted files, 5 test fixtures, 32 tests all passing.

**Decisions:**

- **Unified config — removed JSON settings:** The old `Settings` struct and `LoadSettings(path)` (JSON-based) are deleted. The `Environment` YAML config replaces everything. One config system, not two.
- **Named credential fields via `map[string]SecretRef`:** Each auth type requires specific keys (oauth2: username/password/clientId/clientSecret; apikey: key; bearer: token). Validation checks for required keys per auth type.
- **`SecretRef` with env/literal sources:** Secrets reference environment variables (`source: env, var: VAR_NAME`) or inline literals (`source: literal, value: xyz`). Extensible for vault/KMS sources later.
- **`Duration` custom YAML unmarshaling:** Wraps `time.Duration` with `time.ParseDuration`-based YAML unmarshaling. Rejects bare integers — must be explicit like "120s" or "5m".
- **Defaults applied before validation:** mode → lean, maxRunDuration → 120s, defaultRetries → 2, maxRelaxationDepth → 3, archiveFormat → json.
- **`APIConfig` as flat bridge:** `BuildAPIConfig(ctx)` authenticates and produces a flat `APIConfig{BaseURL, Headers, Values}` suitable for bridging to `adapter.EnvironmentConfig` at the call site (Task 12).
- **Config stays leaf:** No aat imports. The bridge to `adapter.EnvironmentConfig` happens at the call site, not in this package.
- **SQLite deferred to Task 10b:** Local run history indexing via SQLite is split out as a separate sub-task to keep this change focused on the config model.

**Open questions:**

- None — ready for Task 11 (archive writer).
