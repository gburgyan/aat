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
