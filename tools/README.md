# AAT Tools

Standalone utilities for working with AAT artifacts.

## aat-to-junit.py

Converts AAT archive JSON to JUnit XML for Datadog Test Visibility.

**Requirements:** Python 3.6+ (stdlib only, no external dependencies)

### Usage

```bash
# Single run archive → stdout
python3 tools/aat-to-junit.py runs/run-XXXXX/archive.json

# Batch archive (loads per-run archives automatically)
python3 tools/aat-to-junit.py runs/batch-XXXXX/batch.json

# Write to file with pretty-printing
python3 tools/aat-to-junit.py runs/run-XXXXX/archive.json -o report.xml --pretty

# Add service tag for Datadog
python3 tools/aat-to-junit.py runs/run-XXXXX/archive.json --service aat-tests -o report.xml
```

### Datadog Integration

```bash
# Generate JUnit XML and upload to Datadog
python3 tools/aat-to-junit.py runs/run-XXXXX/archive.json --service aat-tests -o report.xml
datadog-ci junit upload --service aat-tests report.xml
```

### Mapping

Each AAT step becomes a `<testcase>`. Cleanup steps are prefixed with `[cleanup]`. Datadog `dd_tags` properties include HTTP method/status/URL, node name, run ID, and step index. Batch runs add plan name, permutation, and layer tags.

### Failure Detection

Steps are marked as failed when (checked in priority order):
1. `step.error` is present
2. `step.validation.passed` is false
3. `step.expectFailure.passed` is false
4. HTTP status >= 400 without a passing expectFailure

## batch-coverage.py

Analyzes AAT batch run results and reports node test coverage — which graph nodes have been tested (passed/failed) and which are untested.

**Requirements:** Python 3.6+ (stdlib only, no external dependencies)

### Usage

```bash
# Terminal report
python3 tools/batch-coverage.py runs/batch-XXXXX ../aat-travelport/graph.yaml

# Machine-readable JSON
python3 tools/batch-coverage.py --json runs/batch-XXXXX ../aat-travelport/graph.yaml
```

### Output

The report categorizes every node in the graph into three buckets:

- **Tested & Passed** — all executions across the batch succeeded
- **Tested & Failed** — at least one execution failed or errored
- **Untested** — node never appeared in any executed step

Each tested node shows its total execution count and pass/fail breakdown. The summary includes overall coverage percentage.
