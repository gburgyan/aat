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

> **Tip:** The overlay file also supports top-level `auth` and `headers` fields that apply to all API calls. See the examples below.

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

### Use different credentials for the whole run

When the environment you're testing against requires different auth than the base `env.yaml`, add a top-level `auth` block. This replaces the environment auth for **all** API calls, not just matched overrides:

```yaml
auth:
  type: oauth2
  tokenUrl: https://auth.staging.example.com/token
  credentials:
    username:
      source: env
      var: STAGING_USERNAME
    password:
      source: env
      var: STAGING_PASSWORD
    clientId:
      source: env
      var: STAGING_CLIENT_ID
    clientSecret:
      source: env
      var: STAGING_CLIENT_SECRET

overrides:
  - match: myService
    baseUrl: http://localhost:3000
```

### Add transaction-level headers

Headers at the top level are merged into every request, useful for access-group tokens or other cross-cutting headers:

```yaml
headers:
  X-Access-Group: my-group-id
  X-Debug: "true"

overrides:
  - match: myService
    baseUrl: http://localhost:3000
```

### Force a negative case for depth testing

Overlays can mutate individual input values and declare `expectFailure` for
matched nodes. This is handy when you want to rerun a normal plan as a
negative test — no edits to the plan itself:

```yaml
overrides:
  - match: createOrder
    values:
      productId: ""       # force a validation error
    expectFailure:
      status: [400]
```

With that overlay active, the existing plan runs as-is, but `createOrder`
receives an empty `productId` and the step passes only if the server
responds with 400. See [Environments: Input-Value and Expected-Failure
Overrides](environments.md#input-value-and-expected-failure-overrides) for
precedence rules and [Plans: Mutations](plans.md#mutations-codified-negative-suites)
for codifying a whole negative suite in the plan itself.

### Target a different backend when running locally

If your local service talks to a different backend than the project default (e.g. the project defaults to `pp` pre-prod, but your locally-running service hits `dev` backend resources), name the backend in the overlay:

```yaml
environment: dev             # selects the dev env from env.yaml
overrides:
  - match: "*"
    baseUrl: http://localhost:8080
```

Now `aat run plan ...` picks up the `dev` environment automatically whenever this overlay is active. Passing `--env pp` on the command line still wins, so the project default is never hidden from you.

Top-level `environment`, `auth`, `headers`, and `overrides` can all be combined in a single file. See [Environments: Overlay Files](environments.md#overlay-files) for the full reference.

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

Transaction-level auth follows a separate chain (later entries replace earlier ones):

1. `env.yaml` `auth:` — base environment credentials
2. `.aat-overrides.yaml` `auth:` — auto-discovered overlay
3. `--env-overlay` file `auth:` — explicit overlay
4. Plan-level `auth:` — per-plan override

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
