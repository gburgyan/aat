# Contributing to AAT

Thanks for your interest in contributing to Adaptive API Toolkit!

## Getting started

1. Fork the repository and clone your fork
2. Install prerequisites: Go 1.24+, Node.js 18+, Make
3. Build: `make build`
4. Run tests: `make test`

## Development workflow

1. Create a branch from `main`
2. Make your changes
3. Run `make check` (fmt + tests + lint — same as CI)
4. Submit a pull request

## What to expect from your PR

**Small changes** (bug fixes, typo corrections, small enhancements) — if the code is clean and tests pass, there's a good chance it gets merged as-is.

**Larger changes** — absolutely welcome, but set your expectations accordingly. I have opinions about how the project should grow, so I may take your idea, reshape the implementation to fit the project's direction, and credit you as a contributor. This isn't a rejection of your work — it means the idea was good enough to adopt. If you'd prefer to discuss an approach before investing significant effort, open an issue first.

**AI-assisted contributions** are completely fine. If you used an LLM to help write the code, include the prompts you used in the PR description. This helps with review and is just good transparency.

The goal here is to set honest expectations so nobody gets defensive. A PR that gets reworked before merging is still a valued contribution — the idea and initiative matter.

## Project structure

AAT is organized into focused packages with one-way dependencies. See [CLAUDE.md](CLAUDE.md) for the full package map and dependency rules.

Key entry points:
- `cmd/aat/` — CLI binary
- `engine/` — execution engine
- `intent/` — LLM-assisted plan generation
- `server/` — web UI backend
- `mcp/` — MCP server for IDE integration

## Code conventions

- Go conventions: `context.Context` first, errors as values with `%w` wrapping, no `init()` or global state
- Tests: use `testify` (assert/require), table-driven where appropriate, per-package
- Dependencies: direct struct fields and function parameters, no DI framework

## Web UI

The Svelte frontend lives in `server/web/`. For frontend development:

```bash
cd server/web && npm run dev    # Vite dev server on :5173
aat web --dev                   # Go server proxies to Vite
```

## Contributor License Agreement

By submitting a pull request, you agree to the terms of the [Contributor License Agreement](CLA.md).
