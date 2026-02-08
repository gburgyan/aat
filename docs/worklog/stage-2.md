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
