# Environments

An environment file tells AAT how to reach your API: where it lives, how to authenticate, and how the engine should behave. Each environment is a single YAML file.

## Quick Start

Create a file called `test.yaml`:

```yaml
environment: test
apiBaseUrl: https://api.example.com

auth:
  type: bearer
  credentials:
    token:
      source: env
      var: API_TOKEN
```

Run a plan against it:

```
aat run --plan plans/smoke.yaml --env test.yaml
```

The `--env` flag takes a direct file path to the environment YAML file.

## File Structure

```yaml
environment: <name>          # required — identifies this environment
apiBaseUrl: <url>            # required — base URL for all API calls

auth:                        # required — how to authenticate
  type: <oauth2|apikey|bearer|none>
  # ... type-specific fields (see below)

headers:                     # optional — static headers added to every request
  X-Custom-Header: value

llm:                         # optional — LLM provider config
  endpoint: <url>
  apiKey:
    source: env
    var: LLM_API_KEY
  model: gpt-5.2
  mode: <strict|lean|adaptive>

overrides:                   # optional — route specific nodes to different servers
  - match: <pattern>
    baseUrl: <url>
    # auth, headers, pathRewrite — see Overrides section

settings:                    # optional — all have defaults
  maxRunDuration: 120s
  defaultRetries: 2
  maxRelaxationDepth: 3
  archiveFormat: json

notes: Free-text notes.      # optional
```

## Authentication

The `auth` section controls how AAT authenticates API requests. Four types are supported.

### `oauth2`

Performs an OAuth2 Resource Owner Password Credentials (ROPC) token exchange. The resulting Bearer token is sent on every API request.

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

**Required fields:** `tokenUrl`, and credentials for `username`, `password`, `clientId`, `clientSecret`.

### `apikey`

Sends a static API key in a custom header.

```yaml
auth:
  type: apikey
  headerName: X-Api-Key
  credentials:
    key:
      source: env
      var: API_KEY
```

**Required fields:** `headerName`, and credentials for `key`.

### `bearer`

Sends a static Bearer token in the `Authorization` header. Use this when you already have a token and don't need AAT to perform a token exchange.

```yaml
auth:
  type: bearer
  credentials:
    token:
      source: env
      var: API_TOKEN
```

**Required fields:** credentials for `token`.

### `none`

No authentication. Useful for public APIs or when auth is handled externally.

```yaml
auth:
  type: none
```

## Custom Headers

The `headers` section adds static headers to every API request. These are applied before authentication headers, so auth headers take precedence if there's a conflict.

```yaml
headers:
  XAUTH_TRAVELPORT_ACCESSGROUP: "36CFECCB-9A27-4A78-8B4D-7272F3830C20"
  Accept: application/json
  Accept-Version: "11"
  Content-Version: "11"
```

Use this for API gateway headers, version headers, access group identifiers, or any other headers your API requires on every request.

## Secrets

Credentials are never stored as plain values in the YAML (though you can for local testing). Each credential is a `SecretRef` with two resolution strategies:

### Environment variable

```yaml
password:
  source: env
  var: API_PASSWORD
```

Reads the value from the `API_PASSWORD` environment variable at runtime. Fails with a clear error if the variable is not set.

### Literal

```yaml
password:
  source: literal
  value: my-password
```

Uses the value directly. Fine for local development; avoid committing literal secrets to version control.

## LLM Configuration

The `llm` section is optional. It configures the LLM provider used for plan generation and adaptive execution.

```yaml
llm:
  endpoint: https://api.openai.com/v1
  apiKey:
    source: env
    var: OPENAI_API_KEY
  model: gpt-5.2
  mode: lean
```

### Execution modes

| Mode | Behavior |
|------|----------|
| `strict` | No LLM involvement during execution. Plans must be fully specified. |
| `lean` | LLM assists only when the engine cannot resolve a value deterministically (default + fallback pool exhausted). |
| `adaptive` | Same as `lean`, plus step-level recovery: relaxes soft constraints and retries steps on HTTP 4xx errors. See [value-flow.md](value-flow.md#soft-constraint-relaxation). |

Default: `lean`.

## Runtime Settings

All settings have defaults and can be omitted entirely.

```yaml
settings:
  maxRunDuration: 120s
  defaultRetries: 2
  maxRelaxationDepth: 3
  archiveFormat: json
```

| Setting | Default | Description |
|---------|---------|-------------|
| `maxRunDuration` | `120s` | Maximum wall-clock time for a single plan run. Accepts Go duration strings: `30s`, `5m`, `2h30m`. |
| `defaultRetries` | `2` | Default retry count for steps that don't specify their own. |
| `maxRelaxationDepth` | `3` | Maximum constraint relaxation depth before giving up. |
| `archiveFormat` | `json` | Archive output format. `json` for readable files, `json.gz` for compressed. |

## Minimal Example

The smallest valid environment file:

```yaml
environment: local
apiBaseUrl: https://localhost:8080
auth:
  type: none
```

Everything else gets defaults: lean mode, 120s timeout, 2 retries, JSON archives.

## Full Example

```yaml
environment: staging
apiBaseUrl: https://staging-api.example.com

auth:
  type: oauth2
  tokenUrl: https://auth.example.com/oauth/token
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

headers:
  X-Api-Version: "2"
  X-Tenant-Id: acme-corp

llm:
  endpoint: https://api.anthropic.com/v1
  apiKey:
    source: env
    var: ANTHROPIC_API_KEY
  model: claude-sonnet-4-5-20250929
  mode: adaptive

settings:
  maxRunDuration: 5m
  defaultRetries: 3
  maxRelaxationDepth: 5
  archiveFormat: json.gz

overrides:                         # optional — route specific nodes differently
  - match: searchFlights
    baseUrl: http://localhost:8080
    auth: { type: none }

notes: |
  Staging environment for pre-release testing.
  Requires VPN access.
```

## Overrides (Multi-Host Routing)

By default, AAT sends every API request to the single `apiBaseUrl`. The `overrides` section lets you route specific nodes to different servers -- with independent auth, headers, and path rewriting. This is essential for local development, mixed-environment testing, and handling infrastructure differences.

### Basic example

Route one node to localhost while everything else hits the real environment:

```yaml
environment: staging
apiBaseUrl: https://api.staging.example.com

auth:
  type: oauth2
  tokenUrl: https://auth.example.com/oauth/token
  credentials:
    username: { source: env, var: API_USERNAME }
    password: { source: env, var: API_PASSWORD }
    clientId: { source: env, var: API_CLIENT_ID }
    clientSecret: { source: env, var: API_CLIENT_SECRET }

overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
    auth:
      type: none
```

Now `searchFlights` calls `http://localhost:8080` with no auth, while `priceOffer`, `addOffer`, and every other node still goes to `https://api.staging.example.com` with the OAuth2 token.

### Override fields

Each override entry supports these fields:

```yaml
overrides:
  - match: <pattern>                # required — node name or glob
    baseUrl: <url>                  # optional — override base URL (inherits top-level if omitted)
    auth:                           # optional — override auth (inherits top-level if omitted)
      type: <oauth2|apikey|bearer|none>
      # ... same fields as top-level auth
    headers:                        # optional — merged with top-level headers
      X-Custom: value
    pathRewrite:                    # optional — URL path transformation
      strip: /prefix/to/remove
      prefix: /prefix/to/add
```

### Pattern matching

The `match` field supports exact node names and glob patterns:

| Pattern | Matches |
|---------|---------|
| `searchFlights` | Only the `searchFlights` node |
| `price*` | `priceOffer`, `priceTicket`, `priceBundle`, ... |
| `step?` | `step1`, `step2`, but not `step12` |
| `[abc]*` | Nodes starting with `a`, `b`, or `c` |

When multiple patterns match a node, exact matches always win over globs. Among glob patterns, the first matching entry wins.

### Auth inheritance

Overrides that **omit** the `auth` field inherit the top-level authentication. This is the most common case -- you want the same token but a different server.

```yaml
# This override uses the same OAuth2 token as the top-level auth
overrides:
  - match: "price*"
    baseUrl: https://api.staging.example.com
```

To explicitly **disable** auth for a node (e.g., a local service that doesn't need it):

```yaml
overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
    auth:
      type: none
```

### Header merge

Override headers are merged with top-level headers. The override wins on conflict:

```yaml
headers:
  Accept: application/json
  X-Version: "11"

overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
    headers:
      X-Version: "12"         # overrides the top-level "11"
      X-Debug: "true"         # added, not present at top-level
    # Accept: application/json is inherited
```

### Path rewriting

When different environments use different URL path structures, `pathRewrite` transforms the path at request time. This avoids maintaining duplicate templates.

```yaml
overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
    pathRewrite:
      strip: /11                     # remove this prefix from the template path
      prefix: /api/v2               # add this prefix instead
```

If a template produces a path like `/11/air/search`, the rewrite transforms it to `/api/v2/air/search` before making the request.

**When to use path rewriting:**
- Load balancers add version prefixes (`/11/`, `/v2/`)
- Local services use different path structures than production
- API gateways route by path prefix

### Overlay files

For persistent, shareable override sets, use overlay files with the `--env-overlay` CLI flag:

```bash
aat run --plan plan.yaml --env env.yaml --env-overlay local-dev.yaml
```

An overlay file contains only the `overrides` section:

```yaml
# local-dev.yaml
overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
    auth:
      type: none
    pathRewrite:
      strip: /11
      prefix: /api/v2
```

Overlay overrides are **appended** to any overrides in the base environment file. This means:
- The base env file defines the production routing
- Each overlay adds or extends overrides for a specific scenario
- You can share overlays across team members (e.g., `local-search.yaml`, `staging-pricing.yaml`)

### Use cases

**Local development:** Route one service to your local machine while calling real APIs for everything else.

```yaml
overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
    auth: { type: none }
```

**Mixed environments:** Test a staging service against production dependencies.

```yaml
overrides:
  - match: "price*"
    baseUrl: https://api.staging.example.com
    # inherits production auth
```

**Canary testing:** Point specific operations at a canary deployment.

```yaml
overrides:
  - match: commitBooking
    baseUrl: https://canary.api.example.com
```

**Path prefix differences:** Your local service doesn't have the load-balancer prefix.

```yaml
overrides:
  - match: searchFlights
    baseUrl: http://localhost:8080
    pathRewrite:
      strip: /11
```

**API gateway routing:** Different services behind different gateway paths.

```yaml
overrides:
  - match: "search*"
    baseUrl: https://gateway.example.com
    pathRewrite:
      prefix: /search-service

  - match: "book*"
    baseUrl: https://gateway.example.com
    pathRewrite:
      prefix: /booking-service
```

### Startup logging

When overrides are active, AAT logs them at startup:

```
aat: override: searchFlights → http://localhost:8080 (auth: none, pathRewrite: strip=/11 prefix=/api/v2)
aat: override: price* → https://api.staging.example.com (auth: inherited)
```

## Validation

AAT validates environment files on load and reports all errors at once:

```
environment validation failed:
- auth.tokenUrl is required for oauth2
- auth.credentials.clientId is required for oauth2
- auth.credentials.clientSecret is required for oauth2
```

Validation checks:
- `environment` (name) is present
- `apiBaseUrl` is present
- Auth type is one of: `oauth2`, `apikey`, `bearer`, `none`
- Required credential keys are present for the auth type
- `llm.mode` is valid if set
- `archiveFormat` is valid if set
- Override `match` patterns are non-empty
- Override auth types are valid if specified

## See Also

- [Plan-Level Auth & Headers](plan-auth.md) -- embedding per-plan credentials that override environment auth
- [Running Tests](running.md) -- executing plans with `aat run`
- [Plan Authoring](plan-authoring.md) -- full plan YAML schema reference
