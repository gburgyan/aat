# Getting Started

This guide takes you from zero to running AAT against your own API. It covers installation, project setup, and your first test run.

## 1. Install AAT

**Prerequisites:** [Go 1.24+](https://go.dev/doc/install)

Clone and build:

```bash
git clone https://github.com/gburgyan/aat.git
cd aat
go build -o aat ./cmd/aat/
```

Move the binary somewhere on your `PATH`, or run it directly with `./aat`.

Verify the install:

```bash
aat --help
```

> Pre-built binaries and `go install` support are planned for future releases.

## 2. See It Work (Optional)

Before investing in your own setup, run the Petstore example to see AAT in action. This hits the public Swagger Petstore API — no keys needed:

```bash
aat run \
  --plan examples/petstore/plan.yaml \
  --env examples/petstore/env.yaml \
  --graph examples/petstore/graph.yaml \
  --templates examples/petstore/templates/
```

You should see each step execute and pass. See the [Petstore tutorial](../../examples/petstore/README.md) for a full walkthrough.

## 3. Project Structure

AAT projects use four file types. Each has a distinct role:

| File | Role |
|------|------|
| **Graph** (`graph.yaml`) | Map of your API: operations (nodes), their inputs/outputs, and data-flow edges between them |
| **Templates** (`templates/*.yaml`) | HTTP request/response details for each operation: method, path, headers, body, extraction rules |
| **Environment** (`env.yaml`) | Connection config: base URL, authentication, retry settings |
| **Plan** (`workflows/*.yaml`) | Test scenario: which steps to run, input values, and assertions |

A typical project looks like:

```
my-api-tests/
  aat-project.yaml          # project manifest (optional but recommended)
  graph.yaml
  templates/
    createUser.yaml
    getUser.yaml
    deleteUser.yaml
  env.yaml
  workflows/
    smoke-test.yaml
    full-workflow.yaml
```

The graph and templates describe your API's structure. The environment tells AAT how to connect. Plans describe what to test. You write each once (except plans — you'll write many for different scenarios).

The optional `aat-project.yaml` manifest lets AAT auto-discover your project files, so you don't need `--env`, `--graph`, and `--templates` on every command. See [Running Tests: Project Discovery](running.md#project-discovery) for setup.

## 4. Define Your API Graph

This is the core modeling step. You have two paths depending on whether you have an OpenAPI spec.

### Path A: From an OpenAPI Spec

If you have an OpenAPI 3.x spec, scaffold a starting point:

```bash
aat generate --oas my-spec.yaml
```

This creates:
- `graph.yaml` — one node per operation, with inputs from parameters and request body, outputs from the response schema
- `templates/` — one template per operation, with HTTP method, path, headers, and body placeholders

The scaffold gives you correct HTTP mechanics but **does not generate edges** — it can't know which operations connect in your workflow. You need to add those manually.

**Before (generated — no edges):**

```yaml
version: "1.0.0"

nodes:
  createUser:
    adapter: createUser
    oas:
      operationId: createUser
    inputs:
      - name: name
        type: string
      - name: email
        type: string
    outputs:
      - name: userId
        type: string
      - name: userName
        type: string

  getUser:
    adapter: getUser
    oas:
      operationId: getUser
    inputs:
      - name: userId
        type: string
    outputs:
      - name: userId
        type: string
      - name: userName
        type: string
      - name: userEmail
        type: string
```

**After (edges added, descriptions filled in):**

```yaml
version: "1.0.0"

nodes:
  createUser:
    description: "Register a new user"
    adapter: createUser
    oas:
      operationId: createUser
    inputs:
      - name: name
        type: string
      - name: email
        type: string
    outputs:
      - name: userId
        type: string
      - name: userName
        type: string

  getUser:
    description: "Fetch a user by ID"
    adapter: getUser
    oas:
      operationId: getUser
    inputs:
      - name: userId
        type: string
    outputs:
      - name: userId
        type: string
      - name: userName
        type: string
      - name: userEmail
        type: string

edges:
  - from: createUser.userId
    to: getUser.userId
```

The edge `createUser.userId → getUser.userId` tells AAT that when a plan runs `getUser` after `createUser`, the user ID flows automatically — no need to wire it in the plan.

See the [Graph Authoring Guide](graph-authoring.md) for the full refinement workflow: removing unused nodes, improving types, trimming outputs, configuring array selections, and validation.

### Path B: Without a Spec (Manual Authoring)

No OpenAPI spec? No problem. You can write the graph and templates directly. This works for any HTTP API regardless of what language or framework it's built with. It's also the right choice for APIs that don't fit cleanly into OAS — gRPC-web, GraphQL over HTTP, custom protocols, etc.

Start with just the operations you want to test. You don't need to model your entire API.

**graph.yaml:**

```yaml
version: "1.0.0"

nodes:
  createUser:
    description: "Register a new user"
    adapter: createUser
    inputs:
      - name: name
        type: string
        description: "User's display name"
      - name: email
        type: string
        description: "User's email address"
    outputs:
      - name: userId
        type: string
        description: "Assigned user ID"
      - name: userName
        type: string

  getUser:
    description: "Fetch a user by ID"
    adapter: getUser
    inputs:
      - name: userId
        type: string
        description: "User ID to look up"
    outputs:
      - name: userId
        type: string
      - name: userName
        type: string
      - name: userEmail
        type: string

edges:
  - from: createUser.userId
    to: getUser.userId
```

Each **node** represents an API operation. Inputs are what the operation needs (path params, query params, body fields). Outputs are what the node produces — how they're extracted from the response is defined in the template's `extract` section.

Each **edge** connects an output from one node to an input on another.

**templates/createUser.yaml:**

```yaml
adapter: createUser
protocol: http

request:
  method: POST
  path: /users
  headers:
    Content-Type: application/json
    Accept: application/json
  body: |
    {
      "name": "{{name}}",
      "email": "{{email}}"
    }

response:
  extract:
    userId: "id"
    userName: "name"
```

**templates/getUser.yaml:**

```yaml
adapter: getUser
protocol: http

request:
  method: GET
  path: /users/{{userId}}
  headers:
    Accept: application/json

response:
  extract:
    userId: "id"
    userName: "name"
    userEmail: "email"
```

Templates define the HTTP details. The `adapter` field links a template to its graph node. Placeholders like `{{name}}` are replaced at runtime with values from the plan or upstream steps. The `extract` section maps output names to [gjson](https://github.com/tidwall/gjson) paths in the response body — this is where you define how JSON response fields map to the graph's output names.

For the full template format (including array extraction with `fields`), see [Templates](templates.md). For the complete graph YAML reference, see [Graph Authoring](graph-authoring.md#graph-yaml-reference).

## 5. Create an Environment File

The environment file tells AAT where your API lives and how to authenticate.

**Minimal (no auth):**

```yaml
environment: dev
apiBaseUrl: https://api.example.com

auth:
  type: none
```

**Bearer token from environment variable:**

```yaml
environment: staging
apiBaseUrl: https://staging.example.com

auth:
  type: bearer
  credentials:
    token:
      source: env
      var: API_TOKEN
```

**API key:**

```yaml
environment: production
apiBaseUrl: https://api.example.com

auth:
  type: apikey
  headerName: X-Api-Key
  credentials:
    key:
      source: env
      var: API_KEY
```

**OAuth2 client credentials:**

```yaml
environment: production
apiBaseUrl: https://api.example.com

auth:
  type: oauth2
  tokenUrl: https://auth.example.com/oauth/token
  credentials:
    clientId:
      source: env
      var: CLIENT_ID
    clientSecret:
      source: env
      var: CLIENT_SECRET
```

For the full environment reference including LLM configuration and retry settings, see [Environments](environments.md).

## 6. Write Your First Plan

A plan defines a test scenario: which steps to execute, what values to use, and what to assert.

**workflows/create-and-verify.yaml:**

```yaml
intent:
  description: "Create a user and verify it was stored correctly"

execution:
  steps:
    - node: createUser
      description: "Create a new user"
      values:
        name: "Alice"
        email: "alice@example.com"
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldExists
            path: "id"

    - node: getUser
      dependsOn: [createUser]
      isGoal: true
      description: "Verify the user exists with correct data"
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldEquals
            path: "name"
            expect: "Alice"
          - type: fieldEquals
            path: "email"
            expect: "alice@example.com"
```

Key concepts:
- **`node`** — which graph node (API operation) to call
- **`dependsOn`** — which steps must complete first (AAT also infers this from graph edges)
- **`values`** — input values for this step; values that flow from upstream edges are resolved automatically
- **`assertions`** — checks to run on the response (status codes, field existence, field equality)
- **`isGoal`** — marks the primary step this test is verifying

Notice that `getUser` doesn't specify a `userId` value. AAT resolves it automatically via the edge `createUser.userId → getUser.userId`.

### Alternative: Generate Plans with an LLM

If you have an LLM configured in your environment, you can generate plans from natural language:

```bash
aat prompt "create a user named Alice and verify it exists"
```

If you don't have an `aat-project.yaml` yet, pass paths explicitly:

```bash
aat prompt "create a user named Alice and verify it exists" \
  --env env.yaml \
  --graph graph.yaml \
  --templates templates/
```

AAT shows the generated plan for review before executing. Use `--save workflows/my-test.yaml` to save it for reuse. See [LLM-Assisted Planning](prompt-workflow.md) for details.

## 7. Run It

With an `aat-project.yaml` manifest:

```bash
aat run --plan workflows/create-and-verify.yaml
```

Or with explicit paths:

```bash
aat run \
  --plan workflows/create-and-verify.yaml \
  --env env.yaml \
  --graph graph.yaml \
  --templates templates/
```

**What success looks like:**

```
Step 1/2: createUser ... PASSED
Step 2/2: getUser ... PASSED

Result: PASSED (2/2 steps passed)
Archive: _output/runs/run-20260211-143022-a1b2c3d4/archive.json
```

**What failure looks like:**

```
Step 1/2: createUser ... PASSED
Step 2/2: getUser ... FAILED
  Assertion failed: fieldEquals path="email" expected="alice@example.com" actual="alice@Example.com"

Result: FAILED (1/2 steps passed)
Archive: _output/runs/run-20260211-143022-a1b2c3d4/archive.json
```

**When something goes wrong, check:**

1. **The archive** — every request and response is recorded in the archive file. Open it to see exact HTTP traffic.
2. **Template paths** — verify `{{placeholder}}` names match graph input names
3. **Extract paths** — verify gjson paths in `response.extract` match the actual API response structure
4. **Environment** — confirm `apiBaseUrl` is correct and reachable

The `--output` flag controls where archives are written (default: `_output/runs/`).

## 8. Using AI to Help (Claude Code / Agentic AI)

AAT includes an MCP server that gives AI tools structured access to your project. This is useful for generating plans, exploring the API graph, debugging failures, and authoring documentation.

**Step 1: Create a project manifest**

If you followed Step 3, you may already have `aat-project.yaml` in your project root. If not, create one:

```yaml
name: my-api
description: "My API test project"
graph: graph.yaml
templates: templates/
environment: env.yaml
workflows: workflows/
archives: _output/runs
traces: _output/traces
```

All paths are relative to the manifest file. This same file also enables CLI project discovery (see [Running Tests: Project Discovery](running.md#project-discovery)).

**Step 2: Configure your IDE**

For Claude Code, add to `.mcp.json` in your project root:

```json
{
  "mcpServers": {
    "aat": {
      "command": "aat",
      "args": ["mcp", "serve"]
    }
  }
}
```

**Step 3: Use it**

With the MCP server running, the AI can:
- Browse your graph nodes and edges
- Inspect templates and domain knowledge
- Generate test plans using `test_workflow`
- Explain how workflows connect using `explain_workflow`
- Help debug failing tests using `debug_failing_test`
- Suggest edges and graph refinements

Note: `aat generate` must be run from the CLI first. The MCP server works with an existing project — it doesn't scaffold new ones.

See [MCP Server](mcp-server.md) for the full tool and resource reference.

## 9. Next Steps

Now that you have a working project, explore based on what you need:

| Goal | Guide |
|------|-------|
| Refine my graph (types, arrays, cleanup nodes) | [Graph Authoring](graph-authoring.md) |
| Understand how values flow between steps | [Value Flow](value-flow.md) |
| Write more sophisticated plans | [Plan Authoring](plan-authoring.md) |
| Create reusable workflow templates | [Workflow Templates](workflow-templates.md) |
| Set up CI/CD for automated testing | [Running Tests](running.md) |
| Use LLM-assisted plan generation | [LLM-Assisted Planning](prompt-workflow.md) |
| Add domain knowledge for smarter value resolution | [Domain Knowledge](domain-knowledge.md) |
| Generate documentation from your graph | `aat docs generate --graph graph.yaml` |
