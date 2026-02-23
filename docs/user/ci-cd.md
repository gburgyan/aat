# CI/CD Integration

AAT is designed for automated pipelines: deterministic exit codes, machine-readable JSON output to stdout, and structured archives for artifact collection. This doc covers everything needed to integrate AAT into a CI/CD system.

## Exit Codes

| Code | Meaning | Example Scenarios |
|------|---------|-------------------|
| `0` | Passed | All steps and assertions succeeded |
| `1` | Failed | One or more assertions failed; a step returned an unexpected status code |
| `2` | Error | Invalid plan file, missing environment config, network failure, authentication error |

For batch runs, the exit code reflects the worst outcome across all plans: if any plan errors, exit code is `2`; if any plan fails (but none error), exit code is `1`; only if all plans pass is the exit code `0`.

## JSON Output

### Single Plan (`--json`)

The `--json` flag writes a `RunSummary` object to stdout. It implies `--quiet`, so no progress output is mixed with the JSON.

```
aat run plan smoke-test --json
```

**RunSummary fields:**

| Field | Type | Description |
|-------|------|-------------|
| `outcome` | string | `"passed"`, `"failed"`, or `"error"` |
| `error` | string | Error message (present only when outcome is `"error"`) |
| `steps` | array | Per-step results (see StepSummary below) |
| `cleanup` | array | Cleanup step results (same schema as steps; omitted if none) |
| `summary` | object | Aggregate stats: `total_steps`, `passed_steps`, `failed_steps`, `duration_ms` |
| `archive_path` | string | Path to the archive directory |
| `attempts` | int | Total execution attempts (omitted if 1) |
| `retried` | bool | Whether any retries occurred (omitted if false) |

**StepSummary fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Step ID from the plan |
| `node` | string | Graph node name |
| `status` | int | HTTP status code |
| `duration_ms` | int | Step duration in milliseconds |
| `passed` | bool | Whether the step passed all assertions |
| `error` | string | Error message (omitted if step passed) |
| `retries` | int | Number of step-level retries |
| `assertions_passed` | int | Number of passing assertions |
| `assertions_failed` | int | Number of failing assertions |
| `display_outputs` | array | Tagged outputs: `label`, `name`, `value` (omitted if none) |

**Example — passed plan:**

```json
{
  "outcome": "passed",
  "steps": [
    {
      "name": "search",
      "node": "listProducts",
      "status": 200,
      "duration_ms": 45,
      "passed": true,
      "retries": 0,
      "assertions_passed": 2,
      "assertions_failed": 0
    },
    {
      "name": "order",
      "node": "createOrder",
      "status": 201,
      "duration_ms": 312,
      "passed": true,
      "retries": 0,
      "assertions_passed": 1,
      "assertions_failed": 0
    },
    {
      "name": "check",
      "node": "getOrderStatus",
      "status": 200,
      "duration_ms": 28,
      "passed": true,
      "retries": 0,
      "assertions_passed": 1,
      "assertions_failed": 0
    }
  ],
  "summary": {
    "total_steps": 3,
    "passed_steps": 3,
    "failed_steps": 0,
    "duration_ms": 385
  },
  "archive_path": "_output/runs/run-20260223-143052-a1b2c3d4"
}
```

**Example — failed plan:**

```json
{
  "outcome": "failed",
  "steps": [
    {
      "name": "search",
      "node": "listProducts",
      "status": 200,
      "duration_ms": 52,
      "passed": true,
      "retries": 0,
      "assertions_passed": 1,
      "assertions_failed": 0
    },
    {
      "name": "order",
      "node": "createOrder",
      "status": 400,
      "duration_ms": 98,
      "passed": false,
      "error": "unexpected status 400",
      "retries": 0,
      "assertions_passed": 0,
      "assertions_failed": 1
    }
  ],
  "summary": {
    "total_steps": 2,
    "passed_steps": 1,
    "failed_steps": 1,
    "duration_ms": 150
  },
  "archive_path": "_output/runs/run-20260223-143105-b2c3d4e5"
}
```

### Batch (`--json`)

Batch JSON output uses a `BatchSummary` envelope containing per-plan results.

```
aat run batch --json
```

**BatchSummary fields:**

| Field | Type | Description |
|-------|------|-------------|
| `outcome` | string | `"passed"`, `"failed"`, or `"error"` |
| `batchId` | string | Batch run identifier |
| `runs` | array | Per-plan results (see BatchRunResult below) |
| `summary` | object | Aggregate: `total_plans`, `passed_plans`, `failed_plans`, `error_plans`, `duration_ms` |
| `archive_path` | string | Path to the batch archive directory |

**BatchRunResult fields:**

| Field | Type | Description |
|-------|------|-------------|
| `plan_name` | string | Plan filename |
| `outcome` | string | `"passed"`, `"failed"`, or `"error"` |
| `step_count` | int | Total steps in the plan |
| `passed_steps` | int | Steps that passed |
| `failed_steps` | int | Steps that failed |
| `duration_ms` | int | Plan execution time |
| `error` | string | Error message (omitted if plan passed) |
| `archive_path` | string | Path to this plan's archive directory |
| `attempts` | int | Total execution attempts (omitted if 1) |
| `layers` | array | Effective layer names applied (omitted if none) |
| `permutation` | string | Layer permutation label (omitted if no layer groups) |

**Example — batch with mixed outcomes:**

```json
{
  "outcome": "failed",
  "batchId": "batch-20260223-150000-e5f6g7h8",
  "runs": [
    {
      "plan_name": "smoke-test.yaml",
      "outcome": "passed",
      "step_count": 3,
      "passed_steps": 3,
      "failed_steps": 0,
      "duration_ms": 385,
      "archive_path": "_output/runs/batch-20260223-150000-e5f6g7h8/run-20260223-150001-i9j0k1l2"
    },
    {
      "plan_name": "full-checkout.yaml",
      "outcome": "passed",
      "step_count": 5,
      "passed_steps": 5,
      "failed_steps": 0,
      "duration_ms": 513,
      "archive_path": "_output/runs/batch-20260223-150000-e5f6g7h8/run-20260223-150002-j0k1l2m3"
    },
    {
      "plan_name": "return-flow.yaml",
      "outcome": "failed",
      "step_count": 4,
      "passed_steps": 3,
      "failed_steps": 1,
      "duration_ms": 892,
      "error": "step cancelOrder: assertion failed",
      "archive_path": "_output/runs/batch-20260223-150000-e5f6g7h8/run-20260223-150003-m3n4o5p6"
    }
  ],
  "summary": {
    "total_plans": 3,
    "passed_plans": 2,
    "failed_plans": 1,
    "error_plans": 0,
    "duration_ms": 1790
  },
  "archive_path": "_output/runs/batch-20260223-150000-e5f6g7h8"
}
```

## Pipeline Patterns

### Validate Then Run

Run validation first to catch configuration errors before execution:

```bash
aat validate && aat run batch --json --output _output/runs
```

If `aat validate` fails (exit code 1), the batch never runs.

### Single Plan

For smoke tests or targeted checks:

```bash
aat run plan smoke-test --json --output _output/runs
```

### Batch with Layers

Run the same plans against multiple configurations:

```bash
aat run batch --json --layer-group "premium,standard"
```

See [Running Tests: Layer Expansion](running.md#layer-expansion) and [Plans: Layers](plans.md#layers) for how layers work.

## GitHub Actions Example

```yaml
name: API Tests
on:
  push:
    branches: [main]
  pull_request:

env:
  API_KEY: ${{ secrets.API_KEY }}

jobs:
  api-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'

      - name: Build AAT
        run: make build

      - name: Validate project
        working-directory: my-ecommerce-api
        run: ../aat validate

      - name: Run tests
        working-directory: my-ecommerce-api
        run: ../aat run batch --json --output _output/runs > results.json

      - name: Upload archives
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: api-test-archives
          path: my-ecommerce-api/_output/runs/
```

Other CI systems follow the same pattern: build the binary, validate, run tests with `--json`, and upload the archive directory as an artifact. The exit codes and JSON output are CI-system-agnostic.

## Environment Management

Secrets are supplied through environment variables and resolved via `SecretRef` in the environment config. Never commit credentials to your repository.

```yaml
# env.yaml
auth:
  type: oauth2
  clientId:
    resolvedFrom: CLIENT_ID
  clientSecret:
    resolvedFrom: CLIENT_SECRET
```

Set the environment variables in your CI system's secrets configuration. AAT resolves them at runtime.

### Per-Environment Configs

Use the `--env` flag to switch between environment files:

```bash
# Development
aat run batch --env env-dev.yaml --json

# Staging
aat run batch --env env-staging.yaml --json
```

### Overlay Files

The `--env-overlay` flag applies a sparse YAML overlay on top of the base environment. This is useful for CI-specific overrides like different base URLs or reduced timeouts:

```yaml
# ci-overlay.yaml
overrides:
  - match: "*"
    baseUrl: https://staging.api.example.com
```

```bash
aat run batch --env-overlay ci-overlay.yaml --json
```

### Pinning the Project Root

Set the `AAT_PROJECT` environment variable to pin the project root directory in CI, so AAT finds the manifest regardless of working directory:

```bash
export AAT_PROJECT=/workspace/my-ecommerce-api
aat run batch --json
```

See [Environments](environments.md) for the full environment config reference.

## Artifact Management

Archives are the primary debugging artifact. Point `--output` to a directory your CI system can collect:

```bash
aat run batch --json --output _output/runs
```

### Archive Naming

- Single run: `run-YYYYMMDD-HHMMSS-XXXXXXXX/archive.json`
- Batch: `batch-YYYYMMDD-HHMMSS-XXXXXXXX/batch.json` + per-plan subdirectories
- Retries: `attempt-01.json`, `attempt-02.json` alongside `archive.json`

Upload the entire output directory as a CI artifact. Archives contain redacted headers, so they are safe to store.

See [Web UI and Archives](web-ui.md) for browsing archives locally after downloading CI artifacts.

## Debugging Failures

When a pipeline fails:

1. **Check the exit code** — `1` means test failure, `2` means infrastructure error
2. **Read the JSON summary** — identify which plan and step failed, check the error message
3. **Inspect the archive** — download the CI artifact and open it with `aat web view`

```bash
# Download CI artifacts, then:
aat web view _output/runs/run-20260223-143105-b2c3d4e5
```

The web UI shows the full request/response, value resolution chain, and assertion results for each step. See [Web UI and Archives: Debugging Patterns](web-ui.md#debugging-patterns) for a detailed walkthrough.

---

*Source: `cmd/aat/run_shared.go`, `cmd/aat/run_batch_cmd.go`.*
