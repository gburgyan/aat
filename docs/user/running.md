# Running AAT

AAT executes API test plans against a live environment. A single `aat run` command loads your graph, templates, plan, and environment config, then executes the workflow end-to-end.

## Quick Start

```
aat run \
  --plan plans/travelport_booking.yaml \
  --env environments/travelport-pp.yaml \
  --graph graph/testdata/valid/travelport_booking.yaml \
  --templates adapter/testdata/templates/travelport/
```

## Command: `aat run`

### Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--plan` | yes | Path to the plan YAML file |
| `--env` | yes | Path to the environment YAML file |
| `--graph` | yes | Path to the graph YAML file |
| `--templates` | yes | Path to the templates directory |
| `--output` | no | Directory for archive output (default: `./runs`) |

### What Happens

1. **Load environment** — Parses the environment YAML and authenticates (e.g., OAuth2 token exchange).
2. **Load graph** — Parses and validates the API graph (nodes, edges, inputs, outputs).
3. **Load plan** — Parses the plan YAML (steps, values, assertions, retry config).
4. **Load templates** — Scans the templates directory and registers all adapters.
5. **Execute plan** — Runs steps in topological order, resolving inputs from edges and plan values. Retries and assertions are applied per-step.
6. **Print summary** — Shows step-by-step results with status codes and timing.
7. **Write archive** — Saves the full run (requests, responses, outputs, selections) as JSON.

### Output

Successful run:
```
aat: loading environment...
aat: loaded environment "travelport-pp"
aat: authenticated via oauth2
aat: loaded graph (7 nodes, 11 edges)
aat: loaded 7 templates
aat: executing plan (6 steps)...

  [1/6] searchFlights        200  851ms
  [2/6] createWorkbench      200  279ms
  [3/6] priceOffer           200  535ms
  [4/6] addOffer             200  722ms
  [5/6] addTraveler          200  302ms
  [6/6] commitBooking        200  2640ms

  cleanup:
    ignoreWorkbench        400  167ms

PASSED (6/6 steps, 5.5s)
Archive: runs/run-20260207-210118-6578a736/archive.json
```

Failed run:
```
  [3/6] addOffer             503  209ms  (retry 1/2)
  [3/6] addOffer             503  312ms  (retry 2/2)
  [3/6] addOffer             503  ERROR [server]

  cleanup:
    ignoreWorkbench        204  167ms

FAILED at step "addOffer": server error after 2 retries
```

### Exit Codes

| Code | Meaning |
|------|---------|
| `0` | All steps passed |
| `1` | One or more steps failed, or an error occurred |

## Building

```
go build -o aat ./cmd/aat/
```

## Archives

Every run produces a JSON archive in the output directory (default `./runs/`). The archive contains:

- Full plan snapshot
- Per-step: request (method, URL, headers, body), response (status, headers, body), extracted outputs, selection decisions, timing, error classification
- Headers are redacted (Authorization, API keys, etc.)

Archives are useful for debugging failed runs, comparing behavior across environments, and tracking regressions.

## Concepts

### Graph

The graph (`--graph`) defines the API's topology: which nodes (API operations) exist, what inputs/outputs they have, and how data flows between them via edges. Edges can be direct (scalar value pass-through) or select (choose from an array output).

### Templates

Templates (`--templates`) are YAML files that define how to build HTTP requests and extract responses for each node. They use `{{placeholder}}` substitution for inputs and gjson paths for output extraction.

### Plan

The plan (`--plan`) specifies what to execute: which graph nodes to run, in what order (via `dependsOn`), with what input values, retry policies, and assertions. Plans can provide literal values, reference upstream outputs, or use selection strategies (first, last, random, min, max, match) on array outputs.

### Environment

The environment (`--env`) configures the target: base URL, authentication, custom headers, and runtime settings. See [environments.md](environments.md) for details.
