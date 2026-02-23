# Plan-Level Auth & Headers

Plans can embed their own authentication credentials and custom headers, overriding the environment defaults. This makes regression test plans self-contained: a plan that tests "customer X can complete a booking" carries the credentials for customer X, regardless of which environment file is loaded.

## Quick Start

Add `auth:` and/or `headers:` to the top level of a plan file:

```yaml
auth:
  type: bearer
  credentials:
    token:
      source: env
      var: CUSTOMER_X_TOKEN

headers:
  X-Test-Customer: customer-x
  Content-Version: "2.0"

execution:
  steps:
    - node: searchFlights
      values:
        origin: DEN
        destination: SFO
        departureDate: "2026-04-01"
```

Run it the same way as any other plan:

```bash
aat run plan workflows/customer-x-booking.yaml
```

AAT authenticates with the plan's credentials instead of the environment's. The plan headers merge on top of environment headers.

## Schema

Two optional top-level fields on the plan, placed between `graph:` and `intent:`:

```yaml
metadata: ...       # optional
graph: ...          # optional

auth:               # optional — overrides environment auth entirely
  type: <oauth2|apikey|bearer|none>
  # ... same fields as environment auth

headers:            # optional — merges on top of environment headers
  Header-Name: value

intent: ...         # optional
execution: ...      # required
```

The `auth` block uses the exact same structure as [environment auth](environments.md#authentication). All four auth types (`oauth2`, `apikey`, `bearer`, `none`) are supported. Credentials use the same `SecretRef` pattern (`source: env` or `source: literal`).

The `headers` block is a flat `key: value` map, identical to [environment headers](environments.md#custom-headers).

## Precedence

### Auth

When multiple auth sources exist, AAT uses the **most specific** one. The first match wins:

| Source | When Used |
|--------|-----------|
| Per-node override | Override has its own `auth:` block |
| Plan `auth:` | Plan specifies `auth:`, no override for this node |
| Environment `auth:` | No plan auth, no override auth |

Plan auth **replaces** the environment auth entirely — it is not merged or combined. If a plan specifies `auth:`, that auth is used for all nodes except those with their own per-node override.

### Headers

Headers merge in layers. Later layers win on conflict:

| Source | Wins Over |
|--------|-----------|
| Per-node override `headers:` | Everything below |
| Auth-generated headers (`Authorization`, API key headers) | Plan and environment headers |
| Plan `headers:` | Environment headers |
| Environment `headers:` | (base layer) |

Plan headers **add to** the environment headers and **override** on conflict. Auth headers (e.g., `Authorization: Bearer ...`) always win over both. Per-node overrides win over everything for that specific node.

### Override inheritance

Environment overrides that **omit** their own `auth:` inherit the **effective** auth — which is the plan auth when present, not the environment auth. This means plan auth cascades naturally through the entire routing system.

```yaml
# env.yaml
overrides:
  - match: "price*"
    baseUrl: https://staging.example.com
    # no auth: block → inherits plan auth (not env auth) when plan auth is present
```

## Use Cases

### Regression testing per-customer credentials

The primary use case: each plan targets a specific customer identity.

```yaml
# workflows/customer-acme-booking.yaml
auth:
  type: oauth2
  tokenUrl: https://auth.example.com/token
  credentials:
    username:
      source: env
      var: ACME_USERNAME
    password:
      source: env
      var: ACME_PASSWORD
    clientId:
      source: literal
      value: acme-client-id
    clientSecret:
      source: env
      var: ACME_CLIENT_SECRET

headers:
  X-Customer-Id: acme-corp

intent:
  description: "Verify booking flow works for ACME Corp credentials"

execution:
  steps:
    - node: searchFlights
      values:
        origin: DEN
        destination: SFO
        departureDate: "{{today + 30 days}}"
    # ... rest of booking flow
```

Different plans can test different customers against the same environment:

```bash
aat run plan workflows/customer-acme-booking.yaml
aat run plan workflows/customer-globex-booking.yaml
```

### API version testing

Test the same flow against different API versions by setting version headers:

```yaml
# workflows/v2-search-test.yaml
headers:
  Accept-Version: "2"
  Content-Version: "2"

execution:
  steps:
    - node: searchFlights
      values:
        origin: LAX
        destination: JFK
        departureDate: "{{today + 14 days}}"
      assertions:
        mechanical:
          - type: status
            expect: 200
          - type: fieldExists
            path: "v2SpecificField"
```

### Testing with a specific API key

```yaml
# workflows/partner-api-test.yaml
auth:
  type: apikey
  headerName: X-Partner-Key
  credentials:
    key:
      source: env
      var: PARTNER_API_KEY

execution:
  steps:
    - node: partnerSearch
      values:
        query: "test item"
```

### Disabling auth for local testing

Override the environment's auth with `none` to test against a local service that doesn't require authentication:

```yaml
# workflows/local-smoke.yaml
auth:
  type: none

execution:
  steps:
    - node: healthCheck
```

### Headers without auth override

You can add plan-level headers without changing the authentication. Omit the `auth:` block and only specify `headers:`:

```yaml
# workflows/debug-trace.yaml
headers:
  X-Debug-Trace: "enabled"
  X-Request-Source: "aat-regression"

execution:
  steps:
    - node: searchFlights
      values:
        origin: DEN
        destination: SFO
        departureDate: "{{today + 7 days}}"
```

The environment auth is used as-is; the plan headers merge on top of environment headers.

## Interaction with `aat prompt`

The `aat prompt` command generates plans from natural language. The LLM pipeline **never** generates `auth:` or `headers:` blocks — authentication is an identity/deployment concern, not something the LLM should decide.

However, plan auth and headers work across three workflows:

### 1. Save, edit, then run

Generate a plan, save it, manually add auth/headers, then run it:

```bash
# Generate and save (don't execute yet)
aat prompt "Book a flight from Rome to New York" \
  --save workflows/rome-to-ny.yaml

# Edit the saved plan to add auth/headers
# (see "Manual editing" below)

# Run with the added credentials
aat run plan workflows/rome-to-ny.yaml
```

### 2. Adjust flow

During the interactive `aat prompt` session, choose `a` (adjust) to edit the plan before execution. Add `auth:` and `headers:` blocks during the edit, then press Enter to reload and execute with those credentials.

```
Execute this plan? [Y/n/a(djust)] a
aat: plan saved to /tmp/aat-plan-abc123/plan.yaml
aat: edit the file, then press Enter to reload...
```

Open the file, add the auth block at the top, save, and press Enter.

### 3. Template plans with placeholder credentials

Create plan templates with auth blocks that reference environment variables. The actual secrets live in the environment, keeping the plan file safe to commit:

```yaml
auth:
  type: oauth2
  tokenUrl: https://auth.example.com/token
  credentials:
    username:
      source: env
      var: TEST_USER_{{env.CUSTOMER_TIER}}
    password:
      source: env
      var: TEST_PASS_{{env.CUSTOMER_TIER}}
    clientId:
      source: env
      var: CLIENT_ID
    clientSecret:
      source: env
      var: CLIENT_SECRET
```

> **Note:** Expression evaluation (`{{env.VAR}}`) is supported in step values but not in auth credential `var` fields. For auth, use direct environment variable names and set the appropriate variables before running.

## Manual Editing

Adding auth and headers to a plan requires editing the YAML directly. The blocks go at the top level, typically between `graph:` and `intent:` (or before `execution:` if there's no intent block).

### Adding auth to an existing plan

Before:

```yaml
intent:
  description: "Search for flights"

execution:
  steps:
    - node: searchFlights
      values:
        origin: DEN
        destination: SFO
```

After:

```yaml
auth:
  type: bearer
  credentials:
    token:
      source: env
      var: CUSTOMER_X_TOKEN

intent:
  description: "Search for flights"

execution:
  steps:
    - node: searchFlights
      values:
        origin: DEN
        destination: SFO
```

### Adding headers to an existing plan

```yaml
headers:
  X-Test-Customer: customer-x
  Content-Version: "2.0"

intent:
  description: "Search for flights"

execution:
  steps:
    - node: searchFlights
      values:
        origin: DEN
        destination: SFO
```

### Auth type reference

The auth block uses the same schema as [environment auth](environments.md#authentication). Here's a compact reference:

**OAuth2:**
```yaml
auth:
  type: oauth2
  tokenUrl: https://auth.example.com/token
  credentials:
    username: { source: env, var: USERNAME }
    password: { source: env, var: PASSWORD }
    clientId: { source: env, var: CLIENT_ID }
    clientSecret: { source: env, var: CLIENT_SECRET }
```

**API Key:**
```yaml
auth:
  type: apikey
  headerName: X-Api-Key
  credentials:
    key: { source: env, var: API_KEY }
```

**Bearer Token:**
```yaml
auth:
  type: bearer
  credentials:
    token: { source: env, var: TOKEN }
```

**No Auth:**
```yaml
auth:
  type: none
```

## Working with Agentic AI (MCP)

AAT's MCP server exposes plan execution tools to IDE-based AI assistants (Cursor, Windsurf, Claude Code, etc.). When an agentic AI executes a plan through the `execute_plan` MCP tool, plan-level auth and headers are fully supported.

### How it helps

An agentic AI assistant can streamline the plan auth workflow in several ways:

**Scaffolding plans with auth blocks.** Ask the AI to create a plan file with the right auth structure for your scenario. The AI knows your graph, your environment config, and the auth types AAT supports. It can generate a complete plan with the correct credential references:

```
"Create a regression test plan for ACME Corp that books a round-trip flight.
 Use their OAuth2 credentials from ACME_* environment variables."
```

The AI generates the full plan YAML including the `auth:` block, step values, assertions, and cleanup — then executes it via the MCP `execute_plan` tool.

**Duplicating plans with different identities.** Given an existing plan, the AI can duplicate it with different auth credentials to test the same flow under different customer identities:

```
"Take the booking-smoke-test plan and create variants for each
 of our three test customers: acme, globex, and initech."
```

**Diagnosing auth failures.** When a plan execution fails with a 401 or 403, the AI can inspect the archive (via `inspect_archive`), identify the auth issue, and suggest corrections to the plan's auth block.

**Adding headers for debugging.** When investigating a test failure, the AI can add trace headers to a plan to enable server-side debugging:

```
"Add X-Debug-Trace and X-Request-Id headers to the failing plan
 and re-run it so we can correlate with server logs."
```

### What the AI cannot do

The MCP `execute_plan` tool runs saved plan files. The AI cannot:

- Inject auth at runtime without modifying the plan file
- Access secrets directly (it references environment variable names, not values)
- Generate auth blocks in the `aat prompt` LLM pipeline (this is by design)

The workflow is: the AI edits the plan YAML on disk, then calls `execute_plan` to run it. Archive results come back through `inspect_archive` for analysis.

### Typical agentic workflow

1. **User describes the test scenario** in natural language, including the customer identity
2. **AI uses `scaffold_template` or `instantiate_workflow`** to get the base plan structure
3. **AI adds `auth:` and `headers:` blocks** based on the described identity
4. **AI saves the plan** to the workflows directory
5. **AI calls `execute_plan`** to run it
6. **AI inspects results** via `inspect_archive` and reports back
7. If the test fails, the AI modifies the plan (adjust values, relax constraints, fix auth) and re-runs

## Validation

Plan auth is validated on load. Validation uses the same rules as environment auth:

- Auth type must be one of: `oauth2`, `apikey`, `bearer`, `none`
- Required credential keys must be present for the auth type
- `tokenUrl` is required for `oauth2`
- `headerName` is required for `apikey`

Validation errors are prefixed with `plan auth:` to distinguish them from other plan validation errors:

```
plan validation failed:
- plan auth: tokenUrl is required for oauth2
- plan auth: credentials.clientSecret is required for oauth2
```

Plan headers require no validation — they're a plain `key: value` map.

## Archive Redaction

Secrets from plan-level auth are redacted in run archives, just like environment secrets. If a plan uses `source: literal` credentials, those values are collected and redacted from response headers and other archive fields. Using `source: env` is preferred for production use, but literal values are safe for archives.

## Security Considerations

Plans with `auth:` blocks contain or reference credentials. Keep these considerations in mind:

- **Prefer `source: env`** for credentials. The plan references the variable name, not the secret value. Safe to commit.
- **Use `source: literal` sparingly.** Fine for local testing; avoid committing literal secrets to version control.
- **Plan files with literal secrets** should be in `.gitignore` or a secrets-management workflow.
- **Archive redaction** catches both env and literal secrets, so archives are safe to share regardless.

## See Also

- [Environments](environments.md) — environment-level auth, headers, and overrides
- [Plan Authoring](plan-authoring.md) — full plan YAML schema reference
- [Running Tests](running.md) — executing plans with `aat run`
- [LLM-Assisted Planning](prompt-workflow.md) — generating plans with `aat prompt`
- [MCP Server](mcp-server.md) — IDE integration for agentic AI workflows
