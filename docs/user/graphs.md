# API Graphs

A graph is the foundational data model in AAT. It declares what API operations exist, what data they accept and produce, and how they relate to each other through ordering rules. Everything else in AAT — templates, plans, workflows, validation — builds on the graph.

## Overview

An AAT graph is a YAML file that models your API as a set of **nodes**. Each node represents one API operation (e.g., "list products", "create order", "cancel order"). Nodes declare typed **inputs** (what data the operation needs) and typed **outputs** (what data it produces).

An input can default to an earlier node's output (`default: {from: createCart.cartId}`), which covers the common case; **plans** and **workflows** wire the rest, where step values reference outputs from earlier steps (e.g., `listProducts.productId`).

Prerequisites between operations are declared with **requires/satisfies tokens** and **conditions**, not with explicit edges. This keeps the graph focused on what each operation *is* rather than how operations compose into specific test scenarios.

## Nodes

Each node in the graph represents one API operation. The node name is the YAML map key, and it must be unique within the graph.

```yaml
nodes:
  listProducts:
    description: "Search for available products"
    adapter: listProducts
    inputs:
      - name: category
        type: string
        description: "Product category to filter by"
      - name: minPrice
        type: float
        description: "Minimum price filter"
        optional: true
      - name: maxResults
        type: integer
        optional: true
        default: 20
    outputs:
      - name: products
        type: product[]
        description: "Matching products"
        elementFields:
          - name: productId
            type: string
          - name: name
            type: string
          - name: price
            type: money
```

The `adapter` field links the node to its [template](templates.md) — the YAML file that defines the actual HTTP request and response extraction.

### Inputs

An input declares one piece of data the operation needs. Inputs are resolved at execution time from plan values, upstream step outputs, or defaults.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Input name, unique within the node |
| `type` | string | yes | Data type (see [Types](#types)) |
| `description` | string | no | Human-readable description |
| `optional` | bool | no | If true, the operation can execute without this input (default: false) |
| `configurable` | bool | no | Marks an optional input for `aat prompt` to fill: the model is offered it as optional configuration to set from the request, even when it has a default, and `aat docs generate` lists it as `configurable`. Requires `optional: true` |
| `default` | varies | no | Default value when no plan value or upstream output provides one |
| `constraints` | map | no | `min`, `max`, `minLength`, `maxLength`, `pattern`, `description`. Documentation only: `aat docs generate` and the MCP tools show them and `aat validate` checks that they are well formed (the pattern compiles, min ≤ max), but the engine does not enforce them. `aat generate` fills them from the spec |

### Input Defaults

Defaults provide fallback values for inputs not supplied by the plan. AAT supports three forms:

**Scalar literal** — a bare value:

```yaml
inputs:
  - name: currency
    type: string
    default: "USD"
```

**Pool shorthand** — a YAML sequence, from which the engine picks one value:

```yaml
inputs:
  - name: region
    type: string
    default: ["us-east", "us-west", "eu-central"]
```

**Rich default** — a map with explicit control over resolution:

```yaml
inputs:
  - name: productId
    type: string
    default:
      from: listProducts.products           # reference an upstream output
      select:
        strategy: min                       # selection strategy
        field: price                        # field to evaluate
        filter: "category == 'electronics'" # predicate filter
```

Rich default fields:

| Field | Description |
|-------|-------------|
| `value` | Literal value |
| `pool` | Array of candidate values, used when there is no `value` or the `value` fails `constraint` |
| `poolStrategy` | How to pick from the pool: `random` (default) or `sequential` |
| `constraint` | Predicate the value must satisfy, over `value` and the inputs already resolved for the step (see [Fallback Pools](value-flow.md#fallback-pools)) |
| `from` | Reference to an upstream step output (`step.output`) |
| `fromResolved` | Name of an input declared earlier on the same node; the input takes that input's resolved value (see [Intra-Step References](value-flow.md#intra-step-references)) |
| `select` | Selection config for array sources: `strategy`, `field`, `filter`, `index`, `sortField` |

### Outputs

An output declares one piece of data the operation produces. Outputs are extracted from the HTTP response by the node's [template](templates.md).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Output name, unique within the node |
| `type` | string | yes | Data type (see [Types](#types)); use `X[]` for arrays |
| `description` | string | no | Human-readable description |
| `optional` | bool | no | If true, `aat validate` does not require the node's template to extract this output. When the output is missing, an optional input that takes it with `from:`, directly or through a named selection, is left out, and a required one fails; `aat validate --strict` warns about a required input that takes an optional output |
| `display` | string | no | Label for surfacing this output to the user. When set, the extracted value is printed under the step in console output (`  Locator: ABC123`), included as `display_outputs` in `--json` summaries, and stored in the archive |
| `elementFields` | list | no | Field definitions for array element structure (see below) |

### Array Outputs and Element Fields

When an output is an array (type ending in `[]`), you can declare `elementFields` to describe the structure of each element. Element fields are the semantic contract that [selection strategies](value-flow.md) use to pick elements.

```yaml
outputs:
  - name: products
    type: product[]
    elementFields:
      - name: productId
        type: string
      - name: price
        type: money
      - name: category
        type: string
      - name: rating
        type: float
```

Each element field has:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Logical field name used in plans and selection configs |
| `type` | string | yes | Data type |
| `path` | string | no | gjson path within the element, used only when the template has no `fields` map for the output; defaults to `name` |

The actual extraction mechanics — mapping JSON paths to logical names — are defined in the [template](templates.md): its `fields` map for the output renames each element's fields (for example `productId: id`). The graph declares *what fields exist*; the template declares *how to extract them*.

The graph-side `path` is an older way to do the same renaming. It is used only when the template gives the output no `fields` map: then a selection that names `productId` reads the element's `id` when the element field sets `name: productId` and `path: id`.

### Cleanup

The `cleanup` field on a node names another node that should run after the plan completes to tear down resources, even if the plan fails. This pairs creation operations with their corresponding deletion or cancellation. The field is the node's name, or a mapping with `node`, `when`, and `releasedBy` that says when the cleanup isn't needed (see **When a cleanup is skipped** below).

```yaml
nodes:
  createOrder:
    adapter: createOrder
    cleanup: cancelOrder       # runs after the plan once createOrder succeeds
    inputs: [...]
    outputs: [...]

  cancelOrder:
    adapter: cancelOrder
    inputs:
      - name: orderId
        type: string
```

The engine registers the pairing once the creating step succeeds (a status below 400 with outputs extracted and no [error detection](#error-detection) rule triggered), even if the step's assertions then fail, so a plan need not list it. After the main flow, the plan's own `cleanup:` steps for other nodes run first, in the order listed; then the registered pairings, last created first, once per resource. Cleanup inputs are matched by name against the outputs of the steps that ran (for a registered pairing, the creating step's outputs first), so `cancelOrder.orderId` takes `createOrder`'s `orderId` output, and two `createOrder` steps each cancel their own order. Recipes and `aat prompt` plans list each pairing in the plan's `cleanup:` section; a listed pairing still runs from the registered entries, and its `runOn` decides whether they run.

**Cleanup chains.** A cleanup node can declare a `cleanup` of its own. When a cleanup step succeeds (a status below 400 and no error detection rule triggered), its node's cleanup runs right after it, before the next resource is released. This covers an API that releases a resource in two calls, such as requesting a refund and then confirming it:

```yaml
nodes:
  createOrder:
    adapter: createOrder
    cleanup: requestRefund
    outputs:
      - name: orderId
        type: string

  requestRefund:
    adapter: requestRefund
    cleanup: confirmRefund       # runs after each requestRefund that succeeds
    inputs:
      - name: orderId
        type: string
    outputs:
      - name: refundId
        type: string

  confirmRefund:
    adapter: confirmRefund
    inputs:
      - name: refundId           # named after requestRefund's output
        type: string
```

How a chain behaves:
- **Inputs.** A chained step takes its inputs first from the outputs of the cleanup steps before it in its chain, nearest first, then the same way as any cleanup step.
- **Isolation.** Those outputs stay inside the chain, so no other cleanup step picks them up.
- **Failure.** When a cleanup step fails, the rest of its chain is skipped.
- **Loops.** A chain that loops back to a node it already passed is rejected when the graph loads.
- **Step IDs and links.** In archives and `--json` output, each cleanup step has its own step ID: `requestRefund` for the first order, `requestRefund_2` for the second. `cleanupFor` (`cleanup_for` in `--json`) names the step whose resource the cleanup releases, or the cleanup step before it in its chain.

**When a cleanup is skipped.** A plan can release a resource itself, such as by capturing a payment whose cleanup would void it. It can also leave the resource in a state its cleanup can't handle. Either way, a cleanup that runs anyway fails. Give the pairing as a mapping to say when it isn't needed:

```yaml
nodes:
  createPayment:
    adapter: createPayment
    cleanup:
      node: voidPayment
      when: 'status == "authorized"'   # void only a payment that is still authorized
      releasedBy: [capturePayment]     # capturing this payment releases it
    outputs:
      - name: paymentId
        type: string
      - name: status
        type: string

  capturePayment:
    adapter: capturePayment
    inputs:
      - name: paymentId
        type: string

  voidPayment:
    adapter: voidPayment
    inputs:
      - name: paymentId
        type: string
```

Before a registered cleanup runs, AAT checks, in order:
1. **`runOn`.** If the plan's `cleanup:` section lists the node, that listing's `runOn` decides first, as described above.
2. **Released.** The cleanup is skipped when a main step already released the resource:
   - it ran after the step that registered the cleanup
   - it ran on the cleanup node itself, or on a node in `releasedBy`
   - it succeeded: a status below 400, no error detection rule triggered, and not a [negative test](plans.md#negative-testing-expectfailure)
   - it sent the same value for every input it shares with the cleanup, and there is at least one such input. Numbers compare by value.

   So an explicit `voidPayment` step skips the pairing for the payment it voided, with no `releasedBy`. A step for another payment doesn't, and neither does one that left a shared input unset. Verification steps never release a cleanup, and no main step releases a cleanup in a chain: it cleans up what the cleanup step before it created, after the main steps ran.
3. **`when`.** A predicate, in the syntax of a selection `filter`, over the outputs of the step that registered the cleanup, or of the cleanup step before it in a chain. The cleanup is skipped when it's false. The predicate reads no other step's outputs, so a later step that returns a `status` of its own doesn't change it. If it can't be evaluated, such as when the step returned no `status`, the cleanup runs, and its record carries `whenError`.

A skipped cleanup sends nothing, so its chain doesn't run either. Where skips appear:
- **Console:** `skipped:` and the reason in place of a status, as in `voidPayment  skipped: released by capturePayment (for createPayment)`.
- **Archive:** `cleanupSkipped`, and `cleanup_skipped` in `--json`.
- **[`aat run show`](archives.md#inspecting-a-run-from-the-cli):** under `cleanup skipped:`.

Loading the graph checks that `releasedBy` names other existing nodes, each once, and that `when` parses and names outputs of the node that declares it. So `aat validate`, `aat run`, and the MCP server all report a bad pairing.

List in `releasedBy` only nodes whose success always ends the resource. If an API reports a failed release in a successful response, give that node an [error detection](#error-detection) rule, so the failure doesn't count as a release.

### Tags

Nodes can carry tags for filtering and categorization:

```yaml
nodes:
  listProducts:
    tags: [search, catalog]
    ...
```

Tags are metadata — they don't affect execution but can be used by tooling and the MCP server to filter operations.

## Node Ordering

AAT uses two mechanisms to declare the valid ordering of operations: **requires/satisfies tokens** and **conditions**. Together, these define prerequisite relationships without coupling nodes to specific data-flow scenarios.

These rules describe the API for the tools that trace it: backward chaining in the MCP server (`trace_workflow`, `trace_dependency_chain`, workflow documentation), the dependency sections of `aat docs generate`, and the token and cycle checks in `aat validate`. They do not order a hand-written plan, which runs its steps in `dependsOn` order; when a recipe or `aat prompt` composes a plan from a workflow, AAT adds `dependsOn` entries from them.

### Requires and Satisfies

Nodes declare abstract tokens they **require** (must happen after) and **satisfy** (make available to others):

```yaml
nodes:
  listProducts:
    satisfies: [searchComplete]
    ...

  addToCart:
    requires: [searchComplete]
    satisfies: [cartPopulated]
    ...

  checkout:
    requires: [cartPopulated]
    ...
```

This creates an ordering: `listProducts` -> `addToCart` -> `checkout`. The tokens are abstract labels — they don't name specific outputs, just logical prerequisites.

When multiple nodes satisfy the same token, the `preferred` flag hints which one to use:

```yaml
nodes:
  listProducts:
    satisfies: [searchComplete]
    preferred: true                      # prefer this over alternatives
    ...

  searchProducts:
    satisfies: [searchComplete]
    ...
```

### Conditions

Conditions are graph-level rules that pull extra nodes into a chain when a predicate holds:

```yaml
conditions:
  - when: "order.international == true"
    require: [addCustomsInfo]
    before: [submitOrder]
```

| Field | Description |
|-------|-------------|
| `when` | Predicate expression, evaluated against a condition context supplied by the caller |
| `require` | Nodes the chain must include when `when` is true |
| `before` | Nodes that each `require` node is ordered before |

Conditions express rules that don't fit the token model (e.g., "international orders need customs information before submission"). Backward chaining evaluates them only when its caller supplies a predicate evaluator and a context, and no command does today: the MCP tracing tools treat every condition as false. `aat validate` checks that `when` is set and that `require` and `before` name existing nodes, and the MCP `aat://graph` resource lists conditions.

### Cycle Breaker

If your ordering rules create a cycle, mark one node as a cycle breaker to allow traversal:

```yaml
nodes:
  updateCartItem:
    cycleBreaker: true
    ...
```

This tells the backward chaining algorithm to stop traversing through this node, breaking the cycle. The node still executes normally — the flag only affects graph analysis.

## Error Detection

APIs sometimes return HTTP 200 with an error payload. Error detection rules let the graph declare patterns that indicate a "successful" response is actually an error.

Rules can be defined at the graph level (apply to all nodes) or at the node level. A node's own rules replace the graph-level rules for that node; they are not merged. Rules are checked on responses with a status below 400, after outputs are extracted, and the first rule that triggers fails the step:

```yaml
# Graph-level: applies to all nodes
errorDetection:
  - path: "error"
    rule: exists
    details:
      message: "error.message"
      code: "error.code"

nodes:
  listProducts:
    # Node-level: applies only to this node
    errorDetection:
      - path: "errors"
        rule: non-empty
        details:
          message: "errors.0.message"
```

### Rule Types

| Rule | Behavior |
|------|----------|
| `exists` | Error if the path exists in the response (non-nil) |
| `non-empty` | Error if the path holds a non-empty string, array, or object, or any number or boolean |
| `equals` | Error if the value at the path equals the specified `value`, which must be a string, a number, or a boolean. Numbers compare by value (`0` matches `0` and `0.0`), and a string never matches a number |

### Detail Mapping

The optional `details` section extracts error information from the response for better error messages:

| Field | Description |
|-------|-------------|
| `message` | gjson path to the error message |
| `code` | gjson path to an error code |
| `category` | gjson path to an error category |

## Types

AAT supports these data types for inputs, outputs, and element fields:

| Type | Description | Example |
|------|-------------|---------|
| `string` | Text value | `"hello"` |
| `integer` | Whole number | `42` |
| `float` | Decimal number | `3.14` |
| `boolean` | True or false | `true` |
| `date` | Date in YYYY-MM-DD format | `"2026-03-15"` |
| `datetime` | Date and time | `"2026-03-15T10:30:00Z"` |
| `money` | Currency amount | `"149.99"` |
| `enum[a, b, c]` | Enumeration of allowed values | `"a"` |
| `X[]` | Array of type X | — |
| *customName* | Domain-specific type (e.g., `sku`, `postalCode`) | `"WIDGET-100"` |

Custom types integrate with the [domain knowledge](domain.md) layer. The domain file can declare type definitions and value pools for custom types, which `aat prompt`, `aat docs generate`, and the MCP tools use to suggest realistic values. The engine does not draw from them; give an input a pool default for varied test data at run time.

## OAS Integration

AAT can scaffold graphs from OpenAPI specs, reference OAS operations for validation, and cross-check graph definitions against the spec.

### Scaffolding from OpenAPI

`aat generate --oas api-spec.yaml` writes a starting-point graph (one node per operation, with an `oas` reference on each) and one template per node. The scaffold has no ordering rules, cleanup pairings, or data wiring; you add those by hand. See [Scaffolding from OpenAPI](generate.md) for the flags, what the generator writes, and how to preview it.

### OAS References

Link graph nodes to their OAS operations for validation:

```yaml
# Graph-level: default spec for all nodes
oas: api-spec.yaml

nodes:
  listProducts:
    oas:
      operationId: ListProducts          # required: the OAS operationId
      spec: other-spec.yaml              # optional: override the graph-level spec
```

Spec paths are resolved relative to the graph file's directory.

### OAS Validation

`aat validate` and `aat validate graph` check every node that has an `oas` reference against its spec:

```bash
aat validate graph --graph graph.yaml --strict
```

**Errors** (always fail validation):

- A referenced spec file cannot be loaded (validation stops there)
- `oas` is set but `operationId` is empty
- The node has no spec (neither a node-level `spec` nor a graph-level `oas`)
- `operationId` not found in the node's spec

**Warnings** (fail only with `--strict`):

- A graph input that is neither an OAS parameter nor a request body property, unless the node's template sends it only in request headers, such as an idempotency key
- A required OAS parameter or required request body property that is not a graph input, unless the node's template sends it itself: a query parameter written into `request.path`, a header in `request.headers`, a top-level JSON body key, or a form body key, outside `{{?…}}` and `{{#…}}` blocks. A bracketed query or form key such as `metadata[source]` counts as `metadata`
- A graph output not found in the first 2xx response schema that declares properties. Each output is looked up at its template extract path, through nested objects and array items, and outputs a Lua transform computes are skipped

The template-aware parts of these checks need the templates: `aat validate` always loads them, and `aat validate graph` loads them from `--templates` or the manifest. Without templates, every input must be a parameter or body property, required fields must be graph inputs, and outputs must be top-level response properties named after the output.

Warnings are informational — intentional divergence from the spec is normal (e.g., omitting optional parameters or extracting only specific response fields).

## Graph YAML Reference

An annotated graph with every top-level field except `examples` (few-shot examples for `aat prompt`'s workflow selection) and the common node fields; the tables above list the rest. `version` is required and must be `X.Y.Z`.

```yaml
version: "1.0.0"
title: "E-Commerce API"
description: "Product catalog, cart, and order operations"
notes: "Covers browse-to-checkout happy path plus cancellation"

# Default OpenAPI spec for OAS validation
oas: api-spec.yaml

# Graph-level error detection (applies to all nodes)
errorDetection:
  - path: "error"
    rule: exists
    details:
      message: "error.message"
      code: "error.code"

# Workflow definitions (see workflows.md for details)
workflows:
  - name: Standard Checkout
    description: "Browse products, add to cart, and complete checkout"
    template: workflows/standard-checkout.yaml

# Conditional prerequisites (see Conditions above)
conditions:
  - when: "order.international == true"
    require: [addCustomsInfo]
    before: [submitOrder]

# Node definitions
nodes:
  listProducts:
    description: "Search the product catalog"
    adapter: listProducts
    tags: [search, catalog]
    satisfies: [searchComplete]
    preferred: true
    oas:
      operationId: ListProducts
    inputs:
      - name: category
        type: string
        description: "Product category to filter by"
      - name: minPrice
        type: float
        optional: true
      - name: maxResults
        type: integer
        optional: true
        default: 20
      - name: region
        type: string
        optional: true
        configurable: true
    outputs:
      - name: products
        type: product[]
        description: "Matching products"
        elementFields:
          - name: productId
            type: string
          - name: price
            type: money
          - name: category
            type: string
            path: "category"
          - name: rating
            type: float

  addToCart:
    description: "Add a product to the cart"
    adapter: addToCart
    requires: [searchComplete]
    satisfies: [cartPopulated]
    inputs:
      - name: productId
        type: string
        default:
          from: listProducts.products
          select:
            strategy: min
            field: price
    outputs:
      - name: cartId
        type: string

  createOrder:
    description: "Create a new order from cart contents"
    adapter: createOrder
    cleanup: cancelOrder
    requires: [cartPopulated]
    satisfies: [orderCreated]
    oas:
      operationId: CreateOrder
    inputs:
      - name: cartId
        type: string
      - name: productId
        type: string
    outputs:
      - name: orderId
        type: string
      - name: status
        type: string

  cancelOrder:
    description: "Cancel an order (cleanup)"
    adapter: cancelOrder
    inputs:
      - name: orderId
        type: string
    outputs:
      - name: status
        type: string

  addCustomsInfo:
    description: "Attach customs information to an international order"
    adapter: addCustomsInfo
    requires: [orderCreated]
    inputs:
      - name: orderId
        type: string
    outputs: []

  submitOrder:
    description: "Finalize and submit an order for processing"
    adapter: submitOrder
    requires: [orderCreated]
    inputs:
      - name: orderId
        type: string
      - name: acceptTerms
        type: boolean
        optional: true
    outputs:
      - name: confirmationId
        type: string
```

## Validation

Run `aat validate graph` to check the graph for structural errors:

```bash
# Structural validation (from manifest or explicit path)
aat validate graph

# With template cross-validation
aat validate graph --templates templates/

# With OAS alignment
aat validate graph --strict

# Explicit paths
aat validate graph --graph graph.yaml --oas api-spec.yaml --templates templates/
```

Structural checks include: `version` present and `X.Y.Z`, `adapter` set on every node, valid type syntax, input/output name uniqueness within a node, `configurable` only on optional inputs, well-formed `constraints` and `errorDetection` rules, every required token satisfied by some node, no requires/satisfies cycles (unless a node in the cycle is a `cycleBreaker`), cleanup node existence, and condition references. Unknown keys are errors, with the file, line, and a suggestion.

When templates are available (`--templates`, or the manifest's `templates`), validation also checks that each node's declared outputs match the extract keys in its template, catching mismatches like graph outputs that the template never extracts (will be nil at runtime) or template extract keys the graph doesn't declare (dead extraction).

See [Validation](validation.md) for the full reference covering all `aat validate` subcommands.

---

*Source: graph types in `graph/types.go`, parsing in `graph/parse.go`, validation in `graph/validate.go` and `graph/oas/validator.go`, chaining in `graph/chain.go`.*
