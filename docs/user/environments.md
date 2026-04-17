# Environments

The environment file configures how AAT connects to your API at runtime — base URL, authentication, headers, LLM settings, and multi-host routing.

## Overview

AAT separates **what to test** (graph, plans, templates) from **where to test** (environment). The environment file holds connection details for a specific target: API endpoints, credentials, static headers, and runtime settings. Switching between development, staging, and production is a matter of selecting a different environment via `--env-name` or the [project manifest](project-setup.md).

AAT supports two environment file formats:
- **Single-environment** (legacy) — one environment per file, selected with `--env`
- **Multi-environment** — multiple environments in one file, selected with `--env-name`

## Single-Environment Format

The simplest format is a YAML document with a top-level `environment` name and `apiBaseUrl`:

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

## Multi-Environment Format

When your project targets multiple environments (dev, staging, production, etc.), use the multi-environment format to define them all in one file. This eliminates duplication of shared config like headers, LLM settings, and runtime defaults.

```yaml
shared:
  headers:
    Accept: application/json
  llm:
    endpoint: https://api.openai.com/v1
    apiKey:
      source: env
      var: OPENAI_API_KEY
    model: gpt-4
  settings:
    maxRunDuration: 5m

environments:
  dev:
    apiBaseUrl: https://api.dev.example.com
    auth:
      type: none

  staging:
    apiBaseUrl: https://api.staging.example.com
    auth:
      type: apikey
      headerName: X-API-Key
      credentials:
        key:
          source: env
          var: STAGING_API_KEY

  prod:
    apiBaseUrl: https://api.example.com
    auth:
      type: oauth2
      tokenUrl: https://auth.example.com/oauth/token
      credentials:
        username:
          source: env
          var: PROD_USERNAME
        password:
          source: env
          var: PROD_PASSWORD
        clientId:
          source: env
          var: PROD_CLIENT_ID
        clientSecret:
          source: env
          var: PROD_CLIENT_SECRET
```

Select an environment with `--env-name`:

```bash
aat run plan checkout.yaml --env-name staging
```

AAT detects the format automatically: if the YAML has an `environments` key, it's multi-environment; if it has `apiBaseUrl` at the top level, it's single-environment.

### Shared Config

The `shared` section provides defaults that merge into every environment. Per-environment fields override shared fields:

- **`headers`**, **`values`**, **`vars`** — map merge (environment keys win, shared keys preserved)
- **`auth`**, **`llm`** — full replace (if the environment specifies auth, it replaces shared auth entirely)
- **`settings`** — field-level merge (environment can override individual settings fields)

### Inheritance with `extends`

Environments can inherit from other environments using `extends`. This eliminates duplication when multiple environments share the same structure:

```yaml
environments:
  _direct:
    auth:
      type: none
    overrides:
      - match: "search*"
        baseUrl: https://search-${env_tag}.internal.example.com
      - match: "price*"
        baseUrl: https://price-${env_tag}.internal.example.com

  dev:
    extends: _direct
    vars:
      env_tag: dev

  staging:
    extends: _direct
    vars:
      env_tag: staging
    auth:
      type: bearer
      credentials:
        token:
          source: env
          var: STAGING_TOKEN
```

When extending, the child environment's fields are merged on top of the resolved parent using the same rules as shared config. Child overrides are prepended before parent overrides (child has higher match priority).

Inheritance chains are supported (`a` extends `b` extends `c`). Circular inheritance is detected and rejected.

### Abstract Environments

Environment names starting with `_` (underscore) are abstract — they serve as templates for inheritance but cannot be selected directly with `--env-name`. This is useful for defining override patterns that are shared across similar environments.

### Variable Substitution

The `vars` map enables parameterized environments. `${var_name}` placeholders in any string field are replaced from the merged vars after inheritance and shared config resolution:

```yaml
environments:
  _base:
    overrides:
      - match: "api*"
        baseUrl: https://${service}.${region}.example.com

  us-east:
    extends: _base
    vars:
      service: api
      region: us-east-1
```

Unresolved `${...}` placeholders after substitution are a validation error.

### File Splitting with `include`

The `include` directive lets you split a multi-environment file into parts — for example, keeping secrets separate from committable structure:

```yaml
# env.yaml (committed to git)
include:
  - env.secrets.yaml

shared:
  headers:
    Accept: application/json

environments:
  dev:
    apiBaseUrl: https://api.dev.example.com
  prod:
    apiBaseUrl: https://api.example.com
```

```yaml
# env.secrets.yaml (gitignored)
environments:
  dev:
    auth:
      type: none
  prod:
    auth:
      type: oauth2
      tokenUrl: https://auth.example.com/oauth/token
      credentials:
        username:
          source: env
          var: PROD_USERNAME
        password:
          source: env
          var: PROD_PASSWORD
        clientId:
          source: env
          var: PROD_CLIENT_ID
        clientSecret:
          source: env
          var: PROD_CLIENT_SECRET
```

Include files use the same format (`shared` + `environments` sections) and are merged into the base file. Paths are resolved relative to the base file's directory. Multiple includes are processed in order. Recursive includes (an include file with its own `include`) are not allowed.

### Environment Name Resolution

When using a multi-environment file, the environment name is resolved from:

1. **`--env-name` flag** (highest priority)
2. **`AAT_ENV_NAME` environment variable**
3. **`defaultEnvironment` in the project manifest**
4. Error listing available environments

### Listing Environments

Use `aat env list` to see available environments:

```bash
$ aat env list
  dev          https://api.dev.example.com
  prod         https://api.example.com
  staging      https://api.staging.example.com
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

#### Custom Grant Type

By default AAT sends `grant_type=password`. Some OAuth2 providers (e.g., Auth0) require a different grant type and additional parameters. Use `grantType` and `extraParams` to customize the token request:

```yaml
auth:
  type: oauth2
  tokenUrl: https://auth.example.com/oauth/token
  grantType: "http://auth0.com/oauth/grant-type/password-realm"
  extraParams:
    realm: my-realm
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

| Field | Default | Description |
|-------|---------|-------------|
| `grantType` | `password` | OAuth2 `grant_type` form parameter |
| `extraParams` | _(empty)_ | Additional key-value pairs appended to the token request form |

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
4. **Auth headers** — authentication headers
5. **Overlay headers** — headers from `.aat-overrides.yaml` or `--env-overlay` (final precedence)

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

**`.aat-overrides.yaml` dotfile** — auto-discovered by walking up from your working directory. Same format as an overlay file, but requires no flags. Ideal for [local development](local-dev.md) where you always want traffic routed to your local service. Use `--no-auto-overrides` to disable.

See the Overlay Files section below.

## Overlay Files

An overlay file is a sparse YAML document that merges with the base environment. It can contain per-node `overrides` (same format as the environment `overrides:` section) and optional **transaction-level** `auth` and `headers` that apply to every API call in the run.

### Per-Node Overrides

The `overrides:` section works the same as in an environment file — each entry matches node names by glob and can override `baseUrl`, `auth`, `headers`, `pathRewrite`, input `values`, and `expectFailure` for matched nodes only:

```yaml
# local-routing.yaml
overrides:
  - match: "payment*"
    baseUrl: https://localhost:8081
    auth:
      type: none
```

When both the base environment and an overlay define overrides for the same match pattern, the overlay's entry appears later in the list and takes precedence (last match wins).

### Input-Value and Expected-Failure Overrides

An override can also inject specific input values and declare that a matched
step is *expected* to fail. This is the primitive for authoring depth/error
tests without editing a plan — run the existing happy-path plan, but an
overlay forces the targeted node to receive a malformed value and pass only
when it fails with the declared status:

```yaml
overrides:
  - match: createBooking
    values:
      passengerAge: -1
      lastName: ""
    expectFailure:
      status: [400, 422]
      description: "invalid payload"
```

Semantics:

- `values:` merge into the resolved inputs map at step execution time, overwriting plan-supplied values. Precedence: overlay values > plan step values > graph defaults.
- `expectFailure:` applies to matched steps only when the plan step doesn't already declare its own `expectFailure`. Status codes must all be `>= 400`.
- Match precedence: exact matches win over glob matches on key conflicts. For `expectFailure`, the first exact match wins; if no exact match, the first glob match wins.

Both fields can be combined with `baseUrl`, `auth`, `headers`, and `pathRewrite` in a single override entry.

### Transaction-Level Auth

A top-level `auth` field in an overlay replaces the environment's auth for the **entire run** — all nodes, not just those matching an override pattern. This is useful when the target environment requires different credentials than the base environment:

```yaml
# overlay with transaction-level auth
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
  - match: "payment*"
    baseUrl: https://localhost:8081
```

In this example, all nodes authenticate with the staging OAuth2 credentials, but payment nodes are routed to localhost. The auth configuration supports the same types and fields as an environment's `auth` section (`oauth2`, `apikey`, `bearer`, `none`).

Auth priority (lowest to highest):

1. `env.yaml` `auth:` — base environment credentials
2. `.aat-overrides.yaml` `auth:` — auto-discovered overlay
3. `--env-overlay` file `auth:` — explicit overlay
4. Plan-level `auth:` — per-plan override

### Transaction-Level Headers

A top-level `headers` map in an overlay is merged into every request. These headers take precedence over environment-level headers and auth headers, making them useful for injecting access-group tokens, correlation IDs, or other cross-cutting headers:

```yaml
# overlay with transaction-level headers
headers:
  XAUTH_TRAVELPORT_ACCESSGROUP: CD87751C-AD46-4EDB-9F53-7B0DE72D751E
  X-Correlation-Id: local-dev-session

overrides:
  - match: placeOnQueue
    baseUrl: http://localhost:8080
```

### Selecting the Environment

A top-level `environment:` field in an overlay selects which named environment the run should target. This is primarily useful for local development: if your overlay routes traffic to a locally-running service, and that local service talks to a different backend than the project default, the overlay can name the backend so you don't have to pass `--env` every time:

```yaml
# .aat-overrides.yaml — local-dev overlay
environment: dev             # backend the local service talks to
overrides:
  - match: "*"
    baseUrl: http://localhost:8080
```

Environment-name priority (highest to lowest):

1. `--env` CLI flag
2. `AAT_ENV_NAME` environment variable
3. `--overlay` file `environment:` — explicit overlay
4. `.aat-overrides.yaml` `environment:` — auto-discovered overlay
5. `defaultEnvironment` from the project manifest

Explicit CLI choices always win, so the overlay's `environment:` behaves as a smart default — it kicks in when no env is specified, and is silently deferred when one is. Combine with `--no-auto-overrides` to skip auto-discovery entirely.

### Combining Auth, Headers, and Overrides

All three sections can appear in a single overlay file:

```yaml
auth:
  type: oauth2
  tokenUrl: https://auth.example.com/token
  grantType: "http://auth0.com/oauth/grant-type/password-realm"
  extraParams:
    realm: my-realm
  credentials:
    username:
      source: env
      var: MY_USERNAME
    password:
      source: env
      var: MY_PASSWORD
    clientId:
      source: env
      var: MY_CLIENT_ID
    clientSecret:
      source: env
      var: MY_CLIENT_SECRET

headers:
  X-Access-Group: my-access-group-id

overrides:
  - match: myService
    baseUrl: http://localhost:3000
    pathRewrite:
      strip: /api/v2
      prefix: /v1
```

Overlays are useful for:
- Routing specific services to local instances during development
- Switching a subset of nodes to a sandbox environment
- Replacing auth credentials for an entire test run
- Injecting transaction-level headers across all API calls

## Validation

`aat validate` checks environment files for structural correctness.

**Single-environment files:**
- `environment` name is required
- `apiBaseUrl` is required
- Auth type must be one of: `oauth2`, `apikey`, `bearer`, `none`
- OAuth2 requires `tokenUrl` and all four credential fields
- API key requires `credentials.key` and `headerName`
- Bearer requires `credentials.token`
- Override entries must have a `match` pattern
- `archiveFormat` must be `json` or `json.gz`

**Multi-environment files** — all the above, plus:
- All `extends` targets must exist
- No circular inheritance chains
- All `${var}` references must resolve after merging
- Abstract environments (underscore prefix) cannot be the `defaultEnvironment`
- Each non-abstract environment must produce a valid configuration after resolution
- `apiBaseUrl` is not required (environments may route entirely through overrides)

See [Validation](validation.md) for the full reference covering all validation subcommands.

## Schema Reference

### Single-Environment Format

```yaml
# env.yaml — single-environment annotated example

environment: staging                      # required — environment name
apiBaseUrl: https://api.staging.example.com  # required — default base URL

auth:                                     # authentication configuration
  type: oauth2                            #   oauth2 | apikey | bearer | none
  tokenUrl: https://auth.example.com/token  #   token endpoint (oauth2 only)
  headerName: X-API-Key                   #   custom header name (apikey only)
  grantType: password                     #   oauth2 grant_type (default: "password")
  extraParams:                            #   extra form params for oauth2 token request
    realm: my-realm                       #     example: Auth0 realm
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
    values:                               #   optional — input value overrides for matched steps
      amount: 0                           #     each key is an input name on the matched node
      currency: "XYZ"
    expectFailure:                        #   optional — flip matched steps to negative-test mode
      status: [400, 422]                  #     all entries must be >= 400
      description: "invalid payment"

values:                                   # optional — key-value pairs for {{env.KEY}}
  region: us-east
```

### Multi-Environment Format

```yaml
# env.yaml — multi-environment annotated example

include:                                  # optional — additional files to merge
  - env.secrets.yaml                      #   resolved relative to this file

shared:                                   # optional — defaults merged into every environment
  headers:
    Accept: application/json
  llm:
    endpoint: https://api.openai.com/v1
    apiKey:
      source: env
      var: OPENAI_API_KEY
    model: gpt-4
  settings:
    maxRunDuration: 5m
    defaultRetries: 2
  values:
    region: us-east

environments:
  dev:                                    # selectable with --env-name dev
    apiBaseUrl: https://api.dev.example.com
    auth:
      type: none

  _base-direct:                           # abstract (underscore) — not directly selectable
    auth:
      type: none
    overrides:
      - match: "search*"
        baseUrl: https://search-${env_tag}.internal.example.com

  staging:
    extends: _base-direct                 # inherits overrides and auth from _base-direct
    vars:                                 # ${env_tag} replaced in inherited strings
      env_tag: staging

  prod:
    apiBaseUrl: https://api.example.com
    auth:
      type: oauth2
      tokenUrl: https://auth.example.com/oauth/token
      credentials:
        username:
          source: env
          var: PROD_USERNAME
        password:
          source: env
          var: PROD_PASSWORD
        clientId:
          source: env
          var: PROD_CLIENT_ID
        clientSecret:
          source: env
          var: PROD_CLIENT_SECRET
```

---

*Source: `config/environment.go`, `config/multi_env.go`, `config/auth.go`, `config/auth_provider.go`, `config/load.go`.*
