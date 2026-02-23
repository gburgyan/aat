# Validation

AAT validates projects at multiple levels — graph structure, OpenAPI alignment, template consistency, plan correctness, and workflow compatibility. You can validate the entire project with a single command or focus on a specific scope with subcommands.

## Full Project Validation

```
aat validate
```

Runs all checks against the project manifest. AAT auto-discovers the manifest via the standard [resolution chain](project-setup.md#auto-discovery), or you can specify one explicitly with `--manifest`.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |
| `--strict` | bool | `false` | Treat warnings as errors |

### Example Output

When everything passes:

```
Manifest:              OK (project: ecommerce)
Graph structure:       OK (12 nodes)
OAS validation:        OK
Adapter outputs:       OK (10 templates)
Template inputs:       OK
Workflow compatibility: OK (3 workflows)
Workflows:             OK (4 files, 2 templates)
Plans:                 OK (6 files, 4 recipes)

Project validation: PASSED
```

When a section fails:

```
Manifest:              OK (project: ecommerce)
Graph structure:       OK (12 nodes)
OAS validation:        FAILED
  loading spec "openapi.yaml": file not found
Adapter outputs:       OK (10 templates)
Template inputs:       OK
Workflow compatibility: OK (3 workflows)
Workflows:             OK (4 files, 2 templates)
Plans:                 FAILED
  smoke-test.yaml: step 2 (createOrder): required input "productId" has no plan value

Project validation: FAILED (2 section(s) with errors)
```

### What It Checks

| Section | What It Validates |
|---------|-------------------|
| Manifest | Manifest discovery, all referenced files and directories exist |
| Graph structure | YAML parsing, node uniqueness, input/output types, required fields |
| OAS validation | OpenAPI spec loading, operationId alignment, method/path consistency |
| Adapter outputs | Template extraction paths match graph output declarations |
| Template inputs | Required template placeholders vs optional graph inputs |
| Workflow compatibility | Inline workflow definitions: kinds, slot references, addon targets |
| Workflows | Workflow directory files parse correctly and validate against graph |
| Plans | Plan directory files parse correctly and validate against graph; recipes reconstitute |

Sections that depend on optional artifacts (OAS specs, workflows directory, plans directory) are skipped when those artifacts are not configured.

## Graph Validation

```
aat validate graph
```

Validates graph structure, OAS alignment, and template consistency in isolation. Use this when you are iterating on a graph file without a full project manifest.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--graph` | path | from manifest | API graph file (required) |
| `--oas` | path | from graph | Override OpenAPI spec path |
| `--templates` | path | — | Templates directory (enables adapter/template checks) |
| `--strict` | bool | `false` | Treat warnings as errors |

### Structural Checks

Graph structural validation catches:

- Missing or invalid version
- Duplicate node names
- Inputs or outputs missing name or type fields
- Invalid types (malformed array syntax, unknown base types)
- Constraint violations (min > max, minLength > maxLength)
- Invalid gjson extraction paths on outputs and elementFields
- Missing adapter references
- Cleanup references to unknown or self-referencing nodes
- Error detection rules with missing paths or unknown rule types
- Condition references to unknown nodes
- Requires/satisfies token mismatches and cycles

### OAS Alignment

When an OpenAPI spec is configured (at the graph level or per-node), AAT checks consistency between the graph and the spec:

- Every `operationId` in the graph exists in the spec
- HTTP method matches
- Input types are compatible with schema parameter/body types
- Output extraction paths are valid for the response schema

The `--oas` flag overrides the graph-level spec path. In `--strict` mode, OAS warnings (like missing optional parameters) become errors.

Spec paths are resolved relative to the graph file's directory.

### Template Checks

When `--templates` is provided, AAT validates that template adapter files are consistent with the graph:

- Template extraction rules produce outputs declared in the graph
- Required template placeholders correspond to graph inputs
- Placeholder types are compatible with input types

## Plan Validation

```
aat validate plan
```

Validates plan correctness against the graph. Operates in two modes depending on whether `--plan` is provided.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--graph` | path | from manifest | API graph file (required) |
| `--plan` | path | — | Specific plan file to validate |
| `--unfed` | bool | `false` | Show inputs with no plan value and no default |

### Single Plan Mode

With `--plan`, validates one plan or recipe.

```
$ aat validate plan --plan plans/smoke-test.yaml
Plan validation: OK (3 steps)
```

For recipes, AAT reconstitutes the full plan from the workflow before validating. This catches issues in both the recipe overrides and the underlying workflow template.

### All Templates Mode

Without `--plan`, validates every workflow template referenced by the graph. Addon workflows, slot options, and base templates with slot markers are skipped — they are intentionally incomplete until composed.

```
$ aat validate plan
Validating 4 workflow templates...

  order-lifecycle.yaml                     OK (5 steps)
  return-flow.yaml                         OK (4 steps)
  inventory-check.yaml                     FAIL
    step 2 (updateStock): required input "warehouseId" has no plan value

Workflow template validation: FAILED
```

### What It Checks

Plan validation catches:

- **Node references** — every step references a node that exists in the graph
- **Value references** — `from` references point to valid steps and outputs
- **Dependency completeness** — `from` references have matching `dependsOn` entries
- **Dependency cycles** — no circular `dependsOn` chains
- **Duplicate step IDs** — step names are unique within the plan
- **Required inputs** — non-optional inputs have a plan value, reference, or default
- **Selection configs** — valid strategy, source exists, field references match elementFields
- **Constraints** — predicate expressions parse correctly, `appliesTo` references valid steps
- **Assertions** — predicate syntax is valid, assertion types are recognized
- **Expect-failure** — status codes are >= 400, no contradicting success assertions
- **Cleanup steps** — nodes exist, `runOn` is `always`, `failure`, or `success`
- **Graph version** — plan's `graphVersion` is compatible (same major version) with graph

### Unfed Inputs (`--unfed`)

The `--unfed` flag lists inputs that have no plan value and no graph default. This is useful for checking workflow template completeness — unfed inputs are the values that must be supplied by a recipe, the LLM pipeline, or a runtime value pool.

```
$ aat validate plan --plan workflows/order-lifecycle.yaml --unfed
Plan validation: OK (5 steps)
    Unfed inputs:
      - listProducts.category
      - confirmOrder.shippingCity
```

## Workflow Validation

```
aat validate workflow
```

Validates inline workflow definitions in the graph and workflow template files in the workflows directory.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--graph` | path | from manifest | API graph file (required) |
| `--strict` | bool | `false` | Treat warnings as errors |

Checks include:

- Workflow kind is valid (`addon` or `slot`)
- `after` field only used on addon workflows and references an existing node
- Slot definitions have names, options, and valid references
- Slot defaults are in the options list
- Workflow template files parse correctly

```
$ aat validate workflow
Workflow compatibility: OK (3 workflows)
Workflows:             OK (4 files, 2 templates)
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Validation passed (no errors; warnings allowed unless `--strict`) |
| `1` | Validation failed (errors found, or warnings with `--strict`) |

## Common Errors

| Error Pattern | Meaning | Fix |
|---------------|---------|-----|
| `node "X" not found in graph` | Step references a node that doesn't exist | Check node name spelling in your plan; run `aat validate graph` to see available nodes |
| `required input "X" has no plan value` | A non-optional input is missing from the step's values | Add a value, `from` reference, or make the input optional in the graph |
| `'from' reference "X" for "Y": "Z" is not a step` | A value's `from` references a step name that doesn't exist in the plan | Check the step ID spelling; `from` uses step names, not node names |
| `has 'from' reference to "X" but does not list it in dependsOn` | A data dependency is missing from `dependsOn` | Add the referenced step to `dependsOn` to ensure execution order |
| `dependsOn cycle detected` | Steps have circular dependencies | Remove the circular reference; draw out the dependency chain to find the loop |
| `unknown selection strategy "X"` | Invalid strategy in a selection config | Use one of: `first`, `last`, `index`, `random`, `min`, `max`, `match`, `llm` |
| `sortField "X" not found in elementFields` | Selection sort field doesn't match any elementField | Check the array output's elementFields in the graph; use a field name, not a path |
| `output "X" is not an array type` | Selection source isn't an array | Selections require array outputs; check the source step's output type |
| `plan graphVersion incompatible with graph version` | Major version mismatch between plan and graph | Update the plan's `graphVersion` or regenerate the plan |
| `expectFailure status N must be >= 400` | Expect-failure has a success status code | Expect-failure is for negative tests; use status codes 400+ |

## Validation in CI/CD

Run validation before execution to catch configuration errors early:

```
aat validate && aat run batch --json
```

If validation fails (exit code 1), the batch command never runs. This is the recommended pattern for CI/CD pipelines. See [CI/CD Integration](ci-cd.md) for full pipeline examples.

---

*Source: `cmd/aat/validate_cmd.go`, `cmd/aat/validate_graph_cmd.go`, `cmd/aat/validate_plan_cmd.go`, `cmd/aat/validate_workflow_cmd.go`, `graph/validate.go`, `plan/validate.go`.*
