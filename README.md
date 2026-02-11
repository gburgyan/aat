# AAT — Adaptive API Testing

AAT is a CLI tool that tests API workflows end-to-end. You define your API as a graph of operations, write test plans that traverse it, and AAT handles the execution — resolving data dependencies between steps, running assertions, and producing detailed archives of every request and response.

With an LLM configured, AAT can also generate test plans from natural language prompts and intelligently select values when explicit data isn't available.

## Quick start

```bash
# Build
go build -o aat ./cmd/aat/

# Run the Petstore example (no API keys needed)
./aat run \
  --plan examples/petstore/plan.yaml \
  --env examples/petstore/env.yaml \
  --graph examples/petstore/graph.yaml \
  --templates examples/petstore/templates/
```

See the [Petstore quickstart tutorial](examples/petstore/README.md) for a full walkthrough, or follow the [Getting Started guide](docs/user/getting-started.md) to set up AAT for your own API.

## How it works

AAT uses four file types to describe and execute API tests:

| File | Purpose |
|------|---------|
| **Graph** (`graph.yaml`) | Defines API operations as nodes with typed inputs/outputs, connected by data-flow edges. This is the "map" of your API. |
| **Templates** (`templates/*.yaml`) | HTTP request/response templates for each operation. Define method, path, headers, body, and response extraction rules. |
| **Environment** (`env.yaml`) | Connection details: base URL, authentication, retry settings. Swap environments to test staging vs production. |
| **Plan** (`plan.yaml`) | Test scenario: which steps to run, input values, and assertions. Can be hand-written or generated from a prompt. |

Data flows automatically between steps via graph edges. When step A produces an output that step B needs, AAT resolves it without manual wiring in the plan.

## Commands

| Command | Description |
|---------|-------------|
| `aat run --plan <file>` | Execute a test plan against a live API |
| `aat prompt "<text>"` | Generate a test plan from a natural language prompt (requires LLM config) |
| `aat generate --oas <spec>` | Scaffold a graph and templates from an OpenAPI spec |
| `aat graph validate --graph <file>` | Validate graph structure and OAS alignment |
| `aat docs generate --graph <file>` | Generate Markdown documentation from a graph |
| `aat mcp serve` | Start the MCP server for IDE integration |

### CI/CD mode

```bash
./aat run --plan plan.yaml --env env.yaml --graph graph.yaml --templates tpl/ \
  --json --quiet
```

- Exit code 0 = all assertions passed
- Exit code 1 = test failure
- Exit code 2 = infrastructure error
- `--json` outputs a machine-readable summary to stdout
- `--quiet` suppresses progress output

## Building

Requires Go 1.24 or later:

```bash
go build -o aat ./cmd/aat/
go test ./...
```

## Documentation

- [Getting Started](docs/user/getting-started.md) — install AAT and set it up for your own API
- [Petstore Quickstart](examples/petstore/README.md) — runnable example with no setup
- [Graph Authoring](docs/user/graph-authoring.md) — nodes, edges, conditions, OAS linking
- [Templates](docs/user/templates.md) — HTTP request/response template format
- [Plan Authoring](docs/user/plan-authoring.md) — test plan YAML schema and assertions
- [Environments](docs/user/environments.md) — auth, headers, LLM configuration
- [Domain Knowledge](docs/user/domain-knowledge.md) — concepts, types, value pools
- [Value Flow](docs/user/value-flow.md) — expressions, selections, constraints, resolution hierarchy
- [Running Tests](docs/user/running.md) — CLI usage, archives, CI/CD integration
- [LLM-Assisted Planning](docs/user/prompt-workflow.md) — generating plans from natural language
- [MCP Server](docs/user/mcp-server.md) — IDE integration for AI-assisted workflows

## Status

AAT is in active development (Stage 2: Intelligence). The core engine, graph model, plan execution, validation, archiving, LLM-assisted planning, and MCP server are complete. See `docs/internal/progress.md` for detailed status.

## License

Not yet determined.
