# How Values Flow Between Steps

AAT plans describe multi-step API workflows where each step produces outputs that feed into later steps. This guide explains how input values are resolved — from simple literals to array selections and dynamic expressions.

## The Basics: Literal Values

The simplest case is a literal value written directly in the plan:

```yaml
steps:
  - node: createPet
    values:
      name: "Buddy"
      status: "available"
      category: "dogs"
```

These values are passed directly to the step's template as-is.

## Scalar References: Passing Outputs Forward

When one step produces an output that another step needs, use a `from` reference:

```yaml
steps:
  - node: createCart
    # produces output: cartId

  - node: addItem
    dependsOn: [createCart]
    values:
      cartId:
        from: createCart.cartId
```

The format is `stepID.outputName`. When a step has no explicit `id`, its step ID is the same as its `node` name, so `createCart.cartId` works in both cases. When using [step aliasing](plan-authoring.md#step-aliasing), use the explicit `id`:

```yaml
values:
  productId:
    from: add_item_1.productId    # references step with id: add_item_1
```

The referenced step must be listed in `dependsOn` so AAT knows to run it first.

## Array Selection: Choosing From Multiple Results

Many APIs return arrays — a product listing returns multiple items, a search endpoint returns multiple results. When a downstream step needs a value from one element of an array, you need a **selection strategy**.

### Single-Field Selection

When only one input comes from an array, use inline `from` + `select`:

```yaml
values:
  productId:
    from: listProducts.products
    select:
      strategy: first
      field: productId
```

This says: "Take the `products` array from `listProducts`, pick the first element, and extract its `productId` field."

### Named Selections: Multiple Fields From the Same Element

Often you need several fields from the **same** array element. For example, adding an item to a cart requires both a `productId` and a `price` from the same product. Named selections guarantee they come from the same element:

```yaml
steps:
  - node: addItem
    dependsOn: [listProducts, createCart]
    selections:
      product:
        from: listProducts.products
        strategy: first
    values:
      productId:
        fromSelection: product.productId
      price:
        fromSelection: product.price
```

The `selections` block picks one element from the array. Then each `fromSelection` reference extracts a different field from that same element. This avoids repeating the `from`/`select` block for each field and — critically — ensures both values come from the same array element, even when using non-deterministic strategies like `random` or `llm`.

### Selection Strategies

| Strategy | Description | Required Fields |
|----------|-------------|-----------------|
| `first` | First element (default) | — |
| `last` | Last element | — |
| `index` | Specific position | `index: N` |
| `random` | Random element | — |
| `min` | Element with lowest field value | `sortField: fieldName` |
| `max` | Element with highest field value | `sortField: fieldName` |
| `match` | First element matching a predicate | `filter: "expression"` |
| `llm` | LLM chooses based on a prompt | `prompt: "description"` |

Examples:

```yaml
# Cheapest product
selections:
  product:
    from: listProducts.products
    strategy: min
    sortField: price

# Only in-stock items
selections:
  product:
    from: listProducts.products
    strategy: match
    filter: "inStock == true"

# LLM picks the best option
selections:
  product:
    from: listProducts.products
    strategy: llm
    prompt: "Choose a popular product under $50 with good reviews"
```

### Filtering

The `filter` field uses predicate expressions to narrow the array before selection. This works with any strategy — you can filter to in-stock items and then pick the cheapest:

```yaml
# Inline selection with filter
values:
  productId:
    from: listProducts.products
    select:
      strategy: min
      sortField: price
      filter: "inStock == true"
      field: productId
```

## Dynamic Expressions

Values can include `{{...}}` expressions that are evaluated at runtime:

```yaml
values:
  deliveryDate: "{{today + 7 days}}"
  expiresAt: "{{today + 30 days}}"
  orderRef: "TEST_{{today}}"
```

### Supported Expressions

| Expression | Result | Example |
|------------|--------|---------|
| `{{today}}` | Current date (YYYY-MM-DD) | `2026-02-08` |
| `{{today + N days}}` | Date offset from today | `2026-03-10` |
| `{{today - N days}}` | Date offset backward | `2026-01-09` |
| `{{env.VAR_NAME}}` | Environment variable | value of `$VAR_NAME` |
| `{{inputName}}` | Reference to another resolved input | value of that input |
| `{{inputName + N days}}` | Date arithmetic on a reference | date offset from reference |

Expressions can appear inside larger strings:

```yaml
values:
  label: "Order_{{productId}}_qty_{{quantity}}_{{deliveryDate}}"
```

When the entire value is a single expression (e.g., `"{{today + 30 days}}"`), it returns a typed value. When mixed with literal text, it returns a concatenated string.

## Constraints and Fallback Pools

Constraints validate that a resolved value meets a condition. Fallback pools provide alternative values to try when the primary value fails the constraint.

```yaml
values:
  deliveryDate:
    default: "{{today + 7 days}}"
    constraint: "value >= today"
    fallbackPool:
      - "{{today + 10 days}}"
      - "{{today + 14 days}}"
      - "{{today + 21 days}}"
```

Resolution works like this:

1. Evaluate `default` (including any expressions)
2. Check the `constraint` predicate — `value` refers to the candidate
3. If it passes, use it
4. If it fails, try each value in `fallbackPool` in order
5. Use the first one that passes the constraint

The `fallbackStrategy` field controls the order: `"sequential"` (default) tries values in order; `"random"` shuffles them first.

### Constraint Expressions

Constraints are predicate expressions where `value` is the candidate and other resolved inputs are available by name:

```yaml
values:
  expiresAt:
    default: "{{today + 30 days}}"
    constraint: "value > deliveryDate"
```

This ensures the expiration date is after the delivery date, regardless of what `deliveryDate` resolved to.

## Execution Modes and LLM Fallback

The resolution chain described above is deterministic — literal values, expressions, fallback pools. When all deterministic paths are exhausted and a value still can't be resolved, AAT can optionally delegate to an LLM. Whether this happens depends on the **execution mode** configured in your environment file.

### Modes

| Mode | Behavior |
|------|----------|
| `strict` | Never calls the LLM. If a value can't be resolved deterministically, the step fails. Use this when you need fully reproducible runs. |
| `lean` | Calls the LLM only after all deterministic options are exhausted (default failed constraint, every pool value failed). This is the default mode for the `aat prompt` command. |
| `adaptive` | Same as `lean`, plus step-level recovery: if a step gets an HTTP 4xx error and has soft constraints, AAT can relax constraints and retry the step. |

Set the mode in your environment file:

```yaml
llm:
  endpoint: https://api.openai.com/v1
  apiKey:
    source: env
    var: OPENAI_API_KEY
  model: gpt-5.2
  mode: lean
```

Or override with `--mode` on the command line:

```
aat run --plan plan.yaml --env env.yaml --graph graph.yaml --templates tpl/ --mode adaptive
```

### How LLM Value Selection Works

When a step value has a constraint and all deterministic values fail it, AAT constructs a prompt with:

- The input name, type, and description (from the graph)
- The constraint expression
- All values that were tried and rejected
- Other inputs already resolved for this step (for context)
- Domain knowledge (concepts, types, value pools) if a domain file is loaded

The LLM returns a single value. If it passes the constraint, it's used. If it fails, the step fails.

In the run archive, LLM-selected values have `"source": "llm"` and include the full prompt/response in the `llmCall` field:

```json
{
  "inputName": "deliveryDate",
  "source": "llm",
  "finalValue": "2026-04-15",
  "constraint": "value > today",
  "constraintOK": true,
  "tried": ["2025-12-01", "2025-11-15"],
  "llmCall": {
    "model": "gpt-5.2",
    "durationMs": 842,
    "inputTokens": 156,
    "outputTokens": 8
  }
}
```

### LLM Array Selection

In addition to scalar values, the LLM can select from arrays. Use `strategy: llm` with a `prompt` describing what to pick:

```yaml
selections:
  product:
    from: listProducts.products
    strategy: llm
    prompt: "Choose a popular in-stock product under $50"
```

The LLM sees a tabular summary of the array elements (using the node's `elementFields` for column selection) and returns an index. This works in both `lean` and `adaptive` modes.

## Soft Constraint Relaxation

In `adaptive` mode, AAT can **relax** soft constraints when resolution would otherwise fail. This is the key difference between `adaptive` and `lean`: adaptive can recover from constraint failures instead of stopping.

### Hard vs Soft Constraints

Plans classify constraints by enforcement level:

```yaml
intent:
  goal: checkout
  constraints:
    hard:
      - type: category
        name: category_constraint
        description: "Must select from electronics"
        applies_to: [listProducts.category]
    soft:
      - type: pricing
        name: price_preference
        description: "Prefer items under $50"
        applies_to: [addItem.price]
    free:
      - shipping method
```

- **Hard constraints** are never relaxed. If they can't be satisfied, the step fails.
- **Soft constraints** can be relaxed as a last resort. When relaxed, the engine accepts a value that would normally fail the constraint.
- **Free dimensions** have no constraints and accept any valid value.

### When Relaxation Happens

Relaxation can occur in three scenarios:

**1. Value resolution exhausted.** The default value, every fallback pool value, and the LLM all failed a constraint. If the constraint is soft, AAT relaxes it and accepts the first value that passed expression evaluation (but failed the constraint check).

```yaml
values:
  deliveryDate:
    default: "{{today + 7 days}}"
    constraint: "value > '2026-03-01' && value < '2026-03-31'"
    fallbackPool:
      - "{{today + 10 days}}"
      - "{{today + 14 days}}"
```

If today is January and none of these dates fall in March, but the constraint is classified as soft, AAT relaxes it and uses the first date (today + 7 days) anyway.

**2. Filter produces no matches.** A selection filter eliminates all array elements. If the filter corresponds to a soft constraint, AAT drops the filter and retries the selection on the full array.

```yaml
selections:
  product:
    from: listProducts.products
    strategy: min
    sortField: price
    filter: "inStock == true"
```

If no products are in stock and the filter is tied to a soft constraint, AAT relaxes the filter and picks the cheapest from all products.

**3. Step-level recovery (adaptive only).** The step executes but gets an HTTP 4xx error. AAT finds an unrelaxed soft constraint for this step, relaxes it, re-resolves inputs, and retries the step. This loop continues until the step succeeds or no more constraints can be relaxed.

### Relaxation Budget

Each step has a relaxation budget (default: 3, configurable via `settings.maxRelaxationDepth` in your environment file). This prevents runaway relaxation:

- Each relaxation counts against the budget
- The same constraint can't be relaxed twice (circular detection)
- When the budget is exhausted, the step fails with whatever error triggered the last relaxation attempt

### Relaxation in Archives

Relaxation events appear in the run archive for full auditability:

```json
{
  "node": "addItem",
  "relaxations": [
    {
      "constraintName": "price_preference",
      "inputRef": "addItem.price",
      "reason": "resolution_exhausted",
      "depth": 1
    }
  ],
  "resolutions": [
    {
      "inputName": "price",
      "source": "plan_default",
      "finalValue": 79.99,
      "constraint": "value < 50",
      "constraintOK": false,
      "relaxed": true,
      "relaxedConstraint": "price_preference"
    }
  ]
}
```

The `relaxed: true` flag and `relaxedConstraint` name make it clear that this value was accepted despite failing its constraint. The `reason` field on the relaxation record indicates which scenario triggered it: `resolution_exhausted`, `filter_empty`, or `step_failed`.

## Type Coercion

After a value is resolved, AAT coerces it to match the input's declared type in the graph. This handles common mismatches between expression results and API expectations:

| Declared Type | Coercion |
|--------------|----------|
| `date` | Truncates datetime strings (`2026-03-15T00:00:00Z` becomes `2026-03-15`) |
| `datetime` | Formats `time.Time` values as RFC3339 |
| `integer` | Parses numeric strings; truncates floats |
| `boolean` | Parses `"true"`/`"false"` strings |
| `float` | Parses numeric strings |

If coercion fails (e.g., `"abc"` for an integer input), the original value is passed through so the downstream API reports the mismatch.

## Graph Defaults

The graph can define default values for inputs. These are used when the plan doesn't provide a value and no edge feeds the input:

```yaml
# In the graph definition
nodes:
  listProducts:
    inputs:
      - name: pageSize
        type: integer
        default: 20
      - name: sortBy
        type: string
        default: "relevance"
```

Plan values override graph defaults. If you want a larger page:

```yaml
values:
  pageSize: 50
  # sortBy not specified → uses graph default "relevance"
```

## Resolution Priority

When AAT resolves an input value, it checks these sources in order:

1. **Graph edge** — scalar output from an upstream step
2. **Select edge** — array output with selection strategy
3. **Named selection** — `fromSelection` reference to a pre-resolved element
4. **Plan value** — literal, expression, or constraint+fallback from the plan YAML
   - Evaluate the `default` value (including `{{...}}` expressions)
   - Check the `constraint` predicate
   - If the constraint fails, try each `fallbackPool` value in order
   - If all pool values fail, ask the LLM (lean/adaptive modes only)
   - If everything fails and the constraint is soft, relax it and accept the first tried value (adaptive mode, or lean with relaxation tracker)
5. **Graph default** — default defined in the graph schema
6. **Optional** — if the input is marked optional, it's skipped
7. **Error** — required input with no value fails the step

The first source that provides a value wins. This means edges (data flow between steps) always take priority over plan-provided literals.

## Putting It All Together

Here's a complete plan that uses most of these features:

```yaml
metadata:
  prompt: "Order the cheapest in-stock product"
  graphVersion: "1.0.0"

intent:
  goal: checkout
  description: "Browse products, add the cheapest to a cart, and complete checkout"

execution:
  steps:
    - node: listProducts
      description: "List available products"
      values:
        category: "electronics"
        pageSize: 50

    - node: createCart
      description: "Create a shopping cart"

    - node: addItem
      dependsOn: [listProducts, createCart]
      description: "Add the cheapest in-stock product to the cart"
      selections:
        product:
          from: listProducts.products
          strategy: min
          sortField: price
          filter: "inStock == true"
      values:
        productId:
          fromSelection: product.productId
        price:
          fromSelection: product.price
        cartId:
          from: createCart.cartId

    - node: addShipping
      dependsOn: [createCart]
      description: "Add standard shipping"
      values:
        cartId:
          from: createCart.cartId
        method: "standard"

    - node: checkout
      dependsOn: [addItem, addShipping]
      isGoal: true
      description: "Complete the order"
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldExists
            path: "orderId"

  cleanup:
    - node: cancelOrder
      runOn: always
```

In this plan:

- `listProducts` gets literal values for the search criteria
- `addItem` uses a **named selection** to extract two fields (`productId`, `price`) from the same product
- `addItem` also gets `cartId` via a **scalar reference** from `createCart`
- `addShipping` gets literal values for the shipping method
- `checkout` receives scalar references from multiple upstream steps (wired by the graph edges, not shown in the plan since the engine resolves them automatically)
- `cancelOrder` runs as cleanup regardless of success or failure

## Debugging Value Resolution

Every run produces a JSON archive in the output directory. The archive includes a `resolutions` array for each step showing exactly how each input was resolved:

```json
{
  "node": "addItem",
  "resolutions": [
    {
      "inputName": "productId",
      "source": "named_selection",
      "finalValue": "prod-101",
      "fromStep": "listProducts",
      "fromOutput": "products"
    },
    {
      "inputName": "deliveryDate",
      "source": "expression",
      "expression": "{{today + 7 days}}",
      "finalValue": "2026-02-23"
    }
  ]
}
```

### Resolution Sources

The `source` field tells you which resolution path was used:

| Source | Meaning |
|--------|---------|
| `edge` | Scalar output from an upstream step via a graph edge |
| `select_edge` | Selected from an upstream array output |
| `named_selection` | Extracted from a pre-resolved named selection |
| `plan_default` | Literal value from the plan YAML |
| `expression` | Computed from a `{{...}}` expression |
| `fallback_pool` | Default failed its constraint; this pool value passed |
| `graph_default` | No plan value; used the graph's default |
| `llm` | All deterministic paths failed; LLM provided the value |
| `optional_skip` | Input is optional and had no value |

### Reading the Audit Trail

When a constraint fails and fallback values are tried, the archive records every attempt:

```json
{
  "inputName": "deliveryDate",
  "source": "fallback_pool",
  "rawValue": "{{today + 10 days}}",
  "finalValue": "2026-03-06",
  "constraint": "value > '2026-03-01'",
  "constraintOK": true,
  "poolIndex": 0,
  "poolSize": 3,
  "tried": ["2026-02-23"]
}
```

The `tried` array shows values that were evaluated but rejected. `poolIndex` tells you which pool entry succeeded. Together these make it easy to trace why a particular value was chosen.

For LLM-assisted values, the `llmCall` object captures the full prompt, response, model, token counts, and timing.

For relaxed constraints, look for `relaxed: true` and the `relaxedConstraint` name. The step-level `relaxations` array shows all constraints relaxed during that step and the reason each was triggered.

### Selections

Array selection decisions are recorded in the `selections` array:

```json
{
  "inputName": "productId",
  "sourceNode": "listProducts",
  "sourceField": "products",
  "sourceSize": 50,
  "filteredSize": 12,
  "filterExpr": "inStock == true",
  "strategy": "min",
  "selectedIndex": 3
}
```

If a filter was relaxed, `filterRelaxed: true` appears. For LLM-assisted selection, the `llmCall` field contains the full exchange.
