# Local Development

When you're developing a service and want AAT's integration tests to hit your local instance instead of the shared environment, drop a `.aat-overrides.yaml` file in your project directory. AAT auto-discovers it on every run — no flags needed.

## Quick Setup

1. Create a `.aat-overrides.yaml` file in your project root (next to `aat-project.yaml`):

```yaml
overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
```

2. Run normally — the override is picked up automatically:

```bash
aat run plan smoke-test
# aat: auto-discovered overrides: /path/to/project/.aat-overrides.yaml
# aat: override: searchFlights → http://localhost:8080
```

The file is already in AAT's `.gitignore`, so it won't be committed.

## Common Scenarios

### Route one node to localhost

The simplest case — send traffic for a single API operation to your local service:

```yaml
overrides:
  - match: createOrder
    baseUrl: http://localhost:3000
```

### Disable auth for local dev

Your local service probably doesn't need OAuth tokens:

```yaml
overrides:
  - match: createOrder
    baseUrl: http://localhost:3000
    auth:
      type: none
```

### Different path structure on local service

If your local service uses a different URL path than production:

```yaml
overrides:
  - match: createOrder
    baseUrl: http://localhost:3000
    pathRewrite:
      strip: /api/v2
      prefix: /v1
```

### Route multiple related nodes with a glob

Override all nodes matching a pattern:

```yaml
overrides:
  - match: "commit*"
    baseUrl: http://localhost:9090
```

### Mix local and remote

Override only the nodes you're working on — everything else stays on the shared environment:

```yaml
overrides:
  - match: createOrder
    baseUrl: http://localhost:3000
    auth:
      type: none
  - match: updateOrder
    baseUrl: http://localhost:3000
    auth:
      type: none
```

## How It Works

### Discovery

AAT walks up from your current working directory looking for `.aat-overrides.yaml`, the same way it finds `aat-project.yaml`. The first file found wins. This means you can place the file:

- In the project root (most common)
- In a subdirectory for scope-specific overrides
- In a parent directory to apply across multiple projects

### Priority Chain

Overrides are applied in this order, with later entries taking precedence:

1. `env.yaml` `overrides:` section (permanent, shared)
2. `.aat-overrides.yaml` (auto-discovered, personal)
3. `--env-overlay` flag (explicit overlay file)
4. `--override` flag (CLI one-off)

### Logging

AAT always logs when it finds a `.aat-overrides.yaml` so you know what's active:

```
aat: auto-discovered overrides: /path/to/project/.aat-overrides.yaml
```

## Keeping It Clean

### Git

`.aat-overrides.yaml` is included in AAT's default `.gitignore`. If your project has its own `.gitignore`, add it there too.

### CI/CD

For CI pipelines or clean runs where you want to guarantee no local overrides are applied:

```bash
aat run batch --no-auto-overrides
```

The `--no-auto-overrides` flag is available on `aat run plan`, `aat run batch`, and `aat prompt`.

## Existing Alternatives

`.aat-overrides.yaml` is the recommended approach for ongoing local development. For other scenarios:

- **`--override NODE=URL`** — one-off overrides on the command line, good for quick experiments
- **`--env-overlay FILE`** — explicit overlay file, useful when you want to version-control an alternate routing config
- **`env.yaml` `overrides:` section** — permanent multi-host routing shared across all developers

See [Environments](environments.md) for full override and auth documentation.
