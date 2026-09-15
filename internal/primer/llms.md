# AAT for AI Assistants

This document is a structural primer for AI coding assistants (Claude Code, Cursor, Copilot, etc.) working on AAT projects. It gives you the schema knowledge to author and iterate on graphs, templates, plans, workflows, and environments without reading the full reference docs.

Read it verbatim rather than through a tool that summarizes pages. `aat docs primer` prints the copy that matches the installed `aat`, and https://gburgyan.github.io/aat/llms-full.txt serves the latest as plain Markdown. Save it once, with `aat docs primer > aat-primer.md`, and search that file instead of fetching pages again. If you need deeper detail on any topic, follow the cross-reference links to the full documentation.

## Project Structure

Every AAT project has a manifest file named `aat-project.yaml` at its root:

```yaml
name: my-api
description: API integration tests
graph: graph.yaml
templates: templates/
environment: env.yaml
workflows: workflows/
plans: plans/
archives: runs/
```

Required fields: `graph` and `templates`. All paths resolve relative to the manifest file. Every project file is decoded strictly: an unknown or misspelled key is an error naming the file, the line, and the nearest valid key, so run `aat validate` after each edit.

Typical directory layout:

```
my-api/
  aat-project.yaml        # manifest (project root marker)
  graph.yaml              # API operation graph
  env.yaml                # environment config (auth, base URL)
  domain.yaml             # domain knowledge (optional)
  templates/              # one YAML file per graph node
  workflows/              # reusable plan templates (optional)
  plans/                  # concrete test plans and recipes
  runs/                   # execution archives (auto-created; `archives:` in the manifest)
```

Cross-ref: [Project Setup](https://gburgyan.github.io/aat/project-setup/)

## Authoring Workflow

The recommended sequence for building an AAT project:

1. **Create the manifest** — `aat-project.yaml` with the graph and templates paths
2. **Define the graph** — nodes (API operations) with typed inputs, outputs, and ordering
3. **Write templates** — one per node, defining HTTP request shape and response extraction
4. **Set up the environment** — base URL, auth credentials, secrets, and `settings.oasValidation: strict` when the graph has an OpenAPI spec
5. **Define workflows** (optional) — reusable test patterns declared in the graph
6. **Write plans or recipes** — concrete test instances
7. **Validate** — `aat validate` checks the entire project
8. **Run** — `aat run plan <name>`, read output, iterate

The key iteration loop: **edit YAML** -> **`aat validate`** -> **`aat run plan`** -> **read output** -> **fix** -> **repeat**.

### API Knowledge Sources

To author accurate graphs and templates, you need access to the target API's specification. Companion MCP servers give you direct access:

- **[exoas](https://github.com/gburgyan/exoas)** — OpenAPI spec server. Browse operations, schemas, and parameters from `.yaml`/`.json` spec files. Use it to look up request/response shapes, path parameters, and field names when writing graph nodes and templates.
- **[expost](https://github.com/gburgyan/expost)** — Postman collection server. Browse requests, saved examples, and environment variables from Postman `.json` exports. Useful when the API's primary documentation is a Postman collection rather than an OpenAPI spec.

Both run as MCP servers (stdio transport) that you configure alongside your coding assistant. When available, query them directly instead of guessing at API shapes — this avoids round-trips of writing incorrect YAML, running, failing, and fixing.

AAT also has its own MCP server (`aat mcp serve`) that exposes graph introspection, workflow listing, validation, and plan scaffolding tools. See [MCP Server](https://gburgyan.github.io/aat/mcp-server/).

### Starting from an OpenAPI Spec

When the API publishes an OpenAPI 3.0 or 3.1 spec, scaffold from it, and let AAT check your project and your runs against it.

- **Scaffold only what you need.** A published spec can have hundreds of operations. Preview on stdout, then write:

  ```bash
  aat generate --oas openapi.json --operation createCart,addItem --output-graph -
  aat generate --oas openapi.json --path /carts --output-graph graph.yaml --output-templates templates/
  ```

  The warnings list what each template leaves to write by hand, such as a multipart body. A form body becomes a `form:` mapping. Specs with circular references load. See [Large Specs](https://gburgyan.github.io/aat/generate/#large-specs).
- **Validate the project against it.** `aat validate --strict` checks that each node's `operationId` exists, that its inputs are parameters or body properties (an input the template sends only in a header, such as an idempotency key, is exempt, and an input that is the whole value of a `form:` field, a query parameter, or a path segment counts as that field: `/orders/{{orderId}}` fills the spec's `/orders/{order}`), that required fields are inputs or written by the template, and that outputs exist in the 2xx response schema.
- **Validate every run against it.** `--oas-validate strict` checks each step's request body, JSON or form-encoded, and its response body against the schema for its status code or the spec's `default` response, and fails a step on a violation. Cleanup steps are checked too: an invalid exchange fails the cleanup step, and the rest of its cleanup chain still runs. A request body of another type, or a schema the validator can't compile, shows `OAS: request not validated` or `OAS: response not validated` and never fails a step. Under `strict`, a spec that fails to load stops the run with exit code 2. Only the operations the graph's nodes name are compiled, so a large spec loads quickly. See [OAS Validation](https://gburgyan.github.io/aat/running/#oas-validation).

## Graph Schema

The graph declares API operations as nodes with typed inputs, outputs, and ordering rules. An input can default to an earlier node's output (`default: {from: node.output}`); plans wire the rest. A graph file also needs a top-level `version: "X.Y.Z"`.

```yaml
nodes:
  createOrder:
    description: "Place a new order"
    adapter: createOrder
    inputs:
      - name: productId
        type: string
      - name: quantity
        type: integer
        default: 1
    outputs:
      - name: orderId
        type: string
      - name: status
        type: string
    satisfies: [order]
    cleanup: cancelOrder

  getOrder:
    description: "Retrieve order details"
    adapter: getOrder
    inputs:
      - name: orderId
        type: string
    outputs:
      - name: status
        type: string
    requires: [order]
```

Outputs have no JSON path in the graph: the node's template extracts each output by name (see [Template Schema](#template-schema)).

### Key Node Fields

| Field | Required | Description |
|-------|----------|-------------|
| `adapter` | yes | Links to the template whose `adapter` field has this name |
| `inputs` | no | Typed inputs resolved at runtime from plan values or defaults |
| `outputs` | no | Named values the template extracts from the HTTP response |
| `satisfies` | no | Prerequisite tokens this node provides |
| `requires` | no | Prerequisite tokens this node depends on |
| `cleanup` | no | Node to run during teardown (e.g., delete what this node created), or `{node, when, releasedBy}` to skip it when it isn't needed (see Cleanup) |
| `errorDetection` | no | Rules that fail a successful response whose body reports an error |
| `oas` | no | `operationId` (and optional `spec`) linking the node to an OpenAPI operation |

Graph-level `conditions` are a separate top-level list, not a node field.

### Input Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique within the node |
| `type` | yes | Data type: `string`, `integer`, `float`, `boolean`, `date`, `datetime`, `money`, `enum[a, b]`, `X[]`, or a custom name |
| `optional` | no | If true, operation can run without this input |
| `default` | no | Scalar literal, pool (YAML list), or rich default with `from`/`select` |

### Default Forms

Scalar: `default: "USD"`

Pool (engine picks one): `default: ["USD", "EUR", "GBP"]`

Rich default (references another node's output):
```yaml
default:
  from: listProducts.products
  select:
    strategy: first
    field: productId
```

### Output Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Output name, referenced as `stepId.outputName` in plans (the step ID defaults to the node name) |
| `type` | yes | Data type |
| `optional` | no | If true, the template need not extract it. A path the response lacks leaves the step without the output, and a null value is extracted as null; a `predicate` assertion that names either fails (see Predicate syntax under Assertion Types) |
| `display` | no | Label that prints the value under the step in run output |
| `elementFields` | no | For array outputs — describes fields on each element |

### Array Outputs

Array outputs use `elementFields` to declare the structure of each element. Plans can then select specific elements using strategies:

```yaml
outputs:
  - name: products
    type: product[]
    elementFields:
      - name: productId
        type: string
      - name: price
        type: money
```

Selection strategies are `first`, `last`, `index`, `random`, `min`, `max`, and `match`.
- **`min` and `max`** compare `sortField`, or `field` when there is no `sortField`, by value. A string that holds a number, such as `"19.99"`, compares as that number.
- **Ties:** when several elements share the smallest or largest value, the first of them in array order after the `filter` wins.
  - The step prints a warning naming the tie. It also appears in the step's `warnings` in `--json` and in `aat run show --step`.
  - To fix a tie, add a `filter` that narrows the candidates to the element you mean, and assert the chosen element's distinguishing field in a later step.
  - `onTie: fail` on the selection fails the step on a tie. `onTie: first` accepts any of them and silences the warning.
  - Inputs that select from the same array share one pick only when they use the same strategy, `filter`, `index`, and compared field.
- **`match`** returns the first element that matches the `filter`. A `filter` without a `strategy` behaves as `match`, and a `filter` does not convert strings.

Cross-ref: [API Graphs](https://gburgyan.github.io/aat/graphs/)

## Template Schema

Each template defines the HTTP request and response extraction for one node. The `adapter` field links it to the graph.

```yaml
adapter: createOrder

request:
  method: POST
  path: /orders
  headers:
    Content-Type: application/json
  body: |
    {
      "productId": "{{productId}}",
      "quantity": {{quantity}}
    }

response:
  extract:
    orderId: id
    status: status
```

### Template Rules

- `adapter` must match a graph node's `adapter` field; the file name does not matter
- `{{placeholder}}` in path, headers, and body are replaced with resolved input values; a placeholder with no value fails the request (`unresolved placeholders: …`), so wrap optional parts in `{{?name}}…{{/name}}`. A header or `form:` field whose whole value is one placeholder is left out instead when that input has no value (absent, null, or `""`)
- `response.extract` is a map from output name to a gjson path into the response JSON (`$.` prefixes are accepted). A path can count an array, `productCount: products.#`, or query it: `'orders.#(status=="open")#'` for every match, `#(…)` without the last `#` for the first, and `|#` after a query for how many. Match a null or missing field with `==~null` and a present, non-null one with `!=~null`; `==null` compares a string and never matches a JSON `null`
- For array extraction, give the output a `path` and a `fields` map from element field name to a path within each element
- A rule whose path may be missing takes `optional: true`, which leaves the output out, or `default:`, the value to use when the path is missing or `null`: `nextCursor: {path: meta.after, default: ""}`. A rule takes one of them, and with `fields` a default is a list, usually `[]`
- A rule can read a response header in place of a body path: `requestId: {header: X-Request-Id}`. Names match in any case; the value is converted to the output's `integer`, `float`, or `boolean` type; `optional:` and `default:` work as for a path; and a template whose rules all read headers needs no JSON body, so a `204` works
- `{{#key}}…{{/key}}` repeats its body once per element of the list input `key`. `{{.}}` is the element, `{{.field}}` is a field of it, and `{{@index}}` is its position from 0. The copies are joined with commas, except in a form body or a query string: there a body that writes a whole `key=value` pair is joined with `&` (`{{#tags}}tags[]={{.}}{{/tags}}`), and a body that starts with `&` is repeated with nothing between. Wrap an optional list in `{{?key}}…{{/key}}`

```yaml
response:
  extract:
    items:
      path: results
      fields:
        itemId: id
        title: name
```

### Form Bodies and Query Strings

- **Write a form body as `form:`,** a mapping of field names to values, in place of `body:`. It is sent as `application/x-www-form-urlencoded`, with every key and value URL-encoded and the brackets of a key kept.
- **A field whose whole value is one placeholder** is left out when that input has no value (absent, null, `""`, or an empty list or map), so optional fields need no conditional blocks.
- **Lists and maps:** a list repeats the key as written (`tags[]` or `tags`). A map, nested in the template or as an input's value, writes bracketed keys, and a map in a list writes `items[0][sku]`.
- **Other text,** such as `"Order {{orderId}}"`, is sent as one value and still needs its placeholders. Iteration blocks aren't allowed in a form.
- **Field names needn't match input names:** `customer: "{{customerId}}"` counts `customerId` as the spec's `customer` field in `aat validate`.

```yaml
request:
  method: POST
  path: /refunds
  form:
    orderId: "{{orderId}}"
    amount: "{{amount}}"      # left out when amount has no value
    skus[]: "{{skus}}"        # one skus[]= pair per element
    metadata:
      source: aat             # metadata[source]=aat
```

A `body:` string with `Content-Type: application/x-www-form-urlencoded` still works:

- **Escaping follows the `Content-Type`** the request is sent with, whether the template, the environment, or the plan sets it. In a form body (`application/x-www-form-urlencoded`) and in a query string, each value is URL-encoded.
- **Write a form body as the query string it sends,** on one line. Whitespace around a form body is removed.
- **Bracketed keys are sent as written:** `metadata[source]={{source}}`.
- **A list right after `key=` repeats the pair:** `tags={{tags}}` sends `tags=a&tags=b`.
- **An iteration block writes one pair per element.** Start its body with `&` so that an empty list sends nothing, and use `{{@index}}` for indexed keys:

```yaml
request:
  method: POST
  path: /refunds
  headers:
    Content-Type: application/x-www-form-urlencoded
  body: 'orderId={{orderId}}{{#skus}}&skus[]={{.}}{{/skus}}{{#items}}&items[{{@index}}][sku]={{.sku}}{{/items}}'
```

See [Body](https://gburgyan.github.io/aat/templates/#body).

### Headers

- **Merge order,** later wins: environment headers, plan headers, template headers, then the auth credential and overlay headers, which a template can't replace. Names compare case-insensitively. See [Header Merge Order](https://gburgyan.github.io/aat/templates/#header-merge-order).
- **Values take placeholders.** A header whose whole value is one placeholder isn't sent when that input has no value, and neither is one whose whole value is a conditional block that resolves to nothing. A placeholder inside other text still needs a value.

**Idempotency keys.** Give the node an optional input that generates a key, and send the header only when the input has a value:

```yaml
# graph.yaml, on the node's inputs
- name: requestKey
  type: string
  optional: true
  default: "{{uuid}}"
# the template
headers:
  Idempotency-Key: "{{requestKey}}"
```

A retried step resends the same key, and a later step replays it with `requestKey: {fromInput: createOrder.requestKey}`. A cleanup step takes its inputs only from earlier outputs, not from graph defaults, so a node that runs as a cleanup sends no key. See [Idempotency Key Header](https://gburgyan.github.io/aat/templates/#idempotency-key-header).

### Lua Transforms

`response.transform` holds a Lua script that computes outputs the extract rules can't, such as counts, flags, or trimmed values.

```yaml
response:
  extract:
    products: {path: products, fields: {sku: sku, inStock: inStock}}
  transform: |
    local n = 0
    for _, p in ipairs(outputs.products or {}) do
      if p.inStock then n = n + 1 end
    end
    outputs.inStockCount = n   -- declare inStockCount on the graph node
    return outputs
```

- **When it runs:** after all extract rules, and only on a status below 400.
- **What it sees:**
  - `outputs`: the extracted values
  - `json_path(path)`: a gjson lookup into the raw response body; `nil` when the path is missing
  - `header(name)`: a response header's value as a string, matched in any case; `nil` when the response has none
  - `print(...)`: writes to stderr
  - only the base, `table`, `string`, and `math` libraries
- **What it can't see:** the step's inputs or any other step. There is no `inputs` global: reading `inputs.x` fails with `attempt to index a non-table object(nil) with key 'x'`.
- **Return value:** the script must `return` a table keyed by output name. That table replaces all outputs, so return `outputs` after adding to it.
- **Limits:** numbers are floats, so keep amounts that must stay exact as strings. A script times out after 5 s.

Cross-ref: [Templates](https://gburgyan.github.io/aat/templates/), [Lua Transforms](https://gburgyan.github.io/aat/lua-transforms/)

## Environment Schema

The environment file configures how AAT connects to your API.

```yaml
environment: staging
apiBaseUrl: https://api.staging.example.com

auth:
  type: apikey
  headerName: X-API-Key
  credentials:
    key:
      source: env
      var: STAGING_API_KEY

headers:
  Accept: application/json

settings:
  oasValidation: strict       # optional: auto (the default), strict, or off
  minRequestInterval: 250ms   # optional: space request starts for a rate-limited API
```

- **`settings.oasValidation`** sets how runs check request and response bodies against the graph's OpenAPI spec: `auto`, the default, only warns about a violation, `strict` fails the step, and `off` skips validation. Set `strict` once, under `shared:` in a multi-environment file, and every run validates strictly without `--oas-validate`, which overrides the setting for one command.
- **`settings.minRequestInterval`** needs a unit (`250ms`, `1s`). One interval covers everything a command sends, including every plan of a `--parallel` batch, retries, verification, and cleanup.

That is the single-environment format. A multi-environment file has top-level `shared:` and `environments:` instead, with `extends`, `vars` (`${name}` substitution), and `include`; select one with `--env` or the manifest's `defaultEnvironment`.

### Auth Types

| Type | Credentials | Description |
|------|-------------|-------------|
| `none` | — | No authentication |
| `apikey` | `key` | Sent in the header specified by `headerName` |
| `bearer` | `token` | Sent as `Authorization: Bearer <token>` |
| `oauth2` | `username`, `password`, `clientId`, `clientSecret` | Token request to `tokenUrl`. `grantType` defaults to `password`; `client_credentials` also works, but all four credentials are still required and sent, so give unused ones a placeholder such as `{source: literal, value: unused}`. `extraParams` adds form fields |

### Secrets

Never hardcode credentials. Use `source: env` to read from OS environment variables:

```yaml
credentials:
  key:
    source: env
    var: MY_API_KEY
```

Cross-ref: [Environments](https://gburgyan.github.io/aat/environments/)

## Plan Schema

AAT supports two plan formats: **recipes** (compact, workflow-based) and **full plans** (explicit steps).

### Recipes (Preferred)

Recipes name a workflow and provide only the overrides:

```yaml
kind: recipe
metadata:
  created: 2026-01-15T10:00:00Z
  prompt: "create and verify an order"
selection:
  workflow: Order Lifecycle
  choices:
    payment: Credit Card
overrides:
  values:
    createOrder.productId: "PROD-123"
```

### Full Plans

Full plans spell out every step:

```yaml
intent:
  goal: verify
  description: "Create an order and verify it exists"
execution:
  steps:
    - id: create
      node: createOrder
      values:
        productId: "PROD-123"
        quantity: 2
      assertions:
        mechanical:
          - type: status
            expect: 201
    - id: verify
      node: getOrder
      isGoal: true
      values:
        orderId:
          from: create.orderId
      dependsOn: [create]
      assertions:
        mechanical:
          - type: fieldEquals
            path: status
            value: "confirmed"
  cleanup:
    - node: cancelOrder
      runOn: always
```

A plan's top-level keys are `metadata` (`created`, `prompt`, `graphVersion`), `graph`, `auth`, `headers`, `intent` (`goal` — the step ID of the `isGoal` step — `description`, and `constraints`), and `execution` (`steps`, `verification`, `cleanup`). A verification step takes `node`, `purpose`, `assertions`, and `values`. Anything else is rejected.

### Step Value Forms

| Form | Example | Description |
|------|---------|-------------|
| Bare scalar | `quantity: 2` | Literal value |
| Reference | `orderId: {from: create.orderId}` | Output from a previous step |
| Selection | `productId: {from: list.products, select: {strategy: first, field: productId}}` | Pick from array |
| Expression | `date: "{{today + 7 days}}"` | Computed when the step runs (see [Expressions](#expressions)) |
| Pool | `shippingTier: {pool: [standard, express]}` | One element, picked per run |
| List | `skus: [SKU-1001, SKU-1006]` | The list itself, for an input a template repeats over with `{{#skus}}…{{/skus}}`. Items can be maps, read with `{{.sku}}`, and strings in them can be expressions |
| Absent | `deliveryDate: {}` | Nothing fills the input: no graph default, layer, or auto-wiring |

- **Lists:** a bare YAML list in a step value is the list itself, as in a slot's `inject`. `{value: [...]}` means the same, and so does `{default: [...]}`; a step value can't take both keys. In a graph default or a layer, a bare list is a pool, so a literal list there is written `{value: [...]}`, the form that works in all four places.
- **`{}` is for optional inputs.** An optional input marked `{}` is left out of the request. A required input marked `{}` still takes its default when that default is a plain value, with its expressions evaluated; a layer that sets the input wins over the graph default, as it does without `{}`. With no default, the step fails with `required input has no value (empty step value)`. Over a default with a pool, `from`, `select`, or a constraint, `{}` leaves a required input out too, so its template must send it inside a `{{?name}}…{{/name}}` block, or the request fails on the unresolved placeholder.

### Expressions

A string containing `{{…}}` is an expression.

| Form | Result |
|------|--------|
| `{{today}}`, `{{today + 7 days}}`, `{{today - 7 days}}` | A `YYYY-MM-DD` date in the local time zone of the machine running `aat` |
| `{{env.KEY}}` | The OS environment variable `KEY`, else the environment file's `values:` entry; fails when both are empty |
| `{{name}}`, `{{name + 3 days}}` | Another input of the same step, declared earlier on the node; date arithmetic needs a `YYYY-MM-DD` value |
| `{{step.output}}` | An earlier main step's output, such as `{{checkout.total}}`, in assertions and `repeat.until` only; a step value uses `from:` |
| `{{uuid}}` | A random version 4 UUID |
| `{{random N}}` | `N` random characters from `0-9a-z`, with `N` from 1 to 64 |
| `{{now}}`, `{{now + 90 minutes}}` | The time in UTC, RFC 3339 to the second. Units: `seconds`, `minutes`, `hours`, `days` |
| `{{unixtime}}`, `{{unixtime - 1 hours}}` | The time as Unix seconds, an integer, with the same units |

- **Types.** A value that is one whole expression keeps the result's type. Mixed text, such as `"Deliver on {{deliveryDate}}"`, becomes a string.
- **Generated values.** Each occurrence is its own value, so two inputs set to `{{uuid}}` differ; reuse one with `fromResolved` or `fromInput`. A step's values are resolved once, so a retried step resends the same ones, and a new run generates new ones. `uuid`, `now`, and `unixtime` are reserved words, and `{{today}}` counts days only.
- **Where they are evaluated:** step values, pools, graph defaults, layers, recipe overrides, slot `inject`, and mutation `set`, including strings inside a list or map value, at any depth. Also a `fieldEquals` `value` and a quoted string in a `predicate` `expr` or `repeat.until`, where they can name the step's inputs and read earlier steps' outputs: `expr: 'quantity == "{{quantity}}"'`, `expr: 'amount == "{{checkout.total}}"'`. A quoted expression that yields a number or a boolean compares as one. A `{{step.output}}` implies `dependsOn`; a step that stored no outputs, a missing output, or a null or list value fails the assertion.
- **Where they are not:** templates (where `{{name}}` is a placeholder for an input), overlay `values:`, `rawBody`, selection filters, and cleanup `when`.
- **Checking.** `aat validate` checks expression syntax in step values (list and map items included), pools, and assertions. An expression that fails to evaluate fails its step, or its assertion.

### Assertion Types

Assertions live under `assertions.mechanical` on each step.

| Type | Fields | Description |
|------|--------|-------------|
| `status` | `expect` | HTTP status: an integer code (`201`, not `"201"`) or a class (`2xx`, `4xx`) |
| `fieldExists` | `path` | Path exists and is not null |
| `fieldEquals` | `path`, `value` | Value at the path equals `value`; a string matches only a string, and a number only a number |
| `predicate` | `expr` | Boolean expression over the same data, such as `total > 0 && currency == "USD"` |
| `schema` | — | Validate response body against the node's OAS response schema. Requires the project to have OAS specs configured on its graph nodes; otherwise the assertion is reported as `skipped`. |

`fieldExists`, `fieldEquals`, and `predicate` read the step's **extracted outputs**, keyed by output name, not the raw HTTP body (they fall back to the body only on a response with status 400 or above). Add `raw: true` to check the raw response body instead. A predicate cannot see the HTTP status: `status >= 200` reads an output named `status`. Check the status with `type: status`:

```yaml
assertions:
  mechanical:
    - type: status
      expect: 2xx
    - type: predicate
      expr: 'orderStatus == "confirmed"'
```

- **Predicate syntax:**
  - comparisons `== != < > <= >=`, and `&&`, `||`, `!`
  - `in`, as in `currency in ["USD", "EUR"]`
  - parentheses, quoted strings, numbers, `true`, and `false`
  - dots for nested fields
  - two decimal numbers written as text, such as `"221.78"`, order as numbers with `< > <= >=`; `==` compares text
  - no `null`, arithmetic, or indexing
  - write `!(a == b)`, not `!a == b`
- **An output the step didn't produce.** A predicate reads outputs by name, and a name the step has no output for fails the assertion with `unknown field "trackingNumber"`; it doesn't read as "not equal". An `optional` output is missing whenever the response lacks its path, and one extracted as null fails too, with `cannot compare type <nil>`. When an output may be absent:
  - assert presence with `{type: fieldExists, path: trackingNumber}` when the output must be there; it fails on a missing or null output
  - keep the name out of predicates on steps whose response may not hold it
  - or set a flag in a transform, so every run has the output: `outputs.shipped = outputs.trackingNumber ~= nil`, with `shipped` declared on the node, then `shipped == false`
- **Array length:** a predicate can't count, but a path can.
  - Assert the count directly: `{type: fieldEquals, path: products.#, value: 6}`.
  - Or extract it with the rule `productCount: products.#`, declare `productCount` on the node, and compare it in a predicate.
- **Assert what was examined.** A check that nothing matched also passes on an empty list, so pair it with a count: `orderCount > 0 && openOrderCount == 0`.
- **Assert state after a change.** After a request that changes a resource, read the resource back. Assert that what should have changed did, and that what should have survived did too. A 2xx status alone doesn't show the change took effect.

### Retry

`retry: {max: 3, on: [transient], failOn: [auth]}` retries a failed step.

- **Rules** in `on` and `failOn` are failure categories or HTTP status codes (`on: [503]`):
  - `transient`: 429, 502, 503, 504, or a refused or reset connection
  - `server`: other 5xx
  - `client`: other 4xx
  - `auth`: 401 and 403
  - `timeout`, `network`, `adapter` (the request couldn't be built or its outputs extracted), and `response_error` (an `errorDetection` rule matched)
- **Defaults:** without `on`, a step retries `transient`, `timeout`, and `server`. `failOn` wins over `on`. Failed assertions and `expectFailure` steps never retry.
- **Waits:** between attempts the step waits a backoff of 500 ms, doubling, capped at 10 s, with ±25% jitter.
  - It waits longer when the failed response asks: through `Retry-After`, or `RateLimit-Reset` on a 429 without one. Either may be seconds or an HTTP date.
  - If a server asks for more than 60 s, the step ends as `failed_fast`.
- **Step retries vs `--retries`:** use step retries for rate limits and flaky responses. `--retries N` on `aat run` reruns the whole plan after 2 s, and it ignores those headers.
- **Same request:** a step's inputs are resolved once, so every attempt sends the same values, pool picks and generated values included.
- **Request timeout:** aat's client waits 30 s for each response. A request that takes longer fails with `no response within aat's 30s request timeout`, in the `timeout` category, which retries by default. The limit isn't configurable.
- **Known rate limits:** set `settings.minRequestInterval` (`250ms`, `1s`) in the environment file. It spaces the start of every request one command sends, `--parallel` plans included, but not OAuth token requests.

### Repeat

`repeat: {until: 'status == "complete"', collect: [items], interval: 2s, max: 30, timeout: 2m}` sends a step's request until `until` holds over a response's outputs, as when polling a background job. `repeat: {next: {after: nextCursor}, collect: [orders, orderCount], max: 50}` reads every page of a listing instead.

- **Defaults:** `until` is required unless `next` is set. `interval` is 1s, or none with `next`, lengthened by a response's `Retry-After` up to 60 s. `max` is 50, at most 1000. There is no default `timeout`.
- **Same request:** the inputs are resolved once, and each request is retried under `retry:`. With `next`, each request after the first sends the previous response's cursor output as that input; an empty cursor removes the input.
- **Outputs:** the last response's, with each `collect` output gathered across the responses: lists are appended, and integers and floats added. The step's assertions run once, on those. `until` reads each response's own outputs, so an output a response leaves out needs `default:` on its extract rule.
- **Failure:** reaching `max` or `timeout` before `until` holds fails the step with a `repeat` assertion result. A request that errors or returns 400 or more ends the repeats.
- **Paging:** with `next`, the step passes when every cursor comes back missing, `null`, or `""` (`exhausted`). Reaching `max` or `timeout` with a cursor left fails it, and so does a cursor an earlier request already sent (`loop`). The step's inputs are the first page's.
- **Not allowed:** with `expectFailure`, or on a node with a cleanup pairing. Verification steps can repeat.
- **Archive:** each request under `iterations` (request, response, outputs, `untilMet`, and `inputs` when paging), and why it stopped under `repeatStop`.

### Cleanup

Cleanup steps run after the plan completes (success or failure) to release resources:

```yaml
cleanup:
  - node: cancelOrder
    runOn: always    # always | success | failure
```

A cleanup step takes only `node` and `runOn`. Its inputs are matched by name against the outputs of the steps that ran, so `cancelOrder`'s `orderId` input takes the `orderId` output of the step that produced one. A node's graph-level `cleanup:` pairing runs even when the plan does not list it.

A cleanup node can have its own `cleanup:`, which makes a chain. The second node runs right after the first succeeds and takes that step's outputs first. That covers a release that takes two calls, such as requesting a refund and then confirming it with the refund's ID. Name the first cleanup node's output after the second one's input.

When a plan may release the resource itself, or leave it in a state the cleanup can't handle, give the pairing as a mapping:

```yaml
checkoutCart:
  adapter: checkoutCart
  cleanup:
    node: cancelOrder
    when: 'status == "created"'   # a predicate over checkoutCart's outputs
    releasedBy: [shipOrder]
```

- **Released.** The cleanup is skipped when a main step after the creating one succeeded on the cleanup node itself, or on a `releasedBy` node, and sent the same value for every input it shares with the cleanup. An explicit `cancelOrder` step needs no `releasedBy`. A step expected to fail, a verification step, and a step for another resource don't count.
- **`when`.** The cleanup is skipped when the predicate is false. It reads only the creating step's outputs, or, for a chained cleanup, the outputs of the cleanup step before it. If it can't be evaluated, the cleanup runs, and its record carries `whenError`.
- **Skipped.** A skipped cleanup's chain doesn't run. Skips are recorded in the archive's `cleanupSkipped` and show under `cleanup skipped:` in `aat run show`.
- **Caution.** List in `releasedBy` only nodes whose success always ends the resource. If an API reports a failed release in a successful response, give that node `errorDetection`.

To cancel only what is still open when the run ends, chain a read in front of the cancel. In a chain, `when` reads the outputs of the cleanup step before it, so the read supplies the current state:

```yaml
checkoutCart:
  cleanup:
    node: getOrderForCleanup   # a read that only cleanup uses
    releasedBy: [cancelOrder, shipOrder]
getOrderForCleanup:
  cleanup:
    node: cancelOrder
    when: 'status in ["created", "paid"]'
```

Give that read a node of its own. A main step on a pairing's cleanup node counts as releasing the resource, so reusing a read node that plans call would skip the cleanup whenever a plan reads the resource.

Cross-ref: [Plans and Recipes](https://gburgyan.github.io/aat/plans/)

### Lists and Pagination

A list step reads one page. To read every page, give it `repeat.next`, which sends each response's cursor as the next request's input:

```yaml
- node: listOrders
  values: {limit: 100}
  repeat:
    next: {after: nextCursor}    # input ← the same node's cursor output, extracted with default: ""
    collect: [orders, orderCount, liveOrderCount]
    max: 50
  assertions:
    mechanical:
      - type: predicate
        expr: orderCount > 0 && liveOrderCount == 0
```

- **Narrow the list to what you need:** filter by a reference the plan generated (`reference: "order-{{random 8}}"`), by a time window (`createdAfter: "{{unixtime - 1 hours}}"`), or by the parent resource, and raise the page size.
- **Pick elements** with a `select` of strategy `match` and a `filter`, from a collected list or a single page.
- **When you assert that nothing is left,** also assert that the listing covered everything: that it returned items, and that every page was read. With `next`, a listing cut off by `max` or `timeout` fails the step.
- **Cursors `next` can't follow,** such as one inside a `Link` header or a page number that needs arithmetic: chain list steps, the second taking the first page's cursor with `from: listOrders.nextCursor`.

## Depth and Negative Testing Primitives

AAT exposes four primitives for authoring per-endpoint error-case suites. All
compose with the normal plan/run pipeline — no separate CLI surface. You, the
agent, compose them into plans; AAT just runs them.

### 1. `expectFailure` on a step

Declare that failure is the expected outcome. The step passes when the response
status matches one of the listed codes and fails on any other status, including
a 2xx. Retries are skipped — the first response wins. Cleanup still runs.

```yaml
- node: createOrder
  values:
    productId: "NONEXISTENT"
  expectFailure:
    status: [400, 404]
    description: "Unknown product should be rejected"
```

All `expectFailure.status` entries must be `>= 400`. An expected-failure step
stores no outputs, so a later step can't read an ID from its error body. To check
the rejected object afterwards, create it in an earlier step that succeeds and
make the rejected call on it, or find it with a list step filtered by a value you
generated. Assertions on the step itself still read the error body, such as a
`fieldEquals` on `error.code`.

### 2. `mutations:` — sibling steps for negative variants

A `mutations:` block on a step expands at plan instantiation into one extra
sibling step per entry. Each sibling inherits the parent's `dependsOn`,
`selections`, and `values`, applies its own `set` overrides, and declares its
own `expectStatus`. The parent step stays in place as the happy-path run.

```yaml
- id: happy
  node: createBooking
  dependsOn: [setup]
  values: { lastName: "Smith", age: 30 }
  assertions:
    mechanical:
      - { type: status, expect: 200 }
      - { type: schema }              # validate response body against OAS
  mutations:
    - name: empty-lastName
      set: { lastName: "" }
      expectStatus: [400]
    - name: negative-age
      set: { age: -1 }
      expectStatus: [400, 422]
    - name: malformed-body
      rawBody: '{"oops":'             # bypasses template substitution entirely
      expectStatus: [400]
```

What happens at runtime:

- Prereq chain (`setup`) runs once.
- `happy` runs; if it returns 200 with a valid schema, it passes.
- Each mutation runs as an independent sibling with id `happy--<name>` (here:
  `happy--empty-lastName`, `happy--negative-age`, `happy--malformed-body`).
- Each sibling produces its own archive entry, so CI output cleanly reports
  which variants passed.

| Mutation field | Required | Description |
|----------------|----------|-------------|
| `name` | yes | Unique within the step. Becomes the id suffix. |
| `description` | no | Carried into the sibling's `expectFailure.description`. |
| `set` | no* | Map of input name → value. Overrides parent values. |
| `rawBody` | no* | Raw request body string. Overwrites the adapter-built body after template substitution. Use for malformed payloads. |
| `expectStatus` | yes | List of acceptable failure status codes (each `>= 400`). |

*At least one of `set` or `rawBody` must be provided.

**Shared vs. isolated prereqs.** By default the prereq chain (anything the
parent step depends on, transitively) runs *once* and every mutation sibling
references its outputs. That's correct for APIs that reject bad input before
touching state.

For stateful APIs — single-use tokens, consumable inventory, the happy path
committing a resource the mutations would then collide with — add
`mutationScope: isolated` on the parent step:

```yaml
- id: addItem
  node: addItem
  dependsOn: [createCart]
  mutationScope: isolated
  values:
    cartId: { from: createCart.cartId }
    productId: "P1"
  mutations: [...]
```

In isolated scope, AAT deep-clones the entire transitive prereq closure once
per mutation. Cloned step ids are `<origId>__<mutationName>`; all
`dependsOn` / `from` / `fromInput` references inside the clone subgraph
(including the mutation sibling) are rewritten to point at the clones, so each
mutation sees its own fresh state. Graph-level `cleanup:` pairing fires per
clone automatically.

Use `shared` (default) when prereqs are cheap and side-effect-free. Use
`isolated` when a mutation's outcome depends on not inheriting state from
earlier runs. Only `""`, `"shared"`, or `"isolated"` are accepted.

Validation rules (enforced at instantiation):

- Mutation names are unique within a step.
- `expectStatus` is non-empty and every entry is `>= 400`.
- `set` or `rawBody` (or both) must be present.
- `mutationScope` is `""`, `"shared"`, or `"isolated"`, and only allowed on steps that declare `mutations:`.
- Isolated-mutation clone ids (`<origId>__<mutationName>`) must not collide with pre-existing step ids in the plan.

**Runtime skip:** `aat run plan <plan> --no-mutations` (or the same flag on
`aat run batch`) strips every `mutations:` block in memory before
instantiation, so the run exercises only the happy path plus its prereq
chain. Useful for CI smoke tests and local iteration — especially with
isolated mutations where the full negative suite re-runs the prereq chain
per mutation and can be expensive.

### 3. `rawBody` on a step (standalone)

For one-off malformed-payload tests without a `mutations:` block, set `rawBody`
directly on a step. When non-empty, it replaces the adapter-built request body
at execution time, bypassing template placeholder substitution.

```yaml
- node: createBooking
  rawBody: '{"this": "is", "not": valid JSON'
  expectFailure:
    status: [400]
```

### 4. Overlay value and `expectFailure` overrides

Overlay files (`.aat-overrides.yaml`, `--overlay`, or `env.yaml`
`overrides:`) can override individual input values and declare expected
failure on matched nodes — without editing the plan. This is the primitive for
CI-driven negative suites, local debugging, and rerunning an existing plan as
a depth test.

```yaml
# .aat-overrides.yaml — run an existing plan as a negative test
overrides:
  - match: createBooking
    values:
      lastName: ""
      age: -1
    expectFailure:
      status: [400]
```

Semantics:

- `values:` merge into the resolved inputs map at step execution, overwriting
  plan-supplied values. Precedence: overlay values > plan step values > graph
  defaults.
- `expectFailure:` is applied to any step whose node matches `match`, but only
  when the step's plan doesn't already declare its own `expectFailure`.
- Match resolution: exact matches win over glob matches on conflicting keys.

### Patterns You'll Use

**Depth test an endpoint that needs setup state:** write a plan whose terminal
step targets the endpoint under test, with a `mutations:` block covering the
negative cases. The prereq chain runs once and each mutation runs as its own
sibling step (use `mutationScope: isolated` when each needs fresh prereqs).

**Turn an existing happy-path plan into a negative test without touching it:**
drop an overlay file with `values:` and `expectFailure:` on the target node.
Useful for local debugging and for layering error cases into a CI matrix.

**Validate response shapes:** add `- type: schema` to the step's mechanical
assertions. Requires OAS specs wired into the graph (the node's `oas:` reference).

**Mix happy-path and error cases in one archive:** combine `mutations:` with
schema assertions on the parent. The archive captures the happy-path step, the
schema validation, and every mutation sibling as separate entries.

## Workflow Schema

Workflows are declared in the graph YAML under `workflows:` and reference template files in `workflows/`.

### Kinds

| Kind | Field | Description |
|------|-------|-------------|
| *(none)* | Base workflow | Complete test skeleton with steps and cleanup |
| `slot` | `kind: slot` | Interchangeable fragment that fills a choice point |
| `addon` | `kind: addon` | Extension that splices steps at a declared insertion point |

### Declaration Example

```yaml
workflows:
  - name: Order Lifecycle
    description: "Create, verify, and clean up an order"
    template: workflows/order-base.yaml
    slots:
      - name: payment
        description: "Payment method"
        options: [Credit Card, PayPal]
        default: Credit Card

  - name: Credit Card
    kind: slot
    template: workflows/slots/credit-card.yaml

  - name: PayPal
    kind: slot
    template: workflows/slots/paypal.yaml

  - name: Loyalty Points
    kind: addon
    description: "Apply loyalty points after the order is created"
    after: createOrder
    wire:
      orderId: createOrder.orderId
    template: workflows/addons/loyalty.yaml
```

An addon's `after` names a node in the base plan (or a list of nodes; the first one present wins), not a slot.

### Workflow Template Files

Templates are plan files — steps with values, assertions, and cleanup. A base workflow marks each choice point with a `slot:` step that composition replaces with the chosen option's steps:

```yaml
# workflows/order-base.yaml
execution:
  steps:
    - id: createOrder
      node: createOrder
      values:
        productId: "PROD-001"
      assertions:
        mechanical:
          - type: status
            expect: 201
    - slot: payment
      dependsOn: [createOrder]
    - id: verifyOrder
      node: getOrder
      dependsOn: [payment]
      values:
        orderId:
          from: createOrder.orderId
  cleanup:
    - node: cancelOrder
      runOn: always
```

**Wiring template inputs.** A template can leave an input for composition to wire:
- **`AUTOWIRE`** takes the output of the same name from another step. That is the last producer in the plan so far, or, in a final pass after slots and addons, the nearest earlier step.
- **Addon `wire:` map.** It names a source for an input instead:
  - `$after.outputName` names the step the addon attached after.
  - `MANUAL` leaves the input for a recipe override.
- **`AUTOWIRE?`** is for an optional input that only some compositions feed, such as a value only an addon produces. It is wired when a step produces the output and left unset otherwise. Mark the graph input `optional: true` and wrap its template field in `{{?name}}…{{/name}}`.
- **An input `from:` an optional output.** When the earlier step didn't return the output, an optional input is left out, as with `AUTOWIRE?`. A required input fails the step. `aat validate --strict` warns when a required input takes `from:` an optional output, in a graph default or in a plan or workflow file.

**Verification and injected values.**
- **Verification.** A slot option or addon that declares `verification` for a node replaces every earlier check of that node. Checks of other nodes are kept, in this order: the base's, then slot options' in slot order, then addons' by priority. `verification` sits under `execution:`.
- **Injected values.** A slot option's `inject` sets an input on every base and slot step whose node declares that input.
  - **Skipped:** a step that already sets a value, pool, reference, or constraint keeps it. An empty `{}` doesn't count as set.
  - **Value forms:** the graph-default forms, except that a bare list is the literal list. `[2, 1]` injects that list, and so does `{value: [2, 1]}`. `{pool: [...]}` injects a pool, and `{from: node.output}` a reference. Any other mapping key, such as `default:`, is an error.
  - **Where it applies:** it doesn't reach addon steps. `inject` on an addon or a base workflow is a validation error.

Cross-ref: [Workflows](https://gburgyan.github.io/aat/workflows/)

## Layers and Batches

A layer is a named set of input values that a run applies on top of the graph defaults, so the same plans run across tiers, regions, or data sets without edits.

```yaml
# layers/express-socks.yaml
name: express-socks
description: Two pairs of socks, express shipping, delivered in three days
selectionHint: A small basket with a fast tier
inputs:
  addItem.sku: SKU-1006                     # node.input: addItem only
  quantity: 2                               # bare: every node with a quantity input
  shippingTier: express
  checkoutCart.deliveryDate: "{{today + 3 days}}"
```

- **The file** has `name`, `description`, `selectionHint` (read only when an LLM chooses layers), and `inputs`. The manifest's `layers:` names the directory that holds layer files.
- **Keys.** A key `node.input` sets that node only. A bare `input` sets every node that declares an input of that name.
  - **Watch for:** a qualified key leaves another node's input of the same name at that node's own default.
  - Use the bare key when every such node should change.
  - `aat run show latest --step ID` prints the inputs a step actually used, each with its source. `--resolutions` gives the details as JSON, including the selection that picked a value and the error for an input that couldn't be resolved.
- **Values** take the forms of a graph default:
  - a scalar
  - a YAML list, which is a pool (one element is picked per run)
  - `{value: [a, b]}` for a literal list
  - `{from: step.output}`
  - expressions such as `{{today + 3 days}}`
- **Precedence**, highest first:
  1. Overlay `values:`
  2. Any value the step sets itself: plan `values`, workflow template values, recipe `overrides.values`, slot `inject`, or `{}`
  3. Layers, where the later one wins: a recipe's `selection.layers` first, then `--layer` flags in order
  4. Graph defaults

  Within one layer, a qualified key wins over a bare one. Layers don't reach cleanup steps, whose inputs come from earlier outputs by name.
- **Unmatched keys.** `aat validate` reports a layer key that matches no node input.

```bash
aat run plan smoke --layer express-socks                 # one run with one layer
aat run batch --layer-group shipping-standard,shipping-express \
              --layer-group basket-gear,basket-apparel   # a matrix of every plan
```

- **Layer groups.** Each `--layer-group a,b` adds a dimension whose choices are none, `a`, or `b`. Two groups of two give (2+1) × (2+1) = 9 permutations of every plan. `--layer X` applies X to every run.
- **Duplicates.** With groups, permutations of a plan whose instantiated `execution` is identical run once.
  - The rest are listed in `batch.json` with `skipped: true` and a `duplicateOf` that names the run that stood in.
  - They count in the totals but not in the outcome.
  - A layer that sets a value the plan already uses produces duplicates.
  - `--no-dedup` runs them all.
- **Watching a long batch.**
  - `--quiet` and `--json` print nothing until the batch ends.
  - Without them, sequential mode prints every step as it runs. `--parallel N` prints a line per finished plan when its output isn't a terminal.
  - A plan that fails to load or validate shows up only in the final counts, `--json`, and `batch.json`.
  - Each run's archive is written when that run ends, so `aat run show latest` reads the most recent finished run while the batch continues.

Cross-ref: [Plans: Layers](https://gburgyan.github.io/aat/plans/#layers), [Matrix Testing](https://gburgyan.github.io/aat/batch-layers/)

## Iteration Loop

### Validate

```bash
aat validate                  # full project validation
aat validate graph            # graph structure, OAS alignment, template outputs
aat validate plan --plan X    # single plan
aat validate workflow         # all workflows
```

### Run

```bash
aat run plan <name-or-path>   # execute a single plan/recipe
aat run batch                 # execute all plans in the plans/ directory
aat run batch orders/         # execute plans in a subdirectory
```

When the graph has an OpenAPI spec, every run validates request and response bodies against it, in the mode `--oas-validate` names, else the environment's `settings.oasValidation`, else `auto`, which only warns. Set `settings.oasValidation: strict` once, so a violation fails its step in every run without the flag. Each run's `summary.json` records the result under `oas`, a clean run included: the mode, the request and response bodies validated, and the violations. `aat run show` prints it under the run's header.

### Read Output

Archives are written to the manifest's `archives` directory (`_output/runs/` when it is not set, or `--output`). The directory structure:

```
runs/
  run-20260223-143052-a1b2c3d4/     # single plan run
    archive.json                     # full execution archive
    summary.json                     # lightweight outcome summary
  batch-20260223-150000-e5f6g7h8/   # batch run
    batch.json                       # aggregated batch results
    run-20260223-150001-i9j0k1l2/   # per-plan run within batch
      archive.json
      summary.json
```

### Inspect a Run

Use `aat run show` to see what the API returned, rather than reading `archive.json` by hand. An archive of large responses can reach hundreds of megabytes.

```bash
aat run show latest                                     # the steps: status, pass or fail, output names
aat run show latest --step checkout                     # one step: URL, status, inputs, outputs, body sizes
aat run show latest --step checkout --response --shape  # the response's structure, one gjson path per line
aat run show latest --step checkout --response --path lines.0.sku
aat run show latest --step checkout --outputs           # what the template extracted
aat run show latest --step checkout --resolutions       # where each input's value came from, and why one failed
aat run show latest --json --compact                    # the step list as one JSON line, for a script
aat run show latest --response --path error.code        # one part of every step that has it
aat run show batch-20260914-112520-9e406b57             # a batch: totals, OAS counts, a row per run, cleanup counts
```

- **Learn a response with `--shape` before you write extract rules.**
  - Each line is a gjson path, usable in `response.extract` or with `--path`, followed by its type, array sizes, and a sample value.
  - Array elements are merged. `in 3 of 12` marks a key that only some elements hold, and `string|null` marks a value that is sometimes null. Give those extract entries `optional: true`, or handle them in a transform.
- **The run** is `latest` (runs inside a batch that is still running included), a run ID, `batch-ID/run-ID`, `batch-ID/PLAN` with a plan name as the batch's PLAN column shows it (`batch-ID/negative/state-machine`), or a path to a run directory, an `archive.json`, or an `.aar` file. A plan that ran as several runs of the batch, one per layer permutation, is an error that lists each run ID with its layers, so name the one you want. `--step` takes a step ID, or the name of a node that ran once.
- **A batch:** a batch ID, or a path to a batch directory or its `batch.json`, shows the batch instead. It gives the totals, the OAS validation mode with the bodies validated and the violations across its runs, a row per run, and per cleanup node how many steps ran, failed, and were skipped. Use it rather than reading `batch.json` or looping over archives.
- **Without `--step`,** a part flag or `--path` prints that part of every step that has it, one line each. `aat run show latest --response --path error.code` lists each refused request's code. `--shape` needs `--step`.
- **Printed parts stop at 64 KB**, with a note on stderr. Narrow them with `--path` or `--shape`, or pass `--max-bytes 0`. `--json` prints the step list or a step as JSON, with `snake_case` keys (`step_id`, `duration_ms`). A step's assertion results are `validation`, as in `archive.json`, whose keys are `camelCase` (`stepId`, `durationMs`).
- **To hand live state to another tool,** run `aat run plan <plan> --stop-after STEP --dump-state state.json`.
  - The dump holds each step's IDs, base URL, headers, inputs, and outputs.
  - Cleanup is skipped, so the resources stay alive.
  - Credentials read `[REDACTED]` unless `--dump-state-secrets` asks for them. Don't print a dump made with that flag: its credentials land wherever the output goes.

Cross-ref: [Archives: Inspecting a Run from the CLI](https://gburgyan.github.io/aat/archives/#inspecting-a-run-from-the-cli), [Checkpoints](https://gburgyan.github.io/aat/checkpoints/)

### Archive Schema (archive.json)

The archive is the primary debugging artifact. Read it to understand what happened during execution.

```json
{
  "metadata": {
    "version": "string",
    "runId": "string",
    "timestamp": "RFC3339",
    "plan": { },
    "instantiatedPlan": { },
    "environment": "string",
    "graphVersion": "string",
    "toolVersion": "string",
    "attempt": 1,
    "totalAttempts": 2,
    "layers": ["string"],
    "oasValidation": "auto | strict | off"
  },
  "steps": [ StepRecord ],
  "cleanup": [ StepRecord ],
  "cleanupSkipped": [
    { "node": "string", "cleanupFor": "string", "reason": "released | when", "releasedBy": "string", "when": "string" }
  ],
  "result": {
    "outcome": "passed | failed | error | aborted | stopped",
    "error": "string (omitted if blank)"
  }
}
```

`plan` is the plan as loaded (a recipe's reconstituted plan); `instantiatedPlan` is the plan after graph defaults, layers, and mutations were applied. `attempt`, `totalAttempts`, and `layers` are omitted when unused, and so is `cleanupSkipped` when no cleanup was skipped. `oasValidation` is the OpenAPI validation mode the run used, omitted when the graph references no spec.

**StepRecord** — one per executed step:

```json
{
  "stepId": "string",
  "node": "string",
  "cleanupFor": "string (cleanup steps only: the step whose resource it releases, or the cleanup step before it in a chain)",
  "whenError": "string (cleanup steps only: why the pairing's when condition couldn't be evaluated; the cleanup ran)",
  "startTime": "RFC3339",
  "durationMs": 0,
  "inputs": { "paramName": "resolvedValue" },
  "request": {
    "method": "POST",
    "url": "https://...",
    "originalUrl": "string (only when an override changed the host or path)",
    "headers": { "Content-Type": "application/json" },
    "body": { }
  },
  "response": {
    "status": 200,
    "headers": { },
    "body": { }
  },
  "outputs": { "fieldName": "extractedValue" },
  "transformScript": "string (the template's Lua transform, when it has one)",
  "displayOutputs": [ { "label": "string", "name": "string", "value": "any" } ],
  "validation": {
    "passed": true,
    "results": [
      {
        "type": "status | fieldExists | fieldEquals | predicate | schema",
        "passed": true,
        "skipped": false,
        "message": "string",
        "path": "string",
        "expr": "string",
        "raw": false
      }
    ]
  },
  "selections": [
    {
      "inputName": "string",
      "sourceNode": "string",
      "sourceField": "string",
      "sourceSize": 10,
      "filterExpr": "string",
      "filteredSize": 3,
      "strategy": "first | last | index | random | min | max | match",
      "selectedIndex": 0,
      "selectionName": "string (named selections only)",
      "field": "string (inline select: the field taken from the chosen element)",
      "sortField": "string (min/max: the field compared)",
      "sortValue": 19.99,
      "ties": 3,
      "onTie": "first | fail"
    }
  ],
  "resolutions": [
    {
      "inputName": "string",
      "source": "plan_default | graph_default | layer | expression | plan_from | select_edge | named_selection | from_input | from_resolved | fallback_pool | optional_skip | override_value | error",
      "layer": "the layer that set the value, when one did",
      "rawValue": "any",
      "finalValue": "any",
      "fromStep": "string",
      "fromOutput": "string",
      "fromInput": "string",
      "expression": "string",
      "constraint": "string",
      "constraintOk": true,
      "poolIndex": 0,
      "poolSize": 3,
      "tried": ["any"],
      "error": "string (source error only: why the input couldn't be resolved)"
    }
  ],
  "errorClassification": {
    "category": "transient | client | auth | server | adapter | network | timeout | response_error",
    "detail": "string",
    "action": "retried | failed | failed_fast",
    "retryAttempt": 0
  },
  "expectFailure": {
    "expected": [400, 422],
    "actual": 400,
    "passed": true
  },
  "responseBodyError": {
    "rulePath": "string",
    "rule": "exists | non-empty | equals",
    "message": "string",
    "code": "string",
    "category": "string"
  },
  "retryCount": 0,
  "retriedOn": ["transient"],
  "error": "string (omitted if blank)"
}
```

Fields with no value are omitted. `errorClassification.category` uses the same names as a step's `retry.on` and `retry.failOn` lists; `responseBodyError` records the graph `errorDetection` rule that failed a successful response.

Sensitive headers (`Authorization`, `Proxy-Authorization`, `X-API-Key`, `X-Auth-Token`, `Cookie`, `Set-Cookie`) are redacted to `"[REDACTED]"`, and known secret values (every secret credential configured for the run) are redacted from every string in the archive, bodies and URLs included; a secret shorter than eight characters only where a whole value equals it. See [Archives](https://gburgyan.github.io/aat/archives/). A JSON body is embedded as JSON; any other body is stored as a JSON string.

Mutation-expanded steps use the id format `<parentId>--<mutationName>` (e.g.,
`createBooking--empty-lastName`). Each mutation sibling appears as a separate
`StepRecord`, with `expectFailure` set and the parent's `dependsOn` inherited.

When an OAS spec is configured on the step's node, the archive's `StepRecord`
also carries an `oasValidation` object. If the step has a `type: schema`
assertion, the same OAS response-validation result is surfaced as an entry in
`validation.results`. Shape:

```json
"oasValidation": {
  "operationId": "createBooking",
  "skipped": false,
  "skipReason": "only present when skipped",
  "request":  { "valid": true, "errors": [], "compilationWarnings": [] },
  "response": { "valid": false, "skipped": true, "skipReason": "the schema could not be compiled: …" }
}
```

Each `errors[]` entry is `{ "path": "/field", "message": "..." }`. A payload that wasn't validated, such as an XML request
body or a schema the validator can't compile, has `skipped: true` and a `skipReason`, and never counts as a violation.

### Summary Schema (summary.json)

A lightweight file for scanning run outcomes without reading the full archive:

```json
{
  "runId": "string",
  "timestamp": "RFC3339",
  "outcome": "passed | failed | error | aborted | stopped",
  "stepCount": 5,
  "passedCount": 4,
  "failedCount": 1,
  "durationMs": 1234,
  "planName": "string",
  "attempt": 1,
  "totalAttempts": 2,
  "layers": ["string"],
  "issues": { "oas": 2 },
  "oas": { "mode": "auto | strict | off", "validatedRequests": 4, "validatedResponses": 7, "violations": 2 }
}
```

`oas` is recorded whenever the run's graph references an OpenAPI spec, a clean run included, so it shows that validation ran: the mode, the request and response bodies validated (cleanup steps included), and the violations, which `issues.oas` counts too.

### Batch Schema (batch.json)

Aggregated results from `aat run batch`:

```json
{
  "metadata": {
    "version": "string",
    "batchId": "string",
    "timestamp": "RFC3339",
    "source": "string",
    "toolVersion": "string",
    "layers": ["string"],
    "layerGroups": [["string"]]
  },
  "runs": [
    {
      "planName": "string",
      "runId": "string",
      "outcome": "passed | failed | error | aborted | skipped",
      "stepCount": 5,
      "passedCount": 5,
      "failedCount": 0,
      "durationMs": 1234,
      "error": "string (omitted if blank)",
      "attempts": 2,
      "layers": ["string"],
      "permutation": "string",
      "skipped": true,
      "duplicateOf": "string",
      "issues": { "oas": 2 }
    }
  ],
  "result": {
    "outcome": "passed | failed | error | aborted",
    "totalRuns": 3,
    "passedRuns": 2,
    "failedRuns": 1,
    "errorRuns": 0,
    "abortedRuns": 0,
    "skippedRuns": 0,
    "totalDurationMs": 3456
  }
}
```

In `summary.json` and `batch.json`, optional fields such as `attempt`, `attempts`, `layers`, `issues`, `oas`, `permutation`, `skipped`, `duplicateOf`, `abortedRuns`, and `skippedRuns` are omitted when unused. `skipped` and `duplicateOf` mark a layer permutation that was skipped as a duplicate of another run.

### Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `line N: unknown key "K" in <section> (did you mean "X"?)` | Misspelled or unsupported key in a project file | Use the suggested key, or remove it |
| `line N: a second YAML document starts here; a project file holds exactly one` | Two YAML documents separated by `---` in one file, such as two plans pasted together | Split them into separate files |
| `adapter "X" not found` | Template missing or adapter name mismatch | Check `adapter` field in template matches graph node |
| `step N: node "X" not found in graph` | Plan references a node not in the graph | Check node name spelling |
| `required input "X" has no plan value` / `has no value` | Required input has no value, no default, and no upstream output | Add a value in the plan or a default in the graph |
| `requires/satisfies cycle detected: A → B → A` | Prerequisite tokens form a circular dependency | Review `requires`/`satisfies` tokens, or mark a node `cycleBreaker: true` |
| `dependsOn cycle detected involving "A" and "B"` | Plan steps depend on each other, through `dependsOn` or through a reference, which the message names | Remove the `dependsOn` entry, or read the value from another step |
| `unresolved placeholders: X` | A template placeholder had no value at run time | Give the input a value or default, or wrap the placeholder in a `{{?X}}…{{/X}}` block |
| `extract path "X" (…) not found in response` | The response lacks a path the template extracts | Fix the path, or give the rule `optional: true` or a `default:` |
| `executing HTTP request: no response within aat's 30s request timeout` | The API took longer than aat's 30-second limit to answer | Check the API; a step `retry` covers `timeout` by default |
| `invalid expression syntax: …`, `random takes a length from 1 to 64`, `today counts days` | A malformed `{{…}}` expression | Fix it; see [Expressions](#expressions) |
| `strict OAS validation: reading OAS spec …` | `--oas-validate strict` with a spec that doesn't load | Fix the graph's `oas:` path, or run with `--oas-validate auto` |
| `additional properties 'X' not allowed` in an OAS request error | The request sends a field the spec doesn't declare | Remove the field, or fix its spelling |
| `a single value, where integer[] takes a list` | A literal value or pool entry doesn't fit the input's type. In a graph default or a layer, a bare list is a pool | Write one list as `{value: [...]}`, or fix the value or the type |

## Tips for AI Assistants

- **Start small**: begin with 2-3 nodes, get them working end-to-end, then expand the graph incrementally.
- **Validate early and often**: run `aat validate` after every change to catch typos before execution.
- **Use default pools**: graph input defaults with pool lists (`default: ["A", "B", "C"]`) provide varied test data without plan-level overrides.
- **Prefer recipes over full plans** when a workflow exists — recipes are shorter and easier to maintain.
- **Graph defaults wire the common case; plans wire the rest**: nodes define what an operation accepts and produces, an input's `default: {from: ...}` names the output it usually takes, and plans override or add wiring for a specific test. When a plan runs that node in several steps, a main step's default reads the nearest earlier one that isn't expected to fail, and a verification step's reads the last such step; set `values` on the step, a verification step included, to read another.
- **Read results with `aat run show`**: after a run, `aat run show latest` lists the steps, and `--step ID` shows one of them: its URL, status, inputs, outputs, and failed assertions. `--response --shape` shows what the API returned without pouring a large body into your context. The same data is in `archive.json`, under `steps[].request`, `steps[].response`, `steps[].validation`, and `steps[].errorClassification`.
- **Ordering is declared, not wired**: nodes use `requires`/`satisfies` tokens, not explicit edges. If node B needs node A to have run, give A a token that B requires; the MCP tracing tools and `aat validate` use them. A full plan runs its steps in `dependsOn` order. A step that a `from`, `fromInput`, or selection reads is added to it, so list only ordering the data doesn't show (composing a recipe adds them from the tokens); data moves through step values (`from`, selections) and graph defaults, not through tokens.
- **Cleanup pairing**: if a node creates a resource, set its `cleanup` field to the deletion node. The engine runs the pairing after the plan even when the plan does not list it. When releasing the resource takes two calls, give the first cleanup node a `cleanup` of its own. The second node runs right after the first succeeds and takes its outputs.
- **Template placeholders must match node inputs**: every `{{name}}` in a template should correspond to an input on the linked node; a placeholder that gets no value fails the request.
- **Keep secrets out of files**: credentials use `source: env` to read OS environment variables (`source: literal` exists for demo values only).
- **No LLM at run time**: `aat run` and the MCP `execute_plan` tool never call a model; nothing selects values or workflows with an LLM while a plan runs. Only `aat prompt` and the MCP `generate_plan` tool call an LLM, and only to draft a plan.
