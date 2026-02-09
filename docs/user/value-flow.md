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

The `source` field tells you which resolution path was used: `edge`, `select_edge`, `named_selection`, `plan_default`, `expression`, `fallback_pool`, `graph_default`, or `llm`.

When a constraint fails and fallback values are tried, the archive records every attempt in the `tried` array, making it easy to see why a particular value was chosen.
