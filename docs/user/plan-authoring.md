# Plan Authoring

A plan defines a test scenario: which API operations to execute, what input values to use, and what to assert about the responses. Plans can be hand-written in YAML or generated from a natural language prompt with `aat prompt`.

## Quick Start

A minimal two-step plan:

```yaml
intent:
  goal: getPet
  description: "Create a pet and verify it exists"

execution:
  steps:
    - node: createPet
      values:
        name: "Buddy"
        status: "available"
      assertions:
        mechanical:
          - type: status
            expect: 200

    - node: getPet
      dependsOn: [createPet]
      isGoal: true
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldEquals
            path: "name"
            expect: "Buddy"
```

## Schema Overview

A plan has six top-level sections:

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

## Metadata

Metadata records when and how the plan was created. It's informational and doesn't affect execution.

```yaml
metadata:
  created: 2026-02-11T12:00:00Z
  prompt: "Create a pet and verify it exists"
  graphVersion: "1.0.0"
```

| Field | Type | Description |
|-------|------|-------------|
| `created` | timestamp | When the plan was created |
| `prompt` | string | The natural language prompt that generated this plan (if any) |
| `graphVersion` | string | Version of the graph this plan was designed for |

## Intent

Intent describes the high-level goal. It guides LLM-assisted plan generation and serves as documentation for human readers.

```yaml
intent:
  goal: getPet
  description: "Create a new pet, then verify it was stored correctly"
  constraints:
    hard:
      - type: status
        name: "success"
        description: "All steps must return 2xx"
    soft:
      - type: value
        name: "prefer_available"
        description: "Prefer available pets"
        applies_to: ["createPet.status"]
    free:
      - "Any pet name is acceptable"
```

| Field | Description |
|-------|-------------|
| `goal` | The graph node that represents the test's primary objective |
| `description` | Human-readable description of what the plan tests |
| `constraints` | Classified requirements (see below) |

### Constraints

Constraints are classified by enforcement level:

| Level | Behavior |
|-------|----------|
| `hard` | Must be satisfied. Failure stops execution. |
| `soft` | Preferred but can be relaxed in `adaptive` mode if they cause failures. |
| `free` | Informational. No enforcement. |

Each hard/soft constraint has:

| Field | Description |
|-------|-------------|
| `type` | Constraint category (e.g., `status`, `value`, `timing`) |
| `name` | Short identifier |
| `description` | What the constraint requires |
| `applies_to` | List of `node.input` references this constraint targets |

## Execution Steps

Steps are the core of a plan. Each step targets a graph node and specifies how to resolve its inputs.

```yaml
execution:
  steps:
    - node: createPet
      description: "Create a new pet"
      values:
        name: "Buddy"
        status: "available"
      assertions:
        mechanical:
          - type: status
            expect: 200
```

### Step Fields

| Field | Required | Description |
|-------|----------|-------------|
| `node` | yes | Graph node to execute |
| `id` | no | Unique step identifier (defaults to `node` if omitted). Required when the same node appears multiple times. |
| `dependsOn` | no | List of step IDs that must complete first |
| `description` | no | Human-readable purpose of this step |
| `isGoal` | no | Marks this as a goal step (used by plan validation) |
| `values` | no | Input value specifications |
| `selections` | no | Named element selections from upstream arrays |
| `retry` | no | Retry configuration |
| `fallback` | no | Fallback behavior after retries are exhausted |
| `assertions` | no | Response assertions |
| `expectFailure` | no | Negative test: expect specific error status codes |

### Step Ordering with `dependsOn`

Steps execute in topological order based on `dependsOn` declarations. Steps without dependencies run first. AAT also infers ordering from graph edges -- if step B uses an output from step A (via an edge), step A runs first even without an explicit `dependsOn`.

The `dependsOn` list uses **step IDs**, not node names. When a step has no explicit `id`, its step ID is the same as its `node` name, so simple plans work as expected:

```yaml
steps:
  - node: createPet
    # step ID is "createPet" (same as node)

  - node: getPet
    dependsOn: [createPet]
    # runs after createPet completes
```

When using step aliasing (see below), `dependsOn` must reference the explicit `id`:

```yaml
steps:
  - id: add_item_1
    node: addItem
    values:
      productId: "prod-101"
      quantity: 2

  - id: add_item_2
    node: addItem
    dependsOn: [add_item_1]
    values:
      productId: "prod-202"
      quantity: 1
```

## Step Aliasing

By default, each step's identity is its `node` name, and each node can only appear once. **Step aliasing** lets you execute the same graph node multiple times by giving each step a unique `id`:

```yaml
steps:
  - id: add_item_1
    node: addItem
    dependsOn: [createCart]
    values:
      productId: "prod-101"
      quantity: 2

  - id: add_item_2
    node: addItem
    dependsOn: [add_item_1]
    values:
      productId: "prod-202"
      quantity: 1
```

### When to Use Step Aliasing

- **Multiple items**: Call `addItem` once per product in a cart
- **Multi-step searches**: Call `searchProducts` once per category
- **Repeated operations**: Any scenario where the same API operation runs with different data

### Rules for Aliased Steps

1. **Explicit `id` required**: When a graph node appears in more than one step, every step using that node must have a unique `id`.

2. **All references use step IDs**: `dependsOn`, `from`, and selection `from` references must use the step's `id`, not its `node` name.

3. **Explicit values required**: Graph-edge auto-wiring is disabled for aliased nodes (since the engine can't determine which step instance to wire from). Every input must use an explicit `from`, `fromSelection`, or literal value.

4. **Separate outputs**: Each aliased step stores its outputs under its own step ID. Downstream steps reference them by step ID:

```yaml
steps:
  - id: add_item_1
    node: addItem
    dependsOn: [createCart]
    values:
      cartId: {from: createCart.cartId}
      productId: "prod-101"
      quantity: 2

  - id: add_item_2
    node: addItem
    dependsOn: [add_item_1]
    values:
      cartId: {from: createCart.cartId}
      productId: "prod-202"
      quantity: 1

  - node: checkout
    dependsOn: [add_item_2]
    values:
      cartId: {from: createCart.cartId}
      lineItem1: {from: add_item_1.lineItemId}    # references step ID
      lineItem2: {from: add_item_2.lineItemId}
```

### Backward Compatibility

Step aliasing is fully backward compatible. If no step has an explicit `id`, everything works exactly as before -- each step's identity is its `node` name, `dependsOn` references node names, and graph-edge auto-wiring resolves normally.

## Values

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
    fallbackPool: ["Max", "Bella", "Charlie"]
    fallbackStrategy: random
    constraint: "len(name) > 2"
  petId:
    from: createPet.petId
  offeringId:
    fromSelection: offering.offeringId
  origin:
    default: "DEN"
    select:
      strategy: first
      from: searchResults.offerings
      filter: "price < 500"
```

### Value Fields

| Field | Description |
|-------|-------------|
| `default` | Literal value to use. For bare scalars (`name: "Buddy"`), this is the only field set. |
| `from` | Reference to an upstream step's output (e.g., `createPet.petId`). Uses step IDs (which default to node names). |
| `fromSelection` | Reference to a named selection (e.g., `offering.offeringId`). See [Named Selections](#named-selections). |
| `select` | Inline selection config for picking from an array output. See [Selection Strategies](#selection-strategies). |
| `fallbackPool` | List of alternative values to try if the default fails. |
| `fallbackStrategy` | How to pick from the fallback pool: `sequential` (default) or `random`. |
| `constraint` | Predicate expression that the resolved value must satisfy. |

For a deep dive into value resolution, see [Value Flow](value-flow.md).

## Named Selections

When a step needs multiple fields from the same array element, use named selections to ensure coordinated extraction:

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

The `selections` block picks an element from an array output. Then `fromSelection` references extract specific fields from that element. The format is `selectionName.fieldName`, where `fieldName` matches an `elementField` defined on the array output in the graph.

### Selection Fields

| Field | Required | Description |
|-------|----------|-------------|
| `from` | yes | Source array: `stepID.outputName` (step ID defaults to node name when `id` is not set) |
| `strategy` | no | How to pick an element (default: `first`). See [Selection Strategies](#selection-strategies). |
| `filter` | no | Predicate to narrow candidates before selection |
| `index` | no | Specific index for `index` strategy |
| `sortField` | no | Field to sort by for `min`/`max` strategies |
| `prompt` | no | Selection criteria for `llm` strategy |

## Selection Strategies

Selections pick one element from an array output. Seven strategies are available:

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

Examples:

```yaml
# Pick the cheapest product
selections:
  product:
    from: listProducts.products
    strategy: min
    sortField: price

# Pick a product the LLM thinks is best
selections:
  product:
    from: listProducts.products
    strategy: llm
    prompt: "Select a popular in-stock product under $50"

# Pick a specific index
selections:
  product:
    from: listProducts.products
    strategy: index
    index: 2

# Filter then pick first
selections:
  product:
    from: listProducts.products
    strategy: first
    filter: "category == 'electronics'"
```

## Assertions

Assertions verify that a step's response meets expectations. AAT supports five mechanical assertion types.

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

### Assertion Types

#### `status` -- HTTP Status Code

Checks the response status code.

```yaml
- type: status
  expect: 200
```

| Field | Required | Description |
|-------|----------|-------------|
| `expect` | yes | Expected HTTP status code (integer) |

#### `fieldExists` -- JSON Field Presence

Checks that a field exists and is not null in the response body.

```yaml
- type: fieldExists
  path: "id"
```

| Field | Required | Description |
|-------|----------|-------------|
| `path` | yes | JSON path to check (gjson syntax) |

#### `fieldEquals` -- JSON Field Value

Checks that a field equals an expected value.

```yaml
- type: fieldEquals
  path: "name"
  expect: "Buddy"
```

| Field | Required | Description |
|-------|----------|-------------|
| `path` | yes | JSON path to the field |
| `expect` | yes | Expected value (string, number, or boolean) |

Note: `fieldEquals` uses the `path` and `expect` fields (not `value`). The `value` field exists in the schema for backward compatibility but `expect` is preferred.

#### `predicate` -- Expression Evaluation

Evaluates a predicate expression against the response body parsed as a JSON object.

```yaml
- type: predicate
  expr: "price > 0 && price < 10000"
```

| Field | Required | Description |
|-------|----------|-------------|
| `expr` | yes | Predicate expression to evaluate |

Predicate expressions support comparison operators (`==`, `!=`, `<`, `>`, `<=`, `>=`), logical operators (`&&`, `||`, `!`), and field access on the response body. See [Value Flow](value-flow.md) for the full expression syntax.

#### `schema` -- Schema Validation

Validates the response against a schema reference. This is a placeholder for future implementation -- currently all schema assertions pass with a "not yet implemented" note.

```yaml
- type: schema
  ref: "PetResponse"
```

| Field | Required | Description |
|-------|----------|-------------|
| `ref` | yes | Schema reference name |

### Semantic Assertions

Plans can also include semantic assertions -- free-text descriptions evaluated by the LLM:

```yaml
assertions:
  semantic:
    - "The response should contain a valid order confirmation"
    - "All price values should be positive"
```

Semantic assertions require an LLM to be configured and are not evaluated in `strict` mode.

## Negative Testing with `expectFailure`

Use `expectFailure` to assert that a step fails with specific HTTP status codes. This is for testing error handling, validation, and authorization.

```yaml
- node: getPet
  description: "Verify deleted pet returns 404"
  dependsOn: [deletePet]
  expectFailure:
    status: [404]
    description: "Pet should not exist after deletion"
```

| Field | Required | Description |
|-------|----------|-------------|
| `status` | yes | List of expected HTTP status codes |
| `description` | no | Why this failure is expected |

When `expectFailure` is set:
- The step **passes** if the response status matches any of the listed codes
- The step **fails** if the response returns a success status or an unexpected error status
- Retries are skipped (failures are expected, not transient)
- Adaptive mode constraint relaxation is skipped

## Retry Configuration

Configure per-step retry behavior:

```yaml
- node: listProducts
  retry:
    max: 3
    on: [server, timeout]
    failOn: [client]
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `max` | yes | -- | Maximum retry attempts |
| `on` | no | all retryable | Error categories to retry on |
| `failOn` | no | none | Error categories that immediately fail (no retry) |

Error categories: `server` (5xx), `client` (4xx), `timeout`, `network`.

## Fallback Configuration

Define what happens when a step fails after all retries:

```yaml
- node: listProducts
  fallback:
    action: skip
    maxAttempts: 2
```

| Field | Required | Description |
|-------|----------|-------------|
| `action` | yes | What to do: `skip` (continue without this step) or `fail` (stop the plan) |
| `maxAttempts` | no | Maximum fallback attempts |

## Verification Steps

Verification steps run after the main execution flow. They're used for post-condition checks that don't affect the main workflow.

```yaml
execution:
  steps:
    - node: createPet
      values:
        name: "Buddy"
        status: "available"

  verification:
    - node: getPet
      purpose: "Confirm pet was persisted"
      assertions:
        mechanical:
          - type: fieldEquals
            path: "name"
            expect: "Buddy"
```

| Field | Required | Description |
|-------|----------|-------------|
| `node` | yes | Graph node to execute |
| `purpose` | no | Why this verification exists |
| `assertions` | no | Assertions to check |

Verification steps receive their inputs from the main execution steps via graph edges, just like regular steps.

## Cleanup Steps

Cleanup steps run after everything else, including on failure. They're used to tear down test data.

```yaml
execution:
  cleanup:
    - node: deletePet
      runOn: always
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `node` | yes | -- | Graph node to execute |
| `runOn` | no | `always` | When to run: `always`, `failure`, `success` |

Cleanup steps also receive inputs from the main execution via graph edges. The graph can also declare cleanup via the `cleanup` field on nodes, which AAT adds automatically.

## Complete Example

Here's a fully annotated plan showing most features:

```yaml
metadata:
  created: 2026-02-11T12:00:00Z
  prompt: "Create a pet, find it by status, then verify and clean up"
  graphVersion: "1.0.0"

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
      description: "Find all available pets"
      values:
        status: "available"
      assertions:
        mechanical:
          - type: status
            expect: 200

    - node: getPet
      dependsOn: [findByStatus]
      isGoal: true
      description: "Retrieve the first pet from the list by ID"
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

    - node: deletePet
      dependsOn: [createPet]
      description: "Clean up: delete the pet we created"
      assertions:
        mechanical:
          - type: status
            expect: 200
```

## Advanced: Multi-Selection from One Array

When an API returns results for multiple logical items in a single array (e.g., a product search returning items from different categories), use multiple named selections with filters to pick different elements:

```yaml
- node: createBundle
  dependsOn: [listProducts]
  selections:
    mainProduct:
      from: listProducts.products
      strategy: match
      filter: "category == 'electronics'"
    accessory:
      from: listProducts.products
      strategy: match
      filter: "category == 'accessories'"
  values:
    mainProductId: {fromSelection: mainProduct.productId}
    mainProductName: {fromSelection: mainProduct.name}
    accessoryId: {fromSelection: accessory.productId}
    accessoryName: {fromSelection: accessory.name}
```

Both selections draw from the same array output but use different filters to pick different elements. The `fromSelection` references then extract fields from each independently.

## Advanced: Step Aliasing for Repeated Operations

Step aliasing enables patterns where the same API operation runs multiple times with different data. For example, adding several items to a cart:

```yaml
execution:
  steps:
    - node: createCart

    - id: add_item_electronics
      node: addItem
      dependsOn: [createCart]
      description: "Add a laptop to the cart"
      values:
        cartId: {from: createCart.cartId}
        productId: "prod-101"
        quantity: 1

    - id: add_item_accessory
      node: addItem
      dependsOn: [add_item_electronics]
      description: "Add a carrying case"
      values:
        cartId: {from: createCart.cartId}
        productId: "prod-202"
        quantity: 1

    - id: add_item_warranty
      node: addItem
      dependsOn: [add_item_accessory]
      description: "Add extended warranty"
      values:
        cartId: {from: createCart.cartId}
        productId: "prod-303"
        quantity: 1
```

Each step executes the same `addItem` graph node with different inputs. Downstream steps reference specific items by step ID (e.g., `add_item_electronics.lineItemId`).

## See Also

- [Plan-Level Auth & Headers](plan-auth.md) -- embedding credentials and custom headers in plans
- [Value Flow](value-flow.md) -- expressions, constraint resolution, selection strategies in depth
- [Running Tests](running.md) -- executing plans with `aat run`
- [LLM-Assisted Planning](prompt-workflow.md) -- generating plans from prompts with `aat prompt`
- [Workflow Templates](workflow-templates.md) -- pre-built plan skeletons attached to graph workflows
- [Templates](templates.md) -- how HTTP adapter templates map to graph nodes
- [Petstore Example](../../examples/petstore/README.md) -- runnable example plans
