# How Values Flow Between Steps

AAT plans describe multi-step API workflows where each step produces outputs that feed into later steps. This guide explains how input values are resolved — from simple literals to array selections and dynamic expressions.

## The Basics: Literal Values

The simplest case is a literal value written directly in the plan:

```yaml
steps:
  - node: searchFlights
    values:
      origin: "DEN"
      destination: "SFO"
      departureDate: "2026-03-15"
```

These values are passed directly to the step's template as-is.

## Scalar References: Passing Outputs Forward

When one step produces an output that another step needs, use a `from` reference:

```yaml
steps:
  - node: createWorkbench
    # produces output: workbenchId

  - node: addOffer
    dependsOn: [createWorkbench]
    values:
      workbenchId:
        from: createWorkbench.workbenchId
```

The format is `stepName.outputName`. At runtime, AAT looks up the output from the completed step and passes it through. The step must be listed in `dependsOn` so AAT knows to run it first.

## Array Selection: Choosing From Multiple Results

Many APIs return arrays — a flight search returns multiple offerings, a hotel search returns multiple rooms. When a downstream step needs a value from one element of an array, you need a **selection strategy**.

### Single-Field Selection

When only one input comes from an array, use inline `from` + `select`:

```yaml
values:
  offeringId:
    from: searchFlights.catalogOfferings
    select:
      strategy: first
      field: offeringId
```

This says: "Take the `catalogOfferings` array from `searchFlights`, pick the first element, and extract its `offeringId` field."

### Named Selections: Multiple Fields From the Same Element

Often you need several fields from the **same** array element. For example, booking a flight requires both an `offeringId` and a `productRef` from the same offering. Named selections guarantee they come from the same element:

```yaml
steps:
  - node: addOffer
    dependsOn: [searchFlights, createWorkbench]
    selections:
      offering:
        from: searchFlights.catalogOfferings
        strategy: first
    values:
      offeringId:
        fromSelection: offering.offeringId
      productRef:
        fromSelection: offering.productRef
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
# Cheapest offering
selections:
  offering:
    from: searchFlights.catalogOfferings
    strategy: min
    sortField: totalPrice

# Non-stop flights only
selections:
  offering:
    from: searchFlights.catalogOfferings
    strategy: match
    filter: "stops == 0"

# LLM picks the best option
selections:
  offering:
    from: searchFlights.catalogOfferings
    strategy: llm
    prompt: "Choose the cheapest direct flight departing in the morning"
```

### Filtering

The `filter` field uses predicate expressions to narrow the array before selection. This works with any strategy — you can filter to non-stop flights and then pick the cheapest:

```yaml
# Inline selection with filter
values:
  offeringId:
    from: searchFlights.catalogOfferings
    select:
      strategy: min
      sortField: totalPrice
      filter: "stops == 0"
      field: offeringId
```

## Dynamic Expressions

Values can include `{{...}}` expressions that are evaluated at runtime:

```yaml
values:
  departureDate: "{{today + 30 days}}"
  returnDate: "{{today + 37 days}}"
  bookingRef: "TEST_{{today}}"
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
  label: "Flight_{{origin}}_to_{{destination}}_{{departureDate}}"
```

When the entire value is a single expression (e.g., `"{{today + 30 days}}"`), it returns a typed value. When mixed with literal text, it returns a concatenated string.

## Constraints and Fallback Pools

Constraints validate that a resolved value meets a condition. Fallback pools provide alternative values to try when the primary value fails the constraint.

```yaml
values:
  departureDate:
    default: "{{today + 30 days}}"
    constraint: "value >= today"
    fallbackPool:
      - "{{today + 25 days}}"
      - "{{today + 20 days}}"
      - "{{today + 15 days}}"
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
  returnDate:
    default: "{{today + 37 days}}"
    constraint: "value > departureDate"
```

This ensures the return date is after the departure date, regardless of what `departureDate` resolved to.

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
  model: gpt-4
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
  "inputName": "departureDate",
  "source": "llm",
  "finalValue": "2026-04-15",
  "constraint": "value > today",
  "constraintOK": true,
  "tried": ["2025-12-01", "2025-11-15"],
  "llmCall": {
    "model": "gpt-4",
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
  offering:
    from: searchFlights.catalogOfferings
    strategy: llm
    prompt: "Choose the cheapest direct flight departing in the morning"
```

The LLM sees a tabular summary of the array elements (using the node's `elementFields` for column selection) and returns an index. This works in both `lean` and `adaptive` modes.

## Soft Constraint Relaxation

In `adaptive` mode, AAT can **relax** soft constraints when resolution would otherwise fail. This is the key difference between `adaptive` and `lean`: adaptive can recover from constraint failures instead of stopping.

### Hard vs Soft Constraints

Plans classify constraints by enforcement level:

```yaml
intent:
  goal: commitBooking
  constraints:
    hard:
      - type: route
        name: route_constraint
        description: "Must fly DEN to SFO"
        applies_to: [searchFlights.origin, searchFlights.destination]
    soft:
      - type: schedule
        name: date_preference
        description: "Prefer dates in March"
        applies_to: [searchFlights.departureDate]
    free:
      - traveler identity
```

- **Hard constraints** are never relaxed. If they can't be satisfied, the step fails.
- **Soft constraints** can be relaxed as a last resort. When relaxed, the engine accepts a value that would normally fail the constraint.
- **Free dimensions** have no constraints and accept any valid value.

### When Relaxation Happens

Relaxation can occur in three scenarios:

**1. Value resolution exhausted.** The default value, every fallback pool value, and the LLM all failed a constraint. If the constraint is soft, AAT relaxes it and accepts the first value that passed expression evaluation (but failed the constraint check).

```yaml
values:
  departureDate:
    default: "{{today + 30 days}}"
    constraint: "value > '2026-03-01' && value < '2026-03-31'"
    fallbackPool:
      - "{{today + 25 days}}"
      - "{{today + 20 days}}"
```

If today is January and none of these dates fall in March, but the constraint is classified as soft, AAT relaxes it and uses the first date (today + 30 days) anyway.

**2. Filter produces no matches.** A selection filter eliminates all array elements. If the filter corresponds to a soft constraint, AAT drops the filter and retries the selection on the full array.

```yaml
selections:
  offering:
    from: searchFlights.catalogOfferings
    strategy: min
    sortField: totalPrice
    filter: "stops == 0"
```

If no offerings are non-stop and the filter is tied to a soft constraint, AAT relaxes the filter and picks the cheapest from all offerings.

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
  "node": "searchFlights",
  "relaxations": [
    {
      "constraintName": "date_preference",
      "inputRef": "searchFlights.departureDate",
      "reason": "resolution_exhausted",
      "depth": 1
    }
  ],
  "resolutions": [
    {
      "inputName": "departureDate",
      "source": "plan_default",
      "finalValue": "2026-02-08",
      "constraint": "value > '2026-03-01'",
      "constraintOK": false,
      "relaxed": true,
      "relaxedConstraint": "date_preference"
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
  searchFlights:
    inputs:
      - name: passengers
        type: integer
        default: 1
      - name: cabinPreference
        type: string
        default: "economy"
```

Plan values override graph defaults. If you want 2 passengers:

```yaml
values:
  passengers: 2
  # cabinPreference not specified → uses graph default "economy"
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
  prompt: "Book a flight from DEN to SFO"
  graphVersion: "1.0.0"

intent:
  goal: commitBooking
  description: "Complete a flight booking from Denver to San Francisco"

execution:
  steps:
    - node: searchFlights
      description: "Search for flights DEN to SFO"
      values:
        origin: "DEN"
        destination: "SFO"
        departureDate: "{{today + 30 days}}"

    - node: createWorkbench
      description: "Create reservation workbench"

    - node: addOffer
      dependsOn: [searchFlights, createWorkbench]
      description: "Add the first available offer to the workbench"
      selections:
        catalogOffering:
          from: searchFlights.catalogOfferings
          strategy: first
      values:
        offeringId:
          fromSelection: catalogOffering.offeringId
        productRef:
          fromSelection: catalogOffering.productRef
        catalogOfferingsId:
          from: searchFlights.catalogOfferingsId

    - node: addTraveler
      dependsOn: [createWorkbench]
      description: "Add passenger details"
      values:
        surname: "Smith"
        givenName: "Jane"
        birthDate: "1990-01-15"
        gender: "Female"

    - node: commitBooking
      dependsOn: [addOffer, addTraveler]
      isGoal: true
      description: "Commit the booking to create PNR"
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldExists
            path: "$.ReservationResponse.Reservation.Receipt.0.Confirmation.Locator.value"

  cleanup:
    - node: ignoreWorkbench
      runOn: always
```

In this plan:

- `searchFlights` gets literal values, with `departureDate` computed dynamically
- `addOffer` uses a **named selection** to extract two fields from the same offering
- `addOffer` also gets `catalogOfferingsId` via a **scalar reference** from the search
- `addTraveler` gets literal values for the passenger
- `commitBooking` receives scalar references from multiple upstream steps (wired by the graph edges, not shown in the plan since the engine resolves them automatically)
- `ignoreWorkbench` runs as cleanup regardless of success or failure

## Debugging Value Resolution

Every run produces a JSON archive in the output directory. The archive includes a `resolutions` array for each step showing exactly how each input was resolved:

```json
{
  "node": "addOffer",
  "resolutions": [
    {
      "inputName": "offeringId",
      "source": "named_selection",
      "finalValue": "o123",
      "fromStep": "searchFlights",
      "fromOutput": "catalogOfferings"
    },
    {
      "inputName": "departureDate",
      "source": "expression",
      "expression": "{{today + 30 days}}",
      "finalValue": "2026-03-10"
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
  "inputName": "departureDate",
  "source": "fallback_pool",
  "rawValue": "{{today + 25 days}}",
  "finalValue": "2026-03-06",
  "constraint": "value > '2026-03-01'",
  "constraintOK": true,
  "poolIndex": 0,
  "poolSize": 3,
  "tried": ["2026-02-08"]
}
```

The `tried` array shows values that were evaluated but rejected. `poolIndex` tells you which pool entry succeeded. Together these make it easy to trace why a particular value was chosen.

For LLM-assisted values, the `llmCall` object captures the full prompt, response, model, token counts, and timing.

For relaxed constraints, look for `relaxed: true` and the `relaxedConstraint` name. The step-level `relaxations` array shows all constraints relaxed during that step and the reason each was triggered.

### Selections

Array selection decisions are recorded in the `selections` array:

```json
{
  "inputName": "offeringId",
  "sourceNode": "searchFlights",
  "sourceField": "catalogOfferings",
  "sourceSize": 12,
  "filteredSize": 3,
  "filterExpr": "stops == 0",
  "strategy": "min",
  "selectedIndex": 1
}
```

If a filter was relaxed, `filterRelaxed: true` appears. For LLM-assisted selection, the `llmCall` field contains the full exchange.
