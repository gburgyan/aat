# AAT Quickstart: Petstore Example

This example uses the public [Swagger Petstore v3 API](https://petstore3.swagger.io/) to demonstrate AAT's core workflow: define an API graph, write a test plan, and run it. No API keys or setup required.

## Prerequisites

- Go 1.24 or later
- Internet access (the Petstore API is public)

## Build AAT

From the repository root:

```bash
go build -o aat ./cmd/aat/
```

## Run the example

```bash
./aat run \
  --plan examples/petstore/plan.yaml \
  --env examples/petstore/env.yaml \
  --graph examples/petstore/graph.yaml \
  --templates examples/petstore/templates/
```

You should see output like:

```
  [1/2] createPet            200  312ms
  [2/2] getPet               200  89ms

  cleanup:
    deletePet              200  45ms

PASSED (2/2 steps, 446ms)
Archive: runs/run-XXXXXXXX-XXXXXX-XXXXXXXX/archive.json
```

## What happened

AAT executed a two-step test plan:

1. **createPet** — Sent a POST request to create a pet named "Buddy" with status "available". Verified the response returned HTTP 200 and included an `id` field.
2. **getPet** — Used the `petId` from step 1 (passed automatically via the graph edge) to GET the pet. Verified the name and status match what we sent.
3. **Cleanup** — Because `createPet` declares `cleanup: deletePet` in the graph, AAT automatically deleted the pet after the plan completed. No explicit cleanup step needed.

The full request/response details are in the archive JSON file.

## Understand the files

| File | Purpose |
|------|---------|
| `graph.yaml` | Defines 4 API nodes (createPet, getPet, findByStatus, deletePet) and the data edges between them. This is the "map" of the API. |
| `templates/*.yaml` | HTTP request/response templates for each node. Define the method, path, headers, body, and response extraction. |
| `env.yaml` | Environment config: base URL, auth (none), retry settings. |
| `plan.yaml` | Test plan: which steps to execute, input values, and assertions to check. |
| `domain.yaml` | Optional domain knowledge: pet name pools and concepts (used with `--domain` flag). |
| `petstore-spec.yaml` | OpenAPI spec for the 4 operations (used for graph validation and scaffold generation). |

### Data flow

The graph defines edges that carry data between steps automatically:

```
createPet ──petId──→ getPet
    │
    │petId
    ↓
deletePet (cleanup)
```

When `createPet` runs, AAT extracts `petId` from the response. When `getPet` runs, AAT resolves its `petId` input by following the edge back to `createPet`'s output. No manual wiring needed in the plan.

## Try the second plan

The second plan demonstrates array selection — finding a pet from a list:

```bash
./aat run \
  --plan examples/petstore/plan-find-and-verify.yaml \
  --env examples/petstore/env.yaml \
  --graph examples/petstore/graph.yaml \
  --templates examples/petstore/templates/
```

This plan:

1. **createPet** — Creates a pet named "Bella"
2. **findByStatus** — Searches for all pets with status "available" (returns an array)
3. **getPet** — Selects the first pet from the array and retrieves it by ID
4. **deletePet** — Explicitly deletes the pet (showing the manual cleanup alternative)

The `getPet` step uses a `selections` block with `strategy: first` to pick an element from the array output, then `fromSelection: pet.petId` to extract the pet's ID. This is how AAT handles list endpoints.

## Validate the graph

Check that the graph structure is valid and consistent with the OpenAPI spec:

```bash
./aat graph validate --graph examples/petstore/graph.yaml
```

You can also scaffold a graph from an OpenAPI spec:

```bash
./aat generate --oas examples/petstore/petstore-spec.yaml --output-graph -
```

This produces a starting-point graph that you can refine with edges and custom output names.

## Next steps

- [Graph Authoring Guide](../../docs/user/graph-authoring.md) — how to define nodes, edges, and conditions
- [Templates](../../docs/user/templates.md) — HTTP request/response template format
- [Plan Authoring](../../docs/user/plan-authoring.md) — test plan YAML schema and assertions
- [Environments](../../docs/user/environments.md) — auth config, headers, LLM setup
- [Domain Knowledge](../../docs/user/domain-knowledge.md) — concepts, types, value pools
- [Value Flow](../../docs/user/value-flow.md) — expressions, selections, constraint resolution
- [Running Tests](../../docs/user/running.md) — CLI flags, CI/CD mode, archives
- [LLM-Assisted Planning](../../docs/user/prompt-workflow.md) — generating plans from prompts

## Note about the Petstore API

The Swagger Petstore is a public demo API. It may occasionally be slow, return errors, or have its data reset. If you see unexpected failures, wait a moment and try again. This does not affect AAT's functionality — it's a characteristic of the shared demo server.
