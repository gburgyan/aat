# AAT — Adaptive API Toolkit

**Model your API as a graph once. Get long-chain integration tests, layer × environment matrices, CI-ready runs, and an MCP server for AI coding tools — all from the same YAML.**

AAT describes an API as a graph of operations: what each one takes and returns, which must run before which, and which undoes which. From that graph it runs multi-step test plans that wire data between steps, check every response, clean up after themselves, and leave an archive of every request and decision. LLMs are optional and authoring-time only: `aat prompt` can draft a plan, and the MCP server teaches AI tools your API. Execution never calls an LLM.

```bash
aat-sandbox init shop && cd shop     # after installing: see Install
aat-sandbox serve &
aat run plan full-lifecycle
```

![aat run plan full-lifecycle against the shop sandbox: fifteen steps stream in with their status codes and durations, two steps retry, cleanup deletes the order and the cart, and the run passes](assets/demo-plan.gif)

## Start Here

- **[Shop example](examples/shop.md)** — watch AAT drive a realistic API in a minute, offline: an order through every state, a layer matrix, two regions, negative tests
- **[Petstore Quickstart](quickstart.md)** — go from an OpenAPI spec to a passing, self-cleaning test in five minutes
- **[MCP Server](mcp-server.md)** — give Claude Code or another MCP client your graph and the tools to write and run tests
- **[Share your API with integrators](integration-kit.md)** — package part of the project your tests use, so your integrators' AI tools learn the API from it

Install with Homebrew, a release archive, Docker, or `go install`, or build from source: see [Install](install.md).

## Getting Started

| Guide | Time | What you get |
|-------|------|--------------|
| [Install](install.md) | 2 minutes | `aat` and `aat-sandbox` from a release, Homebrew, Docker, or source |
| [Shop example](examples/shop.md) | 1 minute | A complete project running against the offline sandbox |
| [Petstore Quickstart](quickstart.md) | 5 minutes | Your first graph, templates, and plan, scaffolded from the Petstore spec |
| [Petstore Walkthrough](petstore-walkthrough.md) | 15 minutes | Every file of a small working project, explained |
| [Tutorial](tutorial.md) | 45 minutes | A project built by hand: environments, plans, workflows, recipes, layers |

## Documentation Map

### Core Guides

Progressive reading order — each builds on the previous.

| Document | What you'll learn |
|----------|-------------------|
| [Project Setup](project-setup.md) | The `aat-project.yaml` manifest, directory layout, and auto-discovery rules |
| [API Graphs](graphs.md) | Nodes, inputs, outputs, ordering, and the operation model your tests build on |
| [Templates](templates.md) | HTTP request/response YAML files, placeholders, extraction, and conditional blocks |
| [Lua Transforms](lua-transforms.md) | Post-processing responses with inline Lua scripts |
| [Environments](environments.md) | Base URLs, auth, secrets, headers, multiple environments, and per-host overrides |
| [Plans and Recipes](plans.md) | Recipes (compact format), full plans, steps, values, assertions, and layers |
| [Workflows](workflows.md) | Reusable plan templates, addons, slots, and composition |
| [Value Resolution](value-flow.md) | How AAT resolves step inputs: literals, references, pools, selections, and expressions |
| [Domain Knowledge](domain.md) | Concepts, custom types, and value pools for test data |

### Running

| Document | What you'll learn |
|----------|-------------------|
| [Running Tests](running.md) | `aat run plan`, `aat run batch`, output modes, exit codes, retries, and cleanup |
| [Matrix Testing](batch-layers.md) | Layer groups, cartesian product batches, duplicate detection, and the test matrix |
| [Local Development](local-dev.md) | Auto-discovered `.aat-overrides.yaml` for routing traffic to localhost |
| [CI/CD Integration](ci-cd.md) | Exit codes, `--json` output, `--quiet` mode, and pipeline examples |
| [Checkpoints](checkpoints.md) | Stopping after a step and handing live state to another tool |
| [Archives](archives.md) | What each run records, redaction, export and import, and pruning |
| [Web UI](web-ui.md) | Browsing runs, batches, and traces in the embedded web viewer |
| [Visualizers](visualizers.md) | Custom HTML renderers for API response data in the web UI |
| [Validation](validation.md) | All `aat validate` subcommands and the errors they report |

### AI and Integration

| Document | What you'll learn |
|----------|-------------------|
| [MCP Server](mcp-server.md) | IDE AI integration: transports, tools, resources, and personas |
| [Share Your API with Integrators](integration-kit.md) | Packaging part of your test project as a kit that integrators' AI tools learn the API from |
| [AI Assistant Primer](llms.md) | Structural reference for AI coding assistants working with AAT projects |
| [Scaffolding from OpenAPI](generate.md) | What `aat generate` writes from a spec, and what to add by hand |
| [Generating API Docs](docs-generate.md) | Markdown documentation from the graph with `aat docs generate` |
| [LLM-Assisted Planning](prompt.md) | `aat prompt`, interactive confirmation, plan saving, and trace debugging |

### Examples

| Document | What you'll learn |
|----------|-------------------|
| [Examples](examples/index.md) | The example projects and what each one shows |
| [Shop example](examples/shop.md) | The offline quick start: slots and addons, a layer matrix with dedup, `us`/`eu` environments, negative tests, checkpoints, and MCP configuration |
| [Petstore Walkthrough](petstore-walkthrough.md) | A line-by-line tour of a working example: graph, templates, workflows, recipes, and how they compose |
| [Airline case study](examples/airline-case-study.md) | The 74-operation project AAT was built for, and the features that scale relies on |

## Concepts Glossary

Alphabetical definitions of key AAT terms. Each links to the doc that covers it in depth.

### Addon
A workflow fragment that extends a base workflow by splicing steps at a declared insertion point. [-> workflows.md](workflows.md)

### Archive
The JSON record every run writes: each request and response, how each input was resolved, assertion results, and cleanup. [-> archives.md](archives.md)

### Assertion
A post-step validation check that verifies response values meet expected conditions. [-> plans.md](plans.md)

### Checkpoint
A run stopped after a named step with `--stop-after`, skipping cleanup and exporting live state (base URL, auth headers, outputs) with `--dump-state`. [-> checkpoints.md](checkpoints.md)

### Cleanup Step
A teardown step that runs after the plan completes, even on failure, to release resources. [-> plans.md](plans.md)

### Domain Knowledge
A YAML file declaring business concepts, custom types, and value pools for test data. [-> domain.md](domain.md)

### Element Field
A named, typed field declared on an array output that describes the structure of each element for selection strategies. [-> graphs.md](graphs.md)

### Environment
Runtime configuration that provides base URLs, auth credentials, static headers, secret references, and LLM settings. [-> environments.md](environments.md)

### Expression
A dynamic value placeholder like `{{today + 7 days}}` or `{{env.API_KEY}}` evaluated at execution time. [-> value-flow.md](value-flow.md)

### Graph
A YAML model of your API's operations — nodes with typed inputs and outputs, ordering rules, and error detection. [-> graphs.md](graphs.md)

### Layer
A YAML overlay that provides alternate test data for a plan without duplicating the entire plan structure. [-> plans.md](plans.md)

### Layer Group
A set of mutually exclusive layers combined via `--layer-group` to produce a cartesian product of batch permutations, with automatic duplicate detection. [-> batch-layers.md](batch-layers.md)

### Manifest
The `aat-project.yaml` file that marks a project root and declares paths to all project artifacts. [-> project-setup.md](project-setup.md)

### MCP Server
`aat mcp serve`: exposes the graph, workflows, and tools to validate and run plans to AI coding assistants over the Model Context Protocol. [-> mcp-server.md](mcp-server.md)

### Node
One API operation in the graph, with a name, adapter reference, typed inputs, and typed outputs. [-> graphs.md](graphs.md)

### Override
An environment entry that routes matching nodes (such as `payment*`) to another base URL, auth, or headers. [-> environments.md](environments.md#multi-host-routing)

### Plan
A concrete, ready-to-run test specification with ordered steps, input values, assertions, and cleanup. [-> plans.md](plans.md)

### Recipe
A compact plan format that names a workflow and provides only the value overrides, letting AAT fill in the rest. [-> plans.md](plans.md)

### Selection
Choosing one element from an array output using a strategy like `first`, `min`, `max`, or `match`. [-> value-flow.md](value-flow.md)

### Slot
A choice point in a base workflow where one of several named workflow fragments can be inserted. [-> workflows.md](workflows.md)

### Step
One operation in a plan, mapped to a graph node, with resolved input values and optional assertions. [-> plans.md](plans.md)

### Template
A YAML file defining the HTTP request shape and response extraction rules for a single graph node. [-> templates.md](templates.md)

### Value Pool
A curated list of valid values for a domain type in the domain file, used by `aat prompt`, `aat docs generate`, and the MCP tools; runs never read it (a `pool` default on an input is what varies run data). [-> domain.md](domain.md)

### Visualizer
A standalone HTML plugin that renders API response data in the web UI, turning complex reference-based JSON into readable visual displays. [-> visualizers.md](visualizers.md)

### Workflow
A reusable plan skeleton with steps, slots, and composition rules; recipes name one and state only what differs, whether you, an AI assistant through the MCP server, or `aat prompt` wrote them. [-> workflows.md](workflows.md)
