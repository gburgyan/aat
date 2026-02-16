# Workflow Templates

Workflow templates are pre-built plan skeletons attached to graph workflows. They encode the structural wiring of a test scenario -- which steps to run, how data flows between them, what selections to make -- so the LLM only needs to fill in creative content like literal values, selection strategies, and assertions.

## Why Workflow Templates?

Without templates, `aat prompt` must generate the entire plan from scratch: figure out which nodes to call, wire `from` references between steps, set up selections, configure cleanup. This works but is error-prone for complex multi-step workflows.

Templates flip the approach: the structural skeleton is pre-authored by a human who understands the API, and the LLM fills in only the variable parts. This produces more reliable plans and reduces LLM token usage.

## Configuring Templates in graph.yaml

Templates are linked to workflows in the graph's `workflows` section:

```yaml
workflows:
  - name: Standard Checkout
    description: "Browse products, add to cart, and complete checkout"
    template: plans/standard-checkout.yaml
    steps: [listProducts, createCart, addItem, addShipping, checkout]

  - name: Apply Coupon
    description: "Validate and apply a coupon code to a cart"
    template: plans/apply-coupon.yaml
    steps: [validateCoupon, applyCoupon]
```

### Workflow Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Human-readable workflow name (matched case-insensitively by the LLM pipeline) |
| `description` | no | What this workflow does |
| `steps` | no | List of node names in this workflow (informational) |
| `template` | no | Path to plan template YAML, relative to the graph file |

The `template` path is resolved relative to the directory containing the graph file. For example, if the graph is at `myapi/graph.yaml` and the template is `plans/standard-checkout.yaml`, the resolved path is `myapi/plans/standard-checkout.yaml`.

## Template Structure

A workflow template is a standard plan YAML file (same schema as `aat run` plans) with placeholder values where the LLM should fill in content.

### Minimal Example

```yaml
metadata:
  prompt: "validate and apply a coupon code"

intent:
  description: "Validate a coupon and apply it to the cart"

execution:
  steps:
    - node: validateCoupon
      description: "Check if the coupon code is valid"
      values:
        cartId: PLACEHOLDER
        couponCode: PLACEHOLDER

    - node: applyCoupon
      description: "Apply the validated coupon to the cart"
      dependsOn: [validateCoupon]
      values:
        cartId: PLACEHOLDER
        couponId: {from: validateCoupon.couponId}
        discountAmount: {from: validateCoupon.discountAmount}
```

### What Templates Define

Templates provide the **structural skeleton**:

- **Steps and their order** — which graph nodes to execute and `dependsOn` relationships
- **Data flow** — `from` references wiring outputs to downstream inputs
- **Selections** — named selections from array outputs with `from` and default `strategy`
- **Selection field extraction** — `fromSelection` references for coordinated field access
- **Cleanup** — cleanup steps with `runOn` conditions
- **Goal** — which step is the test objective (`isGoal: true`)
- **Intent** — the goal node and description

### What the LLM Fills In

The LLM provides the **creative content**:

- **Literal values** — replacing `PLACEHOLDER` defaults with realistic data
- **Selection strategy overrides** — changing `first` to `min`, `match`, `llm`, etc.
- **Selection filters** — adding filter predicates for selection strategies
- **Assertions** — mechanical and semantic assertions on step responses
- **Descriptions** — step descriptions (if not already set in the template)
- **Retry configuration** — per-step retry settings

### PLACEHOLDER Convention

Use the literal string `PLACEHOLDER` as a default value for inputs that need LLM-provided values but should still be considered "wired" in the template:

```yaml
values:
  couponCode: PLACEHOLDER           # LLM replaces with actual value
  cartId: {from: createCart.cartId} # structural wiring, LLM can't change
```

Inputs with `PLACEHOLDER` defaults won't appear in the unfed inputs list, so the LLM knows not to add structural wiring for them -- it just replaces the placeholder with a real value.

### End-to-End Workflows vs Sub-Workflows

Templates fall into two categories:

**End-to-end workflows** create resources and have a clear test objective:
- Set `intent.goal` to the final step
- Mark one step as `isGoal: true`
- Include cleanup (e.g., `cancelOrder` with `runOn: always`)
- Example: Standard Checkout, Return & Refund

**Sub-workflows** operate within an existing context (e.g., an open cart):
- No `intent.goal` or `isGoal` steps
- No cleanup (the calling workflow manages resource lifecycle)
- All caller-provided inputs use `PLACEHOLDER` defaults
- Example: Apply Coupon, Add Shipping, Gift Wrap

## How the LLM Pipeline Uses Templates

When you run `aat prompt`, the planning pipeline:

1. **Goal analysis** — The LLM reads the prompt and identifies a matching workflow name (e.g., "Standard Checkout") and any repetitions (e.g., 3 items)

2. **Template lookup** — `FindWorkflowTemplate` searches the graph's workflows for a case-insensitive name match and returns the template path

3. **Template loading** — `LoadWorkflowTemplate` parses the plan YAML and validates that all referenced nodes exist in the graph

4. **Multiplicity expansion** — `ExpandMultiplicity` replicates steps for repeated operations (e.g., `addItem` becomes `addItem_1`, `addItem_2` with step aliasing)

5. **Unfed input discovery** — `UnfedInputsFromTemplate` identifies inputs that need LLM-provided values (no `from`, `fromSelection`, `select`, or default)

6. **LLM plan call** — The skeleton YAML and unfed inputs list are sent to the LLM, which returns values, strategies, and assertions

7. **Merge** — `MergeLLMValuesWithIDs` merges the LLM's creative content into the skeleton, preserving structural wiring

If template lookup or loading fails, the pipeline falls back to the standard approach: backward chaining from goal nodes to build a skeleton from scratch.

## Validation

Workflow templates go through two levels of validation:

### Plan Validation (`plan.Validate`)

When a template is loaded, it passes through the same validation as any plan:

- All step nodes exist in the graph
- Step IDs are unique
- `dependsOn` references are complete -- every step referenced in a `from` value must appear in the step's `dependsOn` list
- `from` references point to valid step outputs
- Selection sources are valid array outputs
- Required inputs are covered (via `from`, `fromSelection`, `select`, default, or graph edge)

### Structural Checks (`LoadWorkflowTemplate`)

On load, the template function verifies:
- The YAML parses as a valid plan
- Every step node name exists in the graph
- Every cleanup node name exists in the graph

### Integration Tests

It's good practice to write integration tests that validate all templates against the graph. A typical test suite checks:

| What to Check | Why |
|------|----------------|
| Every templated workflow loads and passes `plan.Validate` | Catches broken `from`/`fromSelection` references |
| Each template has the expected number of unfed inputs | Detects regressions when graph outputs change |
| End-to-end workflows have exactly one `isGoal` step; sub-workflows have none | Ensures goal consistency |
| End-to-end workflows include cleanup; sub-workflows do not | Prevents resource leaks |

These tests catch regressions when graph nodes, edges, or outputs change.

## Writing a New Template

1. **Identify the workflow** — Decide which graph nodes participate and in what order

2. **Write the plan YAML** — Start with the steps, wire `from`/`fromSelection` references, add selections:
   ```yaml
   metadata:
     prompt: "short description for LLM matching"

   intent:
     goal: finalStepNode        # omit for sub-workflows
     description: "What this tests"

   execution:
     steps:
       - node: firstStep
         values:
           input1: PLACEHOLDER    # LLM fills
           input2: {from: ...}    # structural wiring
       # ...

     cleanup:                     # omit for sub-workflows
       - node: cleanupNode
         runOn: always
   ```

3. **Ensure `dependsOn` completeness** — Every step referenced in a `from` value must appear in `dependsOn`. This is the most common validation error:
   ```yaml
   # Wrong: checkout references createCart.cartId but doesn't depend on it
   - node: checkout
     dependsOn: [addItem]
     values:
       cartId: {from: createCart.cartId}

   # Correct: createCart is in dependsOn
   - node: checkout
     dependsOn: [addItem, createCart]
     values:
       cartId: {from: createCart.cartId}
   ```

4. **Register in graph.yaml** — Add a `template:` field to the workflow entry

5. **Validate** — Run `go test ./intent/...` or validate programmatically:
   ```go
   g, _ := graph.ParseFile("graph.yaml")
   p, _ := intent.LoadWorkflowTemplate("plans/my-template.yaml", ".", g)
   err := plan.Validate(p, g)
   ```

## See Also

- [Plan Authoring](plan-authoring.md) -- full plan YAML schema reference
- [LLM-Assisted Planning](prompt-workflow.md) -- how `aat prompt` uses templates
- [Graph Authoring](graph-authoring.md) -- graph structure and workflows section
- [Value Flow](value-flow.md) -- how values resolve at runtime
