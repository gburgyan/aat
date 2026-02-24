# Templates

Templates define how AAT translates graph node inputs into HTTP requests and extracts outputs from responses. Each template is a YAML file that maps to one graph node via its `adapter` name.

## Overview

Every node in the graph that makes an API call needs a corresponding template. The template specifies the HTTP method, path, headers, and body (with placeholders for dynamic values) on the request side, and extraction rules for pulling outputs from the JSON response. Templates are the bridge between the abstract graph model and concrete HTTP calls.

## File Structure

Templates live in a directory passed via `--templates` (or resolved from the [project manifest](project-setup.md)). AAT loads all `.yaml` and `.yml` files from this directory and registers each one by its `adapter` field. Subdirectories are ignored.

The file name does not matter — only the `adapter` field links a template to a graph node. By convention, the file is named after the adapter (e.g., `listProducts.yaml` for `adapter: listProducts`), but this is not enforced.

```
templates/
  listProducts.yaml      # adapter: listProducts
  createOrder.yaml       # adapter: createOrder
  cancelOrder.yaml       # adapter: cancelOrder
```

Each template's `adapter` field must be unique across all loaded templates — duplicate adapter names cause a load error.

## Request Definition

### Method and Path

```yaml
adapter: getProduct
protocol: http

request:
  method: GET
  path: /products/{{productId}}
```

The `method` field accepts any HTTP method: GET, POST, PUT, DELETE, PATCH, etc. The `path` is appended to the environment's `apiBaseUrl` at execution time.

The `protocol` field defaults to `"http"` and is the only supported value.

### Headers

```yaml
request:
  method: POST
  path: /orders
  headers:
    Content-Type: application/json
    Accept: application/json
    X-Custom-Header: "{{customValue}}"
```

Headers support `{{placeholder}}` substitution. Static headers like `Content-Type` are set directly; dynamic headers use placeholders resolved from step inputs.

### Body

```yaml
request:
  method: POST
  path: /orders
  headers:
    Content-Type: application/json
  body: |
    {
      "productId": "{{productId}}",
      "quantity": {{quantity}},
      "currency": "{{currency}}"
    }
```

The body is a string template with `{{placeholder}}` substitution. Values are substituted using Go's `%v` formatting — string values in JSON should include surrounding quotes in the template; numeric values should not.

## Placeholders

Templates use `{{key}}` placeholders that are resolved at execution time. Whitespace inside braces is tolerated: `{{ key }}` works the same as `{{key}}`.

### Resolution Order

1. **Step inputs** — values resolved from the plan (literals, upstream step outputs, selections)
2. **Environment config values** — values from the environment's `values` map

If a placeholder can't be resolved from either source, AAT reports an error:

```
path substitution: unresolved placeholders: productId
```

### Placeholders in Different Fields

Placeholders work in `path`, `headers`, and `body`:

```yaml
request:
  method: POST
  path: /orders/{{orderId}}/items       # path parameter
  headers:
    Authorization: Bearer {{token}}     # header value
    X-Request-Id: "{{requestId}}"
  body: |                               # body fields
    {
      "productId": "{{productId}}",
      "quantity": {{quantity}}
    }
```

## Conditional Blocks

Conditional blocks include or exclude sections of a template based on whether an input is present and non-empty.

**Syntax:** `{{?key}}...{{/key}}`

When `key` is present in the step inputs and has a non-empty value (non-empty string, non-nil value), the block content is included. Otherwise, the entire block — including the tags — is removed.

```yaml
body: |
  {
    "orderId": "{{orderId}}"
    {{?giftWrap}},
    "giftWrap": true,
    "giftMessage": "{{giftMessage}}"
    {{/giftWrap}}
    {{?shippingPriority}},
    "shipping": {
      "priority": "{{shippingPriority}}"
      {{?deliveryDate}},"requestedDate": "{{deliveryDate}}"{{/deliveryDate}}
    }
    {{/shippingPriority}}
  }
```

In this example, the `shipping` block is only included when the plan supplies a `shippingPriority` value. Conditional blocks can be nested — the inner `{{?deliveryDate}}` block only appears if both `shippingPriority` and `deliveryDate` are provided.

Conditional blocks are useful for optional API features like additional metadata, optional sub-resources, or feature flags:

```yaml
body: |
  {
    "customer": {
      "name": "{{customerName}}",
      "email": "{{email}}"
      {{?billingAddress}},
      "billingAddress": {
        "street": "{{billingStreet}}",
        "city": "{{billingCity}}",
        "postalCode": "{{billingPostalCode}}"
      }
      {{/billingAddress}}
      {{?promoCode}},
      "promotion": {
        "code": "{{promoCode}}"
      }
      {{/promoCode}}
    }
  }
```

Conditional blocks are expanded before placeholder substitution, so a block guarded by `{{?key}}` will have its `{{key}}` placeholder replaced only if the block is included.

### Compound Conditionals

When a JSON object contains multiple optional sub-sections, use the compound conditional syntax `{{?a|b|c}}...{{/a|b|c}}`. The block is included when **any** of the listed keys is present. Individual sub-sections inside the block use their own simple conditionals.

```yaml
body: |
  {
    {{?cabinPreference|carrierPreference}}"SearchModifiersAir": {
      "@type": "SearchModifiersAir"{{?cabinPreference}},
      "CabinPreference": [
        {
          "preferenceType": "Permitted",
          "cabins": {{cabinPreference}}
        }
      ]{{/cabinPreference}}{{?carrierPreference}},
      "CarrierPreference": [
        {
          "preferenceType": "Permitted",
          "carriers": ["{{carrierPreference}}"]
        }
      ]{{/carrierPreference}}
    },
    {{/cabinPreference|carrierPreference}}"PassengerCriteria": []
  }
```

This produces valid JSON for all four combinations: neither key, cabin only, carrier only, or both. Without compound conditionals, handling N optional sub-sections would require 2^N nested blocks. Compound conditionals scale linearly — add one more `|key` to the wrapper and one more inner `{{?...}}` block.

## Iteration Blocks

Iteration blocks repeat a section of a template for each element in an array input.

**Syntax:** `{{#key}}...{{/key}}`

The block body is repeated for each element in the array, with iterations separated by commas. Inside the block:

- **`{{.}}`** — the element value itself (for scalar arrays)
- **`{{.fieldName}}`** — a named field from a map element

```yaml
body: |
  {
    "lineItems": [
      {{#itemIds}}
      {"itemId": "{{.}}"}
      {{/itemIds}}
    ]
  }
```

If `itemIds` is `["item-1", "item-2", "item-3"]`, this produces:

```json
{
  "lineItems": [
    {"itemId": "item-1"},
    {"itemId": "item-2"},
    {"itemId": "item-3"}
  ]
}
```

Iteration blocks can be combined with conditional blocks — a conditional wrapping an iteration, or vice versa:

```yaml
body: |
  {
    "order": {
      "primaryItem": "{{primaryItemId}}"
      {{?additionalItemIds}},
      "additionalItems": [
        {{#additionalItemIds}}
        {"itemId": "{{.}}"}
        {{/additionalItemIds}}
      ]
      {{/additionalItemIds}}
    }
  }
```

## Input Classification

Based on how inputs are used in the template, AAT classifies them as:

- **Required** — appears in a regular `{{key}}` placeholder; must be resolved or the request fails
- **Conditional** — appears only inside `{{?key}}...{{/key}}` blocks; the block is removed if the input is absent
- **Iterable** — appears as the array in a `{{#key}}...{{/key}}` block; must be an array value

This classification is used by validation to distinguish genuinely missing inputs from intentionally optional ones.

## Response Extraction

The `response.extract` section defines how to pull values from a JSON response body. Each entry maps an output name (matching the graph node's output) to an extraction rule.

### Scalar Extraction

For scalar outputs, use a bare string path:

```yaml
response:
  extract:
    orderId: "id"
    orderStatus: "status"
    customerEmail: "customer.email"
    firstItemName: "items.0.name"
```

### Array Extraction

For array outputs where downstream steps need to select individual elements by field name, use the object form with `path` and `fields`:

```yaml
response:
  extract:
    products:
      path: "data.products"
      fields:
        productId: "id"
        name: "name"
        price: "pricing.retail"
```

The `path` specifies where to find the array in the response. The `fields` map transforms each array element into a flat object with named keys. Each entry maps a logical field name (matching the graph's `elementFields`) to a gjson path within the element.

Before transformation, a raw element might look like:

```json
{"id": "p-42", "name": "Widget", "pricing": {"retail": 19.99, "wholesale": 12.50}, "sku": "W-042"}
```

After transformation, the engine sees:

```json
{"productId": "p-42", "name": "Widget", "price": 19.99}
```

This is especially valuable when the raw JSON uses deeply nested paths:

```yaml
response:
  extract:
    results:
      path: "response.data.searchResults"
      fields:
        resultId: "id"
        score: "metadata.relevance.score"
        itemRef: "associations.0.items.0.ref"
```

The template flattens deeply nested paths so plans and selection strategies reference them by simple names like `score` or `itemRef`.

When `fields` is omitted, the array elements are stored as-is (raw JSON objects). Selection strategies still work but must use gjson paths to access nested fields.

### Path Syntax

Extraction paths use [gjson](https://github.com/tidwall/gjson) syntax with some normalization:

| Path | Extracts | Notes |
|------|----------|-------|
| `"id"` | `{"id": 42}` -> `42` | Simple field |
| `"customer.email"` | `{"customer": {"email": "a@b.com"}}` -> `"a@b.com"` | Nested field |
| `"items.0.name"` | `{"items": [{"name": "Widget"}]}` -> `"Widget"` | Array index |
| `"$"` | entire body | Extracts the whole response as-is |

AAT normalizes paths before evaluation:

- Leading `$.` is stripped: `$.customer.email` becomes `customer.email`
- Lone `$` returns the entire response body
- Bracket notation is converted: `[0]` becomes `.0`

So `$.items[0].name`, `items[0].name`, and `items.0.name` all work equivalently.

### Extraction Errors

If an extract path doesn't match anything in the response:

```
extract path "orderId" (id) not found in response
```

The response body must be valid JSON for extraction to work. Non-JSON responses produce:

```
response body is not valid JSON
```

## Lua Transforms

Some APIs return complex structures where extracted outputs need post-processing — for example, responses that separate reference data from main results, requiring client-side joins to assemble complete records.

The `response.transform` field takes an inline Lua script that post-processes the extracted outputs before they're stored. The script receives the extracted outputs as a mutable table and can query the raw response body for additional data.

```yaml
response:
  extract:
    results:
      path: "data.results"
      fields:
        resultId: "id"
        categoryRef: "refs.category"
  transform: |
    -- Resolve category references from the raw response
    local categories = json_path("data.categories")
    if not categories then return outputs end
    -- ... enrich results with resolved category data ...
    return outputs
```

See [Lua Transforms](lua-transforms.md) for the full guide covering the sandboxed runtime, available globals, and real-world examples.

## Header Merge Order

When a request is built, headers come from multiple sources and are merged in this order (later values override earlier ones for the same key):

1. **Environment headers** — static headers from the environment file's `headers` section
2. **Template headers** — headers defined in the template's `request.headers`
3. **Plan-level headers** — headers set on individual plan steps (see [Plans](plans.md))
4. **Auth headers** — authentication headers added by the auth system (final precedence)

This means you can set common headers like `Accept` in the environment, override per-template when needed, and let auth headers take final precedence.

See [Environments](environments.md) for environment header configuration and [Plans](plans.md) for plan-level header overrides.

## Common Patterns

### GET with Path Parameter

```yaml
adapter: getProduct
request:
  method: GET
  path: /products/{{productId}}
  headers:
    Accept: application/json
response:
  extract:
    productId: "id"
    productName: "name"
```

### GET with Query Parameters (Array Response)

```yaml
adapter: listProducts
request:
  method: GET
  path: /products?category={{category}}&limit={{maxResults}}
  headers:
    Accept: application/json
response:
  extract:
    products:
      path: "data.products"
      fields:
        productId: "id"
        name: "name"
        price: "pricing.retail"
```

Query parameters are part of the path string. The `fields` section transforms each array element so downstream selection strategies can reference `productId`, `name`, etc. by name.

### POST with JSON Body

```yaml
adapter: createOrder
request:
  method: POST
  path: /orders
  headers:
    Content-Type: application/json
    Accept: application/json
  body: |
    {
      "productId": "{{productId}}",
      "quantity": {{quantity}},
      "currency": "{{currency}}"
    }
response:
  extract:
    orderId: "id"
    orderStatus: "status"
    totalAmount: "total.amount"
```

### DELETE with No Response Body

```yaml
adapter: cancelOrder
request:
  method: DELETE
  path: /orders/{{orderId}}
  headers:
    Accept: application/json
response: {}
```

When there's nothing to extract, use an empty response object.

## Validation

Use `aat validate graph` with `--templates` to check templates against the graph:

```bash
aat validate graph --graph graph.yaml --templates templates/
```

This catches:
- **Graph output not extracted by template** — the node declares an output but the template has no extract entry, so the output will be nil at runtime
- **Template extracts undeclared output** — the template extracts a key the graph doesn't declare (dead extraction, likely a typo)
- **Element field mismatch** — for array outputs, the template's `fields` keys should match the graph's `elementFields` names

This check also runs automatically at the start of `aat run`, so mismatches are caught before any API calls are made.

See [Validation](validation.md) for the full reference covering all validation subcommands.

## Schema Reference

```yaml
adapter: <name>              # required — must match a graph node's adapter field
protocol: http               # optional — defaults to "http"

request:
  method: <HTTP method>      # required — GET, POST, PUT, DELETE, PATCH
  path: <path>               # required — URL path (appended to environment base URL)
  headers:                   # optional — request headers
    Header-Name: value       #   supports {{placeholder}} substitution
  body: |                    # optional — request body (typically for POST/PUT)
    template with {{placeholders}}, {{?conditionals}}, and {{#iterations}}

response:
  extract:                   # optional — output extraction rules
    scalarOutput: "path"     #   scalar: bare gjson path string
    arrayOutput:             #   array with element transformation:
      path: "path.to.array"  #     gjson path to the array in response
      fields:                #     element field mappings
        logicalName: "path"  #       logical name -> gjson path within element
  transform: |               # optional — Lua post-processing script
    -- receives `outputs` table and `json_path()` function
    return outputs
```

---

*Source: Restructured from `docs/user/templates.md` with new sections on conditional blocks, iteration blocks, input classification, and Lua transforms.*
