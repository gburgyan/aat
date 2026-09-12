# AAT for AI Assistants

This document is a structural primer for AI coding assistants (Claude Code, Cursor, Copilot, etc.) working on AAT projects. It gives you the schema knowledge to author and iterate on graphs, templates, plans, workflows, and environments without reading the full reference docs.

If you need deeper detail on any topic, follow the cross-reference links to the full documentation.

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

Cross-ref: [Project Setup](project-setup.md)

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

AAT also has its own MCP server (`aat mcp serve`) that exposes graph introspection, workflow listing, validation, and plan scaffolding tools. See [MCP Server](mcp-server.md).

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

`min` and `max` selections compare their `sortField` by value. A string that holds a number, such as `"19.99"`,
compares as that number. A `filter` does not convert strings.

Cross-ref: [API Graphs](graphs.md)

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
- `response.extract` is a map from output name to a gjson path into the response JSON (`$.` prefixes are accepted)
- For array extraction, give the output a `path` and a `fields` map from element field name to a path within each element; add `optional: true` to an entry whose path may be missing

```yaml
response:
  extract:
    items:
      path: results
      fields:
        itemId: id
        title: name
```

Cross-ref: [Templates](templates.md)

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

Cross-ref: [Environments](environments.md)

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
| Expression | `date: "{{today + 7 days}}"` | Dynamic value |

### Assertion Types

Assertions live under `assertions.mechanical` on each step.

| Type | Fields | Description |
|------|--------|-------------|
| `status` | `expect` | HTTP status: an exact code (`201`) or a class (`2xx`, `4xx`) |
| `fieldExists` | `path` | Path exists and is not null |
| `fieldEquals` | `path`, `value` | Value at the path equals `value` |
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

### Cleanup

Cleanup steps run after the plan completes (success or failure) to release resources:

```yaml
cleanup:
  - node: cancelOrder
    runOn: always    # always | success | failure
```

A cleanup step takes only `node` and `runOn`. Its inputs are matched by name against the outputs of the steps that ran, so `cancelOrder`'s `orderId` input takes the `orderId` output of the step that produced one. A node's graph-level `cleanup:` pairing runs even when the plan does not list it.

Cross-ref: [Plans and Recipes](plans.md)

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

All `expectFailure.status` entries must be `>= 400`. Outputs are not stored on
an expected-failure step (the error response has nothing meaningful downstream).

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

Cross-ref: [Workflows](workflows.md)

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

Sensitive headers (`Authorization`, `Proxy-Authorization`, `X-API-Key`, `X-Auth-Token`, `Cookie`, `Set-Cookie`) are redacted to `"[REDACTED]"`, and known secret values (every secret credential configured for the run) are redacted from every string in the archive, bodies and URLs included; a secret shorter than eight characters only where a whole value equals it. See [Archives](archives.md). A JSON body is embedded as JSON; any other body is stored as a JSON string.

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
  "response": { "valid": true, "errors": [], "compilationWarnings": [] }
}
```

Each `errors[]` entry is `{ "path": "/field", "message": "..." }`.

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

## Tips for AI Assistants

- **Start small**: begin with 2-3 nodes, get them working end-to-end, then expand the graph incrementally.
- **Validate early and often**: run `aat validate` after every change to catch typos before execution.
- **Use default pools**: graph input defaults with pool lists (`default: ["A", "B", "C"]`) provide varied test data without plan-level overrides.
- **Prefer recipes over full plans** when a workflow exists — recipes are shorter and easier to maintain.
- **Graph defaults wire the common case; plans wire the rest**: nodes define what an operation accepts and produces, an input's `default: {from: ...}` names the output it usually takes, and plans override or add wiring for a specific test.
- **Read archives on failure**: when a test fails, read `archive.json` — `steps[].request` and `steps[].response` show the actual HTTP exchange, `steps[].validation` shows which assertions failed, and `steps[].errorClassification` explains what went wrong.
- **Ordering is declared, not wired**: nodes use `requires`/`satisfies` tokens, not explicit edges. If node B needs node A to have run, give A a token that B requires; the MCP tracing tools and `aat validate` use them. A full plan runs its steps in `dependsOn` order, so list dependencies there (composing a recipe adds them from the tokens); data moves through step values (`from`, selections) and graph defaults, not through tokens.
- **Cleanup pairing**: if a node creates a resource, set its `cleanup` field to the deletion node. The engine runs the pairing after the plan even when the plan does not list it.
- **Template placeholders must match node inputs**: every `{{name}}` in a template should correspond to an input on the linked node; a placeholder that gets no value fails the request.
- **Keep secrets out of files**: credentials use `source: env` to read OS environment variables (`source: literal` exists for demo values only).
- **No LLM at run time**: `aat run` and the MCP `execute_plan` tool never call a model; nothing selects values or workflows with an LLM while a plan runs. Only `aat prompt` and the MCP `generate_plan` tool call an LLM, and only to draft a plan.
