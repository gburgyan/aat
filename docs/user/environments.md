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
aat run --plan plans/smoke.yaml --env test
```

The `--env` flag looks for `<name>.yaml` in the environments directory.

## File Structure

```yaml
environment: <name>          # required — identifies this environment
apiBaseUrl: <url>            # required — base URL for all API calls

auth:                        # required — how to authenticate
  type: <oauth2|apikey|bearer|none>
  # ... type-specific fields (see below)

llm:                         # optional — LLM provider config
  endpoint: <url>
  apiKey:
    source: env
    var: LLM_API_KEY
  model: gpt-4
  mode: <strict|lean|adaptive>

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
  model: gpt-4
  mode: lean
```

### Execution modes

| Mode | Behavior |
|------|----------|
| `strict` | No LLM involvement during execution. Plans must be fully specified. |
| `lean` | LLM assists only when the engine cannot resolve a value deterministically. |
| `adaptive` | LLM actively participates in value selection, error recovery, and plan adjustment. |

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

notes: |
  Staging environment for pre-release testing.
  Requires VPN access.
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
