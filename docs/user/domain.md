# Domain Knowledge

Domain knowledge teaches AAT about your API's business domain — the concepts, data types, and representative values that make generated plans realistic and meaningful.

## Overview

A domain file (`domain.yaml`) has three sections:

- **Concepts** — semantic rules about how fields relate to each other and what constraints apply
- **Types** — custom data type definitions with formats and validation patterns
- **Value Pools** — curated sets of realistic test data tied to specific types

Domain knowledge is optional but recommended for complex APIs. It improves value resolution in the engine, gives the LLM planning pipeline business context, and enriches generated documentation.

## When to Use Domain Knowledge

Domain knowledge is most valuable when:

- **Multiple endpoints share business concepts** — e.g., "order status" appears in create, update, and query operations, and valid transitions matter
- **Values need to be realistic** — random strings won't do; you need actual SKUs, currency codes, or postal codes
- **The LLM needs business context** — `aat prompt` generates better plans when it understands what your API does
- **You want enriched documentation** — `aat docs generate --domain` annotates graph docs with concept descriptions and type details

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

The pool `currencies` provides values for the type `currency-code`. When the engine needs a currency value and no upstream step supplies one, it draws from this pool.

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
| `constraint` | No | Machine-readable constraint description |
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

When a type references a `pool`, the engine can draw values from that pool during resolution. When `validation` is set, the regex is compiled at parse time — invalid patterns cause a validation error.

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

Value pools provide curated test data for specific types. The engine uses pools as a fallback in the value resolution chain when no upstream step or literal value is available.

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

Both `values` and `groups` can be present on the same pool. All values are combined when the engine samples from the pool.

| Field | Required | Description |
|-------|----------|-------------|
| `description` | Yes | What this pool represents |
| `type` | Yes | The domain type these values belong to |
| `values` | No | Flat list of values (at least one of `values` or `groups` required) |
| `groups` | No | Named categories of values (at least one of `values` or `groups` required) |

### Annotations

YAML inline comments on pool values are extracted as annotations and included in LLM prompts. Head comments above a value mark section boundaries.

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

When formatted for LLM prompts, this produces:

```
Values: [Electronics] ELEC-TV (Televisions and displays), ELEC-AUDIO (Audio equipment),
ELEC-COMP (Computers and peripherals); [Home & Garden] HOME-FURN (Furniture),
HOME-GARDEN (Garden supplies), HOME-KITCHEN (Kitchen appliances)
```

Section labels (from head comments) are shown in brackets. Annotations (from inline comments) are shown in parentheses. Sections are separated by semicolons for visual clarity.

## Integration Points

### Value Resolution

Value pools participate in the engine's resolution chain. When a step input has a matching domain type and no value is available from upstream steps or plan literals, the engine samples a random value from the corresponding pool.

See [Value Flow](value-flow.md) for the full resolution priority chain and how pools interact with other value sources.

### Planning Context

When you run `aat prompt`, the domain knowledge is formatted as structured text and included in the LLM's context. The `FormatForPrompt` method serializes concepts, types, and pools — pool values are truncated to 10 entries for readability, with annotations and section labels preserved.

This gives the LLM enough context to generate plans that use realistic values and respect business rules, without overwhelming the prompt with raw data.

### Documentation

`aat docs generate --domain domain.yaml` enriches generated documentation with:
- Concept descriptions for fields that match `applies_to` entries
- Type format and validation details for typed inputs/outputs
- Value pool examples for fields with matching types

## Merge Behavior

When multiple knowledge bases are merged (e.g., a shared base plus project-specific extensions), the merge rules are:

- **Concepts** — later entries override earlier ones with the same key
- **Types** — later entries override earlier ones with the same key
- **Value pools** — merged additively: `values` lists are appended, `groups` entries are appended (new group keys are added, existing group keys have their values combined)

```yaml
# base-domain.yaml
valuePools:
  currencies:
    description: "Common currencies"
    type: currency-code
    values: [USD, EUR, GBP]

# project-domain.yaml (merged on top)
valuePools:
  currencies:
    description: "Common currencies"
    type: currency-code
    values: [JPY, CAD]

# Result after merge: currencies.values = [USD, EUR, GBP, JPY, CAD]
```

This means you can maintain a shared domain file with common types and pools, then extend it per-project without duplicating the base content.

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

`aat validate` checks domain files for structural correctness:

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

---

*Source: `domain/types.go`, `domain/parse.go`, `domain/validate.go`, `domain/query.go`, `domain/merge.go`.*
