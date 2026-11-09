# AAT — Adaptive API Toolkit

AAT is a Go CLI that models an API as a graph and executes long, multi-step test plans against it: automatic data wiring between steps, layers for matrix testing, multi-environment routing, run archives with a full decision trail, an embedded web UI, and an MCP server so AI coding tools can integrate with the API. Execution never calls an LLM; AI coding tools author plans through the MCP server, and `aat prompt` can draft one.

## Module

`docs/go.mod` declares an empty module so the docs site's images (`docs/user/assets`, from `make demos`) stay out of the module zip that `go install` downloads. Keep Go code out of `docs/`.

## Build System

**Before committing or pushing**, run `make check` to catch issues that CI would flag. This runs `gofmt -s` formatting, tests with the race detector, and linting — the same checks the CI/CD pipeline performs.

## Pull Requests

One feature, one PR against `main`. Don't stack PRs: when a feature is spread over a chain of PRs, no single diff shows the whole feature, which makes review harder, and every merge needs the next PR retargeted. Keep the steps reviewable as well-scoped commits inside the one PR instead. If a feature is truly too big for one PR, split it into pieces that each stand on their own and target `main`, not each other.

## Dependency Rules

Dependencies flow in one direction. No cycles. No lateral imports within a tier.

**Foundation packages** (stdlib and third-party imports only; importable from any tier): `internal/httpstatus`, `internal/yamlx`, `internal/predicate`, `internal/version`, `internal/primer`, `internal/protoreg`, `internal/grpcstatus`, `internal/gjsonpath`
**Leaf packages** (no aat imports other than foundation packages): `config`, `graph`, `domain`, `adapter`, `validate`, `internal/sandbox/shop`, `internal/sandbox/shopgrpc`
**Mid-tier**: `graph/oas` → graph; `graph/proto` → graph; `llm` → config; `plan` → graph, config; `archive` → plan; `fuzz` → graph, domain, plan, adapter
**Orchestrators**: `engine` → graph, graph/oas, adapter, plan, domain, validate, archive, config, fuzz
**Entry points**: `intent` → graph, domain, plan, llm; `mcp` → all packages; `server` → intent, archive, plan, config, adapter (for the protocol a step used)
**Binaries**: `cmd/aat` → every package outside `internal/` (its tests also import `internal/sandbox/shop` and the root embed for the shop end-to-end test); `cmd/aat-sandbox` → internal/sandbox/shop, internal/sandbox/shopgrpc, root embed

`internal/sandbox/shop` is stdlib only, and stays that way. `internal/sandbox/shopgrpc` is the one exception in the sandbox tree: it needs grpc-go and protobuf to serve a gRPC listener, and it holds no logic of its own — every call becomes the HTTP request `shop` already answers, so the two surfaces cannot drift.

Data flows down, decisions flow up. No business logic in `cmd/`.

## Dependency Injection Patterns

Cross-package dependencies use direct struct fields and function parameters — explicit, no framework. No global mutable state.

## Project Manifest

Each AAT project has an `aat-project.yaml` manifest that declares project artifacts for CLI auto-discovery. The CLI walks up from `cwd` to find it (`config.FindManifest()`), or it can be passed explicitly via `--manifest`.

The highest-priority manifest found (`--manifest`, CWD walk-up, `AAT_PROJECT`, user config `default_project`) describes the whole project; lower levels never fill in fields it leaves out.

### Multi-Environment Files

The environment file supports two formats: **single-environment** (legacy, one `apiBaseUrl` per file) and **multi-environment** (multiple named environments with shared config, inheritance via `extends`, variable substitution via `vars`, and file splitting via `include`). Format is auto-detected by presence of the `environments` top-level key. Select an environment with `--env` flag, `AAT_ENV_NAME` env var, or `defaultEnvironment` in the manifest.

## Plans vs Workflows

- **Workflows** (`workflows/` dir) — pre-written reusable templates defined in the graph YAML. Composed at runtime by `intent.Compose` (slots, then addons). Referenced by name in `WorkflowSelection`.
- **Plans** (`plans/` dir) — user-generated execution instances, typically saved from `aat prompt --save`. These are concrete, ready-to-run YAML files.

## Testing Philosophy

- Use `github.com/stretchr/testify` — `assert` for non-fatal checks, `require` for fatal preconditions
- Tests construct types with test implementations directly (no DI framework)
- Table-driven tests preferred for systematic coverage
- Closed-loop: tests verify observable behavior, not internal state
- `internal/testutil/` for shared helpers only — no test logic there

## Go Conventions

- No `init()` functions; no global mutable state. The one exception is cobra wiring in `cmd/`: command variables and their `init()` flag registration
- Decode project YAML with `yamlx.Decode`, never `yaml.Unmarshal` (a `forbidigo` rule enforces it): unknown keys must be errors. A deliberately lenient decode, such as a `kind` probe before the strict decode, carries `//nolint:forbidigo` and a reason. Custom unmarshalers use the callback form `UnmarshalYAML(func(any) error)` with `yamlx.Node` and `yamlx.KindError`, because `yaml.Node.Decode` starts a decoder that ignores unknown keys; their method-less alias types are named `raw<Type>`. A function that reads a file names the file in its errors, so callers do not add the path again

## Documentation

- **`docs/internal/`** — architecture notes (for contributors)
- **Worklog** — decision log entries (date, decisions, rationale), kept out of the public tree in the private sibling repository `../aat-worklog` (`gburgyan/aat-worklog`)
- **`docs/user/`** — user-facing docs, built into the site at https://gburgyan.github.io/aat/ (`mkdocs.yml`, Material for MkDocs; `.github/workflows/docs.yml` deploys it from main). Every page must be listed in the `mkdocs.yml` nav; links, anchors, and nav coverage are checked by `make docs`, so run it before committing doc changes. Keep pages readable on GitHub (plain Markdown); the one site-only construct is the `--8<--` snippet include in `changelog.md`, `examples/shop.md`, and `llms.md`, which pulls in `CHANGELOG.md`, the `examples/shop/README.md` section between its `[start:body]`/`[end:body]` markers, and the AI assistant primer, `internal/primer/llms.md`. The primer links to pages by site URL, which `make docs` does not check, so `internal/primer`'s tests check them; `docs/hooks/llms_txt.py` publishes the primer as `llms-full.txt` beside the static `docs/user/llms.txt` index

Record user-visible changes in `CHANGELOG.md` under *Unreleased* as tasks complete. Add worklog entries for non-trivial decisions in `../aat-worklog`. The launch roadmap lives in `LAUNCH-PLAN.md`, a local working document.

## Worklogs

When making design decisions or completing milestones, add entries to the private sibling repository `../aat-worklog` (one file per milestone, such as `launch-m8.md`); nothing under this tree. Format:

```
## YYYY-MM-DD — Summary

**What:** What was done
**Decisions:** Key choices and their rationale
**Open questions:** Anything deferred
```

## Running AAT

Build both binaries and run the offline shop example (`examples/shop`, served by `aat-sandbox`):

```bash
# Build (injects version/commit/date automatically): aat and aat-sandbox
make build
# Or without rebuilding the web UI (`aat web` then needs a frontend build):
# make cli sandbox

# Shop example against the local sandbox (no network, no credentials)
cd examples/shop/
../../aat-sandbox serve --latency 0 &   # shop API :8765, payments :8766
../../aat validate --strict
../../aat run plan full-lifecycle
../../aat run batch --oas-validate strict
../../aat run batch --layer-group shipping-standard,shipping-express --layer-group basket-gear,basket-apparel --parallel 4
../../aat run plan smoke --env eu
../../aat run plan smoke --stop-after paymentCharge --dump-state -   # state for another tool, credentials redacted
../../aat run plan smoke --var apiHost=localhost:9765                # a sandbox on other ports
../../aat web view latest
../../aat validate --strict --manifest aat-kit.yaml                  # the integration kit on its own
sh package-kit.sh                                                     # package it: _output/shop-kit/ and .tar.gz

# What CI runs against the shop (starts its own sandbox; needs curl, jq, free ports 8765/8766)
make example-shop

# The gRPC example: a cart over HTTP, then charge and refund over gRPC, against
# the same sandbox (shop :8765, payments :8766, gRPC payments :8767)
cd examples/grpc-payments/
../../aat validate --strict                # checks gRPC nodes against payments.protoset, offline
../../aat run plan charge-and-refund       # three HTTP steps, then two gRPC ones
../../aat run plan declined-card           # expectFailure naming a gRPC status
make example-grpc                          # what CI runs

# Petstore example (public API, no credentials), from the repository root
cd examples/petstore/
../../aat run plan plans/create-and-verify.yaml

# Explicit paths instead of manifest auto-discovery:
./aat run plan examples/petstore/plans/create-and-verify.yaml \
  --env-config examples/petstore/env.yaml \
  --graph examples/petstore/graph.yaml \
  --templates examples/petstore/templates/
```

The author's production-grade project (a 74-node airline API graph) lives in a separate private
repository; it is cited by numbers only in public docs. LLM config (endpoint, API key, model) comes
from the `llm:` section in the env YAML. The API key resolves from an OS environment variable via
`SecretRef`.

## Observability & Debugging

AAT has two layers of observability: **run archives** capture execution, **plan traces** capture planning.

### Run Archives (`archive/`)

Every execution writes a JSON archive to the output directory (default `_output/runs/`). Archives contain per-step request/response pairs, timing, status codes, and overall outcome. Sensitive headers are redacted.

### Plan Traces (`intent/`)

When `--trace` is passed to `aat prompt`, the `intent.Interpret()` pipeline captures every intermediate step as a JSON trace file. This is the primary tool for debugging LLM prompt engineering and plan generation issues.

## Web UI

- **Dev mode**: `cd server/web && npm run dev` (Vite on :5173) + `aat web --dev` (Go server proxies to Vite)
- **Production**: `make build` embeds compiled frontend into the Go binary

## Task planning

If a task seems too aggressive to do in one operation, push back and offer to break it down into sub-tasks. When doing this, update the implementation plan with the new information so we can have clearly defined work items.

## Current Stage

Public launch work is tracked milestone by milestone in the local `LAUNCH-PLAN.md`; feature status is in `ROADMAP.md` and `CHANGELOG.md`. The historical stage tracker is archived in `../aat-worklog/progress-archive-2026-02.md`.
