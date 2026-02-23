# LLM-Assisted Planning

The `aat prompt` command turns a natural-language description into an executable test plan. You describe what you want to test in plain English, and AAT's two-step LLM pipeline selects the right workflow, fills in values, and presents a ready-to-run plan for your approval.

## Prerequisites

Before using `aat prompt`, you need:

- **LLM configured** in your environment file — endpoint, API key, and model. See [Environments: LLM Configuration](environments.md#llm-configuration).
- **Graph, templates, and workflows** set up in your project. The LLM needs workflows to choose from and graph metadata to understand your API.
- **Domain knowledge** (optional but recommended) — improves value selection. See [Domain Knowledge](domain.md).

## Quick Start

```
$ aat prompt "create an order for express delivery to New York"
Loading project...
Generating plan...

  Plan: Create Order — Express delivery to New York
  Workflow: order-lifecycle + express-shipping addon

  1. search (listProducts)
     category = "electronics"

  2. order (createOrder)
     productId ← search.productId
     quantity = 1
     shippingPriority = "express"

  3. confirm (confirmOrder)
     orderId ← order.orderId
     shippingCity = "New York"

  4. check (getOrderStatus)
     orderId ← order.orderId
     assert: status == 200
     assert: orderStatus == "confirmed"

  cleanup:
    cancel (cancelOrder)
      orderId ← order.orderId

Execute this plan? [Y/n/a(djust)]
```

Press Enter (or `y`) to execute immediately. Press `n` to abort. Press `a` to edit the plan YAML before running.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--manifest` | path | auto-discovered | Explicit path to `aat-project.yaml` |
| `--env` | path | from manifest | Environment config file |
| `--graph` | path | from manifest | API graph file |
| `--templates` | path | from manifest | Templates directory |
| `--domain` | path | from manifest | Domain knowledge file |
| `--output` | path | `_output/runs` | Archive output directory |
| `--save` | string | — | Save the generated plan (name or path) |
| `--save-full` | bool | `false` | Save as full expanded plan instead of compact recipe |
| `--yes` | bool | `false` | Skip confirmation and execute immediately |
| `--trace` | bool | `false` | Capture planning pipeline trace for debugging |
| `--trace-dir` | path | `_output/traces` | Directory for plan trace output |
| `--layer` | string | — | Data layer to apply (repeatable) |

When a manifest is discoverable, `--env`, `--graph`, `--templates`, and `--domain` are optional.

## Interactive Flow

### Plan Display

After the LLM generates a plan, AAT displays it in a human-readable narrative format. Each step shows the node it calls, its input values, data references to earlier steps, selections, and assertions. Cleanup steps appear at the end.

### Confirmation

The confirmation prompt offers three choices:

| Key | Action |
|-----|--------|
| `y` or Enter | Execute the plan |
| `n` or EOF | Abort without executing |
| `a` | Open the plan YAML in your editor for adjustment |

### Adjusting Plans

Pressing `a` saves the plan to a temporary YAML file and prompts you to edit it. AAT waits for you to finish editing (press Enter when done), then reloads the file, re-validates it against the graph, and shows the updated plan for confirmation again.

The editor is determined by the `$EDITOR` environment variable.

## Saving Plans

The `--save` flag writes the generated plan to disk. Saved plans can be run later with `aat run plan`.

```
aat prompt --save smoke-test "create an order and check status"
```

Name resolution for `--save`:
- Absolute paths are used as-is
- Names with `.yaml` or `.yml` extension are treated as literal paths
- Plain names are resolved through the manifest's plan directories, with `.yaml` appended automatically

### Recipe Format (default)

By default, `--save` writes a compact **recipe** — just the workflow selection and any value overrides. Recipes are small, reusable, and re-compose from the current workflow templates each time they run. This means recipes automatically pick up workflow improvements without being re-saved.

```yaml
kind: recipe
metadata:
  created: 2026-02-23T14:30:52Z
  prompt: "create an order for express delivery to New York"
  graphVersion: "1.0.0"
selection:
  workflow: order-lifecycle
  description: "Express delivery to New York"
  addons:
    - express-shipping
overrides:
  values:
    confirmOrder.shippingCity: "New York"
```

### Full Plan Format (`--save-full`)

The `--save-full` flag saves the fully expanded plan with every step and value frozen. Full plans are independent of workflow changes — they run exactly as generated, every time.

```
aat prompt --save smoke-test --save-full "create an order and check status"
```

Use recipes for most cases. Use full plans when you need exact reproducibility or the plan was manually adjusted.

## How the Pipeline Works

The `aat prompt` pipeline has five stages:

### Step 1: Workflow Selection

The LLM receives a list of available workflows, their descriptions, and your prompt. It selects the base workflow that best matches your intent, plus any addons that apply (e.g., an express-shipping addon for delivery-related prompts). It also selects slot options when the workflow has choice points.

### Step 2: Skeleton Composition

AAT composes the selected workflow with its addons into a plan skeleton. Addon steps are spliced into the correct position based on their `after` declarations. Placeholder values are auto-wired, and the list of unfed inputs (values the LLM needs to provide) is computed.

### Step 3: Value Fill

The LLM receives the plan skeleton, the list of unfed inputs, domain knowledge (concepts, types, value pools), and your original prompt. It fills in values for every unfed input, choosing realistic data that matches your intent.

### Step 4: Post-Processing

AAT merges the LLM's values into the skeleton and performs mechanical fixes: repairing `dependsOn` references, normalizing selection configs, adding cleanup steps for nodes that declare cleanup counterparts, and setting plan metadata.

### Step 5: Validation

The completed plan is validated against the graph. If validation fails (e.g., the LLM referenced a node that doesn't exist), the error is reported. With `--trace` enabled, whatever was captured before the failure is still written to the trace directory.

## Domain Knowledge

The quality of LLM-generated plans depends directly on the domain knowledge available. Domain knowledge teaches the LLM:

- What values are valid for each field type (value pools)
- How fields relate to each other (concepts)
- What constraints apply to specific inputs

A project with rich domain knowledge produces plans with realistic, varied data. A project without domain knowledge gets generic placeholder values.

See [Domain Knowledge](domain.md) for how to write a domain file.

## Debugging with Traces

When something goes wrong with plan generation — the LLM picks the wrong workflow, fills in bad values, or produces an invalid plan — traces are the primary debugging tool.

```
aat prompt --trace "create an order for express delivery"
```

The trace captures every pipeline stage as JSON:

- **Workflow selection call** — full system/user prompts sent to the LLM, raw response, token counts, timing
- **Skeleton** — the composed plan scaffold and YAML sent to the value-fill LLM call, with the list of unfed inputs
- **Value fill call** — full prompts, raw response, token counts, timing
- **Merge and post-processing** — snapshots of the plan after each transformation
- **Validation** — any validation errors

Traces are written to `_output/traces/trace-YYYYMMDD-HHMMSS-XXXXXXXX/plan-trace.json` (or the directory specified by `--trace-dir`).

If the pipeline fails mid-way, whatever was captured so far is still written as a partial trace. This is invaluable for diagnosing LLM response parsing failures.

Browse traces visually with `aat web viewtrace`. See [Web UI and Archives: Viewing Traces](web-ui.md#viewing-traces) for details.

## Tips for Effective Prompts

**Be specific about what to test, not how.** Describe the scenario you want to verify, not the API calls. The LLM knows the workflows and will select the right one.

```
# Good — describes the scenario
"create an order with express shipping to Nashville"

# Less effective — describes the API calls
"call listProducts then createOrder then confirmOrder with express"
```

**Mention data you care about.** If you need a specific city, category, or shipping method, say so. The LLM will use your values and fill in everything else from domain knowledge.

```
"place an order for shoes with standard shipping to Chicago"
```

**Reference known domain concepts.** If your domain file defines concepts like "shipping priority" or "product category", use those terms. The LLM has access to your domain knowledge and will match your language to the right fields.

**Keep prompts concise.** One or two sentences is usually enough. Long, detailed prompts can confuse the workflow selection step.

**Use `--trace` to debug poor results.** If the LLM picks the wrong workflow or fills in bad values, the trace shows exactly what prompts were sent and what came back. Adjust your domain knowledge or workflow descriptions to guide better selections.

---

*Source: `cmd/aat/prompt.go`, `intent/interpret.go`, `intent/compose.go`.*
