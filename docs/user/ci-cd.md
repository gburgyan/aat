# CI/CD Integration

AAT is designed for automated pipelines: deterministic exit codes, machine-readable JSON output to stdout, and structured archives for artifact collection. This doc covers everything needed to integrate AAT into a CI/CD system.

## Exit Codes

| Code | Meaning | Example Scenarios |
|------|---------|-------------------|
| `0` | Passed | All steps and assertions succeeded; also a `--stop-after` checkpoint (`stopped`) |
| `1` | Failed | One or more assertions failed; a step returned an unexpected status code |
| `2` | Error | An unknown flag or subcommand, a manifest or environment file that cannot be loaded, a bad `--var`, a batch that finds no plans, an invalid plan file, a network failure, an authentication error |
| `130` | Aborted | The process received `SIGINT` (Ctrl+C) or `SIGTERM` — a cancelled CI job, a timeout wrapper, a runner shutting down. Cleanup still runs and a partial archive is written |

For batch runs, the exit code reflects the worst outcome across all plans: if any plan was aborted, exit code is `130`; if any plan errors, exit code is `2`; if any plan fails (but none error), exit code is `1`; only if all plans pass is the exit code `0`. Every `aat` command uses these codes; [Exit Codes](running.md#exit-codes) lists what each command returns.

## JSON Output

### Single Plan (`--json`)

The `--json` flag writes a `RunSummary` object to stdout. It implies `--quiet`, so no progress output is mixed with the JSON.

```
aat run plan smoke-test --json
```

**RunSummary fields:**

| Field | Type | Description |
|-------|------|-------------|
| `outcome` | string | `"passed"`, `"failed"`, `"error"`, `"aborted"` (interrupted), or `"stopped"` (`--stop-after` checkpoint) |
| `error` | string | Why the run did not pass, such as `step "createOrder" returned status 400` (omitted when the run passed) |
| `steps` | array | Per-step results (see StepSummary below) |
| `cleanup` | array | Cleanup step results (same schema as steps; omitted if none) |
| `summary` | object | Aggregate stats: `total_steps`, `passed_steps`, `failed_steps` (main steps only), `duration_ms` (the run's wall-clock time, retry waits and cleanup included), and `issues` — a map of issue category to count (currently `oas` for OpenAPI violations; omitted when empty) |
| `archive_path` | string | Path to the run's `archive.json` |
| `attempts` | int | Total execution attempts (omitted if 1) |
| `retried` | bool | Whether any retries occurred (omitted if false) |
| `stopped_at` | string | Checkpoint step ID when the outcome is `"stopped"` (omitted otherwise) |
| `state` | object | Accumulated run state, present only with `--dump-state -` (unredacted; see [Checkpoints](checkpoints.md)) |

**StepSummary fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Step ID from the plan (a mutation sibling's generated ID, such as `addItem--zero-quantity`) |
| `node` | string | Graph node name |
| `status` | int | HTTP status code (`0` when no response arrived) |
| `duration_ms` | int | Step duration in milliseconds, from the first attempt to the end of the last, retry waits included |
| `passed` | bool | Whether the step succeeded: no error, a status below 400 (or one its `expectFailure` lists), and no failed assertion |
| `error` | string | Error message, such as `status 400` (omitted if step passed) |
| `retries` | int | Number of step-level retries |
| `retried_on` | array | Error category of each retried attempt, in order, such as `["transient", "transient"]` (omitted if none) |
| `assertions_passed` | int | Number of passing assertions |
| `assertions_failed` | int | Number of failing assertions |
| `failed_assertions` | array | One `"type: message"` string per failed assertion, such as `"status: expected status 200, got 201"` (omitted if none) |
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
  "archive_path": "_output/runs/run-20260223-143052-a1b2c3d4/archive.json"
}
```

**Example — failed plan:**

```json
{
  "outcome": "failed",
  "error": "step \"createOrder\" returned status 400",
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
      "error": "status 400",
      "retries": 0,
      "assertions_passed": 0,
      "assertions_failed": 1,
      "failed_assertions": [
        "status: expected status 201, got 400"
      ]
    }
  ],
  "summary": {
    "total_steps": 2,
    "passed_steps": 1,
    "failed_steps": 1,
    "duration_ms": 150
  },
  "archive_path": "_output/runs/run-20260223-143105-b2c3d4e5/archive.json"
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
| `outcome` | string | `"passed"`, `"failed"`, `"error"`, `"aborted"`, or `"skipped"` (a duplicate permutation) |
| `batch_id` | string | Batch run identifier; absent when the batch stopped before it started |
| `runs` | array | Per-plan results (see BatchRunResult below) |
| `summary` | object | Aggregate: `total_plans`, `passed_plans`, `failed_plans`, `error_plans`, `duration_ms`; plus `aborted_plans` and `skipped_plans` when non-zero |
| `archive_path` | string | Path to the batch archive directory |

**BatchRunResult fields:**

| Field | Type | Description |
|-------|------|-------------|
| `plan_name` | string | Plan path within its plan directory, without the `.yaml` extension (`smoke-test`, `orders/return-flow`) |
| `outcome` | string | `"passed"`, `"failed"`, `"error"`, or `"aborted"` |
| `step_count` | int | Total steps in the plan |
| `passed_steps` | int | Steps that passed |
| `failed_steps` | int | Steps that failed |
| `duration_ms` | int | Plan execution time |
| `error` | string | Why the plan did not pass (omitted if it passed) |
| `archive_path` | string | Path to this plan's `archive.json` |
| `attempts` | int | Total execution attempts (omitted if 1) |
| `layers` | array | Effective layer names applied (omitted if none) |
| `permutation` | string | Layer permutation label (omitted if no layer groups) |
| `skipped` | bool | `true` when the run was skipped as a duplicate permutation (see [Matrix Testing: Duplicate Detection](batch-layers.md#duplicate-detection)) |
| `duplicate_of` | string | Display name of the canonical run this one duplicates (omitted unless skipped) |

**Example — batch with mixed outcomes:**

```json
{
  "outcome": "failed",
  "batch_id": "batch-20260223-150000-e5f6a7b8",
  "runs": [
    {
      "plan_name": "smoke-test",
      "outcome": "passed",
      "step_count": 3,
      "passed_steps": 3,
      "failed_steps": 0,
      "duration_ms": 385,
      "archive_path": "_output/runs/batch-20260223-150000-e5f6a7b8/run-20260223-150001-c9d0e1f2/archive.json"
    },
    {
      "plan_name": "full-checkout",
      "outcome": "passed",
      "step_count": 5,
      "passed_steps": 5,
      "failed_steps": 0,
      "duration_ms": 513,
      "archive_path": "_output/runs/batch-20260223-150000-e5f6a7b8/run-20260223-150002-d0e1f2a3/archive.json"
    },
    {
      "plan_name": "return-flow",
      "outcome": "failed",
      "step_count": 4,
      "passed_steps": 3,
      "failed_steps": 1,
      "duration_ms": 892,
      "error": "step \"cancelOrder\" failed mechanical validation",
      "archive_path": "_output/runs/batch-20260223-150000-e5f6a7b8/run-20260223-150003-a3b4c5d6/archive.json"
    }
  ],
  "summary": {
    "total_plans": 3,
    "passed_plans": 2,
    "failed_plans": 1,
    "error_plans": 0,
    "duration_ms": 1790
  },
  "archive_path": "_output/runs/batch-20260223-150000-e5f6a7b8"
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

      - name: Install AAT
        run: |
          curl -fsSL https://github.com/gburgyan/aat/releases/latest/download/aat_linux_amd64.tar.gz | tar -xz aat
          sudo install -m 0755 aat /usr/local/bin/aat
          aat --version

      - name: Validate project
        working-directory: my-ecommerce-api
        run: aat validate

      - name: Run tests
        working-directory: my-ecommerce-api
        run: aat run batch --json --output _output/runs > results.json

      - name: Upload archives
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: api-test-archives
          path: my-ecommerce-api/_output/runs/
```

Release archives are named `aat_<os>_<arch>.tar.gz` (`aat_linux_arm64`, `aat_darwin_arm64`, and so on; Windows ships as `.zip`), so the URL above always fetches the latest release for the runner's platform. Pin a specific version by replacing `latest/download` with `download/vX.Y.Z` when you want reproducible pipelines.

Other CI systems follow the same pattern: install the binary, validate, run tests with `--json`, and upload the archive directory as an artifact. The exit codes and JSON output are CI-system-agnostic.

## Environment Management

Secrets are supplied through environment variables and resolved via `SecretRef` in the environment config. Never commit credentials to your repository.

```yaml
# env.yaml
environment: ci
apiBaseUrl: https://api.example.com

auth:
  type: oauth2
  grantType: client_credentials
  tokenUrl: https://auth.example.com/oauth/token
  credentials:
    clientId:
      source: env
      var: CLIENT_ID
    clientSecret:
      source: env
      var: CLIENT_SECRET
    # oauth2 requires username and password even for client_credentials,
    # and sends them in the token request; most servers ignore them.
    username:
      source: literal
      value: unused
    password:
      source: literal
      value: unused
```

Set the environment variables in your CI system's secrets configuration. AAT resolves them at runtime. The `username` and `password` placeholders are needed because `oauth2` validation requires all four credentials whatever the grant type (the default grant is `password`).

### Per-Environment Configs

With a multi-environment file, select the target by name with `--env` (or the `AAT_ENV_NAME` variable, which is convenient in CI):

```bash
# Development
aat run batch --env dev --json

# Staging
AAT_ENV_NAME=staging aat run batch --json
```

To point at a different environment *file* altogether, use `--env-config`:

```bash
aat run batch --env-config env-staging.yaml --json
```

### Overlay Files

The `--overlay` flag applies a sparse YAML overlay on top of the base environment. This is useful for CI-specific overrides like different base URLs, auth, or headers:

```yaml
# ci-overlay.yaml
overrides:
  - match: "*"
    baseUrl: https://staging.api.example.com
```

```bash
aat run batch --overlay ci-overlay.yaml --json
```

Add `--no-auto-overrides` in CI so a developer's `.aat-overrides.yaml` can never leak into a pipeline run.

### Pinning the Project Root

Set the `AAT_PROJECT` environment variable to point AAT at the project in CI, so it finds the manifest from a working directory outside the project. A manifest found by walking up from the working directory still takes priority, and `--manifest` beats both (see [Project Setup: Resolution Priority](project-setup.md#resolution-priority)):

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

Upload the entire output directory as a CI artifact. Archives redact credential headers (`Authorization`, `X-API-Key`, `Cookie`, and similar) and every configured secret credential's value wherever it appears, bodies and URLs included, but not tokens the API issues at run time or personal data it returns. If an API returns either in a body, treat the archives as sensitive and restrict who can download the artifact. The `--json` output is not redacted.

See [Archives](archives.md) for the archive layout and redaction, and for browsing archives locally after downloading CI artifacts.

## JUnit / Datadog

The `tools/` directory in the AAT repository ships two stdlib-only Python scripts for feeding archives into test-reporting systems.

### `tools/aat-to-junit.py`

Converts a run or batch archive into JUnit XML. Each AAT step becomes a `<testcase>` (cleanup steps are prefixed `[cleanup]`), and Datadog-style `dd_tags` properties carry the HTTP method, status, URL, node name, run ID, and step index — batches add plan name, permutation, and layer tags. A step is reported as failed when it has an error, a failed assertion, a failed `expectFailure`, or an HTTP status `>= 400` without a passing `expectFailure`.

```bash
# Single run → stdout
python3 tools/aat-to-junit.py _output/runs/run-XXXXX/archive.json

# Batch (per-run archives are loaded automatically)
python3 tools/aat-to-junit.py _output/runs/batch-XXXXX/batch.json -o report.xml --pretty

# Tag with a service name and upload to Datadog Test Visibility
python3 tools/aat-to-junit.py _output/runs/batch-XXXXX/batch.json --service aat-tests -o report.xml
datadog-ci junit upload --service aat-tests report.xml
```

Any CI system that understands JUnit XML (GitHub's test summaries, GitLab, Jenkins, CircleCI) can consume the same file.

### `tools/batch-coverage.py`

Reports which graph nodes a batch exercised — passed, failed, or never touched — with per-node execution counts and an overall coverage percentage. Pass `--json` for machine-readable output.

```bash
python3 tools/batch-coverage.py _output/runs/batch-XXXXX graph.yaml
```

See [`tools/README.md`](https://github.com/gburgyan/aat/blob/main/tools/README.md) for the full option list.

## Debugging Failures

When a pipeline fails:

1. **Check the exit code** — `1` means test failure, `2` means infrastructure error
2. **Read the JSON summary** — identify which plan and step failed, check the error message
3. **Inspect the archive** — download the CI artifact and open it with `aat web view`

```bash
# Download CI artifacts, then point the viewer at the run directory...
aat web view --output _output/runs run-20260223-143105-b2c3d4e5

# ...or at a single file, no project setup needed
aat web view _output/runs/run-20260223-143105-b2c3d4e5/archive.json
aat web view exported-run.aar
```

The web UI shows the full request/response, value resolution chain, and assertion results for each step. See [Web UI: Debugging Patterns](web-ui.md#debugging-patterns) for a detailed walkthrough.

---

*Source: `cmd/aat/run_shared.go`, `cmd/aat/run_batch_cmd.go`.*
