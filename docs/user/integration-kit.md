# Share Your API with Integrators

An OpenAPI spec describes an API one call at a time. The AAT project you build to test that API describes how the calls work together: which calls reach a goal and in what order, where each input comes from, which fields of a large schema matter, what a failure looks like, and which call undoes which. Your own test runs keep all of it true.

Package part of that project as an **integration kit** and give it to the teams who integrate with you. Their AI coding tool reads the whole workflow through `aat mcp serve`, in a form it can act on, and writes a working client in whatever language they use. On a 74-node airline API, each such client took a single prompt, in Java, C#, Go, Python, Perl, and Lisp. That project is private, but the same test on the shop example is [reproducible](#reproduce-the-single-prompt-test), prompt included. Integrators can also run your reference flows against your sandbox and see the real exchanges.

One framework pays off twice: first when you test your API, then when others integrate with it.

## What a Kit Tells an AI Tool

| An integrator needs to know | An OpenAPI spec says | The kit adds, through the `api` persona |
|---|---|---|
| Which calls reach a goal, and in what order | Nothing | Integration flows composed step by step, each step with its HTTP method, path, and dependencies (`list_integration_flows`, `get_integration_flow`), and the operations a goal needs (`trace_dependency_chain`) |
| Where each input comes from | Its type | The earlier output that feeds it, per operation and as a data-flow summary for each flow (`describe_operation`, `get_data_flow`, `get_integration_flow`) |
| Which item of a list to use | Nothing | Named selections with their strategy and filter, such as the first product whose `inStock` is true |
| Which fields matter | Every field the schema allows | The fields each request template sends and each extraction rule reads (`inspect_request_template`, `get_response_shape`) |
| What a failure looks like | Status codes | Rules for failures reported inside a successful response, and what the operation and field descriptions say about error codes (`describe_operation`) |
| What undoes what | Nothing | Cleanup pairings, such as `checkoutCart` and `deleteOrder` |
| Which values are valid together | Enums, field by field | Domain concepts with their constraints, and value pools (`list_concepts`, `explain_field`) |
| What a real response looks like | Examples, if someone wrote them | Responses recorded by runs of your reference flows (`get_sample_response`) |

The tools serve what the project says. Descriptions on operations, inputs, and outputs, the domain file's concepts, and the README next to the graph make explicit the rules your tests already follow: that a charge must equal the order total, which host takes which credential, which error code an out-of-order call returns. The more of that the project states, the more a single prompt gets right. The shop's graph and domain file show the level of detail to aim for.

## What Goes in a Kit

A kit is a package you build from your project, typically in CI. It doesn't have to mirror the project; it only has to be cheap to rebuild.

| Ship | Keep internal |
|------|---------------|
| The graph, request templates, OpenAPI spec, and domain file | Negative, chaos, and regression plans |
| Workflows, which integrators see as integration flows | Layers and overlays |
| Reference plans for the main journeys | Environments with internal hosts or credentials |
| An environment for your public sandbox or test API, with credentials read from the integrator's environment variables | Run archives, visualizers, and `.aat-overrides.yaml` |
| A README for integrators, and per-node docs if you write them | |

## Layout: One Project, Two Manifests

Keep a single project. Put the suites you don't ship in a directory of their own, and add a second manifest that names what you do ship:

```
my-api-tests/
  aat-project.yaml     your manifest: everything
  aat-kit.yaml         the kit manifest: what integrators get
  graph.yaml  openapi.yaml  domain.yaml  env.yaml
  templates/  workflows/
  plans/               reference plans (shipped)
  internal/plans/      negative, resilience, regression (not shipped)
  layers/  overlays/  visualizers/
  KIT-README.md        becomes the kit's README.md
  package-kit.sh       builds the kit
```

Your manifest lists both plan directories. A plan's name is its path inside its directory, so `internal/plans/negative/state-machine.yaml` is still called `negative/state-machine`:

```yaml
# aat-project.yaml (excerpt)
plans: [plans/, internal/plans/]
layers: layers/
visualizers: visualizers/
```

The kit manifest uses the paths its files will have inside the kit, so packaging copies it unchanged as `aat-project.yaml`:

```yaml
# aat-kit.yaml
name: shop
description: Integration kit for the shop API
graph: graph.yaml
templates: templates/
domain: domain.yaml
workflows: workflows/
plans: plans/
environment: env.yaml
defaultEnvironment: us
archives: _output/runs
```

Before you package anything, `aat validate --strict --manifest aat-kit.yaml` checks the kit, and `aat mcp serve --manifest aat-kit.yaml --persona api` shows it the way an integrator's tool will see it.

### Environments You Don't Ship

Keep internal environments in a second file that includes the shipped one, and point your manifest at it with `environment: env.internal.yaml`:

```yaml
# env.internal.yaml
include: [env.yaml]
environments:
  staging:
    extends: _base
    vars:
      region: us
      postalCode: "78701"
      apiHost: shop.staging.internal
      payHost: pay.staging.internal
```

`staging` extends the `_base` environment from the included file. When both files define the same environment or `shared` key, the included file wins, so give internal environments names of their own. See [Environments: File Splitting with `include`](environments.md#file-splitting-with-include).

## Package It in CI

The shop's `package-kit.sh` is the whole packaging step:

```bash
sh package-kit.sh _output/shop-kit
```

It copies the files the kit manifest names into `_output/shop-kit/`, with `aat-kit.yaml` as `aat-project.yaml` and `KIT-README.md` as `README.md`. Then it writes `_output/shop-kit.tar.gz`. When the kit needs a new file, such as an environment include or a docs directory, add it to the script's copy line.

Check the package the way an integrator will use it, then publish it. In GitHub Actions, with your sandbox reachable from the job:

```yaml
- name: Package the integration kit
  run: sh package-kit.sh _output/shop-kit
- name: Validate and run the packaged kit
  working-directory: _output/shop-kit
  run: |
    aat validate --strict
    aat run batch --oas-validate strict
- uses: actions/upload-artifact@v7
  with:
    name: shop-kit
    path: _output/shop-kit.tar.gz
```

The reference plans run in your pipeline next to your internal suites. A change to the API that breaks an integration flow fails your build before an integrator runs into it.

## What the `api` Persona Reads

`aat mcp serve --persona api` is read-only. From the manifest it loads:

- the graph, and the workflow templates it names (resolved next to the graph file)
- every template in the templates directory
- the domain file, the OpenAPI specs, and the per-node docs directory
- `README.md` next to the graph file, served as `aat://readme`
- run archives, for `get_sample_response`
- the manifest's name, description, and tags

No `api` tool reads plan directories, layers, or overlays, and none exposes the environment file, although the server still loads it at startup when `environment:` is set. Everything else in those files can reach the integrator's tool. Keep internal-only templates out of the kit's templates directory, and internal notes out of its docs and README.

The `test` persona, and a server started without `--persona`, can also list and run plans and read every archive; see [MCP Server](mcp-server.md#personas).

## The Integrator's Side

Unpack the kit into the client's repository and register the server in `.mcp.json`:

```
their-app/
  .mcp.json
  vendor/shop-kit/     the unpacked kit
```

```json
{
  "mcpServers": {
    "shop-api": {
      "command": "aat",
      "args": ["mcp", "serve", "--manifest", "vendor/shop-kit/aat-project.yaml", "--persona", "api"]
    }
  }
}
```

Then:

- **Ask for a client.** The tool looks up operations, request templates, and integration flows instead of guessing at request and response shapes.
- **Run a reference flow** against the sandbox to see real exchanges: `cd vendor/shop-kit && aat run plan full-lifecycle`, then `aat web view latest`. The run's archive also gives `get_sample_response` real responses to return.
- **Start from live state.** `aat run plan smoke --stop-after paymentCharge --dump-state state.json --dump-state-secrets` stops with a paid order still live, and the state file holds its IDs and the session's live credentials for the new client to pick up. Without `--dump-state-secrets`, the credentials read `[REDACTED]`. See [Checkpoints](checkpoints.md).

## Current Limits

- A manifest names one graph, one templates directory, and one workflows directory. An operation or workflow that must stay internal cannot be added on top of a kit, so keep it out of the shipped files.
- `package-kit.sh` copies a fixed list of files. Nothing derives the list from the kit manifest yet.

## The Shop Does This

[`examples/shop`](examples/shop.md) uses this layout. Its `shop-api` MCP server reads `aat-kit.yaml`, and `shop-test` reads the whole project. `make example-shop` packages the kit, unpacks it into an empty directory, and validates and runs it there against the sandbox.

## Reproduce the Single-Prompt Test

The airline project passed a simple test: one prompt, a working client. The shop's kit went through it with less to go on: the packaged kit alone, without the rest of the project. The setup and the prompt below are the ones that were used, so you can run the test yourself.

**Set up an integrator's directory** that holds nothing but the packaged kit:

```bash
mkdir kit-test && cd kit-test
aat-sandbox init shop                                # the example project
aat-sandbox serve &                                  # shop API on :8765, payments API on :8766
sh shop/package-kit.sh integrator/vendor/shop-kit
cd integrator
cat >.mcp.json <<'EOF'
{
  "mcpServers": {
    "shop-api": {
      "command": "aat",
      "args": ["mcp", "serve", "--manifest", "vendor/shop-kit/aat-project.yaml", "--persona", "api"]
    }
  }
}
EOF
```

**The prompt**, verbatim. The Go run replaced `Python 3 (standard library only)` with `Go (standard library only)`:

> Using only the shop-api MCP server for knowledge of the shop API (no web search, no guessing), write a
> working Python 3 (standard library only) command-line client for the shop API in this directory. It must
> place an order for two different in-stock products, pay for it by card, then cancel the order and refund
> the payment, and print the order ID, the order total, and the order's final status and payment status.
> The shop sandbox is already running locally. Run the client and show me its output.

You can paste it into Claude Code opened in `integrator/` (approve the `shop-api` server when asked). The measured runs were stricter. They used headless Claude Code, with the prompt saved as `prompt.txt`. The kit's server was the session's only MCP server, the session could read and write only its own directory and run only the language toolchain, and web tools were off:

```bash
claude -p "$(cat prompt.txt)" \
  --mcp-config .mcp.json --strict-mcp-config \
  --allowedTools "mcp__shop-api__*" "Read(/$PWD/**)" "Write(/$PWD/**)" "Edit(/$PWD/**)" \
    "Bash(python3:*)" "Bash(ls:*)" "Bash(mkdir:*)" \
  --disallowedTools WebFetch WebSearch
```

For the Go client, allow `Bash(go:*)` instead of `Bash(python3:*)`.

**Results** with Claude Code 2.1.269 and the model `claude-opus-5`, on 2026-09-11:

| Client | First run | Time | Turns | MCP tool calls | Cost | Kit files read |
|--------|-----------|------|-------|----------------|------|----------------|
| Python 3, standard library | worked | 85 s | 28 | 20 | $0.58 | none |
| Go, standard library | worked | 146 s | 34 | 23 | $0.77 | none |

Each client authenticated, bought two in-stock products, paid by card, cancelled the order, and refunded it. Reading each order back from the sandbox showed `cancelled` and `refunded`. Both sessions relied on `get_oas_operation` together with `list_integration_flows`, `get_integration_flow`, `list_concepts`, `explain_concept`, and `explain_field`. The Go session's one hiccup came from the lockdown: it was not allowed to run the binary it built, so it used `go run .` instead.

Your times, turn counts, and costs will differ with the model and from run to run. The claim to check is the outcome: a client written from the kit alone that works on its first run.
