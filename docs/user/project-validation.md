# Project Validation

The `aat validate` command checks your entire AAT project for consistency. It loads the project manifest, then validates each layer: graph structure, OAS specs, templates, workflows, and plans.

## Quick Start

From anywhere inside a project with an `aat-project.yaml`:

```bash
aat validate
```

Or point to a specific manifest:

```bash
aat validate --manifest /path/to/aat-project.yaml
```

## What It Checks

Validation runs in six sections, each building on the previous:

| Section | What it checks |
|---------|----------------|
| **Manifest** | Required fields present, all referenced files and directories exist on disk |
| **Graph structure** | YAML parses, nodes/edges/inputs/outputs are structurally valid |
| **OAS validation** | If the graph references OAS specs: operation IDs exist, inputs/outputs match spec parameters and schemas |
| **Adapter outputs** | Template extract keys match graph-declared outputs for every node |
| **Workflow compatibility** | Addon workflows can wire their AUTOWIRE inputs when composed into base workflows |
| **Plans** | Every `.yaml` file in the plans directory parses and validates against the graph |

Sections are skipped when not applicable (e.g., no OAS specs referenced, no plans directory configured).

## Output

On success:

```
Manifest:               OK (project: travelport)
Graph structure:        OK (59 nodes)
OAS validation:         OK
Adapter outputs:        OK (56 templates)
Workflow compatibility: OK (33 workflows)
Plans:                  OK (27 files)

Project validation: PASSED
```

On failure, the failing section shows details:

```
Manifest:               OK (project: travelport)
Graph structure:        OK (59 nodes)
Adapter outputs:        OK (56 templates)
Plans:                  FAILED
  bad-plan.yaml: step "book" references unknown node "doesNotExist"

Project validation: FAILED (1 section(s) with errors)
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--manifest` | auto-discovered | Explicit path to `aat-project.yaml` |
| `--strict` | `false` | Treat warnings as errors (e.g., OAS warnings, workflow compatibility warnings) |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | All sections passed |
| `1` | One or more sections failed, or manifest not found |

## When to Use It

- **Before committing** -- catch broken plans, stale node references, or template mismatches early
- **In CI** -- add `aat validate` as a fast pre-flight check before running actual API tests
- **After graph changes** -- verify that plans and templates still align with the updated graph
- **After adding workflows** -- confirm addon wiring is compatible with base workflows

```yaml
# GitHub Actions example
- name: Validate project
  run: aat validate --strict
```

## Relationship to `aat graph validate`

`aat graph validate` is a focused tool for iterating on a single graph file. It accepts `--graph`, `--oas`, and `--templates` flags directly and doesn't require a manifest.

`aat validate` is the full project check. It requires a manifest and validates everything the manifest references. Both are useful -- different scope.

## Project Manifest

`aat validate` requires an `aat-project.yaml` manifest. If you don't have one yet, create it in your project root:

```yaml
name: my-api
description: "My API test project"
graph: graph.yaml
templates: templates/
environment: env.yaml       # optional
domain: domain.yaml         # optional
plans: plans/               # optional
archives: _output/runs      # optional
traces: _output/traces      # optional
```

All paths are relative to the manifest file. See [Running Tests: Project Discovery](running.md#project-discovery) for how the manifest is also used for auto-resolving CLI flags.

## See Also

- [Running Tests](running.md) -- `aat run` command reference
- [Graph Authoring](graph-authoring.md) -- graph YAML reference
- [Plan Authoring](plan-authoring.md) -- plan YAML reference
- [Workflow Templates](workflow-templates.md) -- workflow and addon authoring
