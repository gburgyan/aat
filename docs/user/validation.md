# Validation

AAT validates projects at multiple levels — graph structure, OpenAPI alignment, template consistency, plan correctness, and workflow compatibility. You can validate the entire project with a single command or focus on a specific scope with subcommands.

## Full Project Validation

```
aat validate
```

Runs all checks against the project manifest. AAT auto-discovers the manifest via the standard [resolution chain](project-setup.md#resolution-priority), or you can specify one explicitly with `--manifest`.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |
| `--strict` | bool | `false` | Treat warnings as errors |
| `--var` | `KEY=VALUE` | — | Set a var of a multi-environment file while validating every environment (repeatable) |

### Example Output

When everything passes (the `examples/shop` project):

```
Manifest:               OK (project: shop)
Environment:            OK (2 environments: eu, us)
Domain:                 OK (3 concepts, 3 types, 6 value pools)
Visualizers:            OK (1 visualizer)
Graph structure:        OK (17 nodes)
OAS validation:         OK
Adapter outputs:        OK (17 templates)
Template inputs:        OK
Workflow compatibility: OK (11 workflows)
Workflows:              OK (10 files, 10 templates)
Layers:                 OK (12 layers)
Plans:                  OK (7 files, 3 recipes)

Project validation: PASSED
```

A section with warnings but no errors shows `WARN` and lists them. Warnings do not fail validation unless you pass `--strict`:

```
OAS validation:         WARN
  Warnings:
    - node "paymentCharge": output "paymentStatus" not found in OAS 2xx response schema for "paymentCharge"
...
Project validation: PASSED with warnings in 1 section (--strict fails on them)
```

When sections fail, each error names its file (relative to the working directory) and, for YAML problems, the line:

```
Manifest:               OK (project: shop)
Environment:            OK (2 environments: eu, us)
Domain:                 FAILED
  domain.yaml: line 44: unknown key "valuePool" in knowledge base (did you mean "valuePools"?)
Visualizers:            OK (1 visualizer)
Graph structure:        OK (17 nodes)
OAS validation:         OK
Adapter outputs:        OK (17 templates)
Template inputs:        OK
Workflow compatibility: OK (11 workflows)
Workflows:              OK (10 files, 10 templates)
Layers:                 OK (12 layers)
Plans:                  FAILED
  plans/full-lifecycle.yaml: line 17: unknown key "fromSelecton" in step value (did you mean "fromSelection"?)

Project validation: FAILED (2 section(s) with errors)
```

### Unknown Keys

Every project file — manifest, environment files and their includes, overlays, graph, templates, domain, visualizers, workflows, layers, plans, and recipes — is decoded strictly: a key that no field accepts is an error, not silently ignored. The message names the line, the key, where it appeared, and either the likely intended key or the keys that are valid there. The same rule applies to plan YAML given to the MCP server's plan tools. A misspelled `fromSelection` or an indented-too-far `optional` would otherwise produce a plan that loads and quietly does something else.

A manifest that exists but fails to load is an error for every command that discovers it, rather than being skipped in favor of a lower-priority project.

### What It Checks

| Section | What It Validates |
|---------|-------------------|
| Manifest | Manifest discovery, all referenced files and directories exist |
| Environment | Every non-abstract environment loads: `extends` chains, `${var}` substitution, auth and override rules |
| Domain | The domain file parses and its concepts, types, and value pools are well formed |
| Visualizers | `visualizers.yaml` parses and each visualizer's HTML file exists |
| Graph structure | YAML parsing, node uniqueness, input/output types, required fields |
| OAS validation | OpenAPI spec loading, operationId alignment, inputs and required parameters, outputs present in the 2xx response schema at their template extract paths (nested objects and array items included) |
| Adapter outputs | Template extraction paths match graph output declarations |
| Template inputs | Required template placeholders vs optional graph inputs |
| Workflow compatibility | Addon `AUTOWIRE` inputs are produced in every base the addon attaches to; a slot counts when all of its options produce the input, because slots are filled before addons are spliced. A plain `AUTOWIRE` in a base or slot option must be produced by the base or its slots; the warning names any addon that produces the output and suggests `AUTOWIRE?` for an optional input. `AUTOWIRE?` never warns as unfed, but it does warn on a required input with no graph default |
| Workflows | Workflow directory files, subdirectories included, parse correctly and validate against graph |
| Layers | Layer files parse, names are unique, and every input key matches a node input in the graph |
| Plans | Plan directory files parse correctly and validate against graph; recipes reconstitute |

Sections that depend on optional artifacts (environment file, domain, visualizers, OAS specs, workflows, layers, plans) are skipped when those artifacts are not configured.

## Graph Validation

```
aat validate graph
```

Validates graph structure, OAS alignment, and template consistency in isolation. Use this when you are iterating on a graph file without a full project manifest.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--graph` | path | from manifest | API graph file (required) |
| `--oas` | path | from graph | Override OpenAPI spec path |
| `--templates` | path | from manifest | Templates directory (enables adapter/template checks) |
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
- Cleanup references to unknown or self-referencing nodes, and cleanup pairings that loop back (`a → b → a`)
- Error detection rules with missing paths or unknown rule types
- Condition references to unknown nodes
- Requires/satisfies token mismatches and cycles

### OAS Alignment

When an OpenAPI spec is configured (at the graph level or per-node), AAT checks consistency between the graph and the spec:

- Every `operationId` in the graph exists in the node's spec (an error)
- Every graph input is a parameter or a request body property of the operation (a warning)
- Every required parameter and body property is a graph input or written by the node's template: a query parameter in the path, a header, a JSON body key, or a form body key, where a bracketed key such as `metadata[source]` counts as `metadata` (a warning)
- Every output exists in the 2xx response schema at its template extract path (a warning)

The checks compare names, not types. Specs with circular references load.

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
- **Unresolved AUTOWIRE** — no input still holds an `AUTOWIRE` marker (composition leaves one only when no step produces the output)
- **Duplicate step IDs** — step names are unique within the plan
- **Required inputs** — non-optional inputs have a plan value, reference, or default
- **Selection configs** — valid strategy, source exists, field references match elementFields
- **Constraints** — predicate expressions parse correctly, `appliesTo` references valid steps
- **Assertions** — predicate syntax is valid. Assertion types are not checked here: an unknown type fails when a full plan's step runs, and composing a recipe drops it
- **Expect-failure** — status codes are >= 400, no contradicting success assertions
- **Cleanup steps** — nodes exist, `runOn` is `always`, `failure`, or `success`
- **Graph version** — plan's `graphVersion` is compatible (same major version) with graph

### Unfed Inputs (`--unfed`)

The `--unfed` flag lists inputs that have no plan value and no graph default. This is useful for checking workflow template completeness — unfed inputs are the values a recipe, an AI assistant, or `aat prompt` must supply.

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
| `1` | Validation failed (errors found, including a manifest that fails to load, or warnings with `--strict`) |
| `2` | Validation could not run: an unknown flag or argument, a bad `--var`, no manifest found, or no `--graph` for `aat validate graph`, `plan`, or `workflow` |

## Common Errors

| Error Pattern | Meaning | Fix |
|---------------|---------|-----|
| `line N: unknown key "X" in Y (did you mean "Z"?)` | A key that no field of `Y` accepts — a typo, wrong indentation, or a key that was removed | Use the suggested key, or one of the listed valid keys; see [Unknown Keys](#unknown-keys) |
| `line N: A must be B, found C` | A value has the wrong shape, such as a list where a step value belongs | Write the value in one of the shapes named |
| `line N: invalid YAML: ...` | The file is not valid YAML | Fix the syntax at or just before line N (the first line's errors carry no number) |
| `node "X" not found in graph` | Step references a node that doesn't exist | Check node name spelling in your plan; run `aat validate graph` to see available nodes |
| `required input "X" has no plan value` | A non-optional input is missing from the step's values | Add a value, `from` reference, or make the input optional in the graph |
| `input "X" is an unresolved AUTOWIRE` | Composition found no step before this one that produces an output named X | Add the step or addon that produces it, wire it with an addon's `wire:` map, set it in a recipe's `overrides.values`, or use `AUTOWIRE?` for an optional input |
| `'from' reference "X" for "Y": "Z" is not a step` | A value's `from` references a step name that doesn't exist in the plan | Check the step ID spelling; `from` uses step names, not node names |
| `has 'from' reference to "X" but does not list it in dependsOn` | A data dependency is missing from `dependsOn` | Add the referenced step to `dependsOn` to ensure execution order |
| `dependsOn cycle detected` | Steps have circular dependencies | Remove the circular reference; draw out the dependency chain to find the loop |
| `unknown selection strategy "X"` | Invalid strategy in a selection config | Use one of: `first`, `last`, `index`, `random`, `min`, `max`, `match` |
| `sortField "X" not found in elementFields` | Selection sort field doesn't match any elementField | Check the array output's elementFields in the graph; use a field name, not a path |
| `output "X" is not an array type` | Selection source isn't an array | Selections require array outputs; check the source step's output type |
| `plan graphVersion incompatible with graph version` | Major version mismatch between plan and graph | Update the plan's `graphVersion` or regenerate the plan |
| `expectFailure status N must be >= 400` | Expect-failure has a success status code | Expect-failure is for negative tests; use status codes 400+ |
| `mutation X has empty name` | A mutation entry is missing its `name` | Add a unique `name` — it becomes the sibling step id suffix |
| `duplicate mutation name "X"` | Two mutations on the same step share a name | Rename one; mutation names must be unique within a step |
| `mutation "X" must declare at least one of set or rawBody` | A mutation has no input overrides | Add `set: {...}` or `rawBody: "..."` (or both) |
| `mutation "X" must declare at least one expectStatus` | Mutation has no expected failure status | Every mutation requires `expectStatus: [400]` (or similar) |
| `mutation "X" expectStatus N must be >= 400` | Mutation expects a success status | Mutations are negative tests; use 400+ |
| `unknown mutationScope "X"` | `mutationScope` is something other than `"shared"` or `"isolated"` | Use one of the two supported values (or omit for the default `"shared"`) |
| `mutationScope is set but step has no mutations` | `mutationScope` is declared on a step without a `mutations:` block | Remove `mutationScope`, or add mutations |
| `cloned step id "X" collides with an existing step` | An isolated-mutation clone id matches a pre-existing step id | Rename either the existing step or the mutation so `<origId>__<mutationName>` is unique |
| `overrides[N]: expectFailure status M must be >= 400` | Overlay override's `expectFailure` has a success status | Overlay `expectFailure` is for negative tests; use 400+ |
| `overrides[N]: expectFailure must have at least one status` | Overlay override has empty `expectFailure.status` | Provide a non-empty list of `>= 400` status codes |

## Validation in CI/CD

Run validation before execution to catch configuration errors early:

```
aat validate && aat run batch --json
```

If validation fails (exit code 1), the batch command never runs. This is the recommended pattern for CI/CD pipelines. See [CI/CD Integration](ci-cd.md) for full pipeline examples.

---

*Source: `cmd/aat/validate_cmd.go`, `cmd/aat/validate_graph_cmd.go`, `cmd/aat/validate_plan_cmd.go`, `cmd/aat/validate_workflow_cmd.go`, `graph/validate.go`, `plan/validate.go`, `internal/yamlx/decode.go`.*
