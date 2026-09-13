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
4. **Set up the environment** — base URL, auth credentials, secrets
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

  The warnings list what each template leaves to write by hand, such as a form property that takes an object, which you write as bracketed pairs (`shipping[city]={{city}}`). Specs with circular references load. See [Large Specs](https://gburgyan.github.io/aat/generate/#large-specs).
- **Validate the project against it.** `aat validate --strict` checks that each node's `operationId` exists, that its inputs are parameters or body properties (an input the template sends only in a header, such as an idempotency key, is exempt), that required fields are inputs or written by the template, and that outputs exist in the 2xx response schema.
- **Validate every run against it.** `--oas-validate strict` checks each step's request body, JSON or form-encoded, and its response body against the schema for its status code or the spec's `default` response, and fails a step on a violation. A request body of another type, or a schema the validator can't compile, shows `OAS: request not validated` or `OAS: response not validated` and never fails a step. Under `strict`, a spec that fails to load stops the run with exit code 2. Only the operations the graph's nodes name are compiled, so a large spec loads quickly. See [OAS Validation](https://gburgyan.github.io/aat/running/#oas-validation).

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
| `cleanup` | no | Node to run during teardown (e.g., delete what this node created) |
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
| `optional` | no | If true, the template need not extract it |
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
- **Ties:** when several elements share the smallest or largest value, the first of them in array order after the `filter` wins. If candidates can tie, add a `filter` that narrows them to the element you mean, and assert the chosen element's distinguishing field in a later step.
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
- `{{placeholder}}` in path, headers, and body are replaced with resolved input values; a placeholder with no value fails the request (`unresolved placeholders: …`), so wrap optional parts in `{{?name}}…{{/name}}`
- `response.extract` is a map from output name to a gjson path into the response JSON (`$.` prefixes are accepted). A path can count an array: `productCount: products.#`
- For array extraction, give the output a `path` and a `fields` map from element field name to a path within each element; add `optional: true` to an entry whose path may be missing
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
- **Values take placeholders.** A header whose whole value is a conditional block that resolves to nothing isn't sent.

**Idempotency keys.** Give the node an optional input that generates a key, and send the header only when the input has a value:

```yaml
# graph.yaml, on the node's inputs
- name: requestKey
  type: string
  optional: true
  default: "{{uuid}}"
# the template
headers:
  Idempotency-Key: "{{?requestKey}}{{requestKey}}{{/requestKey}}"
```

A retried step resends the same key, and a later step replays it with `requestKey: {fromInput: createOrder.requestKey}`. Keep the header conditional: a cleanup step takes its inputs only from earlier outputs, not from graph defaults. See [Idempotency Key Header](https://gburgyan.github.io/aat/templates/#idempotency-key-header).

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
  minRequestInterval: 250ms   # optional: space request starts for a rate-limited API
```

`settings.minRequestInterval` needs a unit (`250ms`, `1s`). One interval covers everything a command sends, including every plan of a `--parallel` batch, retries, verification, and cleanup.

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

A plan's top-level keys are `metadata` (`created`, `prompt`, `graphVersion`), `graph`, `auth`, `headers`, `intent` (`goal` — the step ID of the `isGoal` step — `description`, and `constraints`), and `execution` (`steps`, `verification`, `cleanup`). Anything else is rejected.

### Step Value Forms

| Form | Example | Description |
|------|---------|-------------|
| Bare scalar | `quantity: 2` | Literal value |
| Reference | `orderId: {from: create.orderId}` | Output from a previous step |
| Selection | `productId: {from: list.products, select: {strategy: first, field: productId}}` | Pick from array |
| Expression | `date: "{{today + 7 days}}"` | Computed when the step runs (see [Expressions](#expressions)) |
| Pool | `shippingTier: {pool: [standard, express]}` | One element, picked per run |
| List | `skus: {default: [SKU-1001, SKU-1006]}` | The list itself, for an input a template repeats over with `{{#skus}}…{{/skus}}` |
| Absent | `deliveryDate: {}` | Nothing fills the input: no graph default, layer, or auto-wiring |

- **Lists:** a bare YAML list is an error in a step value (`a step value must be a scalar or a mapping, found a list`), and `value:` is not a step-value key. Write a list as `{default: [...]}`.
- **`{}` is for optional inputs.** An optional input marked `{}` is left out of the request. A required input marked `{}` still takes its graph default when that default is a plain literal, used as written: no expression evaluation, no layers. With no graph default, the step fails with `required input has no value (empty step value)`.

### Expressions

A string containing `{{…}}` is an expression.

| Form | Result |
|------|--------|
| `{{today}}`, `{{today + 7 days}}`, `{{today - 7 days}}` | A `YYYY-MM-DD` date in the local time zone of the machine running `aat` |
| `{{env.KEY}}` | The OS environment variable `KEY`, else the environment file's `values:` entry; fails when both are empty |
| `{{name}}`, `{{name + 3 days}}` | Another input of the same step, declared earlier on the node; date arithmetic needs a `YYYY-MM-DD` value |
| `{{uuid}}` | A random version 4 UUID |
| `{{random N}}` | `N` random characters from `0-9a-z`, with `N` from 1 to 64 |
| `{{now}}`, `{{now + 90 minutes}}` | The time in UTC, RFC 3339 to the second. Units: `seconds`, `minutes`, `hours`, `days` |
| `{{unixtime}}`, `{{unixtime - 1 hours}}` | The time as Unix seconds, an integer, with the same units |

- **Types.** A value that is one whole expression keeps the result's type. Mixed text, such as `"Deliver on {{deliveryDate}}"`, becomes a string.
- **Generated values.** Each occurrence is its own value, so two inputs set to `{{uuid}}` differ; reuse one with `fromResolved` or `fromInput`. A step's values are resolved once, so a retried step resends the same ones, and a new run generates new ones. `uuid`, `now`, and `unixtime` are reserved words, and `{{today}}` counts days only.
- **Where they are evaluated:** step values, pools, graph defaults, layers, recipe overrides, slot `inject`, and mutation `set`.
- **Where they are not:** templates (where `{{name}}` is a placeholder for an input), overlay `values:`, `rawBody`, and assertions.
- **Checking.** `aat validate` does not check expression syntax; a bad expression fails when its step runs.

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

- **Values are compared literally.** `{{…}}` is not expanded in `expect`, `value`, or `expr`: `value: "{{today}}"` compares against the text `{{today}}`. Compute the value in a transform output, and assert on that output.
- **Predicate syntax:**
  - comparisons `== != < > <= >=`, and `&&`, `||`, `!`
  - `in`, as in `currency in ["USD", "EUR"]`
  - parentheses, quoted strings, numbers, `true`, and `false`
  - dots for nested fields
  - no `null`, arithmetic, or indexing
  - write `!(a == b)`, not `!a == b`
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

### Cleanup

Cleanup steps run after the plan completes (success or failure) to release resources:

```yaml
cleanup:
  - node: cancelOrder
    runOn: always    # always | success | failure
```

A cleanup step takes only `node` and `runOn`. Its inputs are matched by name against the outputs of the steps that ran, so `cancelOrder`'s `orderId` input takes the `orderId` output of the step that produced one. A node's graph-level `cleanup:` pairing runs even when the plan does not list it.

A cleanup node can have its own `cleanup:`, which makes a chain. The second node runs right after the first succeeds and takes that step's outputs first. That covers a release that takes two calls, such as requesting a refund and then confirming it with the refund's ID. Name the first cleanup node's output after the second one's input.

Cross-ref: [Plans and Recipes](https://gburgyan.github.io/aat/plans/)

### Lists and Pagination

A step sends one request and reads one response, and plans have no loop, so a list step reads one page.

- **Narrow the list to what you need:** filter by a reference the plan generated (`reference: "order-{{random 8}}"`), by a time window (`createdAfter: "{{unixtime - 1 hours}}"`), or by the parent resource, and raise the page size.
- **Pick elements** with a `select` of strategy `match` and a `filter`.
- **For a fixed number of pages,** chain list steps: the second takes the first page's cursor with `from: listOrders.nextCursor`.
- **When you assert that nothing is left,** also assert that the listing covered everything: that it returned items, and that it wasn't cut off, with an output such as `hasMore == false`.

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

**Verification and injected values.**
- **Verification.** A slot option or addon that declares `verification` for a node replaces every earlier check of that node. Checks of other nodes are kept, in this order: the base's, then slot options' in slot order, then addons' by priority. `verification` sits under `execution:`.
- **Injected values.** A slot option's `inject` sets an input on every base and slot step whose node declares that input, unless the step already sets a value (an empty `{}` doesn't count). It doesn't reach addon steps, and `inject` on an addon is ignored.

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
  - `aat run show latest --step ID` prints the inputs a step actually used.
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
```

- **Learn a response with `--shape` before you write extract rules.**
  - Each line is a gjson path, usable in `response.extract` or with `--path`, followed by its type, array sizes, and a sample value.
  - Array elements are merged. `in 3 of 12` marks a key that only some elements hold, and `string|null` marks a value that is sometimes null. Give those extract entries `optional: true`, or handle them in a transform.
- **The run** is `latest` (runs inside a batch that is still running included), a run ID, `batch-ID/run-ID`, or a path to a run directory, an `archive.json`, or an `.aar` file. `--step` takes a step ID, or the name of a node that ran once.
- **Printed parts stop at 64 KB**, with a note on stderr. Narrow them with `--path` or `--shape`, or pass `--max-bytes 0`. `--json` prints the step list or a step as JSON.
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
    "layers": ["string"]
  },
  "steps": [ StepRecord ],
  "cleanup": [ StepRecord ],
  "result": {
    "outcome": "passed | failed | error | aborted | stopped",
    "error": "string (omitted if blank)"
  }
}
```

`plan` is the plan as loaded (a recipe's reconstituted plan); `instantiatedPlan` is the plan after graph defaults, layers, and mutations were applied. `attempt`, `totalAttempts`, and `layers` are omitted when unused.

**StepRecord** — one per executed step:

```json
{
  "stepId": "string",
  "node": "string",
  "cleanupFor": "string (cleanup steps only: the step whose resource it releases, or the cleanup step before it in a chain)",
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
      "selectionName": "string (named selections only)"
    }
  ],
  "resolutions": [
    {
      "inputName": "string",
      "source": "plan_default | expression | plan_from | select_edge | named_selection | from_input | from_resolved | fallback_pool | graph_default | optional_skip",
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
      "tried": ["any"]
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
  "issues": { "oas": 2 }
}
```

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

In `summary.json` and `batch.json`, optional fields such as `attempt`, `attempts`, `layers`, `issues`, `permutation`, `skipped`, `duplicateOf`, `abortedRuns`, and `skippedRuns` are omitted when unused. `skipped` and `duplicateOf` mark a layer permutation that was skipped as a duplicate of another run.

### Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `line N: unknown key "K" in <section> (did you mean "X"?)` | Misspelled or unsupported key in a project file | Use the suggested key, or remove it |
| `line N: a second YAML document starts here; a project file holds exactly one` | Two YAML documents separated by `---` in one file, such as two plans pasted together | Split them into separate files |
| `adapter "X" not found` | Template missing or adapter name mismatch | Check `adapter` field in template matches graph node |
| `step N: node "X" not found in graph` | Plan references a node not in the graph | Check node name spelling |
| `required input "X" has no plan value` / `has no value` | Required input has no value, no default, and no upstream output | Add a value in the plan or a default in the graph |
| `requires/satisfies cycle detected: A → B → A` | Prerequisite tokens form a circular dependency | Review `requires`/`satisfies` tokens, or mark a node `cycleBreaker: true` |
| `dependsOn cycle detected involving "A" and "B"` | Plan steps depend on each other | Fix the steps' `dependsOn` lists |
| `unresolved placeholders: X` | A template placeholder had no value at run time | Give the input a value or default, or wrap the placeholder in a `{{?X}}…{{/X}}` block |
| `extract path "X" (…) not found in response` | The response lacks a path the template extracts | Fix the path, or mark the extract entry `optional: true` |
| `executing HTTP request: no response within aat's 30s request timeout` | The API took longer than aat's 30-second limit to answer | Check the API; a step `retry` covers `timeout` by default |
| `invalid expression syntax: …`, `random takes a length from 1 to 64`, `today counts days` | A malformed `{{…}}` expression | Fix it; see [Expressions](#expressions) |
| `strict OAS validation: reading OAS spec …` | `--oas-validate strict` with a spec that doesn't load | Fix the graph's `oas:` path, or run with `--oas-validate auto` |
| `additional properties 'X' not allowed` in an OAS request error | The request sends a field the spec doesn't declare | Remove the field, or fix its spelling |

## Tips for AI Assistants

- **Start small**: begin with 2-3 nodes, get them working end-to-end, then expand the graph incrementally.
- **Validate early and often**: run `aat validate` after every change to catch typos before execution.
- **Use default pools**: graph input defaults with pool lists (`default: ["A", "B", "C"]`) provide varied test data without plan-level overrides.
- **Prefer recipes over full plans** when a workflow exists — recipes are shorter and easier to maintain.
- **Graph defaults wire the common case; plans wire the rest**: nodes define what an operation accepts and produces, an input's `default: {from: ...}` names the output it usually takes, and plans override or add wiring for a specific test.
- **Read results with `aat run show`**: after a run, `aat run show latest` lists the steps, and `--step ID` shows one of them: its URL, status, inputs, outputs, and failed assertions. `--response --shape` shows what the API returned without pouring a large body into your context. The same data is in `archive.json`, under `steps[].request`, `steps[].response`, `steps[].validation`, and `steps[].errorClassification`.
- **Ordering is declared, not wired**: nodes use `requires`/`satisfies` tokens, not explicit edges. If node B needs node A to have run, give A a token that B requires; the MCP tracing tools and `aat validate` use them. A full plan runs its steps in `dependsOn` order, so list dependencies there (composing a recipe adds them from the tokens); data moves through step values (`from`, selections) and graph defaults, not through tokens.
- **Cleanup pairing**: if a node creates a resource, set its `cleanup` field to the deletion node. The engine runs the pairing after the plan even when the plan does not list it. When releasing the resource takes two calls, give the first cleanup node a `cleanup` of its own. The second node runs right after the first succeeds and takes its outputs.
- **Template placeholders must match node inputs**: every `{{name}}` in a template should correspond to an input on the linked node; a placeholder that gets no value fails the request.
- **Keep secrets out of files**: credentials use `source: env` to read OS environment variables (`source: literal` exists for demo values only).
- **No LLM at run time**: `aat run` and the MCP `execute_plan` tool never call a model; nothing selects values or workflows with an LLM while a plan runs. Only `aat prompt` and the MCP `generate_plan` tool call an LLM, and only to draft a plan.
