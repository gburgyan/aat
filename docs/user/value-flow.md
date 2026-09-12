# Value Resolution

Every step input needs a value. AAT resolves values through a priority-based chain that tries each source in order, from the most specific (plan-provided) to the most general (graph defaults). Understanding this chain is key to writing effective plans and debugging unexpected results.

## Resolution Priority

When the engine resolves an input for a step, it checks these sources in order and uses the first match:

| Priority | Source | Description |
|----------|--------|-------------|
| 1 | Named selection (`fromSelection`) | Reference to a pre-resolved array selection |
| 2 | Intra-step reference (`fromResolved`) | Reference to an input resolved earlier in the same step |
| 3 | Cross-step input reference (`fromInput`) | Reference to a resolved input from a previous step |
| 4 | Step output reference (`from`) | Value from a previous step's output, optionally with array `select` |
| 5 | Plan default / expression / pool | Literal value, `{{...}}` expression, or fallback pool |
| 6 | Graph default | Default value declared on the node's input in the graph |
| 7 | Optional skip | Input is optional and no value was found — omit from request |
| 8 | Error | Required input has no value |

Priorities 5 and 6 are merged during plan instantiation — graph defaults (including value pools) are copied into the plan's step values before execution, so the engine sees them at priority 5. Layers (when applied) override graph defaults at the same level. This means a well-designed graph with pools on configurable inputs can handle most values automatically — plans only need to specify values that the test requires to be specific.

Two kinds of override sit outside this chain:
- A recipe's `overrides.values` rewrite the composed plan before it runs, replacing an input's literal or its wiring, so the engine sees them at priority 5.
- Override `values:` from `env.yaml` `overrides:`, `.aat-overrides.yaml`, or an `--overlay` file apply after resolution and win over everything. The archive records them with the source `override_value`.

The explicit-absence marker `{}` short-circuits this chain for an optional input: it tells the engine to skip the input entirely, bypassing graph defaults and auto-wiring. On a required input, a plain literal graph default still applies, and without one the step fails.

## Literal Values

The simplest value form is a bare scalar in the plan YAML:

```yaml
values:
  category: "electronics"
  maxResults: 20
  enabled: true
  threshold: 0.95
```

YAML type inference applies: quoted strings remain strings, unquoted numbers become integers or floats, `true`/`false` become booleans. When the graph declares a specific type for the input, the engine coerces the value (see [Type Coercion](#type-coercion)).

To explicitly mark an optional input as absent — preventing it from being filled by graph defaults or auto-wiring — use an empty map:

```yaml
values:
  optionalFilter: {}
```

## References

### Step Output References (from)

The `from` field pulls a value from a previous step's output:

```yaml
values:
  cartId: {from: createCart.cartId}
```

The syntax is `stepId.outputName`. The engine looks up the output value from the referenced step's execution results.

When the referenced output is a scalar, the value is used directly. When the output is an array, you typically need a `select` block (see [Array Selection](#array-selection)) or a named selection to pick one element.

### Dependency Inference

Any `from` reference implies an execution dependency, and the referenced step must appear in the step's `dependsOn`. In a hand-written full plan, list it yourself: `aat validate plan` (and `aat run`) reject a `from`, `fromInput`, or named selection whose step is missing from `dependsOn`. Workflow composition adds the entry for recipes and workflow templates, and plan instantiation adds it for `from` references that come from graph input defaults, so those need no `dependsOn` entry written out.

## Array Selection

When a step input needs a value from an array output, a selection strategy picks one element.

### Inline Selection (from + select)

Add a `select` block alongside `from` for a self-contained selection:

```yaml
values:
  productId:
    from: searchProducts.products
    select:
      strategy: min
      field: productId
      sortField: price
      filter: "inStock == true"
```

The `field` specifies which field to extract from the selected element. If `field` is omitted, the entire element is used as the value.

### Named Selections

For coordinated multi-field extraction — pulling several fields from the *same* array element — define a named selection in the step's `selections` block:

```yaml
  - node: addToCart
    dependsOn: [searchProducts]
    selections:
      bestProduct:
        from: searchProducts.products
        strategy: min
        sortField: price
        filter: "inStock == true"
    values:
      productId: {fromSelection: bestProduct.productId}
      productName: {fromSelection: bestProduct.name}
      unitPrice: {fromSelection: bestProduct.price}
```

All three values come from the same element because they reference the same named selection. Without a named selection, three separate inline `select` blocks could each pick a *different* element.

The `fromSelection` syntax is `selectionName.fieldName`. If the field part is omitted, the entire selected element is used.

### Selection Strategies

| Strategy | Required Fields | Description |
|----------|----------------|-------------|
| `first` | *(none)* | First element (after filtering). This is the default when there is no `filter`; with a `filter` and no `strategy`, the selection works like `match`. |
| `last` | *(none)* | Last element (after filtering) |
| `index` | `index` | Element at the specified zero-based index (after filtering) |
| `random` | *(none)* | Random element (after filtering) |
| `min` | `sortField` | Element with the smallest value of `sortField` |
| `max` | `sortField` | Element with the largest value of `sortField` |
| `match` | `filter` | First element matching the filter predicate (no pre-filtering) |

For `min` and `max`, the `sortField` must resolve to a number or to a string that holds one, such as `"99.10"`: APIs often send prices and totals as decimal strings, and they compare by value. Any other value fails the selection. The `field` parameter (if set) determines which field to extract from the winning element.

A `filter` does not convert strings: comparing a string field with a number fails. Filter such fields with string equality, or let `min` and `max` do the numeric comparison.

### Filtering

A `filter` expression narrows the array *before* the strategy is applied. The predicate runs against each element, keeping only those that evaluate to true:

```yaml
select:
  strategy: first
  filter: "status == 'available' && price < 1000"
```

If filtering removes all elements, the selection fails with an error.

For the `match` strategy, the filter serves as both the narrowing predicate and the selection criterion — it returns the first matching element without pre-filtering.

### Coordinated Multi-Field Extraction

When you need multiple fields from the same array element, always use a named selection:

```yaml
selections:
  chosenItem:
    from: listItems.items
    strategy: first
    filter: "category == 'premium'"
values:
  itemId: {fromSelection: chosenItem.id}
  itemName: {fromSelection: chosenItem.name}
  itemPrice: {fromSelection: chosenItem.price}
```

This guarantees that `itemId`, `itemName`, and `itemPrice` all come from the same element.

## Intra-Step References

`fromResolved` references a value that was resolved earlier in the same step:

```yaml
values:
  outboundOrigin: "JFK"
  outboundDestination: "LAX"
  returnOrigin: {fromResolved: outboundDestination}
  returnDestination: {fromResolved: outboundOrigin}
```

This is useful when inputs are logically related — a return trip reverses the origin and destination. The referenced input must appear before the referencing input in the node's declared input order (inputs are resolved in declaration order).

## Cross-Step Input References

`fromInput` references a resolved input value from a previous step:

```yaml
steps:
  - node: searchFlights
    values:
      origin:
        pool: ["DEN", "LAX", "ORD"]
      destination: "JFK"

  - node: bookFlight
    dependsOn: [searchFlights]
    values:
      origin: {fromInput: searchFlights.origin}
      destination: {fromInput: searchFlights.destination}
```

This differs from `from` (which references step *outputs* extracted from API responses). `fromInput` references the *inputs* that were sent to a previous step. This is useful when multiple steps need consistent input values — for example, using the same origin city for both a search and a booking.

The syntax is `stepId.inputName`. The referenced step must be listed directly in `dependsOn` (a transitive dependency is not enough), and the input name must exist on the source step's graph node.

`fromInput` is mutually exclusive with `from`, `fromSelection`, `fromResolved`, `default`, and `pool`.

## Dynamic Expressions

Expressions use `{{...}}` delimiters and are evaluated at execution time.

### Date Expressions

```yaml
values:
  departureDate: "{{today}}"              # today's date (YYYY-MM-DD)
  returnDate: "{{today + 7 days}}"        # 7 days from today
  pastDate: "{{today - 30 days}}"         # 30 days ago
```

Date expressions always produce `YYYY-MM-DD` format strings.

### Environment Variables

```yaml
values:
  apiKey: "{{env.API_KEY}}"
  region: "{{env.TEST_REGION}}"
```

`{{env.KEY}}` checks the OS environment variable `KEY` first, then the `values:` map of the selected environment in env.yaml. An error is raised if neither provides a non-empty value. This works wherever a value is resolved — plan step values, graph input defaults, layer values, and pool entries — but not in request templates: a template placeholder is filled only from step inputs, and env.yaml `values:` do not reach templates. To send an environment value, give the input a default such as `"{{env.postalCode}}"` in the graph and use `{{postalCode}}` in the template.

### Reference Arithmetic

```yaml
values:
  departureDate: "2026-03-15"
  returnDate: "{{departureDate + 7 days}}"
```

Reference arithmetic operates on a previously resolved input's value. The referenced value must be a date string in `YYYY-MM-DD` format.

### Mixed Expressions

Expressions can be embedded in literal strings:

```yaml
values:
  searchQuery: "flights departing {{today + 3 days}}"
  correlationId: "test-{{env.BUILD_ID}}-run"
```

When an expression is the entire value (no surrounding text), the result retains its native type (e.g., a date string). When embedded in text, the result is always a string.

## Fallback Pools

A pool is a list of candidate values. The engine uses it in two cases only:

- **There is no `default`.** The engine picks from the pool. This is what the list shorthand `default: [a, b, c]` on a graph input produces: a random pick on each run.
- **The `default` fails its `constraint`.** The engine then tries the pool entries (evaluating any expressions) and uses the first one that passes.

A `default` without a `constraint` always wins, so a pool next to it is never used:

```yaml
values:
  origin: "JFK"
  destination:
    default: "JFK"
    pool: ["LAX", "ORD", "SFO"]
    constraint: "value != origin"   # "JFK" fails, so a pool entry is used
```

Pool iteration order depends on `poolStrategy`:

| Strategy | Behavior |
|----------|----------|
| `random` | Shuffled order (default) |
| `sequential` | Declaration order |

A pool entry whose expression cannot be evaluated is skipped. When no entry passes the constraint, the step fails with an error naming the constraint. An expression in the `default` itself that cannot be evaluated is an error, not a reason to fall back.

### Constraints

`constraint` is a [predicate expression](#predicate-expression-syntax) that a candidate value must satisfy. The candidate is available as `value`, and every input of the same step resolved before this one is available by name — inputs resolve in the order the graph node declares them, so `origin` must come before `destination` in the example above. The constraint is checked against the `default` and against pool entries; it is not applied to `from`, `fromSelection`, `fromInput`, or `fromResolved` values. Graph input defaults and layer entries accept the same `pool`, `poolStrategy`, and `constraint` fields (a graph default writes its literal as `value:` instead of `default:`).

## Type Coercion

The engine coerces resolved values based on the graph input's declared type:

| Input Type | Coercion Rules |
|-----------|----------------|
| `date` | Datetime strings (`2026-01-15T00:00:00Z`) are truncated to date (`2026-01-15`); `time.Time` values are formatted as `YYYY-MM-DD` |
| `datetime` | `time.Time` values are formatted as RFC3339 |
| `integer` | String digits are parsed to int; floats are truncated |
| `boolean` | String `"true"`/`"false"` are parsed to bool |
| `float` | String numbers are parsed to float64 |

Coercion is best-effort — if parsing fails, the original value is passed through so the downstream API can report the type mismatch.

## Predicate Expression Syntax

Predicate expressions are used in selection filters, value `constraint`s, and `predicate` assertions. They support standard comparison and logical operators:

### Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `==` | Equal | `status == "active"` |
| `!=` | Not equal | `type != "archived"` |
| `<` | Less than | `price < 100` |
| `>` | Greater than | `quantity > 0` |
| `<=` | Less than or equal | `rating <= 5.0` |
| `>=` | Greater than or equal | `stock >= 10` |
| `&&` | Logical AND | `inStock == true && price < 50` |
| `\|\|` | Logical OR | `status == "active" \|\| status == "pending"` |
| `!` | Logical NOT | `!isExpired` |
| `in` | Membership | `category in ["electronics", "books"]` |

### Value Types

- **Strings**: `"double"` or `'single'` quotes — single quotes avoid escaping inside a double-quoted YAML string, as in `filter: "status == 'available'"`. There are no escape sequences.
- **Numbers**: `42`, `3.14`, `-5` (integer and float)
- **Booleans**: `true`, `false`
- **Identifiers**: `fieldName`, `nested.path` (dot notation through nested objects; no array indexes or queries)
- **Parentheses**: `(a || b) && c`

Predicates evaluate against a context map:

- In selection filters, the context is the array element.
- In a `constraint`, it is `value` plus the step's inputs resolved so far.
- In `predicate` assertions, it is the step's extracted outputs, keyed by output name — or the raw response body when the assertion sets `raw: true`, or when the response status is 400 or above (nothing is extracted then). See [Plans: Assertions](plans.md#assertions).

A field that does not exist in the context is an error rather than `false`, so a filter or assertion that names a missing field fails with `unknown field`. Comparing values of different types (a string with a number) is an error too.

## Debugging Value Resolution

Every input resolution is recorded in the run archive with a `ValueResolution` entry:

| Field | Description |
|-------|-------------|
| `inputName` | Which input was resolved |
| `source` | How it was resolved: `plan_default`, `expression`, `fallback_pool`, `plan_from`, `select_edge`, `named_selection`, `from_resolved`, `from_input`, `graph_default`, `optional_skip` |
| `rawValue` | Value before expression evaluation |
| `finalValue` | Value after evaluation and type coercion |
| `expression` | The `{{...}}` template if evaluated |
| `constraint` | The constraint checked, if any |
| `constraintOk` | Whether the chosen value passed it |
| `poolIndex` | Which pool entry was used (-1 if not from pool) |
| `poolSize` | Total pool size |
| `tried` | Values that were tried and rejected before the winning value |

Array selections are recorded as `SelectionDecision` entries:

| Field | Description |
|-------|-------------|
| `sourceNode` | Step that produced the array |
| `sourceField` | Output name of the array |
| `sourceSize` | Array size before filtering |
| `filteredSize` | Array size after filtering |
| `strategy` | Selection strategy used |
| `selectedIndex` | Index of the selected element |
| `filterExpr` | Filter predicate if applied |
| `selectionName` | Named selection name (if applicable) |

Use the [Web UI](web-ui.md) to inspect these records in the step detail view, or read the archive JSON directly.

## Common Patterns

### Date Expressions for Testing

```yaml
values:
  departureDate: "{{today + 3 days}}"
  returnDate: "{{today + 10 days}}"
```

### Named Selection with Multi-Field Extraction

```yaml
selections:
  bestOffer:
    from: searchProducts.products
    strategy: min
    sortField: price
    filter: "inStock == true && rating >= 4.0"
values:
  productId: {fromSelection: bestOffer.productId}
  productName: {fromSelection: bestOffer.name}
  unitPrice: {fromSelection: bestOffer.price}
```

### Pool with Expressions

```yaml
values:
  departureDate:
    pool: ["{{today + 2 days}}", "{{today + 5 days}}", "{{today + 10 days}}"]
    poolStrategy: random
```

### Intra-Step Reference Chaining

```yaml
values:
  outboundOrigin: "JFK"
  outboundDestination: "LAX"
  returnOrigin: {fromResolved: outboundDestination}
  returnDestination: {fromResolved: outboundOrigin}
```

### Cross-Step Input Consistency

```yaml
steps:
  - node: searchFlights
    values:
      origin:
        pool: ["DEN", "LAX", "ORD", "SFO"]
      destination: "JFK"
      departureDate: "{{today + 7 days}}"

  - node: bookFlight
    dependsOn: [searchFlights]
    values:
      origin: {fromInput: searchFlights.origin}
      destination: {fromInput: searchFlights.destination}
      departureDate: {fromInput: searchFlights.departureDate}
```

### Explicit Absence to Override Defaults

```yaml
values:
  # Suppress the optional discount code — don't use the graph default
  discountCode: {}
```

---

*Source: resolution logic in `engine/resolve.go`, selection strategies in `engine/selection.go`, expressions in `plan/expr.go`, predicates in `plan/predicate.go`, type coercion in `engine/resolve.go`.*
