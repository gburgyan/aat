# Contributing to AAT

Thanks for your interest in contributing to Adaptive API Testing!

## Getting started

1. Fork the repository and clone your fork
2. Install prerequisites: Go 1.24+, Node.js 18+, Make
3. Build: `make build`
4. Run tests: `make test`

## Development workflow

1. Create a branch from `main`
2. Make your changes
3. Ensure `make test` passes
4. Submit a pull request

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
