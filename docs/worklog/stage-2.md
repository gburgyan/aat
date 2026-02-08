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
