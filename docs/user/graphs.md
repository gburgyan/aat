# API Graphs

A graph is the foundational data model in AAT. It declares what API operations exist, what data they accept and produce, and how they relate to each other through ordering rules. Everything else in AAT — templates, plans, workflows, validation — builds on the graph.

## Overview

An AAT graph is a YAML file that models your API as a set of **nodes**. Each node represents one API operation (e.g., "list products", "create order", "cancel order"). Nodes declare typed **inputs** (what data the operation needs) and typed **outputs** (what data it produces).

The graph does *not* declare how data flows between operations. Data flow is wired in **plans** and **workflows**, where step values reference outputs from earlier steps (e.g., `listProducts.productId`). The graph declares the *signatures*; plans declare the *wiring*.

Node ordering is determined by **requires/satisfies tokens** and **conditions**, not by explicit edge declarations. This keeps the graph focused on what each operation *is* rather than how operations compose into specific test scenarios.

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
| `configurable` | bool | no | If true, this input can be set via environment config values |
| `default` | varies | no | Default value when no plan value or upstream output provides one |

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
| `pool` | Array of candidate values |
| `poolStrategy` | How to pick from the pool (e.g., `"random"`, `"sequential"`) |
| `from` | Reference to an upstream step output (`step.output`) |
| `fromResolved` | Pre-resolved reference (internal use) |
| `select` | Selection config for array sources: `strategy`, `field`, `filter`, `index`, `sortField` |

### Outputs

An output declares one piece of data the operation produces. Outputs are extracted from the HTTP response by the node's [template](templates.md).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Output name, unique within the node |
| `type` | string | yes | Data type (see [Types](#types)); use `X[]` for arrays |
| `description` | string | no | Human-readable description |
| `display` | string | no | Display format hint for the web UI |
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
| `path` | string | no | gjson extraction path within the element; defaults to `name` if omitted |

The `path` field allows the logical name to differ from the JSON field name. For example, if the API returns `{"id": "abc"}` but you want plans to reference it as `productId`, set `name: productId` and `path: id`.

The actual extraction mechanics — mapping JSON paths to logical names — are defined in the [template](templates.md). The graph declares *what fields exist*; the template declares *how to extract them*.

### Cleanup

The `cleanup` field on a node names another node that should run after the plan completes to tear down resources, even if the plan fails. This pairs creation operations with their corresponding deletion or cancellation.

```yaml
nodes:
  createOrder:
    adapter: createOrder
    cleanup: cancelOrder       # always runs after plan completes
    inputs: [...]
    outputs: [...]

  cancelOrder:
    adapter: cancelOrder
    inputs:
      - name: orderId
        type: string
```

Cleanup steps are automatically placed in the plan's cleanup section by `aat prompt` and run in reverse order of their corresponding creation steps.

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

AAT uses two mechanisms to determine the valid ordering of operations: **requires/satisfies tokens** and **conditions**. Together, these define prerequisite relationships without coupling nodes to specific data-flow scenarios.

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

Conditions are graph-level rules that express ordering constraints with predicates:

```yaml
conditions:
  - when: submitOrder
    require: [addShipping, addPayment]

  - when: applyCoupon
    before: [submitOrder]
```

| Field | Description |
|-------|-------------|
| `when` | The node this condition applies to |
| `require` | Nodes that must execute before `when` |
| `before` | Nodes that `when` must execute before |

Conditions and requires/satisfies work together. Conditions are useful for expressing ordering rules that don't fit the token model (e.g., "if you apply a coupon, it must happen before order submission").

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

Rules can be defined at the graph level (apply to all nodes) or at the node level (apply to one node):

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
| `non-empty` | Error if the path exists and is not empty (non-empty string, non-empty array, non-zero number) |
| `equals` | Error if the value at the path equals the specified `value` |

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

Custom types integrate with the [domain knowledge](domain.md) layer. The domain file can declare type definitions and value pools for custom types, which the engine uses to produce realistic test data.

## OAS Integration

AAT can scaffold graphs from OpenAPI specs, reference OAS operations for validation, and cross-check graph definitions against the spec.

### Scaffolding from OpenAPI

Use `aat generate` to create a starting-point graph and templates from an OpenAPI 3.x spec:

```bash
aat generate \
  --oas api-spec.yaml \
  --output-graph graph.yaml \
  --output-templates templates/
```

This produces one node per `operationId` with inputs from parameters and request body, outputs from the first 2xx response schema, and a matching template for each node.

The scaffold is intentionally rough — it provides correct HTTP methods, paths, and parameter names but does not generate ordering rules or know which outputs matter for your test flow. Post-scaffold work includes removing unused nodes, refining types, adding requires/satisfies tokens, and configuring array outputs.

**What the scaffold includes:** node names from `operationId`, inputs from path/query/header parameters and request body, output extraction from 2xx response schema, array detection with `elementFields`, OAS links on each node, and template files with placeholders.

**What the scaffold omits:** ordering rules (requires/satisfies), conditions, cleanup pairing, custom types, descriptions (unless the spec has `summary`), and selective output trimming.

To preview without writing files:

```bash
aat generate --oas api-spec.yaml --output-graph -
```

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

Run `aat validate graph` with an OAS spec to check alignment:

```bash
aat validate graph --graph graph.yaml --strict
```

The validator checks 7 rules, classified as errors or warnings:

**Errors** (always fail validation):
- `operationId` not found in any loaded spec
- Spec path referenced but file not found

**Warnings** (fail only with `--strict`):
- Graph input not in OAS parameters or request body
- Required OAS parameter missing from graph inputs
- Graph output not in OAS response schema
- HTTP method mismatch between graph adapter and spec
- Response content type mismatch

Warnings are informational — intentional divergence from the spec is normal (e.g., omitting optional parameters or extracting only specific response fields).

## Graph YAML Reference

Complete annotated example showing all top-level and nested fields:

```yaml
version: "1.0"
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

# Ordering conditions
conditions:
  - when: submitOrder
    require: [addShipping, addPayment]
  - when: applyCoupon
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

Structural checks include: unique node names, valid type syntax, input/output name uniqueness within a node, requires/satisfies token consistency, cleanup node existence, and condition references.

When `--templates` is provided, validation also checks that each node's declared outputs match the extract keys in its template, catching mismatches like graph outputs that the template never extracts (will be nil at runtime) or template extract keys the graph doesn't declare (dead extraction).

See [Validation](validation.md) for the full reference covering all `aat validate` subcommands.

---

*Source: Restructured from `docs/user/graph-authoring.md`. Workflow content moved to [workflows.md](workflows.md).*
