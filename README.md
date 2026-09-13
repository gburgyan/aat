<div align="center">

# AAT — Adaptive API Toolkit

**Model your API as a graph once. Get long-chain integration tests, layer × environment matrices, CI-ready runs, and an MCP server for AI coding tools — all from the same YAML.**

[![CI](https://github.com/gburgyan/aat/actions/workflows/ci.yml/badge.svg)](https://github.com/gburgyan/aat/actions/workflows/ci.yml)
[![Docs](https://github.com/gburgyan/aat/actions/workflows/docs.yml/badge.svg)](https://gburgyan.github.io/aat/)
[![Release](https://img.shields.io/github/v/release/gburgyan/aat)](https://github.com/gburgyan/aat/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/gburgyan/aat)](https://goreportcard.com/report/github.com/gburgyan/aat)
[![License](https://img.shields.io/github/license/gburgyan/aat)](https://github.com/gburgyan/aat/blob/main/LICENSE)

<img src="https://raw.githubusercontent.com/gburgyan/aat/main/docs/user/assets/demo-plan.gif" alt="aat run plan full-lifecycle against the offline shop sandbox: fifteen steps stream in with status codes and durations, two steps retry, cleanup deletes the order and the cart, and the run passes" width="820">

</div>

## Why

Real integrations are not one call. Buying something means browse, cart, checkout, pay, ship, and maybe return and refund: 8 to 20 calls, each needing IDs from the calls before it, leaving state behind that someone has to clean up.

Request runners test one call at a time and leave the chaining to scripts. Test code welds the intent ("a registered customer pays with PayPal and returns the order") to HTTP details, so every variation is another copy of the same calls.

AAT keeps three things apart. **API knowledge** is a graph of operations and request templates, written once. **Test intent** is a plan that lists steps, not wiring. **Variation** is layers (named sets of test data) and environments that turn one plan into a matrix. The API knowledge that runs your tests also teaches AI coding tools, yours and your integrators', how to use the API.

## 60-second quick start

With `aat` and `aat-sandbox` [installed](#install) and on your `PATH`, the offline shop example runs with no signup and no network:

```bash
aat-sandbox init shop && cd shop   # extract the example project
aat-sandbox serve &                # shop API on :8765, payments API on :8766
aat run plan full-lifecycle        # one order through every state, verified and cleaned up
aat run plan smoke --env eu        # the same purchase with EU prices and VAT
aat run batch --layer-group shipping-standard,shipping-express --layer-group basket-gear,basket-apparel --parallel 4
aat web view latest                # the batch in the web UI; press Ctrl+C when done
kill %1                            # stop the sandbox
```

```
  [ 1/15] listProducts         200  0ms
  [ 2/15] checkInventory       200  609ms  retried 1x: response_error
  [ 3/15] createCart           201  0ms
  [ 4/15] addProduct (addItem) 201  0ms
  ...
  [ 8/15] checkout             201  0ms
          Order: ord_0001
          Total: $130.66
  [ 9/15] paymentCharge        201  351ms
  [10/15] shipOrder            201  601ms
  [11/15] getShipment          200  1.3s  retried 2x: transient
  ...
  [15/15] verify_getOrder      200  0ms

  cleanup:
    deleteOrder            204  0ms
    deleteCart             204  0ms

PASSED (15/15 steps, 2.9s)
```

Nobody wired the data by hand: the graph says where each input comes from, and the plan only lists steps. The [shop README](examples/shop/README.md) walks through what each command shows.

## Pick your demo

| Example | What it shows | Needs |
|---------|---------------|-------|
| [Shop](examples/shop/README.md) | Everything: a 17-operation graph, workflows with slots and addons, layers and matrices, two regions, a separately hosted payments API, negative tests, retries, checkpoints, an integration kit, MCP | Nothing: it runs offline against `aat-sandbox` |
| [Petstore](examples/petstore/README.md) | The smallest working project: four operations, two workflows, cleanup pairing | Network access to the public Petstore |

Examples against real APIs (Duffel flight booking, GitHub, Stripe) are next on the [roadmap](ROADMAP.md).

## What it does

| | |
|---|---|
| **Graph, not scripts.** Operations, data flow, ordering, and cleanup live in YAML once; plans list steps. | **Long chains.** Values flow between steps, retries follow error categories, verification runs after the flow, and cleanup unwinds what was created. |
| **Layers → matrix.** `--layer-group` runs every plan across every layer permutation and skips permutations that would send identical requests. | **Multi-environment.** Named environments share a base through `extends` and `vars`, and single operations can route to another host with other credentials. |
| **Archives with a decision trail.** Every request, response, resolved value, retry, and assertion is recorded, with secrets redacted, and browsable in the web UI. | **CI-native.** Exit codes 0/1/2/130, `--json`, JUnit XML via `tools/aat-to-junit.py`, and a Docker image. |
| **Checkpoints.** `--stop-after` keeps resources alive and `--dump-state` hands their IDs to another tool, with the session's credentials on request. | **Depth testing.** `expectFailure`, `mutations`, `rawBody`, and overlay files (per-run input values and expected failures) turn happy paths into negative tests. |
| **From OpenAPI and back.** `aat generate` scaffolds from a spec; `aat validate --strict` and `--oas-validate strict` hold the graph and every exchange to it; `aat docs generate` writes Markdown. | **AI where it helps.** The MCP server teaches AI coding tools your API, and `aat prompt` can draft a plan; see [AI tools and MCP](#ai-tools-and-mcp). |

<img src="https://raw.githubusercontent.com/gburgyan/aat/main/docs/user/assets/demo-batch.gif" alt="aat run batch with two layer groups and --parallel 4: the dedup list, four progress bars updating in place, and Batch: 27/63 PASSED, 36 SKIPPED" width="820">

## Layers and environments

An environment file holds as many named environments as you need. The shop's two regions share everything through `_base` and differ only in variables; payments go to their own host with their own API key:

```yaml
# abridged: auth, headers, and the apiHost/payHost vars are left out
environments:
  _base:
    apiBaseUrl: http://${apiHost}/${region}/v1
    overrides:
      - match: "payment*"
        baseUrl: http://${payHost}/${region}/v1
        auth: {type: apikey, headerName: X-API-Key, credentials: {key: {source: literal, value: pay-demo-key}}}
  us: {extends: _base, vars: {region: us, postalCode: "78701"}}
  eu: {extends: _base, vars: {region: eu, postalCode: "10115"}}
```

A layer is a named set of input values. Each `--layer-group` adds a dimension, so the quick start's batch runs 7 plans × 9 permutations, skips the 36 runs that would repeat another run's requests, and passes the other 27. Pick the environment with `--env eu`, and send one operation to a local build without editing any file:

```bash
aat run plan full-lifecycle --override checkoutCart=http://localhost:9000/us/v1
```

<img src="https://raw.githubusercontent.com/gburgyan/aat/main/docs/user/assets/ui-batch-matrix.png" alt="The shop batch in the web UI's By Test view: one row per plan, one column per layer permutation, and a filter for each layer group" width="820">

## How it works

| File | What it holds |
|------|---------------|
| **Graph** (`graph.yaml`) | Operations with typed inputs and outputs, where each input's value comes from, ordering tokens, and cleanup pairings |
| **Templates** (`templates/`) | One HTTP request and response template per operation: method, path, headers, body, and what to extract |
| **Environments** (`env.yaml`) | Base URLs, auth, headers, and per-operation routing for each named environment |
| **Workflows** (`workflows/`) | Reusable step sequences with slots (pick one option) and addons (splice extra steps in) |
| **Layers** (`layers/`) | Named sets of input values that multiply plans into a matrix |
| **Plans** (`plans/`) | Tests: recipes that pick a workflow and its options, or full step lists with values and assertions |

A node in the shop's graph (abridged):

```yaml
checkoutCart:
  adapter: checkoutCart
  inputs:
    - name: cartId
      default: {from: createCart.cartId}    # wired from an earlier step
    - name: shippingTier
      type: enum[standard, express, overnight]
      default: standard
  outputs:
    - name: orderId
      display: Order                        # printed in run output
  cleanup: deleteOrder                      # undone after the run
  requires: [cartPopulated]                 # runs once something is in the cart
  satisfies: [orderCreated]
```

A recipe, which is a whole test:

```yaml
kind: recipe
selection:
  workflow: Checkout
  choices:
    customer: Registered
    payment: PayPal
  addons: [Apply Coupon, Return After Delivery]
```

## One framework, two wins

An OpenAPI spec describes calls one at a time. The project you build to test your API describes how they work together: which calls reach a goal and in what order, where each input comes from, which fields of a large schema matter, what a failure looks like, and what undoes what. Your test runs keep all of it true, and it is what integrators need, in a form a machine can act on. A second manifest names the part you share, and a short CI step packages it as a kit. Your integrators' AI coding tools read the kit through `aat mcp serve` and write a working client in their own language. Negative tests, internal environments, and archives stay with you.

```
my-api-tests/
  aat-project.yaml     your tests: everything
  aat-kit.yaml         the subset integrators get
  graph.yaml  openapi.yaml  env.yaml  templates/  workflows/
  plans/               reference flows (shipped)
  internal/plans/      negative, chaos, regression (kept)
```

The shop is laid out this way. See [Share your API with integrators](https://gburgyan.github.io/aat/integration-kit/).

## AI tools and MCP

`aat mcp serve` gives an AI coding tool the whole workflow as tools rather than prose: each operation's exact request, the order calls must run in, what each call needs from the calls before it, composed integration flows, the domain's rules and values, OpenAPI schemas, and sample responses from real runs. The `api` persona, the tool set for integrators, has 24 read-only tools (17 without an OpenAPI spec); the `test` persona, for your own team, has 26 for writing, running, and debugging plans. The shop ships this `.mcp.json`:

```json
{
  "mcpServers": {
    "shop-api": {"command": "aat", "args": ["mcp", "serve", "--manifest", "aat-kit.yaml", "--persona", "api"]},
    "shop-test": {"command": "aat", "args": ["mcp", "serve", "--manifest", "aat-project.yaml", "--persona", "test"]}
  }
}
```

With that much machine-readable detail, a working client has taken a single prompt in every language tried: on a 74-node airline API, AI coding tools built search-and-booking clients this way in Java, C#, Go, Python, Perl, and Lisp. That project is private, but the same test on the shop is reproducible: [Reproduce the single-prompt test](https://gburgyan.github.io/aat/integration-kit/#reproduce-the-single-prompt-test) has the exact prompt, the setup, and the results of a Python run and a Go run.

LLMs are optional and authoring-time only: `aat prompt` can draft a plan, and the MCP server teaches AI tools your API. Execution never calls an LLM.

## Install

Homebrew (macOS or Linux) installs `aat` and `aat-sandbox`:

```bash
brew install gburgyan/tap/aat
```

The latest release on macOS or Linux:

```bash
curl -fsSL "https://github.com/gburgyan/aat/releases/latest/download/aat_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" \
  | sudo tar -xz -C /usr/local/bin aat aat-sandbox
```

Windows, in PowerShell (then open a new terminal):

```powershell
$dir = "$env:LOCALAPPDATA\Programs\aat"
Invoke-WebRequest https://github.com/gburgyan/aat/releases/latest/download/aat_windows_amd64.zip -OutFile "$env:TEMP\aat.zip"
Expand-Archive "$env:TEMP\aat.zip" -DestinationPath $dir -Force
[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ";$dir", 'User')
```

Docker, with `aat` only (no sandbox):

```bash
docker run --rm -v "$PWD":/work ghcr.io/gburgyan/aat validate
```

Go, for the CLI and MCP server (the web UI needs a release build or a source build):

```bash
go install github.com/gburgyan/aat/cmd/aat@latest
go install github.com/gburgyan/aat/cmd/aat-sandbox@latest
```

### From source

Requires Go 1.25.7+, Node.js 18+, and `make`:

```bash
git clone https://github.com/gburgyan/aat.git && cd aat
make build   # the web UI, then ./aat and ./aat-sandbox
export PATH="$PWD:$PATH"   # so the quick start finds both
```

Release binaries are not notarized. If macOS blocks one you downloaded with a browser, run `xattr -d com.apple.quarantine aat aat-sandbox`; the Homebrew cask removes the attribute for you. The [install guide](https://gburgyan.github.io/aat/install/) covers checksums, Docker ports, and what each method includes.

## Commands

| Command | What it does |
|---------|--------------|
| `aat run plan <name>` | Run one plan or recipe |
| `aat run batch [dir]` | Run every plan, optionally across layer groups and in parallel |
| `aat run clean` | Delete old, unsaved run archives |
| `aat run rebuild-summaries` | Rebuild run summaries from the full archives |
| `aat validate` | Check the graph, templates, workflows, layers, and plans (`--strict` fails on warnings) |
| `aat web` | Browse runs and batches in the web UI |
| `aat web view [latest\|file.aar]` | Open one run, or an exported archive, in the browser |
| `aat import <file.aar\|file.aab>` | Import an exported run or batch |
| `aat env list` | List the environments of the environment file |
| `aat plan list` | Summarize the saved plans and recipes |
| `aat mcp serve` | Serve the project to AI coding tools over MCP |
| `aat generate --oas <spec>` | Scaffold a graph and templates from an OpenAPI spec |
| `aat docs generate` | Write Markdown documentation from the graph |
| `aat prompt "<text>"` | Draft a plan from a sentence (needs LLM configuration) |
| `aat-sandbox serve` | Run the offline shop API and payments API |
| `aat-sandbox init <dir>` | Extract the shop example project |

## Documentation

The full documentation is at **[gburgyan.github.io/aat](https://gburgyan.github.io/aat/)**. Good places to start:

- [Shop example](https://gburgyan.github.io/aat/examples/shop/): every command above, explained
- [Tutorial](https://gburgyan.github.io/aat/tutorial/): build a project by hand against the sandbox
- [Matrix testing](https://gburgyan.github.io/aat/batch-layers/): layers, layer groups, and dedup
- [Environments](https://gburgyan.github.io/aat/environments/): auth, `extends`, `vars`, and per-operation routing
- [MCP server](https://gburgyan.github.io/aat/mcp-server/): personas, tools, and IDE setup
- [Share your API with integrators](https://gburgyan.github.io/aat/integration-kit/): package a kit in CI
- [CI/CD](https://gburgyan.github.io/aat/ci-cd/): exit codes, JSON output, and JUnit

## Status

Pre-1.0, with one maintainer. AAT was built and proven against a 74-node airline API with 63 workflows, 38 addons, and 6 layers. The graph and plan formats may still change before 1.0; breaking changes are listed in the [changelog](CHANGELOG.md), and the [roadmap](ROADMAP.md) says what's next.

## Contributing

Contributions are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). By submitting a pull request, you agree to the [Contributor License Agreement](CLA.md).

## License

Apache 2.0; see [LICENSE](LICENSE).
