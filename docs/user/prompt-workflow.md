# LLM-Assisted Planning

The `aat prompt` command generates test plans from natural language. You describe what you want to test, and AAT's LLM pipeline analyzes your API graph, builds a plan, and lets you review it before execution.

## Prerequisites

`aat prompt` requires an LLM endpoint configured in your environment file:

```yaml
llm:
  endpoint: https://api.openai.com/v1
  apiKey:
    source: env
    var: OPENAI_API_KEY
  model: gpt-5.2
```

Both OpenAI and Anthropic endpoints are supported. AAT auto-detects the provider from the endpoint URL. See [Environments](environments.md#llm-configuration) for details.

## Quick Start

With an `aat-project.yaml` manifest (see [Running Tests: Project Discovery](running.md#project-discovery)):

```bash
aat prompt "Create a pet and verify it exists"
```

Or with explicit paths:

```bash
aat prompt "Create a pet and verify it exists" \
  --env env.yaml \
  --graph graph.yaml \
  --templates templates/
```

AAT will:
1. Analyze your prompt against the API graph
2. Generate a test plan
3. Show you the plan and ask for confirmation
4. Execute the plan (if confirmed)
5. Write an archive of the results

## Command Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| (positional) | yes | -- | The prompt text (first positional argument) |
| `--env` | auto | -- | Path to environment YAML file |
| `--graph` | auto | -- | Path to graph YAML file |
| `--templates` | auto | -- | Path to templates directory |
| `--domain` | no | -- | Path to domain knowledge YAML file |
| `--output` | no | `runs` | Directory for archive output |
| `--save` | no | -- | Save the generated plan to this file path |
| `--yes` | no | `false` | Skip confirmation and execute immediately |
| `--trace` | no | `false` | Capture planning pipeline trace for debugging |
| `--trace-dir` | no | `traces` | Directory for trace output |

**Auto-resolved flags:** `--env`, `--graph`, and `--templates` are resolved automatically when an `aat-project.yaml` manifest is found. See [Running Tests: Project Discovery](running.md#project-discovery).

## The Interactive Flow

After generating a plan, AAT displays a narrative summary and asks:

```
Execute this plan? [Y/n/a(djust)]
```

Three choices:

| Input | Action |
|-------|--------|
| `y`, `yes`, or Enter | Execute the plan |
| `n`, `no`, or any other input | Cancel and exit |
| `a` or `adjust` | Edit the plan before executing |

EOF (e.g., piped input that ends) is treated as `n`.

### Auto-Confirm Mode

Use `--yes` to skip the confirmation prompt and execute immediately:

```bash
aat prompt "Create a pet and verify it" \
  --env env.yaml --graph graph.yaml --templates templates/ \
  --yes
```

This is useful for scripting or CI workflows where interactive confirmation isn't possible.

## Adjusting a Generated Plan

When you choose `a` (adjust), AAT:

1. Saves the plan to a temporary YAML file
2. Prints the file path
3. Waits for you to edit it

```
aat: plan saved to /tmp/aat-plan-123456/plan.yaml
aat: edit the file, then press Enter to reload...
```

Open the file in your editor, make changes, save it, then press Enter in the terminal. AAT reloads the file, validates it against the graph, and shows the updated plan narrative. You then get the confirmation prompt again.

This workflow lets you fix values, add assertions, adjust step ordering, or make any other changes to the generated plan before execution.

## Saving Plans

Use `--save` to write the generated plan to a file:

```bash
aat prompt "Order the cheapest in-stock laptop" \
  --env env.yaml --graph graph.yaml --templates templates/ \
  --save plans/laptop-order.yaml
```

The plan is saved immediately after generation, before the confirmation prompt. You can save a plan and cancel execution if you just want the plan file for later use with `aat run`.

## Execution

After confirmation, `aat prompt` executes the plan the same way `aat run` does:

1. Loads templates
2. Creates the engine (default mode: `lean`, since an LLM is always available)
3. Runs the plan in topological order
4. Prints a step-by-step summary
5. Writes an archive

The execution mode defaults to `lean` (since `aat prompt` requires an LLM), but respects the `llm.mode` setting from your environment file if set.

## Domain Knowledge

Pass `--domain` to give the LLM context about your API's business rules and valid values:

```bash
aat prompt "Create a pet with a realistic name" \
  --env env.yaml --graph graph.yaml --templates templates/ \
  --domain domain.yaml
```

Domain knowledge helps the LLM:
- Pick realistic values from defined pools
- Understand constraints (e.g., "delivery dates must be in the future")
- Generate more accurate plans

See [Domain Knowledge](domain-knowledge.md) for the full reference.

## Debugging with Traces

When plans don't come out right, use `--trace` to capture the full LLM planning pipeline:

```bash
aat prompt "Order a product" \
  --env env.yaml --graph graph.yaml --templates templates/ \
  --trace --trace-dir traces/
```

This produces a trace file at `traces/trace-YYYYMMDD-HHMMSS-XXXXXXXX/plan-trace.json` containing:

| Section | Contents |
|---------|----------|
| **Workflow selection** | System/user prompts sent to the LLM, raw response, token counts, timing |
| **Skeleton** | The deterministic plan scaffold (from template + addon composition), unfed inputs list |
| **Plan generation** | Full prompts, raw LLM response, token counts, timing |
| **Merge/post-process** | Plan snapshots after merge and after post-processing |
| **Validation** | Any validation errors in the generated plan |

Traces are written even when the pipeline fails partway through -- partial traces help you diagnose where things went wrong.

### Common Debugging Scenarios

**Wrong workflow selected:**
Check the workflow selection section. The LLM may have picked the wrong base workflow or missed an addon. The selection prompt lists all available workflows with descriptions.

**Values are unrealistic:**
Add domain knowledge (`--domain`) with value pools and concepts. Check the skeleton section to see what information the LLM had when generating values.

**Plan fails validation:**
Check the validation section of the trace. Common issues: referencing nodes not in the graph, incorrect `dependsOn` references, or invalid selection sources.

**Addon AUTOWIRE values not wired:**
If an addon step still has `AUTOWIRE` values after composition, the input name doesn't match any output from the base workflow. Add explicit `wire:` overrides to the addon's workflow definition in graph.yaml.

## Workflow Templates

If your graph defines workflows with `template:` fields, `aat prompt` automatically uses pre-built plan skeletons instead of generating everything from scratch. The LLM selects the best-matching workflow, identifies any addon workflows to compose (e.g., seat selection, ancillary services), loads and composes the templates, and fills in literal values, selection strategies, and assertions.

This produces more reliable plans for complex multi-step workflows. See [Workflow Templates](workflow-templates.md) for the full guide on authoring base and addon templates.

## Tips for Writing Prompts

**Be specific about what to test:**
```
# Good
"Create a pet named Buddy, then verify it exists by ID"

# Vague
"Test the pet API"
```

**Name the operations if you know them:**
```
"Use createPet to add a pet, then findByStatus to search for it"
```

**Specify assertions you care about:**
```
"Create a pet and verify the response includes an id field and status 200"
```

**Describe the scenario, not the implementation:**
```
# Good
"Test that deleting a pet makes it no longer retrievable"

# Too implementation-focused
"Call deletePet then call getPet and assert 404"
```

## Full Example Session

```
$ aat prompt "Create a pet and verify it can be retrieved" \
    --env examples/petstore/env.yaml \
    --graph examples/petstore/graph.yaml \
    --templates examples/petstore/templates/

aat: loading environment...
aat: loaded environment "petstore"
aat: authenticated via none
aat: loaded graph (4 nodes, 4 edges)
aat: analyzing prompt with LLM...
aat: plan generated (2 steps)

Plan: Create a pet and verify it can be retrieved

  1. createPet — Create a new pet
     Values: name="Buddy", status="available"
     Assert: status=200, field "id" exists

  2. getPet — Verify the pet exists [GOAL]
     Depends on: createPet
     Assert: status=200, field "name" = "Buddy"

  Cleanup: deletePet

Execute this plan? [Y/n/a(djust)] y

aat: loaded 4 templates
aat: executing plan (2 steps, mode=lean)...

  [1/2] createPet            200  312ms
  [2/2] getPet               200  89ms

  cleanup:
    deletePet              200  45ms

PASSED (2/2 steps, 446ms)
Archive: runs/run-20260211-143022-a1b2c3d4/archive.json
```

## See Also

- [Workflow Templates](workflow-templates.md) -- pre-built plan skeletons for reliable plan generation
- [Plan Authoring](plan-authoring.md) -- plan YAML schema for understanding/editing generated plans
- [Running Tests](running.md) -- executing saved plans with `aat run`
- [Domain Knowledge](domain-knowledge.md) -- improving plan generation with business context
- [Environments](environments.md#llm-configuration) -- LLM endpoint setup
