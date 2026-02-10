# Running AAT

AAT executes API test plans against a live environment. A single `aat run` command loads your graph, templates, plan, and environment config, then executes the workflow end-to-end.

## Quick Start

```
aat run \
  --plan plans/booking_flow.yaml \
  --env environments/staging.yaml \
  --graph graph.yaml \
  --templates templates/
```

## Command: `aat run`

### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--plan` | yes | | Path to the plan YAML file |
| `--env` | yes | | Path to the environment YAML file |
| `--graph` | yes | | Path to the graph YAML file |
| `--templates` | yes | | Path to the templates directory |
| `--output` | no | `./runs` | Directory for archive output |
| `--mode` | no | env config or `strict` | Execution mode: `strict`, `lean`, `adaptive` |
| `--domain` | no | | Path to domain knowledge YAML file |
| `--json` | no | `false` | Output machine-readable JSON summary to stdout |
| `--quiet` | no | `false` | Suppress progress messages, show only final result |

### What Happens

1. **Load environment** -- Parses the environment YAML and authenticates (e.g., OAuth2 token exchange).
2. **Load graph** -- Parses and validates the API graph (nodes, edges, inputs, outputs).
3. **Load plan** -- Parses the plan YAML (steps, values, assertions, retry config).
4. **Validate plan** -- Validates plan references against the graph before execution begins.
5. **Load templates** -- Scans the templates directory and registers all adapters.
6. **Execute plan** -- Runs steps in topological order, resolving inputs from edges and plan values. Retries and assertions are applied per-step.
7. **Print summary** -- Shows step-by-step results with status codes and timing.
8. **Write archive** -- Saves the full run (requests, responses, outputs, selections) as JSON.

## Output Modes

AAT supports three output modes, selected by the `--json` and `--quiet` flags. Each mode is designed for a different use case.

### Default (interactive)

The default mode prints progress messages as the run proceeds, a step-by-step summary, and the archive path. This is what you use when running interactively from a terminal.

```
$ aat run --plan plan.yaml --env env.yaml --graph graph.yaml --templates templates/

aat: loading environment...
aat: loaded environment "staging"
aat: authenticated via oauth2
aat: loaded graph (7 nodes, 11 edges)
aat: loaded 7 templates
aat: executing plan (6 steps, mode=strict)...

  [1/6] searchFlights        200  851ms
  [2/6] createWorkbench      200  279ms
  [3/6] priceOffer           200  535ms
  [4/6] addOffer             200  722ms
  [5/6] addTraveler          200  302ms
  [6/6] commitBooking        200  2640ms

  cleanup:
    ignoreWorkbench        400  167ms

PASSED (6/6 steps, 5.5s)
Archive: runs/run-20260210-143022-a1b2c3d4/archive.json
```

When a step fails:

```
  [1/4] searchFlights        200  643ms
  [2/4] createWorkbench      200  205ms
  [3/4] addOffer             400  312ms

  cleanup:
    ignoreWorkbench        204  167ms

FAILED: step "addOffer" returned status 400
Archive: runs/run-20260210-143048-e5f6a7b8/archive.json
```

When retries are exhausted:

```
  [3/4] addOffer             ERROR [server] (after 2 retries)
```

When assertions fail:

```
  [2/4] priceOffer           200  535ms  ASSERTIONS FAILED
```

### Quiet mode (`--quiet`)

Quiet mode suppresses all progress messages and the step-by-step breakdown. Only the final result line and archive path are printed. Use this in CI logs where you want a clean pass/fail signal without verbose output.

```
$ aat run --quiet --plan plan.yaml --env env.yaml --graph graph.yaml --templates templates/

PASSED (6/6 steps)
Archive: runs/run-20260210-143022-a1b2c3d4/archive.json
```

On failure:

```
$ aat run --quiet --plan plan.yaml --env env.yaml --graph graph.yaml --templates templates/

FAILED: step "addOffer" returned status 400
Archive: runs/run-20260210-143048-e5f6a7b8/archive.json
```

On infrastructure error (e.g., invalid plan):

```
$ aat run --quiet --plan bad_plan.yaml --env env.yaml --graph graph.yaml --templates templates/

aat: plan validation: unknown node "nonExistentNode"
```

### JSON mode (`--json`)

JSON mode outputs a single machine-readable JSON object to stdout. All progress messages and human-readable summaries are suppressed (`--json` implies `--quiet`). Use this when you need to parse results programmatically -- in CI/CD pipelines, monitoring scripts, or downstream tooling.

```
$ aat run --json --plan plan.yaml --env env.yaml --graph graph.yaml --templates templates/

{
  "outcome": "passed",
  "steps": [
    {
      "name": "searchFlights",
      "node": "searchFlights",
      "status": 200,
      "duration_ms": 851,
      "passed": true,
      "retries": 0,
      "assertions_passed": 2,
      "assertions_failed": 0
    },
    {
      "name": "createWorkbench",
      "node": "createWorkbench",
      "status": 200,
      "duration_ms": 279,
      "passed": true,
      "retries": 0,
      "assertions_passed": 0,
      "assertions_failed": 0
    },
    {
      "name": "priceOffer",
      "node": "priceOffer",
      "status": 200,
      "duration_ms": 535,
      "passed": true,
      "retries": 0,
      "assertions_passed": 1,
      "assertions_failed": 0
    },
    {
      "name": "addOffer",
      "node": "addOffer",
      "status": 200,
      "duration_ms": 722,
      "passed": true,
      "retries": 0,
      "assertions_passed": 0,
      "assertions_failed": 0
    }
  ],
  "cleanup": [
    {
      "name": "ignoreWorkbench",
      "node": "ignoreWorkbench",
      "status": 204,
      "duration_ms": 167,
      "passed": true,
      "retries": 0,
      "assertions_passed": 0,
      "assertions_failed": 0
    }
  ],
  "summary": {
    "total_steps": 4,
    "passed_steps": 4,
    "failed_steps": 0,
    "duration_ms": 2387
  },
  "archive_path": "runs/run-20260210-143022-a1b2c3d4/archive.json"
}
```

Failed run:

```
$ aat run --json --plan plan.yaml --env env.yaml --graph graph.yaml --templates templates/

{
  "outcome": "failed",
  "error": "step \"addOffer\" returned status 400",
  "steps": [
    {
      "name": "searchFlights",
      "node": "searchFlights",
      "status": 200,
      "duration_ms": 643,
      "passed": true,
      "retries": 0,
      "assertions_passed": 0,
      "assertions_failed": 0
    },
    {
      "name": "addOffer",
      "node": "addOffer",
      "status": 400,
      "duration_ms": 312,
      "passed": false,
      "error": "status 400",
      "retries": 0,
      "assertions_passed": 0,
      "assertions_failed": 0
    }
  ],
  "summary": {
    "total_steps": 2,
    "passed_steps": 1,
    "failed_steps": 1,
    "duration_ms": 955
  },
  "archive_path": "runs/run-20260210-143048-e5f6a7b8/archive.json"
}
```

Infrastructure error (e.g., missing file, invalid plan):

```
$ aat run --json --plan nonexistent.yaml --env env.yaml --graph graph.yaml --templates templates/

{
  "outcome": "error",
  "error": "loading plan: reading plan file: open nonexistent.yaml: no such file or directory"
}
```

Note that infrastructure errors have no `steps`, `summary`, or `archive_path` because execution never started.

#### JSON Schema Reference

| Field | Type | Description |
|-------|------|-------------|
| `outcome` | string | `"passed"`, `"failed"`, or `"error"` |
| `error` | string | Error message (omitted when outcome is `"passed"`) |
| `steps` | array | Per-step results (omitted on infrastructure errors) |
| `cleanup` | array | Cleanup step results (omitted if no cleanup ran) |
| `summary` | object | Aggregate statistics (omitted on infrastructure errors) |
| `archive_path` | string | Path to the full archive JSON (omitted on infrastructure errors) |

**Step fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Step name (same as `node` for now) |
| `node` | string | Graph node that was executed |
| `status` | int | HTTP status code (0 if no response) |
| `duration_ms` | int | Step execution time in milliseconds |
| `passed` | bool | Whether the step succeeded |
| `error` | string | Error message (omitted on success) |
| `retries` | int | Number of retries performed (0 = no retries) |
| `assertions_passed` | int | Number of mechanical assertions that passed |
| `assertions_failed` | int | Number of mechanical assertions that failed |

**Summary fields:**

| Field | Type | Description |
|-------|------|-------------|
| `total_steps` | int | Total execution steps (excludes cleanup) |
| `passed_steps` | int | Steps that passed |
| `failed_steps` | int | Steps that failed |
| `duration_ms` | int | Total duration across all steps and cleanup |

## Exit Codes

AAT uses granular exit codes to distinguish test failures from infrastructure problems. This matters in CI pipelines where you may want different behavior for "tests failed" vs. "couldn't run."

| Code | Meaning | When |
|------|---------|------|
| `0` | Passed | All steps completed successfully |
| `1` | Test failure | A step returned 4xx/5xx, an assertion failed, or a negative assertion didn't match |
| `2` | Infrastructure error | Config/parse errors, missing files, invalid plan, or an engine-level error |

### Why this matters for CI

With a single exit code, your pipeline can't tell whether tests failed or the test runner is broken. With granular codes:

```yaml
# GitHub Actions example
- name: Run API tests
  run: aat run --plan plan.yaml --env ci.yaml --graph graph.yaml --templates templates/
  continue-on-error: true
  id: aat

- name: Handle results
  run: |
    if [ "${{ steps.aat.outcome }}" = "failure" ]; then
      exit_code=$?
      if [ $exit_code -eq 1 ]; then
        echo "Tests failed -- creating issue"
      elif [ $exit_code -eq 2 ]; then
        echo "Infrastructure error -- paging on-call"
      fi
    fi
```

## CI/CD Integration

### GitHub Actions

```yaml
name: API Tests
on:
  schedule:
    - cron: '0 */4 * * *'  # every 4 hours
  workflow_dispatch:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Run API tests
        run: |
          aat run \
            --json \
            --plan plans/booking_flow.yaml \
            --env environments/ci.yaml \
            --graph graph.yaml \
            --templates templates/ \
            --output ${{ runner.temp }}/runs \
            > result.json

      - name: Check outcome
        if: always()
        run: |
          outcome=$(jq -r '.outcome' result.json)
          echo "Test outcome: $outcome"
          echo "Passed: $(jq '.summary.passed_steps' result.json)/$(jq '.summary.total_steps' result.json)"

      - name: Upload archive
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: aat-archive
          path: ${{ runner.temp }}/runs/
```

### GitLab CI

```yaml
api_tests:
  stage: test
  script:
    - aat run --json --plan plan.yaml --env ci.yaml --graph graph.yaml --templates templates/ > result.json
    - jq '.outcome' result.json
  artifacts:
    when: always
    paths:
      - runs/
    reports:
      junit: result.json  # not JUnit, but archived for inspection
  allow_failure:
    exit_codes:
      - 1  # test failures are soft failures
      # exit code 2 (infra) is a hard failure
```

### Shell Scripts

```bash
#!/bin/bash

aat run --quiet \
  --plan plan.yaml \
  --env production.yaml \
  --graph graph.yaml \
  --templates templates/

case $? in
  0) echo "All tests passed" ;;
  1) echo "Test failure detected" ; notify_slack "API tests failed" ;;
  2) echo "Infrastructure error" ; page_oncall "AAT can't run" ;;
esac
```

### Parsing JSON Output

The `--json` flag produces output that tools like `jq` can parse directly:

```bash
# Get the outcome
aat run --json ... | jq -r '.outcome'

# Get failed step names
aat run --json ... | jq -r '.steps[] | select(.passed == false) | .name'

# Get total duration
aat run --json ... | jq '.summary.duration_ms'

# Check if any assertions failed
aat run --json ... | jq '[.steps[] | .assertions_failed] | add'

# Get the archive path for further inspection
aat run --json ... | jq -r '.archive_path'
```

## Execution Modes

The `--mode` flag controls how AAT resolves input values when explicit values aren't available. It overrides whatever is set in the environment config.

| Mode | LLM Usage | Behavior |
|------|-----------|----------|
| `strict` | Never | Only uses plan values, edge outputs, and fallback pools. Fails if a value can't be resolved. |
| `lean` | After pools exhausted | Tries all deterministic sources first, then asks the LLM as a last resort. |
| `adaptive` | After pools + relaxation | Like `lean`, but also relaxes soft constraints and retries steps on 4xx errors. |

`strict` is the default and is recommended for CI/CD where reproducibility matters. `lean` and `adaptive` require an LLM endpoint configured in the environment file.

```
# Strict (default) -- fully deterministic
aat run --plan plan.yaml --env env.yaml --graph graph.yaml --templates templates/

# Lean -- LLM fills in gaps
aat run --mode lean --plan plan.yaml --env env.yaml --graph graph.yaml --templates templates/

# Adaptive -- LLM + constraint relaxation
aat run --mode adaptive --plan plan.yaml --env env.yaml --graph graph.yaml --templates templates/
```

## Building

```
go build -o aat ./cmd/aat/
```

## Archives

Every run produces a JSON archive in the output directory (default `./runs/`). The archive contains:

- Full plan snapshot
- Per-step: request (method, URL, headers, body), response (status, headers, body), extracted outputs, selection decisions, value resolutions, timing, error classification
- Headers are redacted (Authorization, API keys, etc.)
- For `lean`/`adaptive` mode: LLM call records with prompts, token counts, and timing

Archives are useful for debugging failed runs, comparing behavior across environments, and tracking regressions.

The `--json` summary includes the `archive_path` field so scripts can locate the full archive:

```bash
archive=$(aat run --json ... | jq -r '.archive_path')
cat "$archive" | jq '.steps[0].request'
```

## Concepts

### Graph

The graph (`--graph`) defines the API's topology: which nodes (API operations) exist, what inputs/outputs they have, and how data flows between them via edges. Edges can be direct (scalar value pass-through) or select (choose from an array output).

### Templates

Templates (`--templates`) are YAML files that define how to build HTTP requests and extract responses for each node. They use `{{placeholder}}` substitution for inputs and gjson paths for output extraction.

### Plan

The plan (`--plan`) specifies what to execute: which graph nodes to run, in what order (via `dependsOn`), with what input values, retry policies, and assertions. Plans can provide literal values, reference upstream outputs, or use selection strategies (first, last, random, min, max, match, llm) on array outputs.

### Environment

The environment (`--env`) configures the target: base URL, authentication, custom headers, LLM config, and runtime settings. See [environments.md](environments.md) for details.

### Domain Knowledge

Optional domain knowledge (`--domain`) provides the LLM with context about your API's concepts, types, and valid value pools. Only used in `lean` and `adaptive` modes.
