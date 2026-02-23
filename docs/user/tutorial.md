# Tutorial: Testing Your API

This tutorial walks you through building a complete AAT project from scratch. By the end, you'll have a graph, templates, an environment, a plan with assertions, a workflow, a recipe, and a batch run.

The example uses a fictional **Task Tracker API** — simple enough to teach every concept, rich enough to demonstrate real patterns. Substitute your own API's operations to make it real.

## What You'll Build

The Task Tracker API has five operations:

| Operation | Method | Path | Description |
|-----------|--------|------|-------------|
| createUser | POST | /users | Create a user, returns userId |
| createTask | POST | /tasks | Create a task for a user, returns taskId |
| listTasks | GET | /tasks?userId={userId} | List a user's tasks (array) |
| completeTask | PUT | /tasks/{taskId}/complete | Mark a task complete |
| deleteTask | DELETE | /tasks/{taskId} | Delete a task |

By the end of this tutorial, you'll have:

- A graph modeling all five operations with ordering and cleanup
- Templates defining the HTTP requests and response extraction
- An environment file with auth configuration
- A plan with assertions and value references
- A workflow capturing the common test pattern
- Recipes that instantiate the workflow with different data
- A batch run executing all recipes

## Step 1: Create the Project

Create a project directory and manifest:

```bash
mkdir task-tracker-tests && cd task-tracker-tests
```

```yaml
# aat-project.yaml
name: task-tracker
description: Task Tracker API integration tests
graph: graph.yaml
templates: templates/
environment: env.yaml
plans: plans/
archives: runs/
```

The manifest marks this directory as an AAT project root. When you run any `aat` command from here (or a subdirectory), AAT auto-discovers the manifest by walking up from the current directory.

> **AI shortcut**: "Create an AAT project manifest for a task tracker API with five operations: createUser, createTask, listTasks, completeTask, deleteTask"

Cross-ref: [Project Setup](project-setup.md)

## Step 2: Define the API Graph

Start with two nodes — createUser and createTask — to learn the model before expanding.

```yaml
# graph.yaml
nodes:
  createUser:
    description: "Create a new user"
    adapter: createUser
    inputs:
      - name: userName
        type: string
      - name: email
        type: string
    outputs:
      - name: userId
        type: string
        path: id
    satisfies: [user]

  createTask:
    description: "Create a task for a user"
    adapter: createTask
    inputs:
      - name: userId
        type: string
      - name: title
        type: string
      - name: priority
        type: string
        default: ["high", "medium", "low"]
    outputs:
      - name: taskId
        type: string
        path: id
      - name: status
        type: string
        path: status
    requires: [user]
    satisfies: [task]
```

Each node declares:

- **`adapter`** — links to a template file that defines the actual HTTP call
- **`inputs`** — typed parameters resolved at runtime from plan values or defaults
- **`outputs`** — named values extracted from the response using gjson paths
- **`satisfies`/`requires`** — ordering tokens that determine execution order

The `requires: [user]` on createTask means it runs after any node that `satisfies: [user]`. This is how AAT knows createUser must run before createTask — they share the `user` ordering token.

The `default: ["high", "medium", "low"]` on priority is a value pool. When no plan value is provided, AAT picks one per run, giving you varied test data automatically.

> **AI shortcut**: "Given this API spec, create a graph.yaml with ordering tokens and cleanup pairing for all operations"

Cross-ref: [API Graphs](graphs.md)

## Step 3: Write Templates

Create a `templates/` directory and add one YAML file per node. The `adapter` field links the template to its graph node.

```yaml
# templates/createUser.yaml
adapter: createUser

request:
  method: POST
  path: /users
  headers:
    Content-Type: application/json
  body: |
    {
      "name": "{{userName}}",
      "email": "{{email}}"
    }

response:
  extract:
    - name: userId
      path: id
```

```yaml
# templates/createTask.yaml
adapter: createTask

request:
  method: POST
  path: /tasks
  headers:
    Content-Type: application/json
  body: |
    {
      "userId": "{{userId}}",
      "title": "{{title}}",
      "priority": "{{priority}}"
    }

response:
  extract:
    - name: taskId
      path: id
    - name: status
      path: status
```

Key rules:

- The `adapter` field must match the graph node's `adapter` value exactly
- `{{placeholder}}` names must match input names on the linked graph node
- `response.extract` maps output names to gjson paths into the JSON response body
- Output names must match the graph node's declared outputs

> **AI shortcut**: "Write AAT templates for each node in graph.yaml based on the API spec"

Cross-ref: [Templates](templates.md)

## Step 4: Set Up the Environment

The environment file tells AAT where the API lives and how to authenticate.

```yaml
# env.yaml
environment: dev
apiBaseUrl: http://localhost:3000

auth:
  type: none
```

For a staging environment with API key auth:

```yaml
# env-staging.yaml
environment: staging
apiBaseUrl: https://api.staging.example.com

auth:
  type: apikey
  headerName: X-API-Key
  credentials:
    key:
      source: env
      var: TASK_TRACKER_API_KEY

headers:
  Accept: application/json
```

Secrets are never hardcoded in YAML. The `source: env` directive reads from OS environment variables at runtime. Set the variable before running:

```bash
export TASK_TRACKER_API_KEY="your-key-here"
```

> **AI shortcut**: "Create an env.yaml for the staging environment with API key auth"

Cross-ref: [Environments](environments.md)

## Step 5: Write Your First Plan

A plan tells AAT exactly what steps to run, what values to use, and what to check.

```yaml
# plans/first-plan.yaml
metadata:
  name: first-plan
  description: "Create a user, then create a task for that user"
intent:
  summary: "Basic user and task creation"
execution:
  steps:
    - id: user
      node: createUser
      values:
        userName: "Alice"
        email: "alice@example.com"
    - id: task
      node: createTask
      values:
        userId:
          from: user.userId
        title: "Write documentation"
        priority: "high"
      dependsOn: [user]
```

The plan wires two steps together:

- `user` calls createUser with literal values
- `task` calls createTask, with `userId` pulled from the user step's output (`from: user.userId`)
- `dependsOn: [user]` ensures the user step completes before the task step runs

### Run It

```bash
aat run plan first-plan
```

Expected output:

```
Step user (createUser)     ... PASSED (201)
Step task (createTask)     ... PASSED (201)

Plan first-plan: PASSED
```

### Iterate

If something fails, AAT tells you which step failed and why. Common issues:

- **`adapter "X" not found`** — template file missing or `adapter` name doesn't match
- **`unresolved input "X"`** — plan doesn't provide a value and the graph has no default
- **HTTP 4xx/5xx** — check the archive in `runs/` for the actual request and response

The archive contains the full HTTP request (method, URL, headers, body) and response (status, headers, body) for every step. Use `aat web` to browse archives visually.

> **AI shortcut**: "Run aat run plan first-plan and fix any errors you find"

Cross-ref: [Plans and Recipes](plans.md), [Running Tests](running.md)

## Step 6: Add Assertions

Assertions verify that API responses meet your expectations. Add them to the plan:

```yaml
# plans/first-plan.yaml (updated)
metadata:
  name: first-plan
  description: "Create a user and task with assertions"
intent:
  summary: "Basic user and task creation with verification"
execution:
  steps:
    - id: user
      node: createUser
      values:
        userName: "Alice"
        email: "alice@example.com"
      assertions:
        mechanical:
          - type: status
            expect: 201
          - type: fieldExists
            path: userId
    - id: task
      node: createTask
      values:
        userId:
          from: user.userId
        title: "Write documentation"
        priority: "high"
      dependsOn: [user]
      assertions:
        mechanical:
          - type: status
            expect: 201
          - type: fieldEquals
            path: status
            value: "pending"
```

Assertions live under `assertions.mechanical`. AAT supports four mechanical assertion types:

| Type | Fields | What it checks |
|------|--------|---------------|
| `status` | `expect` | HTTP status code matches expected value |
| `fieldExists` | `path` | JSON path exists in the response |
| `fieldEquals` | `path`, `value` | JSON path value equals expected value |
| `predicate` | `expr` | Boolean expression evaluates to true |

Run again — assertions now show pass/fail detail:

```bash
aat run plan first-plan
```

Cross-ref: [Plans and Recipes: Assertions](plans.md#assertions)

## Step 7: Expand the Graph

Add the remaining three operations to the graph: listTasks (array output), completeTask, and deleteTask.

```yaml
# graph.yaml (complete)
nodes:
  createUser:
    description: "Create a new user"
    adapter: createUser
    inputs:
      - name: userName
        type: string
        default: ["Alice", "Bob", "Carol", "Dave"]
      - name: email
        type: string
        default: ["alice@test.com", "bob@test.com", "carol@test.com"]
    outputs:
      - name: userId
        type: string
        path: id
    satisfies: [user]

  createTask:
    description: "Create a task for a user"
    adapter: createTask
    inputs:
      - name: userId
        type: string
      - name: title
        type: string
      - name: priority
        type: string
        default: ["high", "medium", "low"]
    outputs:
      - name: taskId
        type: string
        path: id
      - name: status
        type: string
        path: status
    requires: [user]
    satisfies: [task]
    cleanup: deleteTask

  listTasks:
    description: "List tasks for a user"
    adapter: listTasks
    inputs:
      - name: userId
        type: string
    outputs:
      - name: tasks
        type: task[]
        path: tasks
        elementFields:
          - name: taskId
            type: string
          - name: title
            type: string
          - name: status
            type: string
    requires: [task]

  completeTask:
    description: "Mark a task as complete"
    adapter: completeTask
    inputs:
      - name: taskId
        type: string
    outputs:
      - name: status
        type: string
        path: status
    requires: [task]

  deleteTask:
    description: "Delete a task"
    adapter: deleteTask
    inputs:
      - name: taskId
        type: string
    requires: [task]
```

Key additions:

- **Default value pools** on createUser inputs — varied test data per run
- **`cleanup: deleteTask`** on createTask — pairs creation with deletion
- **`elementFields`** on listTasks — declares the structure of each array element, enabling selection strategies in plans
- **Ordering tokens** chain the operations: `user` -> `task`

Now write templates for the new nodes:

```yaml
# templates/listTasks.yaml
adapter: listTasks

request:
  method: GET
  path: /tasks?userId={{userId}}

response:
  extract:
    - name: tasks
      type: array
      path: tasks
      fields:
        - name: taskId
          path: id
        - name: title
          path: title
        - name: status
          path: status
```

```yaml
# templates/completeTask.yaml
adapter: completeTask

request:
  method: PUT
  path: /tasks/{{taskId}}/complete
  headers:
    Content-Type: application/json

response:
  extract:
    - name: status
      path: status
```

```yaml
# templates/deleteTask.yaml
adapter: deleteTask

request:
  method: DELETE
  path: /tasks/{{taskId}}
```

### Validate

Run validation to check everything fits together:

```bash
aat validate
```

Expected output:

```
Manifest:              OK (project: task-tracker)
Graph structure:       OK (5 nodes)
Adapter outputs:       OK (5 templates)
Template inputs:       OK

Project validation: PASSED
```

If something is wrong, validation tells you exactly what:

```
Template inputs:       FAILED
  - createTask: template placeholder "userId" has no matching node input "userID" (case mismatch)
```

> **AI shortcut**: "Add listTasks, completeTask, and deleteTask nodes to graph.yaml and create their templates. Then run aat validate to check."

Cross-ref: [Validation](validation.md)

## Step 8: Create a Workflow

A workflow captures a common test pattern so you can reuse it with different data. Declare it in the graph and write a template file.

Add to the bottom of `graph.yaml`:

```yaml
# graph.yaml (add after nodes:)
workflows:
  - name: Task Lifecycle
    description: "Create a user, create a task, complete it, verify via list"
    template: workflows/task-lifecycle.yaml
```

Create the workflow template:

```yaml
# workflows/task-lifecycle.yaml
execution:
  steps:
    - id: user
      node: createUser
      assertions:
        mechanical:
          - type: status
            expect: 201
    - id: task
      node: createTask
      values:
        userId:
          from: user.userId
      dependsOn: [user]
      assertions:
        mechanical:
          - type: status
            expect: 201
    - id: complete
      node: completeTask
      values:
        taskId:
          from: task.taskId
      dependsOn: [task]
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldEquals
            path: status
            value: "completed"
    - id: list
      node: listTasks
      values:
        userId:
          from: user.userId
      dependsOn: [complete]
      assertions:
        mechanical:
          - type: status
            expect: 200
  cleanup:
    - node: deleteTask
      values:
        taskId:
          from: task.taskId
      runOn: always
```

The workflow template looks like a plan's execution block — steps with values, references, assertions, and cleanup. The key difference: workflows omit values that graph defaults handle (like `userName` and `priority`), letting the engine fill them automatically.

> **AI shortcut**: "Create a 'Task Lifecycle' workflow that creates a user, creates a task, completes it, verifies via list, and cleans up"

Cross-ref: [Workflows](workflows.md)

## Step 9: Write a Recipe

A recipe instantiates a workflow with specific overrides. Compare the size:

```yaml
# plans/lifecycle-recipe.yaml
kind: recipe
metadata:
  created: 2026-02-23T10:00:00Z
selection:
  workflow: Task Lifecycle
  description: "Task lifecycle with user Bob"
overrides:
  values:
    user.userName: "Bob"
    user.email: "bob@example.com"
    task.title: "Write documentation"
    task.priority: "high"
```

That's it — 12 lines vs the ~40-line full plan. The recipe names the workflow and provides only the values that differ from defaults. Everything else (step wiring, assertions, cleanup) comes from the workflow template.

Run it the same way as a full plan:

```bash
aat run plan lifecycle-recipe
```

Expected output:

```
Step user (createUser)         ... PASSED (201)
Step task (createTask)         ... PASSED (201)
Step complete (completeTask)   ... PASSED (200)
Step list (listTasks)          ... PASSED (200)
Cleanup deleteTask             ... OK (200)

Plan lifecycle-recipe: PASSED
```

> **AI shortcut**: "Write a recipe for the Task Lifecycle workflow with user name 'Bob' and task title 'Write documentation'"

Cross-ref: [Plans: Recipes](plans.md#recipes)

## Step 10: Batch Runs

Create a few recipe variations to test different scenarios:

```yaml
# plans/lifecycle-alice.yaml
kind: recipe
selection:
  workflow: Task Lifecycle
  description: "Task lifecycle with user Alice, low priority"
overrides:
  values:
    user.userName: "Alice"
    user.email: "alice@example.com"
    task.title: "Review pull request"
    task.priority: "low"
```

```yaml
# plans/lifecycle-carol.yaml
kind: recipe
selection:
  workflow: Task Lifecycle
  description: "Task lifecycle with user Carol, urgent task"
overrides:
  values:
    user.userName: "Carol"
    user.email: "carol@example.com"
    task.title: "Fix production bug"
    task.priority: "high"
```

Run all plans in the `plans/` directory as a batch:

```bash
aat run batch
```

Expected output:

```
Running 3 plans...

lifecycle-recipe     ... PASSED
lifecycle-alice      ... PASSED
lifecycle-carol      ... PASSED

Batch: 3/3 PASSED
```

You can also run a subset by passing a subdirectory:

```bash
aat run batch lifecycle/
```

> **AI shortcut**: "Create three recipe variations with different users and task priorities, then run them as a batch"

Cross-ref: [Running Tests](running.md)

## Step 11: Validate Everything

Run full project validation to check all artifacts together:

```bash
aat validate
```

```
Manifest:              OK (project: task-tracker)
Graph structure:       OK (5 nodes)
Adapter outputs:       OK (5 templates)
Template inputs:       OK
Workflow compatibility: OK (1 workflow)
Plans:                 OK (3 files, 3 recipes)

Project validation: PASSED
```

Validation catches structural issues without making any HTTP calls:

- **Graph**: ordering cycles, missing cleanup nodes, duplicate names
- **Templates**: adapter mismatches, placeholder/input misalignment, missing extraction
- **Workflows**: undefined slot options, template/graph inconsistencies
- **Plans**: unknown nodes, unresolvable references, invalid assertions

When validation fails, it tells you exactly what's wrong and where:

```
Plans:                 FAILED
  - lifecycle-recipe.yaml: unknown workflow "Task Lifecycl" (did you mean "Task Lifecycle"?)
```

Fix the typo, run `aat validate` again, and confirm it passes before executing.

Cross-ref: [Validation](validation.md)

## Recap

Here's what you built and the concepts you learned:

| Concept | What you learned | Reference |
|---------|-----------------|-----------|
| Project manifest | `aat-project.yaml` marks the root and declares artifact paths | [Project Setup](project-setup.md) |
| Graph | Nodes with typed inputs, outputs, and ordering tokens | [API Graphs](graphs.md) |
| Templates | HTTP request/response YAML with placeholders and extraction | [Templates](templates.md) |
| Environment | Base URL, auth, secrets via environment variables | [Environments](environments.md) |
| Plans | Explicit steps with values, references, and assertions | [Plans and Recipes](plans.md) |
| Workflows | Reusable patterns declared in the graph | [Workflows](workflows.md) |
| Recipes | Compact plans that instantiate a workflow with overrides | [Plans: Recipes](plans.md#recipes) |
| Batch runs | Execute all plans in a directory | [Running Tests](running.md) |
| Validation | Structural checks across the entire project | [Validation](validation.md) |

## Next Steps

| Topic | Link |
|-------|------|
| How values flow from defaults to selections | [Value Resolution](value-flow.md) |
| Business concepts and value pools | [Domain Knowledge](domain.md) |
| LLM-assisted plan generation | [LLM-Assisted Planning](prompt.md) |
| CI/CD pipeline integration | [CI/CD Integration](ci-cd.md) |
| Visual archive browser | [Web UI and Archives](web-ui.md) |
| IDE AI integration | [MCP Server](mcp-server.md) |
| AI-facing schema reference | [AI Assistant Primer](llms.md) |
