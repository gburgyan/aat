# AAT Quickstart: Petstore Example

This example uses the public [Swagger Petstore API](https://petstore.swagger.io/) to demonstrate AAT's core workflow: define an API graph, write test plans, and run them. No API keys or setup required.

> **Want the full story?** See the [Petstore Walkthrough](https://gburgyan.github.io/aat/petstore-walkthrough/) on the documentation site ([source](../../docs/user/petstore-walkthrough.md)) for a detailed, line-by-line explanation of how these files fit together.

## Prerequisites

- `aat`: see [Install](https://gburgyan.github.io/aat/install/) for Homebrew, release archives, Docker, and `go install`.
- Internet access (the Petstore API is public)

In a clone of this repository you can also build `aat` from source. From the repository root (Go 1.25.7 or later, and Node.js for the web UI):

```bash
make build   # ./aat and ./aat-sandbox with the web UI; `make cli` builds ./aat alone, without Node.js
```

The commands below run that build from this directory as `../../aat`; with `aat` installed on your `PATH`, type `aat` instead.

## Run the example

From the repository root, change into `examples/petstore/`; there the project manifest (`aat-project.yaml`) auto-discovers all config files, so no extra flags are needed:

```bash
cd examples/petstore

# Run a single plan
../../aat run plan plans/create-and-verify.yaml
```

You should see output like:

```
  [1/2] createPet            200  164ms
  [2/2] getPet               200  38ms

  cleanup:
    deletePet              200  40ms

PASSED (2/2 steps, 243ms)
Archive: _output/runs/run-XXXXXXXX-XXXXXX-XXXXXXXX/archive.json
```

## Run all plans as a batch

```bash
../../aat run batch
```

This discovers and runs every plan in the `plans/` directory.

## View results in the browser

After running a plan, you can inspect the full request/response details in the web UI:

```bash
../../aat web view latest
```

This opens the most recent run in your browser, starting a local server if one is not already running (stop it with Ctrl+C). You'll see a Gantt timeline of each step, request/response bodies, headers, and assertion results. Without `latest`, it opens the list of runs.

> **Note:** The web UI requires `make build` (not plain `go build`) since it embeds the compiled Svelte frontend into the binary.

## Validate the project

Check that the graph, templates, plans, and workflows are all valid:

```bash
../../aat validate
```

## What happened

AAT executed a two-step test plan:

1. **createPet** — Sent a POST request to create a pet with a name picked from the graph's default pool (e.g. "Buddy", "Luna", "Cooper") and status "available". Verified the response returned HTTP 200 and that the template extracted a `petId` (from the response's `id` field).
2. **getPet** — Used the `petId` from step 1 to GET the pet. The data flow is wired at the graph level (`default: {from: createPet.petId}`), so neither the workflow nor the recipe needs to specify it. Verified the pet exists.
3. **Cleanup** — Because `createPet` declares `cleanup: deletePet` in the graph, AAT automatically deleted the pet after the plan completed. The cleanup step's `petId` input is matched by name to the `petId` output of `createPet` — no explicit cleanup step needed.

The full request/response details are in the archive JSON file.

## Understand the files

| File | Purpose |
|------|---------|
| `aat-project.yaml` | Project manifest — lets AAT auto-discover all config files so you don't need `--env-config`, `--graph`, `--templates` flags. |
| `graph.yaml` | Defines 4 API nodes with default values, data-flow wiring, and 2 workflow templates. This is the "map" of the API. |
| `templates/*.yaml` | HTTP request/response templates for each node. Define the method, path, headers, body, and response extraction. |
| `env.yaml` | Environment config: base URL and auth (none). |
| `plans/*.yaml` | Recipes: compact test plans that reference a workflow and add value/assertion overrides. |
| `workflows/*.yaml` | Workflow templates: reusable step patterns that recipes instantiate at runtime. |
| `domain.yaml` | Optional domain knowledge: pet name pools and concepts. The manifest loads it; `aat prompt`, `aat docs generate`, and the MCP server use it, while plan runs do not. |
| `petstore-spec.yaml` | OpenAPI spec for the 4 operations (used by `aat validate`, by runtime request and response validation, and as input to `aat generate`). |

### Recipes and workflows

Plans use the **recipe** format — a compact YAML that selects a workflow and adds overrides:

```yaml
# plans/create-and-verify.yaml (abridged)
kind: recipe
selection:
  workflow: Create and Verify
  description: "Create a pet using pool defaults and verify it was stored correctly"
overrides:
  assertions:
    createPet:
      - type: status
        expect: 200
      - type: fieldExists
        path: petId
```

At runtime, AAT reconstitutes the recipe into a full plan by composing the workflow template with the overrides. Input values like `name` come from the graph's default pool (a random pet name each run), `status` defaults to "available", and data flow between steps (e.g. `petId` from `createPet` to `getPet`) is wired at the graph level via `default: {from: ...}`. The recipe only needs to add assertions.

## Try the second plan

The second plan uses the "Search and Retrieve" workflow, which demonstrates array selection — finding a pet from a list:

```bash
../../aat run plan plans/find-and-verify.yaml
```

The workflow template defines the structural complexity (array selection with `strategy: first`, `fromSelection: pet.petId`), while the recipe just adds assertions. Input defaults and data wiring come from the graph. This is how AAT keeps plans compact even for non-trivial scenarios.

## Running without a manifest

If you prefer explicit flags, from the repository root:

```bash
./aat run plan examples/petstore/plans/create-and-verify.yaml \
  --env-config examples/petstore/env.yaml \
  --graph examples/petstore/graph.yaml \
  --templates examples/petstore/templates/
```

## Validate the graph against the OpenAPI spec

```bash
../../aat validate graph --strict
```

You can also scaffold a graph from an OpenAPI spec. With `--output-graph -` the graph is printed and no files are written:

```bash
../../aat generate --oas petstore-spec.yaml --output-graph -
```

This produces a starting-point graph that you refine with ordering (`requires`/`satisfies`), cleanup pairings, data wiring, and custom output names. See [Scaffolding from OpenAPI](../../docs/user/generate.md).

## Next steps

The guides are on the [documentation site](https://gburgyan.github.io/aat/); the links below open their sources in this repository.

- [API Graphs](../../docs/user/graphs.md) — how to define nodes, ordering, and conditions
- [Templates](../../docs/user/templates.md) — HTTP request/response template format
- [Plan Authoring](../../docs/user/plans.md) — test plan YAML schema and assertions
- [Environments](../../docs/user/environments.md) — auth config, headers, LLM setup
- [Domain Knowledge](../../docs/user/domain.md) — concepts, types, value pools
- [Value Flow](../../docs/user/value-flow.md) — expressions, selections, constraint resolution
- [Running Tests](../../docs/user/running.md) — CLI flags and run options
- [CI/CD Integration](../../docs/user/ci-cd.md) — exit codes, JSON output, pipelines
- [Archives](../../docs/user/archives.md) — what a run records and how to browse it
- [LLM-Assisted Planning](../../docs/user/prompt.md) — generating plans from prompts

## Note about the Petstore API

The Swagger Petstore is a public demo API. It may occasionally be slow, return errors, or have its data reset. If you see unexpected failures, wait a moment and try again. This does not affect AAT's functionality — it's a characteristic of the shared demo server.
