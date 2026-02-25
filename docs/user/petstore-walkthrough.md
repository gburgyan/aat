# The Petstore Walkthrough

Trace from a 21-line recipe to a fully automated, self-cleaning API test.

This walkthrough takes you through every file in the [Petstore example](../../examples/petstore/) and explains how they compose into working tests. By the end, you will understand how AAT models an API, wires data between steps, runs cleanup automatically, and keeps test plans short.

**Prerequisites:** Go 1.24+, internet access (the Petstore API is public).

**Time:** ~15 minutes to read in full. Pass 1 is 2 minutes if you just want to run something.

> If you would rather scaffold your own project from an OpenAPI spec, skip to the [Quickstart](quickstart.md) instead.

---

## Pass 1: Run It and See

### Build and run

From the repository root:

```bash
make build
cd examples/petstore
../../aat run plan plans/create-and-verify.yaml
```

You should see something like:

```
  [1/2] createPet            200  312ms
  [2/2] getPet               200  89ms

  cleanup:
    deletePet              200  45ms

PASSED (2/2 steps, 446ms)
Archive: _output/runs/run-XXXXXXXX-XXXXXX-XXXXXXXX/archive.json
```

That is the entire test. Two API calls, both passed, and a cleanup step deleted the pet afterward.

Here is the thing: you did not write cleanup logic. You did not wire the pet ID between steps. You did not pick a pet name. AAT did all of that. The recipe that drove this run is 21 lines of YAML, and most of those are assertions.

### Run the second plan

```bash
../../aat run plan plans/find-and-verify.yaml
```

This one searches for pets by status, picks the first result from the array, and retrieves it by ID. Array selection, driven by three lines in a workflow template.

### Run everything as a batch

```bash
../../aat run batch
```

This discovers and runs every plan in the `plans/` directory. Add `--json --quiet` for CI pipelines.

### Inspect the results

Every run writes a JSON archive to the `_output/runs/` directory (configured by `archives` in the manifest). You can browse the full request/response details in the web UI:

```bash
../../aat web view
```

This opens the most recent run in your browser with a Gantt timeline of each step, request and response bodies, headers, and assertion results.

### What you are about to learn

The rest of this walkthrough traces through five layers that made those runs work:

1. **The manifest** tells AAT where everything lives.
2. **The environment** provides the base URL and auth.
3. **The graph** models the API: operations, data types, defaults, data flow, and cleanup.
4. **Templates** translate between the graph's semantic names and actual HTTP requests.
5. **Workflows and recipes** compose the above into executable tests.

Each layer does one job and delegates the rest. That is how a 21-line recipe produces a self-cleaning, data-wired, multi-step API test.

---

## The Project Manifest (`aat-project.yaml`)

```yaml
name: petstore
description: Swagger Petstore API quickstart example
tags: [quickstart, petstore]
graph: graph.yaml
templates: templates/
domain: domain.yaml
environment: env.yaml
workflows: workflows/
plans: plans/
archives: _output/runs
```

The manifest marks the project root and tells AAT where to find every artifact. When you run a command from this directory (or any subdirectory), AAT walks up the file tree until it finds `aat-project.yaml` and resolves all paths relative to it.

This is why `../../aat run plan plans/create-and-verify.yaml` works without `--env`, `--graph`, or `--templates` flags. The manifest supplies all of them.

Each field maps to one file or directory:

| Field | Points to | Purpose |
|-------|-----------|---------|
| `graph` | `graph.yaml` | The API operation model |
| `templates` | `templates/` | HTTP request/response definitions |
| `domain` | `domain.yaml` | Optional domain knowledge (value pools, concepts) |
| `environment` | `env.yaml` | Base URL, auth, settings |
| `workflows` | `workflows/` | Reusable plan skeletons |
| `plans` | `plans/` | Concrete test recipes |
| `archives` | `_output/runs` | Where run results are written |

> **Further reading:** [Project Setup](project-setup.md)

---

## The Environment (`env.yaml`)

```yaml
environment: petstore
apiBaseUrl: https://petstore.swagger.io/v2

auth:
  type: none

settings:
  defaultRetries: 1
```

The environment file is the only file that changes between environments (dev, staging, production). It provides:

- **`apiBaseUrl`** — prepended to every template's request path.
- **`auth`** — authentication configuration. The Petstore API is public, so `type: none`. Real APIs would use `bearer`, `apiKey`, `oauth2`, or `custom` here.
- **`settings`** — runtime defaults. `defaultRetries: 1` means AAT retries each step once on transient failure.

Everything else (the graph, templates, workflows, recipes) stays the same across environments.

> **Further reading:** [Environments](environments.md)

---

## The Graph (`graph.yaml`) — The Heart

The graph is where the real work happens. It models your API as a set of **nodes** (operations) with typed **inputs** and **outputs**, default values, data-flow wiring, and cleanup declarations. Here is the full file, then we will walk through each node.

```yaml
version: "1.0.0"

oas: petstore-spec.yaml

nodes:
  createPet:
    description: "Create a new pet in the store"
    adapter: createPet
    oas:
      operationId: addPet
    inputs:
      - name: name
        type: string
        description: "Name of the pet"
        default:
          pool: [Buddy, Max, Bella, Charlie, Luna, Cooper, Daisy, Rocky]
      - name: status
        type: enum[available, pending, sold]
        description: "Availability status"
        optional: true
        default: available
    outputs:
      - name: petId
        type: integer
        description: "Assigned pet ID"
      - name: petName
        type: string
        description: "Pet name as stored"
      - name: petStatus
        type: string
        description: "Pet status as stored"
    cleanup: deletePet

  getPet:
    description: "Retrieve a pet by its ID"
    adapter: getPet
    oas:
      operationId: getPetById
    inputs:
      - name: petId
        type: integer
        description: "Pet ID to look up"
        default:
          from: createPet.petId
    outputs:
      - name: petId
        type: integer
        description: "Pet ID"
      - name: petName
        type: string
        description: "Pet name"
      - name: petStatus
        type: string
        description: "Pet status"

  findByStatus:
    description: "Find pets by their availability status"
    adapter: findByStatus
    oas:
      operationId: findPetsByStatus
    inputs:
      - name: status
        type: enum[available, pending, sold]
        description: "Status to filter by"
        default: available
    outputs:
      - name: pets
        type: pet[]
        description: "List of matching pets"
        elementFields:
          - name: petId
            type: integer
          - name: petName
            type: string
          - name: petStatus
            type: string

  deletePet:
    description: "Delete a pet from the store"
    adapter: deletePet
    oas:
      operationId: deletePet
    inputs:
      - name: petId
        type: integer
        description: "Pet ID to delete"
        default:
          from: createPet.petId
    outputs: []

workflows:
  - name: Create and Verify
    description: "Create a pet and verify it was stored correctly"
    template: workflows/create-and-verify.yaml

  - name: Search and Retrieve
    description: "Find pets by status and retrieve one by ID"
    template: workflows/search-and-retrieve.yaml
```

That is 98 lines. Let's break it down.

### `createPet` — defaults and cleanup

```yaml
createPet:
  inputs:
    - name: name
      type: string
      default:
        pool: [Buddy, Max, Bella, Charlie, Luna, Cooper, Daisy, Rocky]
    - name: status
      type: enum[available, pending, sold]
      optional: true
      default: available
  outputs:
    - name: petId
      type: integer
    - name: petName
      type: string
    - name: petStatus
      type: string
  cleanup: deletePet
```

Two things to notice:

**Default values.** The `name` input has a `pool` default — AAT picks a random name from the list each run. The `status` input has a literal default of `"available"`. Neither the workflow nor the recipe needs to specify these values. They only override when they want something specific.

**Cleanup declaration.** `cleanup: deletePet` tells AAT: "whenever a plan runs `createPet`, schedule a `deletePet` call after the plan finishes, even if a step fails." The cleanup step's own `petId` input resolves automatically (see `deletePet` below). You declare cleanup once in the graph, and every plan that creates a pet inherits it for free.

### `getPet` — data flow

```yaml
getPet:
  inputs:
    - name: petId
      type: integer
      default:
        from: createPet.petId
```

The `from` default says: "if no one provides a `petId`, take it from `createPet`'s `petId` output." This is data-flow wiring at the graph level. When a plan has a `createPet` step followed by a `getPet` step, the pet ID flows between them automatically.

The recipe does not mention `petId` at all. The workflow does not mention it either. The graph handles it.

### `findByStatus` — arrays and element fields

```yaml
findByStatus:
  outputs:
    - name: pets
      type: pet[]
      elementFields:
        - name: petId
          type: integer
        - name: petName
          type: string
        - name: petStatus
          type: string
```

The `type: pet[]` marks this output as an array. The `elementFields` describe the shape of each element in that array — what fields exist and what types they have.

This metadata is what makes selection strategies possible. When a workflow says "take the first pet from this list and use its `petId`," AAT knows how to reach into each array element and extract the right field. You will see this in action in the Search and Retrieve workflow.

### `deletePet` — cleanup target

```yaml
deletePet:
  inputs:
    - name: petId
      type: integer
      default:
        from: createPet.petId
  outputs: []
```

Simple node, but notice the `default: {from: createPet.petId}` — same wiring as `getPet`. When AAT runs this as a cleanup step after `createPet`, the pet ID resolves from the step that created the pet. No explicit wiring needed in the recipe.

### Workflows section

```yaml
workflows:
  - name: Create and Verify
    description: "Create a pet and verify it was stored correctly"
    template: workflows/create-and-verify.yaml

  - name: Search and Retrieve
    description: "Find pets by status and retrieve one by ID"
    template: workflows/search-and-retrieve.yaml
```

This registers two workflows by name. Recipes reference them by name (e.g., `workflow: Create and Verify`), and AAT loads the corresponding template file. The names are for humans; the template paths are for AAT.

> **Further reading:** [API Graphs](graphs.md), [Value Resolution](value-flow.md)

---

## Templates (`templates/*.yaml`)

Templates are the translation layer between the graph's semantic names and actual HTTP requests. Each node in the graph has a matching template that defines the method, path, headers, body, and response extraction rules.

### `createPet.yaml` — POST with extraction

```yaml
adapter: createPet
protocol: http

request:
  method: POST
  path: /pet
  headers:
    Content-Type: application/json
    Accept: application/json
  body: |
    {
      "name": "{{name}}",
      "status": "{{status}}",
      "photoUrls": []
    }

response:
  extract:
    petId: "id"
    petName: "name"
    petStatus: "status"
```

The `{{name}}` and `{{status}}` placeholders correspond to the graph node's input names. At execution time, AAT resolves those inputs (from defaults, overrides, or references) and substitutes them into the template.

The `response.extract` section maps API response field names to graph output names. The Petstore API returns `"id"` in its JSON; the graph calls it `petId`. This renaming happens here, so the rest of the system works with consistent semantic names regardless of the API's raw field names.

The `path` is relative — AAT prepends the environment's `apiBaseUrl` to produce `https://petstore.swagger.io/v2/pet`.

### `getPet.yaml` — path parameter

```yaml
adapter: getPet
protocol: http

request:
  method: GET
  path: /pet/{{petId}}
  headers:
    Accept: application/json

response:
  extract:
    petId: "id"
    petName: "name"
    petStatus: "status"
```

The `{{petId}}` placeholder appears directly in the URL path. AAT substitutes the resolved value (e.g., `/pet/12345`). Same extraction rules as `createPet` — the API returns the same shape.

### `findByStatus.yaml` — array extraction

```yaml
adapter: findByStatus
protocol: http

request:
  method: GET
  path: /pet/findByStatus?status={{status}}
  headers:
    Accept: application/json

response:
  extract:
    pets:
      path: "$"
      fields:
        petId: "id"
        petName: "name"
        petStatus: "status"
```

This template returns an array. The `path: "$"` means "the entire response body is the array" (the Petstore API returns a top-level JSON array). The `fields` block maps each element's API field names to the graph's `elementField` names — same renaming as before, but applied to every element in the array.

After extraction, the `pets` output contains a structured array where each element has `petId`, `petName`, and `petStatus` fields. Selection strategies can then pick elements from this array by those names.

### `deletePet.yaml` — minimal

```yaml
adapter: deletePet
protocol: http

request:
  method: DELETE
  path: /pet/{{petId}}
  headers:
    Accept: application/json

response: {}
```

No extraction — the delete endpoint does not return useful data. The empty `response: {}` tells AAT there is nothing to capture.

> **Further reading:** [Templates](templates.md)

---

## Workflows (`workflows/*.yaml`)

A workflow is a reusable plan skeleton. It defines what steps run, in what order, with what dependencies. It delegates everything else — input values, cleanup, assertions — to the graph and the recipe.

### `create-and-verify.yaml` — two steps

```yaml
intent:
  description: "Create a pet and verify it was stored correctly"

execution:
  steps:
    - node: createPet
      description: "Create a new pet"

    - node: getPet
      dependsOn: [createPet]
      isGoal: true
      description: "Verify the pet exists with correct data"
```

Thirteen lines. The workflow says:

1. Run `createPet`.
2. Then run `getPet` (it depends on `createPet`).
3. `getPet` is the goal step — the one whose success means the test passed.

Notice what the workflow does not say:

- What name to use for the pet. (The graph's pool default handles it.)
- What status to use. (The graph's literal default handles it.)
- How to pass the pet ID from step 1 to step 2. (The graph's `from` default handles it.)
- What to clean up afterward. (The graph's `cleanup: deletePet` handles it.)

The workflow only defines structure. Everything else is inherited.

### `search-and-retrieve.yaml` — array selection

```yaml
intent:
  description: "Find pets by status and retrieve one by ID"

execution:
  steps:
    - node: findByStatus
      description: "Search for pets by status"

    - node: getPet
      dependsOn: [findByStatus]
      isGoal: true
      description: "Retrieve the first matching pet by ID"
      selections:
        pet:
          from: findByStatus.pets
          strategy: first
      values:
        petId:
          fromSelection: pet.petId
```

This workflow introduces selections. The `getPet` step needs a `petId`, but there is no `createPet` to provide one. Instead, it selects from the array returned by `findByStatus`:

- **`selections.pet`** — defines a selection named `pet`. It takes from `findByStatus.pets` (the array output) using `strategy: first` (pick the first element).
- **`values.petId`** — sets `petId` to `pet.petId`, which is the `petId` element field from the selected array element.

Three lines turn "search returns many results" into "use one specific result's ID." The selection strategies available (`first`, `last`, `min`, `max`, `match`, `random`) cover most real-world scenarios, and they are declared in the workflow, not coded in a script.

> **Further reading:** [Workflows](workflows.md), [Value Resolution](value-flow.md)

---

## Recipes (`plans/*.yaml`) — The Payoff

A recipe is what pulls a workflow together into a specific test. It selects a workflow, then customizes it: overriding input values, adding assertions, enabling optional features, or exercising atypical paths. The Petstore recipes are simple (just assertions), but recipes in larger projects can activate addons to splice extra steps into a workflow, fill slots to choose between workflow variants, or apply layers to swap in alternate test data — all without duplicating the workflow itself. The recipe is the minimal representation of what you want to test.

### `create-and-verify.yaml`

```yaml
kind: recipe
metadata:
  created: 2026-02-11T12:00:00Z
  prompt: "Create a pet and verify it exists"
  graphVersion: "1.0.0"
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
    getPet:
      - type: status
        expect: 200
      - type: fieldExists
        path: petName
```

Twenty-one lines. This is the entire test.

The `selection` block picks the "Create and Verify" workflow. The `overrides` block adds assertions: verify that `createPet` returns HTTP 200 and includes a `petId` field, and that `getPet` returns 200 and includes a `petName` field.

That is it. No step definitions, no input values, no data wiring, no cleanup logic. The workflow provides the step structure. The graph provides input defaults, data flow, and cleanup. The recipe customizes the workflow for this particular test — here, by adding assertions. In a larger project, the same workflow could back dozens of recipes, each exercising a different scenario.

### `find-and-verify.yaml`

```yaml
kind: recipe
metadata:
  created: 2026-02-11T12:00:00Z
  prompt: "Find pets by status and retrieve one"
  graphVersion: "1.0.0"
selection:
  workflow: Search and Retrieve
  description: "Search for available pets and retrieve the first match by ID"
overrides:
  assertions:
    findByStatus:
      - type: status
        expect: 200
    getPet:
      - type: status
        expect: 200
      - type: fieldExists
        path: petName
```

Even the array selection complexity is invisible at the recipe level. The workflow handles the selection logic. The recipe customizes to this test's needs.

These Petstore recipes only use assertion overrides, but the recipe format supports much more: addons that splice extra steps into a workflow, slots that choose between workflow variants, and layers that swap in alternate test data sets. All of these compose at runtime without duplicating the underlying workflow.

> **Further reading:** [Plans and Recipes](plans.md) — covers addons, slots, layers, and the full override syntax

---

## Seeing It All Together

Here is what happens when you run `../../aat run plan plans/create-and-verify.yaml`, traced through every layer:

**1. Discovery.** AAT finds `aat-project.yaml` in the current directory and resolves all paths: graph, templates, environment.

**2. Recipe expansion.** The recipe says `workflow: Create and Verify`. AAT loads `workflows/create-and-verify.yaml` and merges the recipe's assertion overrides into the workflow's step structure, producing a full plan with two steps.

**3. Input resolution for `createPet`.** The step has two inputs: `name` and `status`. Neither the recipe nor the workflow provides values, so AAT falls back to the graph defaults. `name` picks a random value from the pool (say, "Cooper"). `status` uses the literal default "available".

**4. Template filling for `createPet`.** AAT loads `templates/createPet.yaml`, substitutes `{{name}}` with "Cooper" and `{{status}}` with "available", prepends the base URL, and sends the POST request.

**5. Extraction.** The API responds with `{"id": 12345, "name": "Cooper", "status": "available", ...}`. The template's `response.extract` maps `id` to `petId`, `name` to `petName`, and `status` to `petStatus`. These values enter the execution state.

**6. Assertions for `createPet`.** AAT checks: status is 200 (pass), `petId` field exists (pass).

**7. Input resolution for `getPet`.** The step needs `petId`. The graph default says `from: createPet.petId`. AAT looks up `petId` from the `createPet` step's extracted outputs — it is 12345.

**8. Template filling, execution, and assertions for `getPet`.** Same flow: load template, substitute `{{petId}}` with 12345, send GET to `/pet/12345`, extract, check assertions.

**9. Cleanup.** Because `createPet` declares `cleanup: deletePet` in the graph, AAT scheduled a cleanup step when `createPet` executed. Now that the plan is done (whether it passed or failed), AAT runs `deletePet`. Its `petId` input resolves via the same graph default (`from: createPet.petId`) — 12345. The pet is deleted.

Nine things happened, and you configured exactly three of them: the workflow name, and two sets of assertions. The graph defined the operations, defaults, data flow, and cleanup. The templates handled HTTP translation. The workflow defined step ordering. The recipe added the test criteria.

---

## What You Can Do Next

- **Validate the project:** `aat validate` checks graphs, templates, plans, and workflows for consistency. See [Validation](validation.md).

- **Browse results in the web UI:** `aat web view` opens the most recent run with a Gantt timeline and request/response details. See [Web UI and Archives](web-ui.md).

- **Run in CI/CD:** Add `--json --quiet` to get machine-readable output and clean exit codes. See [CI/CD Integration](ci-cd.md).

- **Build your own project:** The [Quickstart](quickstart.md) scaffolds a graph from an OpenAPI spec in 5 minutes. The [Tutorial](tutorial.md) builds everything from scratch.

- **Generate plans with an LLM:** `aat prompt "create a pet and verify it"` uses an LLM to generate a plan from a natural-language description. See [LLM-Assisted Planning](prompt.md).
