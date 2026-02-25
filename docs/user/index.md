# AAT Documentation

AAT (Adaptive API Testing) is a CLI tool that tests API workflows end-to-end. It models your API as a graph of operations, then generates and runs multi-step test plans against it.

## Quick Links by Role

### I want to run existing tests
- [Running Tests](running.md) — execute plans and batches from the command line
- [CI/CD Integration](ci-cd.md) — exit codes, JSON output, pipeline setup

### I want to create tests for my API
- [Quickstart](quickstart.md) — scaffold a graph from an OpenAPI spec and run your first test in 5 minutes
- [Tutorial](tutorial.md) — build a complete test suite from scratch
- [API Graphs](graphs.md) — define your API's operations and data model

### I want to understand a specific concept
- [Concepts Glossary](#concepts-glossary) — one-line definitions with links to full docs
- [Documentation Map](#documentation-map) — progressive reading order

### I'm an AI assistant working with AAT
- [AI Assistant Primer](llms.md) — structural reference for authoring and iterating on AAT projects
- [MCP Server](mcp-server.md) — tools and resources exposed via the Model Context Protocol
- [API Graphs](graphs.md) — the data model you'll work with most
- [Templates](templates.md) — HTTP request/response definitions

## Getting Started

### Quickstart (5 minutes)

Start here if you have an OpenAPI spec and want to see AAT in action immediately. You'll scaffold a graph, write a simple plan, and execute it — all from the command line. [Go to Quickstart](quickstart.md)

### Petstore Walkthrough (15 minutes)

Start here if you want to understand what AAT does by reading through a working example. You'll see every file in the Petstore project and learn how they compose into automated, self-cleaning API tests. [Go to Petstore Walkthrough](petstore-walkthrough.md)

### Tutorial (30 minutes)

Start here if you want to understand AAT from the ground up. You'll build a graph by hand, write templates, compose a plan, and learn how value resolution works along the way. [Go to Tutorial](tutorial.md)

## Documentation Map

### Core Guides

Progressive reading order — each builds on the previous.

| Document | What you'll learn |
|----------|-------------------|
| [Project Setup](project-setup.md) | The `aat-project.yaml` manifest, directory layout, and auto-discovery rules |
| [API Graphs](graphs.md) | Nodes, inputs, outputs, ordering, and the operation model your tests build on |
| [Templates](templates.md) | HTTP request/response YAML files, placeholders, extraction, and conditional blocks |
| [Lua Transforms](lua-transforms.md) | Post-processing extracted outputs with inline Lua scripts |
| [Environments](environments.md) | Base URLs, auth configuration, secrets, headers, and LLM settings |
| [Plans and Recipes](plans.md) | Recipes (compact format), full plans, steps, values, assertions, and layers |
| [Workflows](workflows.md) | Pre-built plan templates, addons, slots, and composition |
| [Value Resolution](value-flow.md) | How AAT resolves step inputs: literals, references, pools, selections, and expressions |
| [Domain Knowledge](domain.md) | Concepts, custom types, and value pools for test data |

### Integration & Tools

| Document | What you'll learn |
|----------|-------------------|
| [Running Tests](running.md) | `aat run plan`, `aat run batch`, output directories, and progress display |
| [CI/CD Integration](ci-cd.md) | Exit codes, `--json` output, `--quiet` mode, and pipeline examples |
| [LLM-Assisted Planning](prompt.md) | `aat prompt`, interactive confirmation, plan saving, and trace debugging |
| [Validation](validation.md) | All `aat validate` subcommands: graph, plan, workflow, and unified validation |
| [Web UI and Archives](web-ui.md) | Run archives, the Svelte web viewer, Gantt timelines, and step inspection |
| [MCP Server](mcp-server.md) | IDE AI integration via stdio transport, available tools, and resource URIs |
| [AI Assistant Primer](llms.md) | Structural reference for AI coding assistants working with AAT projects |

### Examples

| Document | What you'll learn |
|----------|-------------------|
| [Petstore Walkthrough](petstore-walkthrough.md) | A line-by-line tour of a working example: graph, templates, workflows, recipes, and how they compose |
| [Case Study: Travelport](travelport-example.md) | A real-world airline booking API with selections, addons, Lua transforms, and domain knowledge |

## Concepts Glossary

Alphabetical definitions of key AAT terms. Each links to the doc that covers it in depth.

### Addon
A workflow fragment that extends a base workflow by splicing steps at a declared insertion point. [-> workflows.md](workflows.md)

### Assertion
A post-step validation check that verifies response values meet expected conditions. [-> plans.md](plans.md)

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

### Manifest
The `aat-project.yaml` file that marks a project root and declares paths to all project artifacts. [-> project-setup.md](project-setup.md)

### Node
One API operation in the graph, with a name, adapter reference, typed inputs, and typed outputs. [-> graphs.md](graphs.md)

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
A curated list of valid values for a domain type, used by the engine when resolving inputs. [-> domain.md](domain.md)

### Workflow
A reusable plan skeleton with steps, slots, and composition rules that `aat prompt` can select and customize. [-> workflows.md](workflows.md)
