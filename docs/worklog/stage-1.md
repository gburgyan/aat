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
