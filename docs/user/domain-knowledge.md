# Domain Knowledge

Domain knowledge teaches AAT about your API's business concepts, data types, and valid values. It's optional -- AAT works without it -- but it significantly improves LLM-assisted plan generation and value selection.

## When to Use Domain Knowledge

Domain knowledge matters in two scenarios:

1. **Plan generation** (`aat prompt`) -- The LLM uses domain concepts to understand what values make sense for your API and how inputs relate to business rules.
2. **Value resolution** (`lean`/`adaptive` modes) -- When the engine needs to pick a value and no explicit value is provided, domain knowledge helps the LLM make realistic choices.

In `strict` mode with fully-specified plans, domain knowledge has no effect on execution.

## Quick Start

Create a `domain.yaml`:

```yaml
concepts:
  petAvailability:
    description: "Whether a pet is available for adoption"
    applies_to: [status]

types:
  petName:
    description: "Common name for a pet"
    validation: "^[A-Za-z][A-Za-z ]{0,49}$"
    pool: petNames

valuePools:
  petNames:
    type: string
    values:
      - Buddy
      - Max
      - Bella
      - Charlie
      - Luna
```

Use it (with `aat-project.yaml`, only `--domain` is needed if not in the manifest):

```bash
aat prompt "Create a pet and verify it exists" --domain domain.yaml
```

Or with explicit paths:

```bash
aat prompt "Create a pet and verify it exists" \
  --env env.yaml --graph graph.yaml --templates templates/ \
  --domain domain.yaml
```

Or with `aat run` in lean/adaptive mode:

```bash
aat run --plan plan.yaml --domain domain.yaml --mode lean
```

## Schema Reference

A domain knowledge file has three top-level sections, all maps keyed by name:

```yaml
concepts:                    # semantic rules about your API
  <name>:
    description: ...
    applies_to: [...]
    constraint: ...
    examples:
      <category>: [...]

types:                       # data type definitions
  <name>:
    description: ...
    format: ...
    validation: ...
    fields:
      <field>:
        type: ...
        description: ...
    pool: ...

valuePools:                  # representative values
  <name>:
    description: ...
    type: ...
    values: [...]
    groups:
      <category>: [...]
```

## Concepts

Concepts describe semantic rules or constraints that apply to specific fields in your API. They help the LLM understand domain logic that isn't obvious from the API schema alone.

```yaml
concepts:
  futureTravel:
    description: "Travel dates must be in the future"
    applies_to: [departureDate, returnDate]
    constraint: "date > today"
    examples:
      valid: ["2026-06-15", "2026-12-01"]
      invalid: ["2020-01-01", "yesterday"]

  roundTrip:
    description: "Round trips require a return date after departure"
    applies_to: [departureDate, returnDate]
    constraint: "returnDate > departureDate"
```

### Concept Fields

| Field | Required | Description |
|-------|----------|-------------|
| `description` | yes | What this concept means in your domain |
| `applies_to` | yes | List of input names this concept is relevant to |
| `constraint` | no | A rule expression (informational for the LLM) |
| `examples` | no | Categorized example values (map of category to string lists) |

The `applies_to` field references input names as they appear in the graph. When the LLM generates a plan or selects values, it sees which concepts apply to each input.

## Types

Types define custom data formats that map to the `type` field on graph node inputs. They help the LLM generate valid values and understand composite structures.

```yaml
types:
  airportCode:
    description: "IATA 3-letter airport code"
    format: "XXX (uppercase letters)"
    validation: "^[A-Z]{3}$"
    pool: airports

  traveler:
    description: "Traveler information for a booking"
    fields:
      firstName:
        type: string
        description: "Legal first name"
      lastName:
        type: string
        description: "Legal last name"
      dateOfBirth:
        type: date
        description: "Birth date in YYYY-MM-DD format"
        constraint: "must be in the past"
      gender:
        type: string
        description: "Gender code"
        strategy: random
```

### TypeDef Fields

| Field | Required | Description |
|-------|----------|-------------|
| `description` | yes | What this type represents |
| `format` | no | Human-readable format description |
| `validation` | no | Regex pattern for value validation |
| `fields` | no | Sub-fields for composite types |
| `pool` | no | Name of a ValuePool to draw values from |

### FieldDef Fields (within `fields`)

| Field | Required | Description |
|-------|----------|-------------|
| `type` | yes | Data type of this field |
| `description` | no | What this field represents |
| `constraint` | no | Rule the field must satisfy |
| `strategy` | no | Selection strategy hint (e.g., `random`) |

### Mapping to Graph Types

Types connect to the graph through input type names. If a graph node declares an input with `type: airportCode`, the LLM looks up the `airportCode` type definition for format rules and the associated value pool.

## Value Pools

Value pools provide lists of representative values for a type. The engine and LLM use these when they need to pick a value and no explicit one is provided.

### Flat List

```yaml
valuePools:
  petNames:
    description: "Common pet names"
    type: string
    values:
      - Buddy
      - Max
      - Bella
      - Charlie
      - Luna
      - Cooper
      - Daisy
```

### Grouped Values

```yaml
valuePools:
  airports:
    description: "Major airport codes by region"
    type: string
    groups:
      US:
        - DEN
        - LAX
        - JFK
        - ORD
        - SFO
      Europe:
        - LHR
        - CDG
        - FRA
        - AMS
      Asia:
        - NRT
        - HKG
        - SIN
```

### ValuePool Fields

| Field | Required | Description |
|-------|----------|-------------|
| `description` | no | What these values represent |
| `type` | yes | Data type of the values |
| `values` | no* | Flat list of values |
| `groups` | no* | Categorized values (map of category to value lists) |

*At least one of `values` or `groups` must be present.

## How Domain Knowledge Integrates

### With Plan Generation

When you run `aat prompt`, the domain knowledge is formatted into the LLM's system prompt. The LLM sees:

- Which concepts apply to each input it needs to fill
- What types are available and their formats
- What value pools exist for selecting realistic data

This helps the LLM generate plans with valid, realistic values instead of generic placeholders.

### With Value Resolution

During execution in `lean` or `adaptive` mode, when the engine can't resolve an input from the plan, edges, or fallback pools, it asks the LLM. The domain knowledge provides context so the LLM can pick appropriate values:

- Value pools are tried before the LLM (pool values are deterministic)
- If pools are exhausted, the LLM uses type definitions and concepts to generate a value
- Constraints from concepts inform the LLM about what values are valid

### Without Domain Knowledge

AAT works fine without domain knowledge:

- In `strict` mode, all values must be in the plan -- domain knowledge is irrelevant
- In `lean`/`adaptive` mode without domain, the LLM uses only the graph's type annotations and descriptions
- Plan generation still works but may produce less realistic values

## Complete Example

```yaml
concepts:
  petAvailability:
    description: "Whether a pet is available for adoption"
    applies_to: [status]
    examples:
      valid: ["available", "pending"]
      sold: ["sold"]

  petNaming:
    description: "Pet names should be friendly and recognizable"
    applies_to: [name]
    constraint: "alphabetic characters only, 1-50 chars"

types:
  petName:
    description: "Common name for a pet"
    format: "Capitalized word"
    validation: "^[A-Za-z][A-Za-z ]{0,49}$"
    pool: petNames

  petStatus:
    description: "Pet availability status"
    format: "One of: available, pending, sold"

valuePools:
  petNames:
    description: "Common pet names"
    type: string
    values:
      - Buddy
      - Max
      - Bella
      - Charlie
      - Luna
      - Cooper
      - Daisy
      - Rocky
      - Sadie
      - Bear
```

## See Also

- [Environments](environments.md) -- LLM configuration for modes that use domain knowledge
- [Value Flow](value-flow.md) -- how value resolution uses pools and LLM fallback
- [LLM-Assisted Planning](prompt-workflow.md) -- using domain knowledge with `aat prompt`
- [Petstore Example](../../examples/petstore/README.md) -- includes a `domain.yaml`
