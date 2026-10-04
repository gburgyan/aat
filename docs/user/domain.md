# Domain Knowledge

A graph says what an operation takes; it does not say that a refund cannot exceed the charge, or which currency codes are real. The domain file holds that kind of knowledge for the tools that write plans — `aat prompt`, an assistant through the MCP server — and for the docs `aat docs generate` writes. Runs never read it.

Domain knowledge teaches AAT about your API's business domain — the concepts, data types, and representative values that make generated plans realistic and meaningful.

## Overview

A domain file (`domain.yaml`) has three sections:

- **Concepts** — semantic rules about how fields relate to each other and what constraints apply
- **Types** — custom data type definitions with formats and validation patterns
- **Value Pools** — curated sets of realistic test data tied to specific types

Domain knowledge is optional but recommended for complex APIs. It gives the LLM planning pipeline (`aat prompt`) business context and example values, feeds the MCP server's domain tools and resources, and adds example values to generated documentation. It does not affect plan execution: `aat run` never draws values from domain pools (value pools for execution live in graph defaults and plan values; see [Value Resolution](value-flow.md)).

## When to Use Domain Knowledge

Domain knowledge is most valuable when:

- **Multiple endpoints share business concepts** — e.g., "order status" appears in create, update, and query operations, and valid transitions matter
- **Values need to be realistic** — random strings won't do; you want `aat prompt` and AI assistants to use actual SKUs, currency codes, or postal codes
- **The LLM needs business context** — `aat prompt` generates better plans when it understands what your API does
- **You want enriched documentation** — `aat docs generate --domain` fills an Examples column for inputs from concepts and pools

If your API is simple or you're only running pre-written plans with hardcoded values, you can skip the domain file entirely.

## Quick Start

A minimal domain file with one pool and one type:

```yaml
concepts: {}

types:
  currency-code:
    description: "ISO 4217 currency code"
    format: "3-letter uppercase code"
    validation: "^[A-Z]{3}$"
    pool: currencies

valuePools:
  currencies:
    description: "Common currency codes for testing"
    type: currency-code
    values:
      - USD
      - EUR
      - GBP
      - JPY
```

The pool `currencies` provides values for the type `currency-code`. When `aat prompt` asks the model for a value of an input typed `currency-code`, it shows sample values from this pool. To have runs draw from a list of values, give the graph input a pool default instead (`default: [USD, EUR, GBP]`).

## Concepts

Concepts describe semantic rules that apply to fields across your API. Each concept has a name, a description, and an `applies_to` list of field names it governs.

```yaml
concepts:
  order-lifecycle:
    description: >
      Orders progress through a defined lifecycle: pending → confirmed →
      shipped → delivered. Cancellation is only valid before shipment.
    applies_to: [orderStatus, status]
    constraint: "Status transitions must follow the lifecycle order"
    examples:
      valid: ["pending → confirmed", "confirmed → shipped"]
      invalid: ["delivered → pending", "shipped → confirmed"]

  pricing-consistency:
    description: >
      Line item prices multiplied by quantity must equal the line total.
      The sum of line totals plus tax must equal the order total.
    applies_to: [unitPrice, quantity, lineTotal, orderTotal]
    constraint: "Arithmetic consistency across pricing fields"
```

The `applies_to` field uses **bare field names** (e.g., `status`, `orderTotal`) that are matched against graph node input and output names.

| Field | Required | Description |
|-------|----------|-------------|
| `description` | Yes | What this concept means and why it matters |
| `applies_to` | Yes | List of field names this concept governs |
| `constraint` | No | Constraint in prose, shown to the model (not evaluated) |
| `examples` | No | Named example groups (map of name to string list) |

## Types

Types define custom data formats with optional validation. They map to the `type` field on graph node inputs and outputs.

```yaml
types:
  sku:
    description: "Stock Keeping Unit identifier"
    format: "PREFIX-NNNNN (letter prefix, dash, 5 digits)"
    validation: "^[A-Z]+-\\d{5}$"
    pool: product-skus

  postal-code:
    description: "US ZIP code"
    format: "5-digit or ZIP+4 format"
    validation: "^\\d{5}(-\\d{4})?$"

  shipping-address:
    description: "Complete shipping address"
    format: "Composite type with street, city, state, and postal code"
    fields:
      street:
        type: string
        description: "Street address line"
      city:
        type: string
        description: "City name"
      state:
        type: string
        description: "2-letter state code"
        constraint: "Must be a valid US state abbreviation"
      postalCode:
        type: postal-code
        description: "ZIP code"
```

| Field | Required | Description |
|-------|----------|-------------|
| `description` | Yes | Human-readable description of the type |
| `format` | Yes | Expected format (used in LLM prompts) |
| `validation` | No | Regex pattern for value validation |
| `pool` | No | Name of the value pool to draw from |
| `fields` | No | Sub-fields for composite types |

When a type references a `pool`, `aat prompt`, `aat docs generate`, and the MCP tools show values from that pool for inputs of that type. The `validation` regex is descriptive: it is compiled when the file loads (an invalid pattern is a validation error), but no value is checked against it.

### Composite Types

Types with `fields` describe structured data. Each field has its own type, description, and optional constraint or strategy:

```yaml
types:
  line-item:
    description: "Order line item"
    format: "Product reference with quantity and pricing"
    fields:
      productId:
        type: sku
        description: "Product identifier"
      quantity:
        type: integer
        description: "Number of units"
        constraint: "Must be >= 1"
      unitPrice:
        type: decimal
        description: "Price per unit"
        strategy: "extract"
```

## Value Pools

Value pools provide curated test data for specific types. People and models read them when they choose values — `aat prompt` samples them into the model's prompt, and the MCP server lists them — and a graph default, a layer, or a step value can draw from one with `poolRef`:

```yaml
# graph.yaml
- name: origin
  type: string
  default:
    poolRef: airportCodes.us        # one group; poolRef: airportCodes takes them all
- name: destination
  type: string
  default:
    poolRef: airportCodes.us
    constraint: "value != origin"
```

The engine reads the pool when the step runs, so it behaves as a `pool` written in place: `constraint` and `poolStrategy` apply, the run's [seed](value-flow.md#replaying-a-runs-picks) decides the pick, and the archive records which pool the value came from. One list in the domain file then serves every input that takes an airport, and a layer switches them all to Europe with `poolRef: airportCodes.eu`. `aat validate` reports a `poolRef` that names no pool or group, with a suggestion.

### Flat Pools

A simple list of values:

```yaml
valuePools:
  currencies:
    description: "ISO 4217 currency codes"
    type: currency-code
    values:
      - USD
      - EUR
      - GBP
      - JPY
      - CAD
```

### Grouped Pools

Values organized into named categories:

```yaml
valuePools:
  shipping-methods:
    description: "Available shipping options"
    type: shipping-method
    groups:
      standard:
        - ground
        - economy
      express:
        - next-day
        - two-day
        - same-day
      international:
        - intl-standard
        - intl-express
```

Both `values` and `groups` can be present on the same pool. All values are combined when a pool is sampled or listed, and when a `poolRef` names the pool; `poolRef: shipping-methods.express` names one group.

| Field | Required | Description |
|-------|----------|-------------|
| `description` | Yes | What this pool represents |
| `type` | Yes | The domain type these values belong to |
| `values` | No | Flat list of values (at least one of `values` or `groups` required) |
| `groups` | No | Named categories of values (at least one of `values` or `groups` required) |

### Annotations

YAML inline comments on pool values are extracted as annotations, and head comments above a value mark section boundaries. Both appear when the MCP server formats the domain for a model (the `aat://domain` resource and its prompts); `aat prompt` shows plain sample values.

```yaml
valuePools:
  product-categories:
    description: "Product category codes"
    type: category
    values:
      # Electronics
      - ELEC-TV        # Televisions and displays
      - ELEC-AUDIO     # Audio equipment
      - ELEC-COMP      # Computers and peripherals
      # Home & Garden
      - HOME-FURN      # Furniture
      - HOME-GARDEN    # Garden supplies
      - HOME-KITCHEN   # Kitchen appliances
```

When formatted for a model, this produces:

```
Values: [Electronics] ELEC-TV (Televisions and displays), ELEC-AUDIO (Audio equipment),
ELEC-COMP (Computers and peripherals); [Home & Garden] HOME-FURN (Furniture),
HOME-GARDEN (Garden supplies), HOME-KITCHEN (Kitchen appliances)
```

Section labels (from head comments) are shown in brackets. Annotations (from inline comments) are shown in parentheses. Sections are separated by semicolons for visual clarity.

## Integration Points

### Value Resolution

The engine does not use the domain file: `aat run` loads it (an invalid file stops the run) but resolves inputs only from plan values, step outputs, and graph defaults. To give runs varied realistic data, put a pool in the graph input's default (`default: ["USD", "EUR"]`) or in the plan. See [Value Flow](value-flow.md) for the resolution chain.

### Planning Context

When `aat prompt` asks the model to fill in values, it describes each input it asks about with domain knowledge:

- An input whose `type` names a domain type gets that type's description and `format`, plus up to 8 sampled values from the type's `pool`
- An input whose name is in a concept's `applies_to` gets the concept's description and `constraint`; when no type pool supplied values, it gets samples from a type named like the concept, or else the concept's `examples`

The MCP server gives assistants the whole file instead: the `aat://domain` resource and several MCP prompts include every concept, type, and pool, with pool values truncated to 10 entries and annotations and section labels preserved, and the domain tools (`list_concepts`, `list_types`, `list_value_pools`, `explain_concept`) query it.

This gives the model enough context to generate plans that use realistic values and respect business rules, without overwhelming the prompt with raw data.

### Documentation

`aat docs generate --domain domain.yaml` adds an Examples column to each node's input table. For each input it uses the first source that has values, up to 5 of them:

- The `examples` of concepts whose `applies_to` lists the input name
- The pool of a domain type named like the input's `type`
- The pool of a domain type named like the input
- The values of an `enum[...]` type

## Merge Behavior

A project has one domain file: the manifest's `domain` and the `--domain` flag each take a single path, and no command merges several. The `domain` package's `Merge` function, for Go callers, combines knowledge bases: concepts and types with the same key are replaced by later ones, and value pools are merged additively (`values` appended, `groups` entries added or appended).

## Complete Example

A full domain file for an e-commerce order management API:

```yaml
concepts:
  order-lifecycle:
    description: >
      Orders follow a strict lifecycle: draft → pending → confirmed →
      processing → shipped → delivered. Cancellation is allowed before
      the processing stage. Returns are allowed after delivery within
      30 days.
    applies_to: [orderStatus, status]
    constraint: "Status transitions must follow the defined lifecycle"
    examples:
      valid: ["draft → pending", "pending → confirmed", "confirmed → processing"]
      invalid: ["delivered → draft", "shipped → pending"]

  inventory-availability:
    description: >
      Products can only be added to orders when they are in stock.
      The requested quantity must not exceed available inventory.
    applies_to: [productId, quantity, stockLevel]
    constraint: "quantity <= stockLevel for the given productId"

types:
  sku:
    description: "Stock Keeping Unit — unique product identifier"
    format: "Category prefix, dash, 5 digits (e.g., ELEC-00042)"
    validation: "^[A-Z]+-\\d{5}$"
    pool: product-skus

  currency-code:
    description: "ISO 4217 currency code"
    format: "3-letter uppercase alphabetic code"
    validation: "^[A-Z]{3}$"
    pool: currencies

  postal-code:
    description: "US ZIP code"
    format: "5-digit or ZIP+4 (e.g., 90210 or 90210-1234)"
    validation: "^\\d{5}(-\\d{4})?$"
    pool: us-zip-codes

  order-status:
    description: "Current state of an order in the lifecycle"
    format: "One of: draft, pending, confirmed, processing, shipped, delivered, cancelled, returned"
    validation: "^(draft|pending|confirmed|processing|shipped|delivered|cancelled|returned)$"

valuePools:
  product-skus:
    description: "Sample product SKUs for testing"
    type: sku
    groups:
      electronics:
        - ELEC-00001
        - ELEC-00042
        - ELEC-00099
      clothing:
        - CLTH-00010
        - CLTH-00025
      home:
        - HOME-00003
        - HOME-00017

  currencies:
    description: "Common currency codes"
    type: currency-code
    values:
      - USD    # US Dollar
      - EUR    # Euro
      - GBP    # British Pound
      - JPY    # Japanese Yen
      - CAD    # Canadian Dollar

  us-zip-codes:
    description: "Sample US ZIP codes across regions"
    type: postal-code
    values:
      # West Coast
      - "90210"    # Beverly Hills, CA
      - "94102"    # San Francisco, CA
      - "98101"    # Seattle, WA
      # East Coast
      - "10001"    # New York, NY
      - "02101"    # Boston, MA
      - "33101"    # Miami, FL
      # Central
      - "60601"    # Chicago, IL
      - "75201"    # Dallas, TX

  shipping-methods:
    description: "Available shipping options"
    type: shipping-method
    groups:
      domestic:
        - ground
        - two-day
        - next-day
      international:
        - intl-standard
        - intl-express
```

## Validation

`aat validate` checks the manifest's domain file for structural correctness:

- Unknown keys are errors, reported with the line and a suggestion
- The knowledge base must define at least one concept, type, or value pool
- Concepts require `description` and at least one `applies_to` entry
- Types require `description` and `format`
- Type `validation` must be a valid regex (compiled at parse time)
- Type `pool` must reference an existing value pool name
- Value pools require `description` and `type`
- Value pools must have at least one non-empty `values` list or `groups` entry

See [Validation](validation.md) for the full reference covering all validation subcommands.

## Schema Reference

```yaml
# domain.yaml — complete annotated example

concepts:                                  # semantic rules for fields
  concept-name:
    description: "What this concept means"  # required
    applies_to: [field1, field2]            # required — bare field names
    constraint: "Machine-readable rule"     # optional
    examples:                               # optional — named example groups
      valid: ["example 1", "example 2"]
      invalid: ["bad example"]

types:                                     # custom data type definitions
  type-name:
    description: "What this type represents"  # required
    format: "Expected format or pattern"      # required
    validation: "^regex-pattern$"             # optional — validated at parse time
    pool: pool-name                           # optional — value pool to draw from
    fields:                                   # optional — composite type sub-fields
      fieldName:
        type: other-type                      #   field type name
        description: "Field description"      #   optional
        constraint: "Field constraint"        #   optional
        strategy: "resolution strategy"       #   optional

valuePools:                                # curated test data
  pool-name:
    description: "What these values represent"  # required
    type: type-name                             # required — domain type
    values:                                     # flat value list
      # Section Label (from head comment)
      - value1    # Annotation (from inline comment)
      - value2
    groups:                                     # grouped values (alternative to flat list)
      group-name:
        - value-a
        - value-b
```

<!-- Source: `domain/types.go`, `domain/parse.go` (including `Merge`), `domain/validate.go`, `domain/query.go`; used by `intent/targeted.go`, `cmd/aat/docs_cmd.go`, and `mcp/`. -->
