# Stage 2: Intelligence — Worklog

## 2026-02-07 — Task 13: Domain Knowledge Layer

**What:** Implemented domain knowledge package (concepts, types, value pools).
**Decisions:** Leaf package with zero AAT imports. YAML-based KB with merge support.
**Open questions:** None.

## 2026-02-07 — Task 14: Deterministic Backward Chaining

**What:** Implemented backward chaining in the graph package to compute the minimal subgraph needed to reach goal nodes.

**Decisions:**
- Added `CycleBreaker` field to Node and `Preferred` field to Edge in graph schema
- CycleBreaker nodes stop upstream traversal during BFS — they're included as entry nodes but their dependencies aren't followed (breaks auth cycles)
- Modified `detectCycles` in validation to suppress cycle errors when the cycle path includes a CycleBreaker node
- 3-phase algorithm: (1) condition evaluation, (2) BFS backward from goals, (3) path selection + topo sort
- Conditions only ADD requirements (via Require), never remove reachable nodes — simpler and safer
- Multiple-path resolution: prefer `Preferred` edges, then shallowest depth, then lexicographic tiebreak
- PredicateFunc injected as function type to avoid importing plan package (graph is leaf)
- Deterministic Kahn's algorithm with sorted queue for consistent output
- 33 tests covering basic chaining, cycles, conditions, multiple paths, entry nodes, ordering, errors, deduplication

**Files:**
- `graph/types.go` — added CycleBreaker (Node), Preferred (Edge)
- `graph/validate.go` — cycle-breaker tolerance in detectCycles
- `graph/chain.go` — new: BackwardChain, types, algorithm
- `graph/chain_test.go` — new: 33 tests
- `graph/testdata/valid/with_cycle_breaker.yaml` — new fixture (auth cycle)
- `graph/testdata/valid/with_multiple_paths.yaml` — new fixture (preferred edges)

**Open questions:** None.

## 2026-02-08 — Task 15: Prompt-to-Plan Transformer

**What:** Implementing the intent interpretation layer: LLM client, graph/chain formatters, YAML extraction, and the two-call interpret pipeline.

**Decisions:**
- Split into 15a (llm/ package), 15b (intent formatters/extraction), 15c (interpret pipeline)
- Hybrid two-call LLM architecture: first call identifies goal + classifies constraints (lightweight JSON), second call generates full plan YAML after deterministic backward chaining narrows the search space
- Provider-agnostic LLM client: `Client` interface with `Complete(ctx, *Request) (*Response, error)`. Two providers: OpenAI-compatible (covers OpenAI, Azure, vLLM, Ollama) and Anthropic Messages API
- Auto-detect provider from endpoint URL (anthropic.com → Anthropic, else → OpenAI); explicit `Provider` field on LLMConfig for disambiguation
- Pre-chain strategy: backward chaining runs between the two LLM calls, so the second call gets a focused subgraph (fewer tokens, more accurate plans)
- PostProcess applies deterministic fixes after LLM output: dependsOn from graph edges, cleanup steps, selection defaults, metadata. This makes the LLM's job easier — it doesn't need to perfectly reproduce graph topology
- `InterpretResult` returns `Plan`, `GoalAnalysis`, and `ChainResult` — callers (future confirmation UX) can inspect reasoning

**Open questions:** None.

## 2026-02-08 — Task 16: Plan Validation Enhancements

**What:** Enhanced plan validation with 8 new checks covering LLM-generated plan correctness.

**Decisions:**
- Added `splitRef` helper in plan/validate.go (same pattern as graph/parse.go and engine/scheduler.go)
- New lookup maps: `outputsByNode` for output existence/type checks, `edgesByTarget` for implicit select edge discovery
- Gap 1 (From field): validates format, source step existence, source output existence
- Gap 2 (Array selection source): validates From output is array type, or implicit select edge exists
- Gap 3 (SortField): validates against elementFields when available; skips nested paths (dots) and outputs without elementFields
- Gap 4 (Constraint AppliesTo): extracts node name from `node.input` format before checking — LLMs produce `"searchFlights.origin"` naturally
- Gap 5 (Cleanup/Verification): validates node existence in graph, runOn values, predicate assertions
- Gap 6 (Unknown values): catches hallucinated input names not matching any node input
- Gap 8 (Goal consistency): validates intent.Goal references a plan step, isGoal matches intent.Goal, at most one isGoal
- Gap 9 (DependsOn completeness): From references imply dependsOn entries
- Fixed existing test: `min_with_sortField_ok` used `"price"` which isn't in elementFields; changed to `"stops"`
- 32 new test cases, all existing tests still pass

**Open questions:** None.

## 2026-02-08 — Task 17: Confirmation UX + End-to-End Prompt Command

**What:** Implemented the `aat prompt` command connecting the full pipeline: natural language → LLM-generated plan → narrative display → confirmation → execution → archive.

**Decisions:**
- Three sub-tasks: 17a (plan narrative formatter), 17b (YAML serializer), 17c (CLI prompt command)
- `FormatNarrative(p, g)` in `plan/` package — reusable by future web server, not just CLI
- `Marshal`/`WriteFile` in `plan/` — `MarshalYAML` on StepValue emits bare scalars when only Default is set (mirrors UnmarshalYAML pattern)
- `io.Reader` injection for confirmation input — entire command pipeline testable without real stdin
- Adjust flow: save YAML to temp file → user edits externally → press Enter → reload + revalidate + redisplay
- Shared `writeArchive` helper extracted from `runCommand`, reused by `promptCommand`
- `--yes` flag for non-interactive/CI usage, `--save` to persist generated plan, `--domain` optional for domain knowledge enrichment
- Clear error message when environment lacks LLM config (endpoint check)

**Files:**
- `plan/format.go` — new: FormatNarrative + helpers (~165 lines)
- `plan/format_test.go` — new: 13 table-driven tests
- `plan/write.go` — new: Marshal, WriteFile (~35 lines)
- `plan/write_test.go` — new: 8 tests (round-trip, bare scalars, directory creation)
- `plan/parse.go` — modified: added MarshalYAML to StepValue
- `cmd/aat/prompt.go` — new: promptCommand, confirmPlan, adjustPlan, executePlan, writeArchive (~215 lines)
- `cmd/aat/prompt_test.go` — new: 15 tests (flag validation, LLM config check, confirm parsing, adjust flow)
- `cmd/aat/main.go` — modified: added prompt case, shared writeArchive helper, cleaned up unused imports

**Open questions:** None.

## 2026-02-08 — Fix: elementField Path annotations for correct selection fields

**What:** LLM-generated plans used wrong field names in selection configs (e.g., `offeringId` instead of `id`). Added `Path` field to graph elementFields so the skeleton builder can resolve the correct gjson extraction paths.

**Decisions:**
- Added `Path string` to `graph.Field` with `EffectivePath()` method (returns Path if set, else Name)
- Added `lookupElementFieldPath` helper in intent/postprocess.go — resolves the gjson path from the source output's elementField matching the input name
- Updated both `BuildSkeleton` and `fixSelectionConfigs` to use the helper instead of bare `inp.Name`
- Added path annotations to LLM context formatting (`FormatGraph`, `FormatChainResult`) so the LLM sees the mapping
- Added `productRef` elementField to travelport booking graph with deeply nested path
- No changes needed to engine (already uses `SelectionConfig.Field` as gjson path), plan schema, or MergeLLMValues (Field is skeleton-authoritative)

**Verification:** End-to-end CLI test produced `field: id` and `field: ProductBrandOptions.0.ProductBrandOffering.0.Product.0.productRef` — matching the hand-written plan. Full booking flow executed successfully (5/5 steps passed).

**Files:**
- `graph/types.go` — added Path field, EffectivePath method
- `graph/testdata/valid/travelport_booking.yaml` — path annotations, productRef field
- `intent/postprocess.go` — lookupElementFieldPath helper, updated BuildSkeleton + fixSelectionConfigs
- `intent/format.go` — path annotations in FormatGraph/FormatChainResult, updated FormatPlanSchema example
- `graph/parse_test.go`, `intent/postprocess_test.go`, `intent/format_test.go` — updated assertions, 6 new unit tests

**Open questions:** None.

## 2026-02-08 — Task 17a-obs: Planning Pipeline Observability (Plan Trace)

**What:** Added opt-in tracing to the intent.Interpret() pipeline. When enabled, captures both LLM calls (prompts, responses, tokens, timing), backward chaining results, skeleton construction, merge results, and validation outcomes as a JSON file for debugging.

**Decisions:**
- Types live in `intent` package (PlanTrace, LLMCallTrace, ChainTrace, SkeletonTrace, etc.) — avoids dependency cycles with archive
- Opt-in via `InterpretRequest.EnableTrace` bool — zero overhead when false
- Refactored `analyzeGoal` to return `goalResult` struct carrying response metadata and prompts (internal, no API breakage)
- Own trace ID format `trace-YYYYMMDD-HHMMSS-XXXXXXXX` (same pattern as archive.GenerateRunID but avoids intent→archive import)
- Partial trace on error: when tracing is enabled and an error occurs mid-pipeline, `InterpretResult{Trace: trace}` is returned alongside the error so the CLI can still write the trace
- `WritePlanTrace` mirrors `archive.Write` pattern (MkdirAll + MarshalIndent)
- CLI: `--trace` flag enables tracing, `--trace-dir` sets output directory (default "traces")

**Files:**
- `intent/trace.go` — new: PlanTrace types, WritePlanTrace, generateTraceID, helper converters (~140 lines)
- `intent/trace_test.go` — new: 9 tests (write, parent dirs, ID format, uniqueness, trace on/off, error, fallback, LLM error)
- `intent/interpret.go` — refactored analyzeGoal return type, added trace capture throughout Interpret(), partial trace on error
- `cmd/aat/prompt.go` — added TracePlan/TraceDir to promptArgs, --trace/--trace-dir flags, trace writing after Interpret
- `cmd/aat/prompt_test.go` — 2 new tests (trace flag parsing, defaults)
- `docs/internal/progress.md` — marked complete

**Open questions:** None.

## 2026-02-08 — Task 18: Value Resolution Hierarchy

**What:** Added pre-execution value resolution with expression evaluation, constraint checking, fallback pools, LLM-assisted value selection, and execution mode enforcement.

**Decisions:**
- Three sub-tasks: 18a (expression evaluator), 18b (enhanced resolution), 18c (mode + LLM)
- Expression evaluator (`plan/expr.go`): `{{...}}` template syntax with `today`, `today +/- N days`, `env.VAR`, `ref +/- N days`. Simple regex-based parser, not a full grammar — sufficient for date arithmetic and env lookups
- `ContainsExpr` is a fast `strings.Contains("{{")` check — zero overhead for the common case of no expressions
- `ExprContext` carries `Now`, `Env`, `Values` — injectable for testing, defaults to `time.Now()`/`os.Getenv`
- `ResolveInputsWithContext` wraps the original chain with expression evaluation, constraint checking, and fallback pool iteration. `ResolveInputs` delegates to it with `nil` context for backward compatibility
- `resolvedInputs` accumulates as we iterate `node.Inputs`, so later inputs can reference earlier ones in expressions (e.g., `returnDate = departureDate + 7 days`)
- Edge-resolved values are NOT subject to expression evaluation (they come from real API responses)
- Constraints evaluated via existing `plan.EvalPredicate` with candidate as `"value"` in context plus all resolved inputs
- `FallbackStrategy`: `nil`/"sequential" = in-order, "random" = shuffled via `rand.Shuffle`
- Engine gains `Mode`, `KB`, `LLMClient` fields with builder methods (`WithMode`, `WithDomain`, `WithLLM`)
- `executeStep` now always constructs a `ResolveContext` (expressions and constraints are always active)
- Mode enforcement: `strict` → never calls LLM, `lean` → calls LLM only when pool exhausted, `adaptive` → same as lean for now (Task 20 adds recovery)
- CLI: `--mode` and `--domain` flags added to `run` command. `prompt` command defaults to `lean` mode
- `buildValueSelectionPrompt` includes input metadata, constraint, tried values, resolved inputs, and domain knowledge (type defs, value pools, applicable concepts)

**Files:**
- `plan/expr.go` — new: EvalExpr, ContainsExpr, ValidateExpr, ExprContext (~195 lines)
- `plan/expr_test.go` — new: 30 test cases (today, env, references, mixed, errors, boundaries)
- `engine/resolve.go` — modified: added ResolveContext, ResolveInputsWithContext, evaluateValue, checkConstraint, resolveWithFallback
- `engine/resolve_test.go` — modified: 20 new test cases (expressions, constraints, pools, references, mixed inputs)
- `engine/engine.go` — modified: added Mode/KB/LLMClient fields, WithMode/WithDomain/WithLLM builders, buildResolveContext
- `engine/llm_values.go` — new: llmSelectValue, buildValueSelectionPrompt (~100 lines)
- `engine/llm_values_test.go` — new: 12 test cases (strict/lean/adaptive mode, prompt content, error cases)
- `cmd/aat/main.go` — modified: added --mode/--domain flags, LLM client creation, engine wiring
- `cmd/aat/prompt.go` — modified: updated executePlan to wire mode/domain/LLM

**Open questions:** None.

## 2026-02-08 — Task 18d: Wire LLM Fallback + Resolution/LLM Call Tracking in Archives

**What:** Connected the LLM value selection into the resolution chain (was defined/tested but never called), and added full audit trail for how every input was resolved — including LLM call details (prompts, response, model, tokens, timing).

**Decisions:**
- `ResolveInputsWithContext` gains `ctx context.Context` param (needed for LLM calls) and returns `[]ValueResolution` as 3rd return value
- `ResolveInputs` backward-compat wrapper passes `context.Background()` and drops the resolutions
- Every path through `resolveInputEnhanced` builds a `ValueResolution` record: `edge`, `select_edge`, `plan_default`, `expression`, `fallback_pool`, `graph_default`, `llm`, `optional_skip`
- `resolveWithFallback` now calls `llmSelectValue` when all pool values fail constraint and mode allows — this was the main missing wiring
- `llmSelectValue` returns `(any, *LLMCallRecord, error)` — captures timing, prompts, tokens, model from the LLM response. On LLM error, record is still returned (with Error field) for debugging
- Engine types: `ValueResolution`, `LLMCallRecord`, `LLMMessage` in `engine/result.go`; `StepResult.Resolutions` field
- Archive types: `ValueResolutionRecord`, `LLMCallRecord`, `LLMMessageRecord` in `archive/types.go`; `StepRecord.Resolutions` field
- `convertResolutions` / `convertLLMCall` follow the existing `convertSelections` pattern
- Archive `ConstraintOK` is `*bool` (pointer) so it omits cleanly when no constraint was checked
- Types are independent of `intent.LLMCallTrace` to avoid import cycles (engine → intent not allowed)

**Files:**
- `engine/result.go` — added ValueResolution, LLMCallRecord, LLMMessage types; added Resolutions to StepResult
- `engine/resolve.go` — added ctx param, returns []ValueResolution, builds resolution records in all paths, wires LLM fallback
- `engine/llm_values.go` — returns *LLMCallRecord, captures timing/tokens/prompts
- `engine/engine.go` — passes ctx to resolve, stores resolutions in StepResult
- `archive/types.go` — added ValueResolutionRecord, LLMCallRecord, LLMMessageRecord; added Resolutions to StepRecord
- `engine/archive.go` — added convertResolutions, convertLLMCall
- `engine/resolve_test.go` — 8 new resolution record tests, updated signatures
- `engine/llm_values_test.go` — updated for 3-value return, verify LLMCallRecord content
- `engine/archive_test.go` — 2 new tests (resolution conversion, LLM call round-trip)

**Open questions:** None.

## 2026-02-08 — Task 19: ElementField Name Resolution + LLM-Assisted Array Selection

**What:** Two sub-tasks: (19a) Plans now use elementField names (e.g., `offeringId`, `productRef`) instead of raw gjson paths; the engine resolves names to paths at runtime. (19b) Added `strategy: llm` for array element selection, where the LLM examines array elements and picks the best one based on a prompt.

**Decisions:**
- 19a: `lookupElementFieldPath` in `intent/postprocess.go` now returns `ef.Name` instead of `ef.EffectivePath()` — plans are human-readable
- 19a: `resolveElementFieldPath(g, sourceNode, sourceOutput, fieldRef)` in engine resolves names → gjson paths via graph elementFields lookup. Falls back to fieldRef unchanged for backward compatibility with raw gjson paths
- 19a: `resolveSelectionFields` creates a shallow copy of SelectionConfig with resolved Field and SortField — original untouched for dedup cache keys
- 19a: `resolveSelectEdge` and `resolveSelectValue` gained `ctx`, `g`, `rctx` parameters (same refactor needed for 19b)
- 19b: `llmSelectElement` follows `llmSelectValue` patterns: mode enforcement (strict → error), LLM call with audit trail, index parsing (plain int or JSON `{"index": N}`)
- 19b: Array summarization: tabular format using elementFields when available (token-efficient), raw JSON fallback, max 10 elements shown
- 19b: Filter interaction: if `sel.Filter` is set, filter first, then pass filtered array to LLM
- 19b: `SelectionConfig` gained `Prompt` field; validation requires non-empty prompt for `llm` strategy
- 19b: `SelectionDecision` and `SelectionRecord` gained `LLMCall` field for audit trail
- Both sub-tasks implemented together since they share the same signature refactor, minimizing churn

**Files:**
- `intent/postprocess.go` — returns name not path; added Prompt to MergeLLMValues override list
- `engine/resolve.go` — resolveElementFieldPath, resolveSelectionFields, parameter threading, LLM strategy routing
- `engine/llm_selection.go` — new: llmSelectElement, parseElementIndex, buildElementSelectionPrompt, helpers
- `engine/result.go` — LLMCall field on SelectionDecision
- `plan/types.go` — Prompt field on SelectionConfig
- `plan/validate.go` — llm in validStrategies, prompt required for llm
- `archive/types.go` — LLMCall on SelectionRecord
- `engine/archive.go` — copy LLM call in convertSelections
- `plans/travelport_booking.yaml` — field names instead of gjson paths
- `plan/testdata/valid/travelport_booking.yaml` — same
- `intent/postprocess_test.go` — updated for name expectations
- `plan/parse_test.go` — updated for name expectations
- `engine/resolve_test.go` — 5 new elementField resolution tests + unit test
- `engine/llm_selection_test.go` — new: 20+ tests (modes, parsing, filter+llm, elementFields, integration)
- `plan/validate_test.go` — 2 new tests (llm with/without prompt)

**Open questions:** None.

## 2026-02-09 — Task 20: Constraint-Aware Fallback with Relaxation Guard

**What:** Implemented soft constraint relaxation across three scenarios: value resolution, filter selection, and step-level re-execution (adaptive mode only). Four sub-tasks (20a-20d), each independently testable.

**Decisions:**
- RelaxationTracker is per-step, not per-run — prevents cross-step state leakage and keeps budget accounting simple
- Three relaxation reasons: `resolution_exhausted`, `filter_empty`, `step_failed` — explicit enum for archive readability
- `tryRelaxResolution` returns `tried[0]` (first value that passed expression eval but failed constraint) — deterministic fallback, no re-evaluation needed
- `tryRelaxFilter` retries selection with `filter = ""` via shallow copy — preserves original SelectionConfig for audit trail
- Step-level relaxation (executeStepAdaptive) only triggers on CategoryClient (4xx), not CategoryServer (5xx) — server errors are infrastructure problems, not constraint mismatches
- ExpectFailure steps skip relaxation entirely — they're supposed to fail
- IsRelaxed pre-check at top of constraint evaluation in resolveWithFallback — enables step-level re-execution path to skip previously-blocking constraints without re-running the full resolution chain
- `config.RuntimeSettings.MaxRelaxationDepth` already existed in the codebase — just wired it through to engine
- `splitAppliesTo` handles both "node.input" and "node" formats in constraint AppliesTo refs

**Files changed:**
- `engine/relaxation.go` — NEW: RelaxationTracker, RelaxationRecord, FindSoftConstraintForInput, 14 tests
- `engine/relaxation_test.go` — NEW: tracker lifecycle, circular detection, budget exhaustion, lookup
- `engine/resolve.go` — ResolveContext.Plan/Tracker, resolveWithFallback relaxation, tryRelaxResolution, tryRelaxFilter, 8 new tests
- `engine/engine.go` — MaxRelaxationDepth, plan field, WithMaxRelaxationDepth, executeStepAdaptive, splitAppliesTo, Run loop branching
- `engine/retry.go` — executeStepWithTracking wrapper, tracker param threading
- `engine/result.go` — Relaxed/RelaxedConstraint on ValueResolution, FilterRelaxed on SelectionDecision, Relaxations on StepResult
- `engine/selection_test.go` — 8 new filter relaxation tests
- `engine/engine_test.go` — 8 new step-level relaxation tests
- `engine/archive.go` — convertRelaxations, extended convertStepResult/convertSelections/convertResolutions
- `engine/archive_test.go` — 4 new archive conversion tests
- `archive/types.go` — RelaxationArchiveRecord, Relaxations on StepRecord, FilterRelaxed on SelectionRecord, Relaxed/RelaxedConstraint on ValueResolutionRecord
- `cmd/aat/main.go` — Wire WithMaxRelaxationDepth
- `cmd/aat/prompt.go` — Wire WithMaxRelaxationDepth

**Open questions:** None.

## 2026-02-09 — Task 60 Expanded: MCP as API Lifecycle Platform

**What:** Expanded Task 60 from "graph authoring tool" to full API integration lifecycle platform.

**Decisions:**
- MCP server exposes five lifecycle lenses: coding, testing, docs, CI/CD, monitoring
- A test plan and a synthetic monitor are the same thing — different scheduling
- Project manifest (`aat-project.yaml`) centralizes config for MCP server and CLI
- Per-node Markdown documentation enriched by Claude Code via MCP prompts
- Sub-tasks: 60a (graph knowledge), 60b (docs), 60c (testing), 60d (CI/CD + monitoring)
- 60a-60c in Stage 2, 60d deferred to Stage 3a+
- SDK: mark3labs/mcp-go (mature, well-documented)
- New `mcp/` package at orchestrator tier

**Rationale:** The graph already contains everything needed to assist with coding, testing, documentation, and monitoring. The MCP server is a thin adapter layer that exposes existing package capabilities. No new business logic — just new access patterns. Production monitoring is the natural end-state: if you can test a workflow, you can monitor it.

**Open questions:**
- Project manifest schema: what other fields beyond graph/templates/domain/envs?
- Doc file naming convention: by node name or by adapter name?
- Monitor daemon architecture: embedded scheduler vs systemd/cron integration?

## 2026-02-09 — Strategic Review: Roadmap Resequencing

**What:** Product Owner + Staff Engineer review of remaining work, technical debt, and feature sequencing.

**Decisions:**
- Defer GopherLua (Task 21) indefinitely. Templates + Tier 3 external adapters cover use cases.
- Pull forward `aat docs generate` (Task 59) and `aat mcp serve` (Task 60) from Stage 4 into Stage 2.
- Pull forward CI/CD mode (Task 28) from Stage 3a into Stage 2 — table stakes.
- Move semantic validation (Task 22) from Stage 2 to Stage 3a.
- Redefine Task 23: OAS is a reference/enrichment layer, not a generator. Scaffold generation is a starting point; graph-OAS validation closes the feedback loop; OAS enriches the MCP server with authoritative request/response shapes.
- The onboarding story is AI-assisted authoring: MCP server + Claude Code + `aat graph validate`, not a code generator.
- Add pre-release hardening: context cancellation, schema stub honesty, archive secret redaction, CLAUDE.md accuracy.
- Add quickstart example using PetStore public API.

**Rationale:** The engine quality is excellent. The gap is accessibility. MCP server positions AAT as "the API graph platform" — the graph is the asset; testing, docs, and AI integration are lenses on that asset. AI-assisted authoring via Claude Code + MCP is a more natural onboarding path than a code generator trying to own the full API surface.

**Open questions:**
- MCP server transport: stdio (Claude Code native) vs HTTP/SSE vs both
- OAS library: kin-openapi vs libopenapi
- Graph-OAS linking schema: how nodes reference operationIds
- How much OAS detail the MCP server exposes (just schemas? or also examples, descriptions?)

## 2026-02-09 — Task 22a: Negative Assertions (expectFailure)

**What:** Wired the existing `ExpectFailure` plan type into the engine execution loop with full lifecycle support: inverted success/failure logic, retry skip, archive recording, plan validation, and LLM schema documentation.

**Decisions:**
- ExpectFailure steps invert the success condition: matching an expected error status is a PASS; getting 2xx is a FAIL
- Outputs are NOT stored and cleanup is NOT pushed for expectFailure steps (error responses have no useful outputs, no resource was created)
- Retry is unconditionally skipped for expectFailure steps (the failure IS the expected behavior)
- Mechanical assertions still run on expectFailure steps — users can assert on error response bodies (e.g., `fieldExists: "$.error.message"`)
- Plan validation enforces: non-empty status list, all statuses >= 400, no contradicting `status: 200` assertion
- Intent `fixAssertions` defaults to the first expected failure code instead of 200 for expectFailure steps
- `FormatPlanSchema` documents the feature so the LLM knows about it

**Open questions:** None.
