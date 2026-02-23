# Environments

The environment file configures how AAT connects to your API at runtime — base URL, authentication, headers, LLM settings, and multi-host routing.

## Overview

AAT separates **what to test** (graph, plans, templates) from **where to test** (environment). The environment file holds connection details for a specific target: API endpoints, credentials, static headers, and runtime settings. Switching between development, staging, and production is a matter of pointing to a different environment file via `--env` or the [project manifest](project-setup.md).

## File Structure

An environment file is a YAML document with a top-level `environment` name and `apiBaseUrl`:

```yaml
environment: dev
apiBaseUrl: https://api.dev.example.com

auth:
  type: none
```

A more typical configuration includes authentication and headers:

```yaml
environment: staging
apiBaseUrl: https://api.staging.example.com

auth:
  type: apikey
  headerName: X-API-Key
  credentials:
    key:
      source: env
      var: STAGING_API_KEY

headers:
  Accept: application/json
  X-Client-Version: "2.1"
```

## Authentication

AAT supports four authentication types. Tokens are cached by an internal `AuthProvider` to avoid redundant auth calls — OAuth2 tokens are refreshed 30 seconds before expiry, while API key and bearer tokens are cached indefinitely.

### OAuth2

Resource Owner Password Credentials (ROPC) flow. AAT exchanges credentials for a token at the specified `tokenUrl`:

```yaml
auth:
  type: oauth2
  tokenUrl: https://auth.example.com/oauth/token
  credentials:
    username:
      source: env
      var: API_USERNAME
    password:
      source: env
      var: API_PASSWORD
    clientId:
      source: env
      var: API_CLIENT_ID
    clientSecret:
      source: env
      var: API_CLIENT_SECRET
```

Required credentials: `username`, `password`, `clientId`, `clientSecret`. All four must be present for OAuth2.

### API Key

A static key sent as a custom header. The `headerName` field controls which header carries the key:

```yaml
auth:
  type: apikey
  headerName: X-API-Key
  credentials:
    key:
      source: env
      var: INVENTORY_API_KEY
```

Required: `credentials.key` and `headerName`.

### Bearer Token

A pre-obtained token sent as `Authorization: Bearer <token>`:

```yaml
auth:
  type: bearer
  credentials:
    token:
      source: env
      var: ANALYTICS_TOKEN
```

Required: `credentials.token`.

### No Auth

For public APIs or when authentication is handled externally:

```yaml
auth:
  type: none
```

An empty or missing `type` field is treated as `none`.

## Custom Headers

Static headers added to every request. These form the base layer — template and auth headers can override them:

```yaml
headers:
  Accept: application/json
  X-Client-Id: aat-test-runner
  X-Request-Source: automated-testing
```

Header merge order (later values override earlier ones for the same key):

1. **Environment headers** — this `headers` section
2. **Template headers** — per-template `request.headers` (see [Templates](templates.md))
3. **Plan-level headers** — per-step header overrides (see [Plans](plans.md))
4. **Auth headers** — authentication headers (final precedence)

## Secrets

Credentials and API keys are stored as `SecretRef` values. Each ref specifies a `source` and a resolution method:

### Environment Variable (Recommended)

```yaml
credentials:
  key:
    source: env
    var: MY_API_KEY
```

AAT resolves the value from the OS environment variable at runtime. If the variable is not set, authentication fails with a clear error message.

### Literal Value

```yaml
credentials:
  key:
    source: literal
    value: sk-test-1234567890
```

The value is stored directly in the YAML file. Use this only for local development — never commit literal secrets to version control.

### Redaction

All resolved secret values are automatically redacted from run archives. AAT collects secrets from auth credentials and the LLM API key, then scrubs matching values from archived headers and responses.

## LLM Configuration

The `llm` section configures the language model used by `aat prompt` for plan generation:

```yaml
llm:
  endpoint: https://api.openai.com/v1/chat/completions
  apiKey:
    source: env
    var: OPENAI_API_KEY
  model: gpt-5.2
```

| Field | Description |
|-------|-------------|
| `endpoint` | LLM API endpoint URL |
| `apiKey` | Secret reference for the API key |
| `model` | Model identifier to use |
| `provider` | `"openai"` or `"anthropic"` — auto-detected from the endpoint URL when omitted |

Provider auto-detection uses the endpoint hostname: URLs containing `anthropic` use the Anthropic protocol; all others default to OpenAI-compatible.

## Runtime Settings

Execution-time defaults for the engine:

```yaml
settings:
  maxRunDuration: 120s
  defaultRetries: 2
  archiveFormat: json
```

| Field | Default | Description |
|-------|---------|-------------|
| `maxRunDuration` | `120s` | Maximum wall-clock time for a single plan run |
| `defaultRetries` | `2` | Plan-level retry count on failure |
| `archiveFormat` | `json` | Archive format: `json` or `json.gz` |

Duration values use Go duration syntax: `30s`, `5m`, `2h30m`, etc.

## Multi-Host Routing

When your API spans multiple services, use `overrides` to route specific nodes to different base URLs:

```yaml
apiBaseUrl: https://api.example.com

overrides:
  - match: "payment*"
    baseUrl: https://payments.example.com
  - match: "inventory*"
    baseUrl: https://inventory.example.com
    auth:
      type: apikey
      headerName: X-Inventory-Key
      credentials:
        key:
          source: env
          var: INVENTORY_KEY
  - match: "notifications*"
    baseUrl: https://notify.internal.example.com
    auth:
      type: none
    headers:
      X-Internal-Caller: aat
```

Each override matches node names using glob patterns. When a node matches:

- **`baseUrl`** — replaces the top-level `apiBaseUrl`. If omitted, inherits the top-level base URL.
- **`auth`** — replaces the top-level auth for that node. If omitted, inherits the top-level auth.
- **`headers`** — merged with the environment-level headers (override-specific headers win on conflict).

Overrides are evaluated in order. For nodes matching multiple patterns, the last match wins.

### Path Rewriting

Overrides can rewrite URL paths when the target service uses a different path structure:

```yaml
overrides:
  - match: "catalog*"
    baseUrl: https://catalog.example.com
    pathRewrite:
      strip: /api/v2
      prefix: /v1
```

With this config, a template path of `/api/v2/products/{{productId}}` becomes `/v1/products/{{productId}}` when routed to the catalog service.

| Field | Description |
|-------|-------------|
| `strip` | Prefix to remove from the template path |
| `prefix` | Prefix to add after stripping |

Both fields are optional — you can strip without adding, add without stripping, or do both.

### Runtime Overrides

Two mechanisms let you adjust routing without editing the environment file:

**`--override` flag** — routes a specific node to a different URL:

```bash
aat run plan checkout.yaml --override createPayment=https://sandbox.payments.example.com
```

This flag is repeatable for multiple overrides.

**`--env-overlay` flag** — merges a sparse overlay file on top of the base environment:

```bash
aat run plan checkout.yaml --env-overlay local-routing.yaml
```

See the Overlay Files section below.

## Overlay Files

An overlay file is a sparse YAML document containing only `overrides`. It merges with (appends to) the base environment's overrides:

```yaml
# local-routing.yaml
overrides:
  - match: "payment*"
    baseUrl: https://localhost:8081
    auth:
      type: none
```

When both the base environment and an overlay define overrides for the same match pattern, the overlay's entry appears later in the list and takes precedence (last match wins).

Overlays are useful for:
- Routing specific services to local instances during development
- Switching a subset of nodes to a sandbox environment
- Adding temporary auth overrides for testing

## Validation

`aat validate` checks environment files for structural correctness:

- `environment` name is required
- `apiBaseUrl` is required
- Auth type must be one of: `oauth2`, `apikey`, `bearer`, `none`
- OAuth2 requires `tokenUrl` and all four credential fields
- API key requires `credentials.key` and `headerName`
- Bearer requires `credentials.token`
- Override entries must have a `match` pattern
- `archiveFormat` must be `json` or `json.gz`

See [Validation](validation.md) for the full reference covering all validation subcommands.

## Schema Reference

```yaml
# env.yaml — complete annotated example

environment: staging                      # required — environment name
apiBaseUrl: https://api.staging.example.com  # required — default base URL

auth:                                     # authentication configuration
  type: oauth2                            #   oauth2 | apikey | bearer | none
  tokenUrl: https://auth.example.com/token  #   token endpoint (oauth2 only)
  headerName: X-API-Key                   #   custom header name (apikey only)
  credentials:                            #   named credential fields
    username:                             #     oauth2: username, password, clientId, clientSecret
      source: env                         #     source: env (recommended) or literal
      var: API_USERNAME                   #     env var name (when source=env)
    password:
      source: env
      var: API_PASSWORD
    clientId:
      source: env
      var: API_CLIENT_ID
    clientSecret:
      source: env
      var: API_CLIENT_SECRET
    key:                                  #     apikey: key
      source: env
      var: API_KEY
    token:                                #     bearer: token
      source: env
      var: BEARER_TOKEN

headers:                                  # optional — static headers on every request
  Accept: application/json
  X-Client-Id: aat

llm:                                      # LLM configuration (for aat prompt)
  endpoint: https://api.openai.com/v1/chat/completions
  apiKey:
    source: env
    var: OPENAI_API_KEY
  model: gpt-4o
  provider: openai                        # optional — auto-detected from endpoint

settings:                                 # optional — runtime defaults
  maxRunDuration: 120s                    #   max plan execution time (default: 120s)
  defaultRetries: 2                       #   plan-level retries (default: 2)
  archiveFormat: json                     #   json or json.gz (default: json)

notes: "Staging environment for QA"       # optional — freeform notes

overrides:                                # optional — per-node routing overrides
  - match: "payment*"                     #   glob pattern matching node names
    baseUrl: https://payments.example.com #   override base URL (optional)
    auth:                                 #   override auth (optional, inherits top-level)
      type: apikey
      headerName: X-Payment-Key
      credentials:
        key:
          source: env
          var: PAYMENT_KEY
    headers:                              #   additional headers (merged with env headers)
      X-Payment-Version: "3"
    pathRewrite:                          #   optional URL path rewriting
      strip: /api/v2                      #     prefix to remove
      prefix: /v1                         #     prefix to add
```

---

*Source: `config/environment.go`, `config/auth.go`, `config/auth_provider.go`, `config/load.go`.*
