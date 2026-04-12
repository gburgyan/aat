# Project Setup and Manifest

The project manifest (`aat-project.yaml`) marks your project root and tells AAT where all its artifacts live, so most CLI flags become optional.

## Overview

AAT separates **what to test** (graph, templates, domain knowledge) from **where things live** (file paths, directory layout). The manifest bridges these — it declares artifact locations once so every command can find them automatically. When a manifest is discoverable, you can run `aat prompt "..."` or `aat run batch` without passing `--graph`, `--templates`, or `--env` flags.

## Project Directory Layout

A typical project directory looks like this:

```
my-ecommerce-api/
  aat-project.yaml        # project manifest (discovery root)
  graph.yaml              # API graph
  env.yaml                # environment config (auth, endpoints)
  domain.yaml             # domain knowledge (optional)
  templates/              # request/response templates
    listProducts.yaml
    createOrder.yaml
    cancelOrder.yaml
    getOrderStatus.yaml
  workflows/              # reusable workflow templates (optional)
    order-lifecycle.yaml
    return-flow.yaml
  plans/                  # saved plan instances (optional)
    smoke-test.yaml
    full-checkout.yaml
  runs/                   # execution archives (auto-created)
  traces/                 # planning traces (auto-created)
```

These paths are convention, not enforced. You can name files and directories however you like — the manifest maps each artifact type to its actual location.

## The Project Manifest

The manifest is a YAML file named `aat-project.yaml` that declares your project's artifacts:

```yaml
name: ecommerce
description: E-commerce order management API tests
tags: [orders, inventory, payments]

graph: graph.yaml
templates: templates/
domain: domain.yaml
environment: env.yaml

workflows: workflows/
plans: plans/
archives: runs/
traces: traces/
```

All paths are resolved relative to the manifest file's directory. Absolute paths are also accepted and used as-is.

### Required vs Optional Fields

Only two fields are required by the manifest loader:

| Field | Required | Description |
|-------|----------|-------------|
| `graph` | Yes | Path to the API graph YAML |
| `templates` | Yes | Path to the templates directory |

Everything else is optional. When omitted, commands that need those paths will either use sensible defaults or require explicit flags.

### Schema Reference

| Field | YAML Key | Type | Description |
|-------|----------|------|-------------|
| Name | `name` | string | Project name (for display and identification) |
| Description | `description` | string | Brief project description |
| Tags | `tags` | list | Freeform tags for categorization |
| Graph | `graph` | string | Path to the API graph YAML file |
| Templates | `templates` | string | Path to the templates directory |
| Domain | `domain` | string | Path to the domain knowledge YAML file |
| Workflows | `workflows` | string | Path to the workflows directory |
| Layers | `layers` | string | Path to the graph layers directory |
| Plans | `plans` | string or list | Path(s) to plan directories |
| Archives | `archives` | string | Path to the run archive directory |
| Traces | `traces` | string | Path to the planning trace directory |
| Visualizers | `visualizers` | string | Path to the visualizer plugins directory |
| Environment | `environment` | string | Path to the default environment YAML file |
| Default Environment | `defaultEnvironment` | string | Default environment name for multi-env files |

The `plans` field accepts either a single path or a list of paths:

```yaml
# Single directory
plans: plans/

# Multiple directories
plans:
  - plans/
  - /shared/plans/regression
```

## Auto-Discovery

AAT automatically finds your manifest by walking up from the current working directory toward the filesystem root, looking for a file named `aat-project.yaml`. The first match wins.

```
/home/user/projects/ecommerce/plans/   # cwd — no manifest here
/home/user/projects/ecommerce/         # aat-project.yaml found here
```

Commands that use auto-discovery: `aat run`, `aat prompt`, `aat validate`, `aat web`, `aat mcp serve`, `aat plan list`, `aat env list`, and `aat docs generate`.

To override auto-discovery, pass the `--manifest` flag:

```bash
aat validate --manifest /path/to/other-project/aat-project.yaml
```

## Resolution Priority

AAT resolves project paths through a 4-level priority chain. Each level overwrites paths from the previous level:

1. **User config** — a persistent `default_project` setting in your user-level config file
2. **`AAT_PROJECT` environment variable** — a directory or manifest path
3. **CWD walk-up** — automatic discovery from the current working directory
4. **`--manifest` flag** — explicit manifest path (overrides discovery)

After manifest resolution, **explicit CLI flags** (`--graph`, `--templates`, `--env`, etc.) always take final precedence over any manifest-derived path.

### User Config

The user config file lives at the platform-native config directory:

| Platform | Path |
|----------|------|
| macOS | `~/Library/Application Support/aat/config.yaml` |
| Linux | `~/.config/aat/config.yaml` |
| Windows | `%AppData%/aat/config.yaml` |

The file sets a default project that applies when no other manifest is found:

```yaml
default_project: /home/user/projects/ecommerce
```

The value can be a directory (AAT looks for `aat-project.yaml` inside) or a direct path to a manifest file.

### AAT_PROJECT Environment Variable

Set `AAT_PROJECT` to point at a project directory or manifest file:

```bash
export AAT_PROJECT=/home/user/projects/ecommerce
aat validate   # uses that project's manifest
```

This is useful in CI/CD environments where you want to pin the project root without relying on the working directory.

## Overriding Manifest Paths

CLI flags always override the corresponding manifest field. This lets you test against alternate configurations without modifying the manifest:

| CLI Flag | Manifest Field |
|----------|---------------|
| `--graph` | `graph` |
| `--templates` | `templates` |
| `--env` | `environment` |
| `--domain` | `domain` |
| `--output` | `archives` |
| `--trace-dir` | `traces` |

```bash
# Use the project's graph and templates, but a different environment
aat run plan checkout.yaml --env staging-env.yaml

# Use the project's environment, but a different graph
aat validate graph --graph experimental-graph.yaml
```

## Multiple Environments

A single project often targets multiple environments (development, staging, production). The recommended approach is to define all environments in a single multi-environment file:

```
my-ecommerce-api/
  aat-project.yaml          # environment: env.yaml, defaultEnvironment: dev
  env.yaml                  # shared config + all environment definitions
  env.secrets.yaml          # auth credentials (gitignored)
  graph.yaml
  templates/
```

```bash
# Uses the manifest's defaultEnvironment
aat run batch

# Selects a specific environment
aat run batch --env-name staging

# List all available environments
aat env list
```

Set `defaultEnvironment` in the manifest to avoid passing `--env-name` every time. The `AAT_ENV_NAME` environment variable also works, which is useful for CI/CD.

You can also use the `include` directive in env.yaml to split secrets into a separate, gitignored file. See [Environments: File Splitting](environments.md#file-splitting-with-include) for details.

See [Environments](environments.md) for the full environment file reference.

## Bootstrapping from OpenAPI

If you have an OpenAPI specification, `aat generate` scaffolds an initial graph and templates directory:

```bash
aat generate --oas openapi.yaml --output-graph graph.yaml --output-templates templates/
```

This creates one graph node per operation and one template per node. You then add edges, domain knowledge, and the manifest by hand.

See [Graphs](graphs.md) for details on OpenAPI integration and the generated graph structure.

## Schema Reference

```yaml
# aat-project.yaml — complete annotated example

name: ecommerce                    # project name (for display)
description: Order management API  # brief description (optional)
tags: [orders, inventory]          # freeform tags (optional)

graph: graph.yaml                  # required — path to API graph
templates: templates/              # required — path to templates directory
domain: domain.yaml                # optional — domain knowledge file
environment: env.yaml              # optional — default environment file
defaultEnvironment: dev            # optional — default env name (multi-env files)
workflows: workflows/              # optional — workflow templates directory
layers: layers/                    # optional — graph layers directory
plans: plans/                      # optional — plan directory (string or list)
archives: runs/                    # optional — run archive output directory
traces: traces/                    # optional — planning trace output directory
visualizers: visualizers/          # optional — visualizer plugins directory
```

---

*Source: `config/manifest.go`, `config/resolver.go`, `config/user_config.go`.*
