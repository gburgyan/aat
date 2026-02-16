# Workflow Templates

Workflow templates are pre-built plan skeletons attached to graph workflows. They encode the structural wiring of a test scenario -- which steps to run, how data flows between them, what selections to make -- so the LLM only needs to fill in creative content like literal values, selection strategies, and assertions.

## Why Workflow Templates?

Without templates, the LLM must generate the entire plan from scratch: figure out which nodes to call, wire `from` references between steps, set up selections, configure cleanup. This works but is error-prone for complex multi-step workflows.

Templates flip the approach: the structural skeleton is pre-authored by a human who understands the API, and the LLM fills in only the variable parts. This produces more reliable plans and reduces LLM token usage.

## Configuring Templates in graph.yaml

Templates are linked to workflows in the graph's `workflows` section:

```yaml
workflows:
  # Base workflows — standalone test scenarios
  - name: Standard Checkout
    description: "Browse products, add to cart, and complete checkout"
    template: plans/standard-checkout.yaml

  - name: Express Checkout
    description: "Single-step checkout for returning customers"
    template: plans/express-checkout.yaml

  # Addon workflows — splice into a base workflow
  - name: Apply Coupon
    kind: addon
    after: addItem
    description: "Validate and apply a coupon code to the cart"
    template: plans/apply-coupon.yaml

  - name: Gift Wrap
    kind: addon
    after: addItem
    description: "Add gift wrapping to the order"
    template: plans/gift-wrap.yaml
    wire:
      cartId: createCart.cartId
```

### Workflow Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Human-readable workflow name (matched case-insensitively by the LLM pipeline) |
| `description` | no | What this workflow does |
| `template` | no | Path to plan template YAML, relative to the graph file |
| `kind` | no | Set to `"addon"` for workflows that splice into a base workflow |
| `after` | addon only | Node name in the base workflow to splice after |
| `wire` | no | Explicit PLACEHOLDER overrides for the addon (map of input name to `stepID.outputName` ref, or `"MANUAL"` to leave for the LLM) |

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
- **Intent** — description of what the workflow tests

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

### Base Workflows vs Addon Workflows

Templates fall into two categories:

**Base workflows** are standalone test scenarios:
- Mark one step as `isGoal: true`
- Include cleanup (e.g., `cancelOrder` with `runOn: always`)
- Provide the complete structural skeleton
- Example: Standard Checkout, Full-Payload Booking

**Addon workflows** splice into a base workflow at a specific point:
- Declared with `kind: addon` in graph.yaml
- Specify `after:` — the node in the base workflow to insert after
- All inputs from the base workflow use `PLACEHOLDER` defaults
- No cleanup of their own (the base workflow manages resource lifecycle)
- Optionally specify `wire:` for explicit PLACEHOLDER overrides
- Example: Apply Coupon, Seat Selection, Gift Wrap

When an addon is composed with a base workflow, its steps are prefixed (e.g., `inc0_searchSeatMap`) to avoid ID collisions, and `PLACEHOLDER` values are auto-wired to matching outputs from the base workflow's steps.

## How the LLM Pipeline Uses Templates

When you run `aat prompt`, the planning pipeline:

1. **Workflow selection** — The LLM reads the prompt against the list of available workflows and selects the best match (e.g., "Standard Checkout"). It also identifies any addon workflows to compose (e.g., "Apply Coupon") and classifies constraints as hard, soft, or free.

2. **Template loading** — `LoadWorkflowTemplate` parses the base workflow's plan YAML and validates that all referenced nodes exist in the graph.

3. **Addon composition** — If the LLM selected addons, `ComposeWithAddons` loads each addon template, prefixes its step IDs (e.g., `inc0_`), auto-wires `PLACEHOLDER` values to matching outputs from the base workflow, and splices the addon steps into the base plan at the `after:` insertion point.

4. **Multiplicity expansion** — `ExpandMultiplicity` replicates steps for repeated operations (e.g., `addItem` becomes `addItem_1`, `addItem_2` with step aliasing).

5. **Unfed input discovery** — `UnfedInputsFromTemplate` identifies inputs that need LLM-provided values (no `from`, `fromSelection`, `select`, or default).

6. **LLM plan call** — The skeleton YAML and unfed inputs list are sent to the LLM, which returns values, strategies, and assertions.

7. **Merge** — `MergeLLMValuesWithIDs` merges the LLM's creative content into the skeleton, preserving structural wiring. Only inputs that genuinely need values (unfed inputs) accept new LLM literals — this prevents the LLM from hallucinating values that would shadow auto-wired references.

If the LLM fails to select a recognized workflow, the pipeline returns an error listing available workflows.

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

### Base Workflow Template

1. **Identify the workflow** — Decide which graph nodes participate and in what order.

2. **Write the plan YAML** — Start with the steps, wire `from`/`fromSelection` references, add selections:
   ```yaml
   intent:
     description: "What this tests"

   execution:
     steps:
       - node: firstStep
         values:
           input1: PLACEHOLDER    # LLM fills
           input2: {from: ...}    # structural wiring
       - node: lastStep
         dependsOn: [firstStep]
         isGoal: true
         values:
           id: {from: firstStep.id}

     cleanup:
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

4. **Register in graph.yaml** — Add a workflow entry with a `template:` field.

5. **Validate** — Run `go test ./intent/...` or validate programmatically:
   ```go
   g, _ := graph.ParseFile("graph.yaml")
   p, _ := intent.LoadWorkflowTemplate("plans/my-template.yaml", ".", g)
   err := plan.Validate(p, g)
   ```

### Addon Workflow Template

Addon templates are self-contained sub-workflows that get composed into a base workflow.

1. **Write the addon template** — Use `PLACEHOLDER` for all inputs that come from the base workflow:
   ```yaml
   intent:
     description: "Apply a coupon code to the cart"

   execution:
     steps:
       - node: validateCoupon
         values:
           cartId: PLACEHOLDER      # auto-wired from base workflow
           couponCode: PLACEHOLDER   # LLM fills
       - node: applyCoupon
         dependsOn: [validateCoupon]
         values:
           cartId: PLACEHOLDER
           couponId: {from: validateCoupon.couponId}
   ```

2. **Register in graph.yaml** as an addon:
   ```yaml
   - name: Apply Coupon
     kind: addon
     after: addItem              # node in the base workflow to splice after
     description: "Validate and apply a coupon code"
     template: plans/apply-coupon.yaml
   ```

3. **Add `wire:` overrides if needed** — When a PLACEHOLDER input name doesn't match any base workflow output name, use explicit wiring:
   ```yaml
   - name: Apply Coupon
     kind: addon
     after: addItem
     template: plans/apply-coupon.yaml
     wire:
       cartId: createCart.cartId    # explicit: "cartId" output doesn't exist, wire to createCart.cartId
   ```
   Use `wire: { inputName: "MANUAL" }` to clear a placeholder entirely, leaving it for the LLM to fill.

### How Auto-Wiring Works

When an addon is composed into a base workflow:

1. Each addon step ID is prefixed (e.g., `validateCoupon` becomes `inc0_validateCoupon`).
2. For each `PLACEHOLDER` value in the addon:
   - If there's an explicit `wire:` override, use that reference.
   - If `wire:` says `"MANUAL"`, clear the value (LLM fills it).
   - Otherwise, scan all base workflow step outputs for a matching name. Last producer wins.
3. `dependsOn` entries are automatically added for any cross-workflow references.
4. Addon steps are spliced into the base plan immediately after the `after:` insertion point.

## See Also

- [Plan Authoring](plan-authoring.md) -- full plan YAML schema reference
- [LLM-Assisted Planning](prompt-workflow.md) -- how `aat prompt` uses templates
- [Graph Authoring](graph-authoring.md) -- graph structure and workflows section
- [Value Flow](value-flow.md) -- how values resolve at runtime
- [Travelport Example](travelport-example.md) -- real-world example with base and addon workflows
