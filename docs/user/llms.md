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

Required fields: `name` and `graph`. All paths resolve relative to the manifest file.

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
  runs/                   # execution archives (auto-created)
```

Cross-ref: [Project Setup](project-setup.md)

## Authoring Workflow

The recommended sequence for building an AAT project:

1. **Create the manifest** — `aat-project.yaml` with name and graph path
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

The graph declares API operations as nodes with typed inputs, outputs, and ordering rules. Nodes have signatures; plans wire data between them.

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
        path: id
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
        path: status
    requires: [order]
```

### Key Node Fields

| Field | Required | Description |
|-------|----------|-------------|
| `adapter` | yes | Links to a template file by adapter name |
| `inputs` | no | Typed inputs resolved at runtime from plan values or defaults |
| `outputs` | no | Named values extracted from the HTTP response |
| `satisfies` | no | Ordering tokens this node provides |
| `requires` | no | Ordering tokens this node depends on |
| `cleanup` | no | Node to run during teardown (e.g., delete what this node created) |
| `conditions` | no | Error detection rules on response status codes |

### Input Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique within the node |
| `type` | yes | Data type: `string`, `integer`, `float`, `boolean`, `date`, `money`, custom |
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
| `name` | yes | Output name, referenced as `nodeName.outputName` in plans |
| `type` | yes | Data type |
| `path` | no | gjson extraction path into the JSON response |
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
    - name: orderId
      path: id
    - name: status
      path: status
```

### Template Rules

- `adapter` must match a graph node's `adapter` field
- `{{placeholder}}` in path, headers, and body are replaced with resolved input values
- `response.extract` maps output names to gjson paths into the response JSON
- For array extraction, use `type: array` with `fields` listing each element field

```yaml
response:
  extract:
    - name: items
      type: array
      path: results
      fields:
        - name: itemId
          path: id
        - name: title
          path: name
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
```

### Auth Types

| Type | Credentials | Description |
|------|-------------|-------------|
| `none` | — | No authentication |
| `apikey` | `key` | Sent in the header specified by `headerName` |
| `bearer` | `token` | Sent as `Authorization: Bearer <token>` |
| `oauth2` | `username`, `password`, `clientId`, `clientSecret` | ROPC flow via `tokenUrl` |

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
metadata:
  name: create-and-verify-order
  description: "Create an order and verify it exists"
intent:
  summary: "End-to-end order creation test"
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
      values:
        orderId:
          from: create.orderId
      runOn: always
```

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
| `status` | `expect` | HTTP status code |
| `fieldExists` | `path` | JSON path exists in response |
| `fieldEquals` | `path`, `value` | JSON path value matches expected |
| `predicate` | `expr` | Boolean expression (e.g., `status >= 200 && status < 300`) |

### Cleanup

Cleanup steps run after the plan completes (success or failure) to release resources:

```yaml
cleanup:
  - node: cancelOrder
    values:
      orderId: {from: create.orderId}
    runOn: always    # always | success | failure
```

Cross-ref: [Plans and Recipes](plans.md)

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
    description: "Apply loyalty points after payment"
    after: payment
    wire:
      orderId: createOrder.orderId
    template: workflows/addons/loyalty.yaml
```

### Workflow Template Files

Templates look like plan execution blocks — steps with values, assertions, and cleanup:

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
    # slot: payment (filled at composition time)
    - id: verifyOrder
      node: getOrder
      values:
        orderId:
          from: createOrder.orderId
  cleanup:
    - node: cancelOrder
      values:
        orderId:
          from: createOrder.orderId
      runOn: always
```

Cross-ref: [Workflows](workflows.md)

## Iteration Loop

### Validate

```bash
aat validate                  # full project validation
aat validate graph            # graph structure only
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

Archives are written to `runs/` (or the configured archive directory). The directory structure:

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
    "environment": "string",
    "graphVersion": "string",
    "toolVersion": "string"
  },
  "steps": [ StepRecord ],
  "cleanup": [ StepRecord ],
  "result": {
    "outcome": "passed | failed | error",
    "error": "string (omitted if blank)"
  }
}
```

**StepRecord** — one per executed step:

```json
{
  "stepId": "string",
  "node": "string",
  "startTime": "RFC3339",
  "duration_ms": 0,
  "inputs": { "paramName": "resolvedValue" },
  "request": {
    "method": "POST",
    "url": "https://...",
    "headers": { "Content-Type": "application/json" },
    "body": { }
  },
  "response": {
    "status": 200,
    "headers": { },
    "body": { }
  },
  "outputs": { "fieldName": "extractedValue" },
  "validation": {
    "passed": true,
    "results": [
      {
        "type": "status | fieldExists | fieldEquals | predicate",
        "passed": true,
        "message": "string"
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
      "strategy": "first | last | random | min | max | match | llm",
      "selectedIndex": 0
    }
  ],
  "resolutions": [
    {
      "inputName": "string",
      "source": "literal | step | pool | llm | env | expression",
      "rawValue": "any",
      "finalValue": "any",
      "fromStep": "string",
      "fromOutput": "string"
    }
  ],
  "errorClassification": {
    "category": "ClientError | ServerError | Timeout | Parsing",
    "detail": "string",
    "action": "Retry | Skip | Fail | Relax"
  },
  "expectFailure": {
    "expected": [400, 422],
    "actual": 400,
    "passed": true
  },
  "error": "string (omitted if blank)"
}
```

Sensitive headers (Authorization, X-API-Key, Cookie, etc.) are redacted to `"[REDACTED]"`. Request and response bodies are embedded JSON.

### Summary Schema (summary.json)

A lightweight file for scanning run outcomes without reading the full archive:

```json
{
  "runId": "string",
  "timestamp": "RFC3339",
  "outcome": "passed | failed | error",
  "stepCount": 5,
  "passedCount": 4,
  "failedCount": 1,
  "durationMs": 1234,
  "planName": "string"
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
    "source": "string"
  },
  "runs": [
    {
      "planName": "string",
      "runId": "string",
      "outcome": "passed | failed | error",
      "stepCount": 5,
      "passedCount": 5,
      "failedCount": 0,
      "durationMs": 1234,
      "error": "string (omitted if blank)"
    }
  ],
  "result": {
    "outcome": "passed | failed | error",
    "totalRuns": 3,
    "passedRuns": 2,
    "failedRuns": 1,
    "errorRuns": 0,
    "totalDurationMs": 3456
  }
}
```

### Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `adapter "X" not found` | Template missing or adapter name mismatch | Check `adapter` field in template matches graph node |
| `unknown node "X"` | Plan references a node not in the graph | Check node name spelling |
| `unresolved input "X"` | Required input has no value, no default, and no upstream output | Add a value in the plan or a default in the graph |
| `cycle detected` | Ordering tokens form a circular dependency | Review `requires`/`satisfies` tokens |
| `template input "X" not in node` | Template placeholder has no matching node input | Add the input to the graph node or remove the placeholder |

## Tips for AI Assistants

- **Start small**: begin with 2-3 nodes, get them working end-to-end, then expand the graph incrementally.
- **Validate early and often**: run `aat validate` after every change to catch typos before execution.
- **Use default pools**: graph input defaults with pool lists (`default: ["A", "B", "C"]`) provide varied test data without plan-level overrides.
- **Prefer recipes over full plans** when a workflow exists — recipes are shorter and easier to maintain.
- **The graph declares signatures; plans declare wiring**: nodes define what an operation accepts and produces, plans wire specific outputs to specific inputs.
- **Read archives on failure**: when a test fails, read `archive.json` — `steps[].request` and `steps[].response` show the actual HTTP exchange, `steps[].validation` shows which assertions failed, and `steps[].errorClassification` explains what went wrong.
- **Ordering is implicit**: nodes use `requires`/`satisfies` tokens, not explicit edges. If node B needs data from node A, they should share an ordering token.
- **Cleanup pairing**: if a node creates a resource, set its `cleanup` field to the deletion node. Plans and workflows pick up cleanup automatically.
- **Template placeholders must match node inputs**: every `{{name}}` in a template must correspond to an input on the linked node.
- **Secrets are never inline**: credentials always use `source: env` to reference OS environment variables.
