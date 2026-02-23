# Plan Authoring

Plans define test scenarios in AAT. There are two representations:

- **Recipes** — compact YAML (often 10-20 lines) that capture a workflow selection plus targeted overrides. This is the primary format.
- **Full plans** — expanded YAML with every step, value, selection, and assertion spelled out. This is what the engine actually executes.

Recipes are the recommended way to author plans. `aat prompt --save` produces them, and `aat run plan` accepts both formats. At runtime, recipes are reconstituted into full plans deterministically by replaying the composition pipeline.

## Recipes

### Quick Start

Here is a real recipe that books a round-trip flight:

```yaml
kind: recipe
metadata:
    created: 2026-02-22T10:19:48Z
    prompt: book a two leg trip
    graphVersion: 1.0.0
selection:
    workflow: Booking
    description: Book a flight using a 2-leg round-trip search and reference pricing.
    choices:
        payment: Cash
        trip-search: Round-Trip
```

That's 12 lines. It selects the `Booking` workflow, fills two slot choices (trip type and payment method), and lets everything else use defaults. At runtime, this reconstitutes into a ~10-step plan with search, pricing, traveler, payment, commit, and cleanup steps.

### Recipe Schema

A recipe has four top-level fields:

```yaml
kind: recipe              # required — distinguishes recipes from full plans

metadata:                 # optional — provenance
  created: ...
  prompt: ...
  graphVersion: ...

selection:                # required — workflow composition choices
  workflow: ...
  description: ...
  layers: [...]
  choices: {...}
  addons: [...]
  repetitions: {...}

overrides:                # optional — targeted value/assertion overrides
  values: {...}
  selections: {...}
  assertions: {...}
  descriptions: {...}
```

#### `selection` Fields

| Field | Required | Description |
|-------|----------|-------------|
| `workflow` | yes | Base workflow name (matched case-insensitively) |
| `description` | no | Human-readable description of the test scenario |
| `layers` | no | List of layer names to apply (see [Layers](#layers)) |
| `choices` | no | Slot name to option name map (see [Slots](#slots)) |
| `addons` | no | List of addon workflow names to compose in |
| `repetitions` | no | Map of node name to repeat count for multiplicity |

### Workflow and Choices

The `selection.workflow` field picks a base workflow. If the workflow defines slots (polymorphic choice points), use `selection.choices` to fill them:

```yaml
selection:
    workflow: Booking
    choices:
        trip-search: Round-Trip     # could also be One-Way, Multi-City 3-Leg, etc.
        payment: Card               # could also be Cash
```

Omitted choices use the slot's declared default. For example, if `payment` defaults to `Cash` and you omit it, cash payment is used.

### Adding Addons

Addons splice optional sub-workflows into the base at declared insertion points. List them in `selection.addons`:

```yaml
kind: recipe
metadata:
    prompt: book a trip with extras, then cancel
selection:
    workflow: Booking
    description: Book a round-trip with document overrides and seat selection, then cancel.
    choices:
        payment: Card
        trip-search: Round-Trip
    addons:
        - Document Overrides
        - Seat Selection
        - Retrieve Booking
        - Fare Rules Check
        - Cancel Booking
```

Each addon is composed into the plan at its declared `after:` insertion point. Multiple addons at the same insertion point are chained in priority order.

### Value Overrides

Use `overrides.values` to set specific input values. Keys use `stepID.inputName` format:

```yaml
kind: recipe
metadata:
    prompt: book a round trip flight from nashville to seattle
selection:
    workflow: Booking
    description: Book a round-trip from Nashville to Seattle.
    choices:
        payment: Cash
        trip-search: Round-Trip
overrides:
    values:
        searchFlights2Leg.leg1Origin: BNA
        searchFlights2Leg.leg1Destination: SEA
        searchFlights2Leg.leg2Origin: SEA
        searchFlights2Leg.leg2Destination: BNA
```

Override values replace what the graph defaults as augmented by layers would provide. Only override what you need — everything else resolves from the workflow template, graph defaults, and layers.

### Selection and Assertion Overrides

`overrides.selections` changes how array elements are picked for a named selection:

```yaml
overrides:
    selections:
        offering:
            strategy: min
            sortField: price
```

`overrides.assertions` adds assertions to specific steps:

```yaml
overrides:
    assertions:
        commitReservation:
            - type: status
              expect: 200
            - type: fieldExists
              path: locator
```

`overrides.descriptions` sets step descriptions:

```yaml
overrides:
    descriptions:
        searchFlights2Leg: "Search for round-trip flights BNA-SEA"
```

### Creating Recipes

Three ways to create recipes:

1. **`aat prompt --save`** — Generate from a natural language prompt. The LLM selects the workflow, choices, addons, and fills values. The recipe is saved to the file you specify.

   ```bash
   aat prompt --save plans/my-test.yaml \
     "book a round trip from Nashville to Seattle with a credit card"
   ```

2. **Hand-write** — Create a YAML file with `kind: recipe` and fill in the selection and overrides.

3. **Edit a saved recipe** — Start from an `aat prompt --save` output and tweak values, add addons, or change choices.

### Running Recipes

`aat run plan` auto-detects the format:

```bash
# Recipes are reconstituted into full plans at runtime
aat run plan plans/round-trip.yaml

# Full plans are executed directly
aat run plan plans/legacy-full-plan.yaml
```

## The Composition Model

Recipes are compact because the structural complexity lives in three reusable building blocks: **workflows**, **slots**, and **addons**.

### Workflows

A base workflow defines the structural skeleton of a test scenario. It is declared in the graph's `workflows` section and points to a template file:

```yaml
# In graph.yaml
workflows:
  - name: Booking
    description: "Book a flight. Choose trip type and payment method."
    template: workflows/booking-base.yaml
    slots:
      - name: trip-search
        description: "How to search for and price flights"
        options: [One-Way, Round-Trip, Multi-City 3-Leg, Multi-City 4-Leg]
        default: One-Way
      - name: payment
        description: "Form of payment"
        options: [Cash, Card]
        default: Cash
```

The template file is a plan YAML with step ordering, data flow wiring, and cleanup already defined. See [Workflow Templates](workflow-templates.md) for the full template authoring guide.

### Slots

Slots are named choice points in a base workflow. The template uses `- slot: <name>` markers where options get spliced in:

```yaml
# workflows/booking-base.yaml (simplified)
execution:
  steps:
    - node: createWorkbench

    - slot: trip-search          # <-- replaced by chosen option's steps

    - node: addTraveler
      dependsOn: [trip-search]

    - slot: payment              # <-- replaced by chosen option's steps
      dependsOn: [trip-search]

    - node: addPayment
      dependsOn: [trip-search, payment, addTraveler]

    - node: commitReservation
      dependsOn: [addPayment]
      isGoal: true

  cleanup:
    - node: ignoreWorkbench
      runOn: always
```

Each slot option is a separate workflow with `kind: slot` and its own template file:

```yaml
# In graph.yaml
- name: Round-Trip
  kind: slot
  description: "Round-trip: leg-based search with BuildNext + reference pricing"
  template: workflows/slots/trip-search/roundtrip.yaml

- name: Cash
  kind: slot
  description: "Cash form of payment"
  template: workflows/slots/payment/cash.yaml
```

When a recipe selects `choices: { trip-search: Round-Trip, payment: Cash }`, the composition pipeline replaces each `- slot:` marker with the corresponding option's steps.

### Addons

Addons are optional sub-workflows that splice into a base workflow at a declared insertion point. They are declared with `kind: addon` in graph.yaml:

```yaml
- name: Seat Selection
  kind: addon
  after: [priceOfferFullPayload, priceOfferReference]
  description: "Search seat map and add seat offer"
  template: workflows/addons/seat-selection.yaml
  wire:
    offerListIdentifier: $after.offerIdentifierValue
    offerId: $after.offerId

- name: Cancel Booking
  kind: addon
  after: commitReservation
  priority: 90
  description: "Cancel the reservation after booking"
  template: workflows/addons/cancel-addon.yaml
  wire:
    locator: commitReservation.locator
```

Key addon fields:

| Field | Description |
|-------|-------------|
| `after` | Node(s) in the base plan to splice after (string or list; first match wins) |
| `wire` | Explicit input wiring overrides. `$after.field` resolves to the matched after node. `MANUAL` leaves input for the LLM. |
| `priority` | Ordering when multiple addons share the same insertion point (lower runs first) |

Addon templates use `AUTOWIRE` for inputs that come from the base workflow. During composition, these are automatically resolved to matching outputs from upstream steps.

### How Reconstitution Works

When `aat run plan` receives a recipe, it replays the composition pipeline:

1. **Load base template** — Parse the workflow's plan YAML
2. **Fill slots** — Replace `- slot:` markers with the chosen option's steps
3. **Compose addons** — Splice each addon's steps after its `after:` node, auto-wire inputs, chain dependencies
4. **Expand repetitions** — Replicate steps for multiplicity (e.g., `addItem` x3 becomes `addItem_1`, `addItem_2`, `addItem_3`)
5. **Apply overrides** — Merge recipe's `overrides.values`, `overrides.selections`, and `overrides.assertions` into the skeleton
6. **Post-process** — Fix dependency chains, normalize selection configs, add cleanup steps
7. **Resolve layers** — Apply layer defaults between graph defaults and plan values
8. **Validate** — Structural validation against the graph

The result is a fully-wired plan ready for engine execution. This pipeline is deterministic — the same recipe always produces the same plan (given the same graph and templates).

## Layers

### What Are Layers

Layers are named sets of input default overrides. They sit between graph defaults and plan values in the resolution priority chain:

```
graph defaults  →  layer overrides  →  plan values  →  runtime resolution
```

Layers let you vary test data without duplicating recipes. For example, a `european` layer swaps airport codes to European cities, while a `near-term` layer sets departure dates to 2-5 days from today.

### Layer File Format

Each layer is a YAML file in the project's `layers/` directory:

```yaml
# layers/near-term.yaml — bare keys apply to all nodes with that input
name: near-term
description: Near-term travel dates (2-5 days out)
inputs:
  departureDate:
    pool: ["{{today + 2 days}}", "{{today + 3 days}}", "{{today + 5 days}}"]
  returnDate:
    pool: ["{{today + 9 days}}", "{{today + 10 days}}", "{{today + 12 days}}"]
```

```yaml
# layers/european.yaml — qualified keys target specific node.input pairs
name: european
description: European airport codes for intercontinental routes
inputs:
  searchFlights.origin: [CDG, LHR, FRA, AMS, FCO, MAD, BCN]
  searchFlights.destination: [CDG, LHR, FRA, AMS, FCO, MAD, BCN]
  searchFlights2Leg.leg1Origin: [CDG, LHR, FRA, AMS, FCO, MAD, BCN]
  searchFlights2Leg.leg1Destination: [CDG, LHR, FRA, AMS, FCO, MAD, BCN]
```

Layer inputs support two key formats:

- **Bare keys** (e.g., `departureDate`) — apply to every node that has an input with that name
- **Qualified keys** (e.g., `searchFlights.origin`) — target a specific node's input

Within a single layer, qualified entries take priority over bare entries for the same node. Values can be bare scalars (shorthand for a single-element pool), lists (shorthand for a pool), or the full mapping form with `value`, `pool`, `poolStrategy`, `constraint`, `from`, and `select`.

### Using Layers

**In recipes** — list layer names in `selection.layers`:

```yaml
selection:
    workflow: Booking
    layers: [european, near-term]
    choices:
        trip-search: Round-Trip
```

**Via CLI** — use the `--layer` flag (repeatable):

```bash
aat run plan plans/round-trip.yaml --layer european --layer near-term
```

Layers stack in order — later layers override earlier ones. CLI layers are appended after recipe layers.

### Layer Groups and Permutation Testing

The `--layer-group` flag enables cartesian product testing across layer sets. Each `--layer-group` defines a group of interchangeable layers, and `aat run batch` runs every combination:

```bash
aat run batch \
  --layer-group "european,international" \
  --layer-group "near-term"
```

This runs each plan in the batch twice: once with `[european, near-term]` and once with `[international, near-term]`. With multiple groups, the combinations multiply — useful for systematic coverage across regions, date ranges, and payment methods.

## Full Plan Reference

Full plans are the expanded format that the engine executes. You rarely need to write them by hand, but understanding the format helps with debugging and advanced scenarios.

### When You Need Full Plans

- **Debugging** — Inspect the reconstituted plan to understand what the engine sees
- **Edge cases** — Scenarios that don't fit the workflow/recipe model
- **Migration** — Plans written before the recipe format existed

### Schema Overview

A full plan has six top-level sections:

```yaml
metadata:        # optional — provenance information
  created: ...
  prompt: ...
  graphVersion: ...

graph: ...       # optional — path to graph file (informational)

auth:            # optional — override environment auth (see plan-auth.md)
  type: bearer
  credentials: ...

headers:         # optional — merge on top of environment headers (see plan-auth.md)
  X-Custom: value

intent:          # optional — goal and constraints
  goal: ...
  description: ...
  constraints: ...

execution:       # required — what to execute
  steps: [...]
  verification: [...]
  cleanup: [...]
```

### Execution Steps

Steps are the core of a plan. Each step targets a graph node and specifies how to resolve its inputs.

| Field | Required | Description |
|-------|----------|-------------|
| `node` | yes | Graph node to execute |
| `id` | no | Unique step identifier (defaults to `node`). Required when the same node appears multiple times. |
| `dependsOn` | no | List of step IDs that must complete first |
| `description` | no | Human-readable purpose of this step |
| `isGoal` | no | Marks this as a goal step |
| `values` | no | Input value specifications |
| `selections` | no | Named element selections from upstream arrays |
| `retry` | no | Retry configuration |
| `fallback` | no | Fallback behavior after retries |
| `assertions` | no | Response assertions |
| `expectFailure` | no | Negative test: expect specific error status codes |

Steps execute in topological order based on `dependsOn` declarations. AAT also infers ordering from graph edges — if step B uses an output from step A (via `from`), step A runs first even without an explicit `dependsOn`.

#### Step Aliasing

When the same graph node appears multiple times, give each step a unique `id`:

```yaml
steps:
  - id: add_item_1
    node: addItem
    dependsOn: [createCart]
    values:
      cartId: {from: createCart.cartId}
      productId: "prod-101"

  - id: add_item_2
    node: addItem
    dependsOn: [add_item_1]
    values:
      cartId: {from: createCart.cartId}
      productId: "prod-202"
```

All references (`dependsOn`, `from`, `fromSelection`) use step IDs (which default to node names when `id` is not set).

### Values

Each entry in `values` specifies how to resolve a node input. The simplest form is a bare scalar:

```yaml
values:
  name: "Buddy"
  status: "available"
```

For more control, use the full mapping form:

```yaml
values:
  name:
    default: "Buddy"
    pool: ["Max", "Bella", "Charlie"]
    poolStrategy: sequential
    constraint: "len(name) > 2"
  petId:
    from: createPet.petId
  offeringId:
    fromSelection: offering.offeringId
```

| Field | Description |
|-------|-------------|
| `default` | Literal value. For bare scalars (`name: "Buddy"`), this is the only field set. |
| `from` | Reference to an upstream step's output (e.g., `createPet.petId`). |
| `fromSelection` | Reference to a named selection (e.g., `offering.offeringId`). See [Named Selections](#named-selections). |
| `fromResolved` | Reference to another input's resolved value within the same step. See [Value Flow](value-flow.md#intra-step-references-fromresolved). |
| `select` | Inline selection config for picking from an array output. |
| `pool` | List of alternative values to try or pick from. |
| `poolStrategy` | How to pick from the pool: `random` (default) or `sequential`. |
| `constraint` | Predicate expression the resolved value must satisfy. |

For a deep dive into value resolution, see [Value Flow](value-flow.md).

### Named Selections

When a step needs multiple fields from the same array element, use named selections:

```yaml
- node: getPet
  dependsOn: [findByStatus]
  selections:
    pet:
      from: findByStatus.pets
      strategy: first
  values:
    petId:
      fromSelection: pet.petId
```

The `selections` block picks an element from an array output. Then `fromSelection` extracts specific fields from that element using `selectionName.fieldName`.

| Field | Required | Description |
|-------|----------|-------------|
| `from` | yes | Source array: `stepID.outputName` |
| `strategy` | no | How to pick an element (default: `first`) |
| `filter` | no | Predicate to narrow candidates before selection |
| `index` | no | Specific index for `index` strategy |
| `sortField` | no | Field to sort by for `min`/`max` strategies |
| `prompt` | no | Selection criteria for `llm` strategy |

### Selection Strategies

| Strategy | Description | Required Fields |
|----------|-------------|-----------------|
| `first` | First element (after filtering) | -- |
| `last` | Last element (after filtering) | -- |
| `random` | Random element (after filtering) | -- |
| `index` | Element at a specific position | `index` |
| `min` | Element with the smallest value of a field | `sortField` |
| `max` | Element with the largest value of a field | `sortField` |
| `match` | First element matching a filter | `filter` |
| `llm` | LLM chooses based on a prompt | `prompt` |

### Assertions

Assertions verify that a step's response meets expectations. Five mechanical types are supported:

```yaml
assertions:
  mechanical:
    - type: status
      expect: 200
    - type: fieldExists
      path: "id"
    - type: fieldEquals
      path: "name"
      expect: "Buddy"
    - type: predicate
      expr: "price > 0 && price < 10000"
    - type: schema
      ref: "PetResponse"
```

| Type | Description | Required Fields |
|------|-------------|-----------------|
| `status` | HTTP status code check | `expect` (integer) |
| `fieldExists` | JSON field presence (gjson path) | `path` |
| `fieldEquals` | JSON field value check | `path`, `expect` |
| `predicate` | Expression evaluation against response body | `expr` |
| `schema` | Schema validation (placeholder — not yet implemented) | `ref` |

Semantic assertions are free-text descriptions evaluated by the LLM:

```yaml
assertions:
  semantic:
    - "The response should contain a valid order confirmation"
```

### Negative Testing with `expectFailure`

Use `expectFailure` to assert that a step fails with specific HTTP status codes:

```yaml
- node: getPet
  dependsOn: [deletePet]
  expectFailure:
    status: [404]
    description: "Pet should not exist after deletion"
```

When `expectFailure` is set, the step **passes** if the response status matches any listed code, and **fails** on success or unexpected errors. Retries and adaptive relaxation are skipped.

### Intent and Constraints

```yaml
intent:
  goal: commitReservation
  description: "Book a flight and verify the reservation"
  constraints:
    hard:
      - type: status
        name: "all_success"
        description: "All non-negative steps must return 2xx"
    soft:
      - type: value
        name: "prefer_available"
        description: "Prefer available flights"
        applies_to: ["searchFlights.status"]
    free:
      - "Any airline is acceptable"
```

| Constraint Level | Behavior |
|------------------|----------|
| `hard` | Must be satisfied. Failure stops execution. |
| `soft` | Preferred but can be relaxed in `adaptive` mode. |
| `free` | Informational. No enforcement. |

### Retry and Fallback

```yaml
- node: listProducts
  retry:
    max: 3
    on: [server, timeout]
    failOn: [client]
  fallback:
    action: skip
```

Retry `on` categories: `server` (5xx), `client` (4xx), `timeout`, `network`. The `fallback.action` is `skip` (continue without this step) or `fail` (stop the plan).

### Verification and Cleanup Steps

**Verification steps** run after the main execution flow for post-condition checks:

```yaml
execution:
  verification:
    - node: getPet
      purpose: "Confirm pet was persisted"
      assertions:
        mechanical:
          - type: fieldEquals
            path: "name"
            expect: "Buddy"
```

**Cleanup steps** run after everything else, including on failure:

```yaml
execution:
  cleanup:
    - node: deletePet
      runOn: always    # always | failure | success
```

Both receive inputs from main execution steps via graph edges.

### Complete Example

A fully annotated full plan:

```yaml
metadata:
  created: 2026-02-11T12:00:00Z
  prompt: "Create a pet, find it, then clean up"

intent:
  goal: getPet
  description: "Create a pet, find it in the listing, retrieve by ID, then delete"
  constraints:
    hard:
      - type: status
        name: "all_success"
        description: "All non-negative steps must return 2xx"

execution:
  steps:
    - node: createPet
      description: "Create a new pet named Bella"
      values:
        name: "Bella"
        status: "available"
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldExists
            path: "id"

    - node: findByStatus
      dependsOn: [createPet]
      values:
        status: "available"

    - node: getPet
      dependsOn: [findByStatus]
      isGoal: true
      selections:
        pet:
          from: findByStatus.pets
          strategy: first
      values:
        petId:
          fromSelection: pet.petId
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldExists
            path: "name"

  cleanup:
    - node: deletePet
      runOn: always
```

## See Also

- [Workflow Templates](workflow-templates.md) -- authoring base and addon workflow templates
- [Value Flow](value-flow.md) -- expressions, constraint resolution, selection strategies in depth
- [Running Tests](running.md) -- executing plans with `aat run`
- [LLM-Assisted Planning](prompt-workflow.md) -- generating plans from prompts with `aat prompt`
- [Plan-Level Auth & Headers](plan-auth.md) -- embedding credentials and custom headers
- [Graph Authoring](graph-authoring.md) -- graph structure, workflows, and slots
