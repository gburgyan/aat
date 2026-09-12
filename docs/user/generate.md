# Scaffolding from OpenAPI

`aat generate` reads an OpenAPI 3 spec and writes a starting [graph](graphs.md) with one node per operation, plus one [request template](templates.md) per node. It saves the typing: the scaffold carries each operation's method, path, parameters, body properties, and response fields.

What it cannot know is how your API is used: which operation must run before which, which one undoes another, where a value comes from, or which of a response's fields a test cares about. A scaffold is a first draft to trim and wire up by hand. [What a Hand-Tuned Graph Adds](#what-a-hand-tuned-graph-adds) compares a generated graph with the shop example's.

## Usage

```bash
aat generate --oas openapi.yaml --output-graph graph.yaml --output-templates templates/
```

```text
Generated 17 nodes, 17 templates written to templates/
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--oas` | path | — | The OpenAPI spec to read, YAML or JSON (required) |
| `--output-graph` | path | `graph.yaml` | Graph file to write; `-` prints the graph to stdout instead |
| `--output-templates` | path | `templates` | Directory for the templates, created if missing. With `--output-graph -`, templates are written only when this flag is given |
| `--force` | bool | `false` | Replace a graph file or templates that already exist |

Paths are relative to the working directory. `aat generate` does not look for or read a project manifest. Warnings go to stderr. A failure exits with code `2`. Failures include:

- a missing `--oas`
- an unreadable spec
- a spec with no operations that have an `operationId`
- files that already exist, without `--force`

The spec must be OpenAPI 3.0 or 3.1. A Swagger 2.0 file fails with `supplied spec is a different version (oas2)`; convert it to OpenAPI 3 first.

## What It Writes

- **A graph file** with `version: 1.0.0`, an `oas:` reference to the spec (relative to the graph file's directory), and one node per operation.
- **One template per node**, named `<operationId>.yaml`, in the templates directory.

Nothing else: no `aat-project.yaml`, environment file, domain file, workflows, layers, or plans. [From Scaffold to Project](#from-scaffold-to-project) lists what to add.

Operations of every method are generated: GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS, and TRACE. An operation without an `operationId` is skipped with a warning:

```text
warning: skipping DELETE /widgets/{widgetId}: no operationId
```

Other warnings name a part of a request that the template leaves for you to write, such as a multipart body (see [Request Bodies](#request-bodies)) or a parameter with a `style` other than the default:

```text
warning: POST /receipts (uploadReceipt): the multipart/form-data body is not generated; its properties are inputs, so write the body by hand
```

Regenerating from the same spec with `--force` produces identical files. Nodes and extract rules are sorted by name, inputs and outputs keep the spec's order, and the YAML is indented with four spaces.

## Nodes

Each node is named after its `operationId`, which is also its `adapter` and its `oas.operationId`. The operation's `summary` becomes the node `description`. This is the generated node for the shop's `listProducts`, from `aat generate --oas examples/shop/openapi.yaml`:

```yaml
    listProducts:
        description: List catalog products
        adapter: listProducts
        inputs:
            - name: category
              type: string
              optional: true
        outputs:
            - name: region
              type: string
            - name: currency
              type: string
            - name: products
              type: string[]
        oas:
            operationId: listProducts
```

The node's name is its key in the graph. Nodes with no summary, inputs, or outputs get `description: ""`, `inputs: []`, or `outputs: []`.

### Inputs

Inputs come from the operation's parameters (path-level and operation-level; an operation-level parameter replaces a path-level one with the same name and location), followed by the top-level properties of the request body, including those of `allOf` branches. [Request Bodies](#request-bodies) says which of the body's media types is used.

- Path parameters are required. Query, header, and cookie parameters are `optional: true` unless the spec marks them `required: true`.
- Body properties are optional unless listed in the body schema's `required`.
- A parameter's own `description` is not copied. A schema `description` lands in `constraints.description`.

Types map as follows:

| OpenAPI schema | Graph type |
|----------------|------------|
| `type: string` | `string` |
| `type: string` with `format: date` | `date` |
| `type: string` with `format: date-time` | `datetime` |
| `type: string` with `enum` | `string` (the values are dropped) |
| `type: integer` | `integer` |
| `type: number` | `float` |
| `type: boolean` | `boolean` |
| `type: array` of a scalar type | that type with `[]`, such as `string[]` |
| `type: array` of objects | `string[]`, with no `elementFields` |
| `type: object`, or no `type` with `properties` or `allOf` | `object` |
| any other schema without a `type` (including `oneOf` and `anyOf`), or a parameter without a `schema` | `string` |

In an OpenAPI 3.1 type list, `"null"` is skipped: `[string, "null"]` maps to `string`, and `["null", integer]` to `integer`.

Schema validation keywords become [input constraints](graphs.md#inputs): `minLength`, `maxLength`, and `pattern` keep their names, and `minimum` and `maximum` become `min` and `max`. [Custom types](graphs.md#types), `enum[...]` types, and defaults are yours to add.

### Outputs

Outputs come from the first `2xx` response, in the spec's order, that has an `application/json` schema. Other responses are ignored.

- **An object response** becomes one output per top-level property, including those of `allOf` branches, each extracted by its own name. Nested objects and arrays of objects are extracted whole, typed as in the table above.
- **An array response** becomes a single output of type `object[]`:
  - Its `elementFields` are the item's properties.
  - It is extracted from the whole body with `path: '@this'` and a `fields` entry per property.
  - It is named after the operation with a leading `list`, `search`, `find`, `get`, `fetch`, or `query` removed, so `listWidgets` gives `widgets`. It is named `items` when the operation starts with none of those words.
- **No JSON 2xx response** (a `204`, say) gives no outputs and a template with `response: {}`.

Property descriptions are not copied. A property the response schema does not list as `required` becomes an `optional: true` output with an optional extract rule, so a response that leaves it out still passes. The shop API omits `couponCode` from a cart with no coupon, and the generated `createCart` template allows for that:

```yaml
response:
    extract:
        cartId: cartId
        couponCode:
            path: couponCode
            optional: true
```

A spec that does not list its required properties makes every output optional; mark the ones a test depends on as required by hand. Delete the outputs you do not need from both the graph node and the template.

## Templates

### Paths and Query Strings

Path parameters become placeholders: `/carts/{cartId}` becomes `/carts/{{cartId}}`. Required query parameters follow in spec order, and each optional one sits inside a [conditional block](templates.md#conditional-blocks), with separators arranged so the query string is correct for any combination of present values. The shop's `listProducts` has one optional parameter:

```yaml
request:
    method: GET
    path: /products{{?category}}?category={{category}}{{/category}}
```

After a required parameter, each optional block simply starts with `&`. When every parameter is optional, the separators depend on which earlier values are present, so the blocks nest. For an operation with optional `q`, `page`, and `tags`:

```yaml
    path: /search{{?q|page|tags}}?{{/q|page|tags}}{{?q}}q={{q}}{{/q}}{{?page}}{{?q}}&{{/q}}page={{page}}{{/page}}{{?tags}}{{?q|page}}&{{/q|page}}tags={{tags}}{{/tags}}
```

That renders as `/search` with no values, `/search?page=2` with only `page`, and `/search?q=tent&page=2&tags=a&tags=b` with all three. A list value repeats its `key=` pair, OpenAPI's default for query parameters; an empty list still sends `tags=`. If your API expects another form, such as `tags=a,b`, rewrite that parameter by hand.

### Headers

A template with a body sends a `Content-Type` of the body's media type. Header parameters become headers: a required one is a plain placeholder, and an optional one is a conditional block, which AAT leaves out of the request when it resolves to nothing:

```yaml
    headers:
        X-Request-Id: '{{?X-Request-Id}}{{X-Request-Id}}{{/X-Request-Id}}'
        X-Tenant: '{{X-Tenant}}'
```

Cookie parameters become one `Cookie` header: required cookies first, then each optional one in a conditional block. A cookie value is inserted as it is.

```yaml
    headers:
        Cookie: 'sessionId={{sessionId}}{{?theme}}; theme={{theme}}{{/theme}}'
```

### Request Bodies

The scaffold builds the body from one of the operation's request media types. It prefers `application/json`, then another JSON type such as `application/merge-patch+json`, then `application/x-www-form-urlencoded`, then `multipart/*`. The schema's top-level properties become inputs, including those of `allOf` branches.

A **JSON body** is a flat JSON object with one placeholder per property: required properties first, in spec order, then each optional property in a conditional block that carries its own comma.

- **String values** are quoted.
- **Integer, number, boolean, array, and object values** are inserted as JSON literals.

The generated `paymentCharge` template from the shop spec:

```yaml
adapter: paymentCharge
protocol: http
request:
    method: POST
    path: /payments/charges
    headers:
        Content-Type: application/json
    body: |-
        {
          "orderId": "{{orderId}}",
          "amount": {{amount}},
          "currency": "{{currency}}",
          "method": "{{method}}"{{?cardNumber}},
          "cardNumber": "{{cardNumber}}"{{/cardNumber}}{{?giftCardCode}},
          "giftCardCode": "{{giftCardCode}}"{{/giftCardCode}}{{?paypalEmail}},
          "paypalEmail": "{{paypalEmail}}"{{/paypalEmail}}
        }
response:
    extract:
        amount: amount
        amountDisplay: amountDisplay
        createdAt: createdAt
        currency: currency
        method: method
        orderId: orderId
        orderStatus: orderStatus
        paymentId: paymentId
        status: status
```

Its body is the same as the hand-written one in `examples/shop/templates/paymentCharge.yaml`. With only the required values, it sends `{"orderId": ..., "amount": ..., "currency": ..., "method": ...}`; a body whose properties are all optional renders as `{}` when none are set.

An object property is typed `object` and placed as `"shipping": {{shipping}}`. Give it a map, such as a plan value `shipping: {default: {city: Austin}}`, or JSON text. Either one is sent as a nested object.

A **form body** is a query string, `orderId={{orderId}}{{?note}}&note={{note}}{{/note}}`, sent with `Content-Type: application/x-www-form-urlencoded`. A list value repeats its pair, as in a query string; an object value is sent as JSON text.

These bodies are left for you to write, each with a warning:

- **Multipart:** the properties become inputs, but the template has no `body` and no `Content-Type`, which must carry the multipart boundary.
- **Any other media type,** such as `application/octet-stream` or `application/xml`: no inputs, no `body`, and no `Content-Type`.
- **A JSON or form schema without properties,** such as a bare `type: object` or an array: no `body` and no `Content-Type`.
- **`oneOf` or `anyOf`:** the alternatives are left out. Properties declared outside them still become inputs and body fields.

## What It Ignores

| In the spec | What the scaffold does |
|-------------|------------------------|
| `servers`, including per-path servers | Nothing. Base URLs, and routing some operations to another host, belong in the [environment file](environments.md#multi-host-routing) |
| `securitySchemes` and `security` | Nothing. Configure [authentication](environments.md#authentication) in the environment file |
| A parameter `style` other than the default, or `explode: false` on a list or object | A warning. The template sends the value the default way |
| Parameter descriptions, `enum`, `default`, and `example` values | Dropped |
| Non-`2xx` responses, response headers, and non-JSON response bodies | Dropped |
| Tags, `deprecated`, callbacks, and links | Dropped |

## Previewing

`--output-graph -` prints the graph to stdout and writes no files:

```bash
aat generate --oas openapi.yaml --output-graph -
```

Add `--output-templates DIR` to write the templates as well while the graph goes to stdout.

## Gotchas

- **The `oas:` reference is relative to where the graph is written.**
  - `aat generate --oas specs/shop.yaml --output-graph project/graph.yaml` writes `oas: ../specs/shop.yaml`, which AAT resolves from the graph file's directory.
  - With `--output-graph -`, the path is relative to the working directory. Adjust it if you save the graph somewhere else.
  - See [OAS References](graphs.md#oas-references).
- **Existing files are not replaced without `--force`.**
  - Before writing, the command checks the graph file and every template it would write.
  - If any exist, it lists them, writes nothing, and exits with code `2`.
  - Templates in the directory for other adapters are always left alone.
  - Two operationIds that differ only in case are an error, since their templates would be one file on a case-insensitive file system.
- **Valid is not the same as runnable.** A scaffold that passes `aat validate --strict` can still send the wrong thing. A query parameter that the API expects as `tags=a,b` is sent as `tags=a&tags=b`; a spec that says so with `explode: false` only gets a warning. Read the warnings: each names a part of a request to write by hand.

## From Scaffold to Project

1. Generate into a new or scratch directory.
2. Add `aat-project.yaml` pointing at the graph and templates, and an environment file with the base URL and authentication. See [Project Setup](project-setup.md) and [Environments](environments.md).
3. Delete the nodes, outputs, and extract rules you do not need, and fix the bodies and query parameters described above.
4. Wire the graph: ordering, cleanup, and input defaults. See [the next section](#what-a-hand-tuned-graph-adds) and [API Graphs](graphs.md).
5. Run `aat validate --strict` (see [Validation](validation.md)), then write a first plan (see [Plans](plans.md)).

The [Quickstart](quickstart.md) walks through these steps on the Petstore spec, and the [Tutorial](tutorial.md) builds a project for the shop sandbox by hand.

## What a Hand-Tuned Graph Adds

The [shop example](examples/shop.md) ships the spec its sandbox serves (`examples/shop/openapi.yaml`) and a graph written for it. Generating from that spec gives the same 17 nodes, with 185 outputs where the shop's graph declares 58. Here is `addItem` from each (generated outputs trimmed):

```yaml
    addItem:
        description: Add an item to a cart
        adapter: addItem
        inputs:
            - name: cartId
              type: string
            - name: sku
              type: string
            - name: quantity
              type: integer
              constraints:
                min: 1
        outputs:
            - name: cartId
              type: string
            # ... 12 more: status, currency, customerEmail, couponCode, lineCount, subtotal, ...
        oas:
            operationId: addItem
```

```yaml
  addItem:
    description: >-
      Add a product to a cart; adding a SKU again merges quantities. Rejects bad input (400),
      an unknown cart (404), a checked-out cart (409 CART_NOT_OPEN), an unknown SKU (404), and
      more than is in stock (409 OUT_OF_STOCK).
    adapter: addItem
    tags: [carts]
    oas:
      operationId: addItem
    inputs:
      - name: cartId
        type: string
        default:
          from: createCart.cartId
      - name: sku
        type: string
        description: SKU to add; defaults to the first in-stock product listed by listProducts
        default:
          from: listProducts.products
          select:
            strategy: match
            field: sku
            filter: inStock == true
      - name: quantity
        type: integer
        default: 1
    outputs:
      - name: cartId
        type: string
      - name: lineCount
        type: integer
      - name: subtotal
        type: integer
        description: Minor units
    requires: [cartOpen]
    satisfies: [cartPopulated]
```

Across the whole project, the hand-tuned version adds:

| Area | Generated | `examples/shop` |
|------|-----------|-----------------|
| Ordering | None | [`requires`/`satisfies`](graphs.md#requires-and-satisfies) tokens: `createCart` satisfies `cartOpen`, `addItem` requires it and satisfies `cartPopulated`, `checkoutCart` requires that |
| Cleanup | None | [Cleanup pairs](graphs.md#cleanup): `createCart` with `deleteCart`, `checkoutCart` with `deleteOrder` |
| Input defaults | None | [Defaults](graphs.md#input-defaults) wired with `from` (`cartId` from `createCart.cartId`), a `select` that picks the first in-stock SKU, literal defaults (`quantity: 1`), and `{{env.postalCode}}` from the environment |
| Types | `string` for enums, `string[]` for arrays of objects | `enum[standard, express, overnight]`, and `product[]` or `cartLine[]` with `elementFields` |
| Outputs | Every response property; those not in `required` are optional | A few per node (2 of the 13 for `createCart`), with descriptions and `display` labels |
| Array extraction | Arrays extracted whole | [`fields`](templates.md#array-extraction) that flatten each product to `sku`, `name`, `category`, `price`, `inStock` |
| Failure signals | None | [`errorDetection`](graphs.md#error-detection) on `checkInventory`, turning a 200 with `status: ERROR` into a `response_error` failure that a step's `retry` can act on |
| Response shaping | None | A [Lua transform](lua-transforms.md) on `getCart` that joins product names and prices into cart lines |
| Graph metadata | `version` and `oas` | A `title`, a `description`, and 11 [workflows](workflows.md) |
| Environment | None | `env.yaml` with OAuth2 for the shop API and a `payment*` override that sends payments to their own host with an API key |
| Test data | None | A domain file, 12 [layers](batch-layers.md), and plans and recipes |

An extract rule can also rename an output, since the extract key is the output name and the path is where the value lives (`orderId: id`). The shop keeps the API's names, so none of its templates do.

---

*Source: `cmd/aat/generate_cmd.go`, `graph/oas/generate.go`, `graph/oas/oas.go`, `examples/shop/graph.yaml`, `examples/shop/templates/`.*
