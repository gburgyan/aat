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
