# Environments

The environment file configures how AAT connects to your API at runtime — base URL, authentication, headers, LLM settings, and multi-host routing.

## Overview

AAT separates **what to test** (graph, plans, templates) from **where to test** (environment). The environment file holds connection details for a specific target: API endpoints, credentials, static headers, and runtime settings. Switching between development, staging, and production is a matter of selecting a different environment via `--env` or the [project manifest](project-setup.md).

AAT supports two environment file formats:

- **Single-environment** (legacy) — one environment per file, selected by pointing `--env-config` (or the manifest's `environment:`) at the file
- **Multi-environment** — multiple environments in one file, selected with `--env`

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
    oasValidation: auto

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

Select an environment with `--env`:

```bash
aat run plan checkout.yaml --env staging
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

When extending, the child environment's fields are merged on top of the resolved parent using the same rules as shared config. Child overrides are appended after the parent's, so a child entry wins over an inherited entry of the same kind for the same node (the last registered match wins).

Inheritance chains are supported (`a` extends `b` extends `c`). Circular inheritance is detected and rejected.

### Abstract Environments

Environment names starting with `_` (underscore) are abstract — they serve as templates for inheritance but cannot be selected directly with `--env`. This is useful for defining override patterns that are shared across similar environments.

### Variable Substitution

The `vars` map enables parameterized environments. `${var_name}` placeholders in every string of the environment are replaced from the merged vars after inheritance and shared config resolution — base URLs, headers, auth fields and credentials, override entries (including their auth, values, and `expectFailure` descriptions), LLM settings, and `settings`. Map keys, such as header names, are not substituted, and the `vars` map itself is not:

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

#### Setting vars from the command line

`--var KEY=VALUE` (repeatable) sets a var for one invocation and wins over the vars the file declares or inherits. It is accepted by `aat run plan`, `aat run batch`, `aat prompt`, `aat validate`, `aat env list`, and `aat mcp serve`, and applies to multi-environment files only. Use it to point a project at a different host without editing the file — for example the shop example against a sandbox in Docker or on other ports:

```bash
aat run plan smoke --var apiHost=localhost:9765 --var payHost=localhost:9766
```

A key that the file never declares or references with `${key}` is an error, so a typo does not silently change nothing.

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

1. **`--env` flag** (highest priority)
2. **`AAT_ENV_NAME` environment variable**
3. **`environment:` in an overlay file** — an explicit `--overlay` file first, then an auto-discovered `.aat-overrides.yaml` (see [Selecting the Environment](#selecting-the-environment))
4. **`defaultEnvironment` in the project manifest**, for the environment file the manifest names (not for one given with `--env-config`)
5. Error listing available environments

A single-environment file has no names to choose from. `--env` is an error for it, and the other sources are ignored, so `AAT_ENV_NAME` or a manifest's `defaultEnvironment` does not stop `--env-config` from loading one.

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

#### Client Credentials

`grantType: client_credentials` works, but AAT still requires `username` and `password` for every `oauth2` configuration and sends them in the token request. Give them empty literal values, which the shop sandbox's token endpoint (like most) ignores for this grant:

```yaml
auth:
  type: oauth2
  tokenUrl: https://auth.example.com/oauth/token
  grantType: client_credentials
  credentials:
    clientId:
      source: env
      var: API_CLIENT_ID
    clientSecret:
      source: env
      var: API_CLIENT_SECRET
    username: {source: literal, value: ""}   # required by AAT, unused by this grant
    password: {source: literal, value: ""}
```

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

Static headers added to every request. These form the base layer — every other header source can override them:

```yaml
headers:
  Accept: application/json
  X-Client-Id: aat-test-runner
  X-Request-Source: automated-testing
```

Header merge order. A later value replaces an earlier one with the same name, whatever the case of the name:

1. **Environment headers** — this `headers` section
2. **Plan headers** — the plan's top-level `headers` (see [Plans](plans.md#plan-level-auth-and-headers))
3. **Template headers** — per-template `request.headers` (see [Templates](templates.md#header-merge-order))
4. **Auth credential** — `Authorization: Bearer …`, or the API key header, from the effective auth
5. **`.aat-overrides.yaml` headers** — its top-level `headers`
6. **`--overlay` headers** — the overlay file's top-level `headers`

A plan or template header therefore cannot replace the credential, and an overlay header replaces everything before it.

A node matched by an override that routes it (one that sets `baseUrl`, `auth`, `headers`, or `pathRewrite`, or a `--override` flag) takes the same environment, plan, and template headers. If the override declares its own `auth`, the inherited credential is dropped. Then come the override's `headers`, the credential of its effective auth, and the overlay headers, in that order. A template header cannot replace the override's headers either.

## Values

The `values` map holds per-environment data such as a region's postal code. Plan values, graph input defaults, and layers read it with a `{{env.KEY}}` expression, which checks the OS environment variable `KEY` first and then this map:

```yaml
values:
  postalCode: "78701"
```

```yaml
# graph.yaml — a node input
- name: postalCode
  type: string
  default: "{{env.postalCode}}"
```

Values do not fill template placeholders directly: a template's `{{postalCode}}` resolves only from the step's inputs, so route an environment value through an input as above. See [Value Resolution: Environment Variables](value-flow.md#environment-variables).

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

Run archives redact request and response headers by name: `Authorization`, `Proxy-Authorization`, `X-API-Key`, `X-Auth-Token`, `Cookie`, and `Set-Cookie` values become `[REDACTED]`. AAT also collects the resolved values of every secret credential that can apply to a run — the environment's and its host overrides' auth, the plan's auth, overlay auth, and the LLM API key; not the oauth2 `username` or `clientId` — and redacts them from every string in the archive, bodies and URLs included. A secret shorter than eight characters is redacted only where a whole value equals it. Tokens an API issues at run time are not known secrets outside credential headers, so review an archive before sharing it. See [Archives: What Is Redacted, and What Is Not](archives.md#what-is-redacted-and-what-is-not).

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
  oasValidation: auto
  minRequestInterval: 250ms
```

| Field | Default | Description |
|-------|---------|-------------|
| `oasValidation` | `auto` | OpenAPI validation mode: `auto`, `strict`, or `off` |
| `minRequestInterval` | *(none)* | Least time between the starts of two requests, such as `250ms` or `1s`; see [Request Pacing](#request-pacing) |

Retries are not an environment setting: set them per step with `retry:` (see [Plans: Retry](plans.md#retry)) or per run with `--retries` (see [Running Tests: Retries](running.md#retries)).

### Request Pacing

An API with a rate limit rejects requests that arrive too fast, often with `429 Too Many Requests`. `minRequestInterval` spaces the starts of requests at least that far apart, so a run stays under the limit instead of recovering from it:

```yaml
shared:
  settings:
    minRequestInterval: 250ms   # at most four requests a second
```

- **One interval per command.** Everything one command sends shares it: every plan of `aat run batch`, including plans running side by side with `--parallel`, and every retry, verification step, and cleanup step. `--parallel 4` with `250ms` still sends at most four requests a second.
- **Every way of running a plan** honors it: `aat run plan`, `aat run batch`, `aat prompt`, and the MCP server's `execute_plan`. The MCP server paces across calls for as long as it runs.
- **Waits count toward durations.** A step's duration includes the time it waited for its turn, as it includes retry waits.
- **OAuth2 token requests are not paced.**
- **The value is a duration with a unit** (`ms`, `s`, `m`):
  - A bare number such as `250` is rejected when the file loads.
  - Empty or `0s` turns pacing off.
  - Like any string in the file, it can come from a var: `minRequestInterval: ${pace}` with `--var pace=1s`.

### OAS Validation Mode

When the graph references an OpenAPI spec, every step's request and response is checked against it at runtime. `oasValidation` sets the per-environment default:

| Value | Behavior |
|-------|----------|
| `auto` | Validate whenever specs are present; violations are reported as warnings (default) |
| `strict` | Like `auto`, but a violation in the request or response fails the step (cleanup still runs); `expectFailure` steps are exempt |
| `off` | Skip loading specs and validating entirely |

The `--oas-validate` flag on `aat run plan`, `aat run batch`, and `aat prompt` overrides the environment setting for a single invocation (CLI flag > `settings.oasValidation` > `auto`). Any other value is rejected, in the environment file when it loads and on the command line, so a typo such as `stirct` cannot quietly mean `auto`. Turning it `off` in a busy environment saves the spec-loading time; keeping it on surfaces contract drift as `OAS: N warning(s)` markers and an `issues` count in the archive. See [Running Tests: OAS Validation](running.md#oas-validation) and [API Graphs: OAS Validation](graphs.md#oas-validation).

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
- **`auth`** — replaces the top-level auth for that node. The top-level credential (the `Authorization` header, or the top-level API key header) is dropped first, so it is never sent to the override's host. If omitted, inherits the top-level auth.
- **`headers`** — merged over the run's headers (environment, plan, and overlay headers); override-specific headers win on conflict, and the credential is set again after them (see [Custom Headers](#custom-headers)).

An entry that sets none of `baseUrl`, `auth`, `headers`, or `pathRewrite` — only `values:` or `expectFailure:` — does not change routing: the node keeps the route that a broader glob or the top-level configuration gives it. An overlay can therefore turn `paymentCharge` into a negative test without pulling it off a `payment*` route.

Overrides are matched against the node name with two rules: an exact name always beats a glob pattern, and within each kind (exact or glob) the **last registered** entry wins. Entries register in this order — `env.yaml` `overrides:`, then `.aat-overrides.yaml`, then the `--overlay` file, then `--override` flags — so a later source overrides an earlier one for the same node, whether both are globs or both are exact names. See [Local Development: Priority Chain](local-dev.md#priority-chain).

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

This flag is repeatable for multiple overrides. Each one behaves exactly like an entry `- match: NODE` with `baseUrl: URL`: the request keeps the environment headers, the plan headers, overlay headers, and the credential of the effective auth.

**`--overlay` flag** — merges a sparse overlay file on top of the base environment:

```bash
aat run plan checkout.yaml --overlay local-routing.yaml
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

When both the base environment and an overlay define overrides that match the same node, the overlay's entry registers later and takes precedence: exact names beat globs, and among entries of the same kind the last registered wins. Registration order is `env.yaml` `overrides:` → `.aat-overrides.yaml` → `--overlay` → `--override`.

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

- `values:` merge into the resolved inputs map at step execution time, overwriting plan-supplied values. Precedence: overlay values > plan step values > graph defaults. They are used exactly as written: `{{...}}` expressions such as `{{today}}` are not evaluated, and the input's graph type is not applied. The archive records each one as the input's resolution, with the source `override_value`, so the decision trail shows the value that was sent.
- `expectFailure:` applies to matched steps only when the plan step doesn't already declare its own `expectFailure`. Status codes must all be `>= 400`.
- Match precedence: exact matches win over glob matches on key conflicts, and later registrations overwrite earlier ones (`env.yaml` → `.aat-overrides.yaml` → `--overlay` → `--override`). For `expectFailure`, the last exact match wins; if no exact match, the last glob match wins.

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
3. `--overlay` file `auth:` — explicit overlay
4. Plan-level `auth:` — per-plan override

### Transaction-Level Headers

A top-level `headers` map in an overlay is merged into every request. These headers take precedence over environment-level headers, plan headers, and the auth credential (template headers still come after them; see [Custom Headers](#custom-headers)), making them useful for injecting access-group tokens, correlation IDs, or other cross-cutting headers:

```yaml
# overlay with transaction-level headers
headers:
  X-Access-Group: my-access-group-id
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
5. `defaultEnvironment` from the project manifest, when the run uses the environment file the manifest names

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

`aat validate` checks environment files for structural correctness. A key that no field accepts, such as a misspelled `apiBaseURL`, is an error naming the file, the line, and the likely intended key.

**Single-environment files:**

- `environment` name is required
- `apiBaseUrl` is required
- Auth type must be one of: `oauth2`, `apikey`, `bearer`, `none`
- OAuth2 requires `tokenUrl` and all four credential fields
- API key requires `credentials.key` and `headerName`
- Bearer requires `credentials.token`
- Override entries must have a `match` pattern
- `settings.oasValidation` must be `auto`, `strict`, or `off`
- `settings.minRequestInterval` must be a duration with a unit, such as `250ms`

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
  oasValidation: auto                     #   auto, strict, or off (default: auto)
  minRequestInterval: 250ms               #   least time between request starts (default: none)

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
    oasValidation: auto
    minRequestInterval: 250ms
  values:
    region: us-east

environments:
  dev:                                    # selectable with --env dev
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
