# Changelog

All notable changes to AAT are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow semver with a 0.x caveat:
the graph and plan formats may still change before 1.0.

## [Unreleased]

### Added
- `settings.minRequestInterval` paces requests for APIs with rate limits: the starts of any two requests are at
  least that far apart, such as `250ms`. One interval covers everything a command sends, including the plans of
  a parallel batch, retries, verification, and cleanup. `aat prompt` and the MCP server's `execute_plan` honor it
  too. A value without a unit, such as `250`, is rejected when the environment file loads.
- Step retries honor the server's `Retry-After` header, and on a 429 without one, `RateLimit-Reset`. A retry waits
  at least as long as the failed response asks, in seconds or until an HTTP date, instead of only the backoff.
- Cleanup chains: a cleanup node can declare its own `cleanup`. It runs right after that cleanup step succeeds and
  takes its outputs first, so a resource released in two calls (request, then confirm) is cleaned up completely.
  Chained outputs stay inside the chain. Archive cleanup records gain `cleanupFor` (`cleanup_for` in `--json`),
  naming the step each one cleans up after.
- `AUTOWIRE?` in workflow templates marks an optional input that only some compositions feed. It is wired when a
  step produces the output, such as one an addon adds, and left unset otherwise.
- `aat generate` scaffolds HEAD, OPTIONS, and TRACE operations, form-encoded request bodies, and cookie parameters,
  which it sends as one `Cookie` header. Body and response properties from `allOf` branches become inputs and
  outputs, and an OpenAPI 3.1 type list such as `["null", integer]` maps to its non-null type. The MCP server's
  operation search lists operations of every method too.
- `aat run show <run>` prints what an archive recorded, without a browser.
  - Without `--step`, it lists the steps with their node, HTTP status, result, duration, and output names, then the
    verification and cleanup steps.
  - `--step` shows one step. `--request`, `--response`, `--inputs`, and `--outputs` print that part as JSON.
  - `--path` narrows the part with a gjson path.
  - `--shape` prints the part's structure: each path with its type, array sizes, how many objects hold a key, and a
    sample value.
  - The run is `latest` (runs inside batches included), a run ID, a batch ID and run ID joined by a slash, or a path
    to a run directory, an archive file, or an `.aar` export.
- The MCP server's `get_sample_response` takes `path` and `shape`, as `aat run show` does.
  - Its `run_id` accepts `latest` and a batch ID with a run ID, and finds runs inside saved batches.
  - The same goes for `inspect_archive`, `analyze_failure`, and `diff_archives`.
- The AI assistant primer is published as raw Markdown, for tools that fetch pages.
  - `llms-full.txt` at the docs site's root holds the whole primer, and `llms.txt` indexes it and the reference pages.
  - `aat docs primer` prints the same primer from the binary, in the version that matches it.
  - The primer links to docs pages by URL, so its links also work outside the site.
- OAS validation checks form-encoded request bodies (`application/x-www-form-urlencoded`) against the operation's
  schema. Bracketed keys such as `items[0][sku]=…` and `tags[]=…` are read as nested objects and arrays, and values
  take the types the schema allows. Before, only JSON request bodies were validated, and a form body was not checked.

### Changed
- Composition wires the AUTOWIRE markers that the slot and addon passes leave, once the plan is complete: a base or
  slot step can take an output that only an addon produces, and a base workflow without slots or addons resolves
  its markers. The final pass takes the nearest earlier producer that does not depend on the step. Markers the
  earlier passes already wired are unchanged.
- A plan that still holds an `AUTOWIRE` marker after composition fails validation, naming the step and input,
  instead of sending the word `AUTOWIRE`. `aat prompt` asks the model for those inputs, and `aat validate plan`
  composes standalone base workflows before validating them.
- Workflow compatibility checks cover base workflows and slot options. A plain `AUTOWIRE` that the base and its slots
  cannot feed is a warning that names any addon producing the output. `AUTOWIRE?` on a required input with no graph
  default is also a warning. `AUTOWIRE?` in an addon never warns as unfed.
- `aat generate` no longer replaces an existing graph file or template: it lists them, writes nothing, and exits 2
  unless given `--force`. OperationIds that differ only in case are an error, since their templates would be one
  file on a case-insensitive file system.
- A generated template sets `Content-Type` only when it has a body, to that body's media type; before, every operation
  with a request body got `application/json`. `aat generate` warns about what a template leaves to write by hand:
  multipart and other bodies, a body schema with `oneOf`, `anyOf`, or no properties, and a parameter with a
  non-default `style` or `explode: false`. OAS validation reads request body properties from `allOf` branches too.
- A list value in a request URL or form body is no longer sent as JSON text. Right after `key=` in a query or form body
  it repeats the pair (`tags=a&tags=b`); anywhere else its elements are encoded one by one and joined with commas
  (`/items/1,2`).
- A step whose failed response asks for a wait longer than 60 seconds stops retrying (`failed_fast`), and the
  error detail says how long the server asked for. Before, the retries went out after the backoff regardless.
- Docs: the Homebrew cask is documented for Linux as well as macOS. `brew install gburgyan/tap/aat` installs
  `aat` and `aat-sandbox` with Homebrew on Linux.
- `min` and `max` selection compare numbers sent as strings by value: a `sortField` of `"99.10"` sorts below
  `"1000.00"`, so an API that returns prices or totals as decimal strings selects the cheapest element. A
  string that is not a number still fails the selection, naming the element.
- Graph validation rejects cleanup pairings that loop back, such as a node cleaned up by `b` whose own cleanup is
  that node again. The error names the cycle once, as `a → b → a`.
- Cleanup step IDs are unique within a run: a node's second cleanup step is `deleteCart_2` in archives and `--json`
  output, as the web UI already named it. Cleanup responses are checked against the graph's error detection rules.
  A flagged response records `responseBodyError` and ends its chain; the run outcome is unchanged.
- **Breaking:** `aat run plan --dump-state` redacts credentials by default, as run archives do.
  - Credential headers such as `Authorization` and `X-API-Key` read `[REDACTED]`, at the top level and in every step.
  - Known secrets are replaced wherever they appear, and the export gains `"redacted": true`.
  - `--dump-state-secrets` keeps live credentials, for a harness that sends requests as the run's session. It warns
    on stderr when the dump goes to stdout, and it is an error without `--dump-state`.
  - A harness that reads `auth.headers` to send requests needs the new flag.
- Docs: the AI assistant primer covers more ground:
  - layers and batches
  - step value forms: pools, literal lists, and `{}`
  - expressions and assertion details
  - retries and selection ties
  - Lua transforms, and reading results with `aat run show`
- Docs: three pages disagreed with the code and are corrected:
  - `plans.md` no longer says `{}` skips graph defaults for required inputs.
  - `value-flow.md` says an inline `min` or `max` can use `field` alone, and how ties break.
  - `validation.md` no longer claims plan validation checks assertion types.
- `--oas-validate strict` stops before the first request, with exit code 2, when a spec the graph references fails to
  load. Before, the run printed a warning and continued without validating, and `--quiet` and `--json` hid the
  warning. In `auto` mode the warning now goes to stderr, where `--quiet` and `--json` keep it visible. `aat prompt`
  behaves the same way.

### Fixed
- OpenAPI specs with circular references load. A schema that refers back to itself, directly or through another
  schema, used to fail with `infinite circular reference detected`, so `aat generate`, `aat validate`, and the MCP
  server rejected the spec, and `aat run` skipped OAS validation. Large published specs have such cycles. Other
  errors in a spec still fail it, and libopenapi's log lines no longer reach stdout.
- Runs with OAS validation start quickly on a spec with hundreds of operations. The validator is built for the
  operations the graph's nodes name, not for the whole spec, which could take most of a minute before the first
  request.
- OAS validation accepts `null` for an OpenAPI 3.0 schema marked `nullable: true` that is built with `anyOf` or
  `oneOf`, such as a field that holds either an ID or an expanded object. The validator added `null` only to a
  schema's own `type`, so a response with such a field set to `null` failed with `got null, want object`.
- A request body that is neither JSON nor form-encoded, or a schema the validator can't compile, is reported as not
  validated. The archive marks the payload `skipped` with a reason, the step line shows `OAS: request not validated` or
  `OAS: response not validated`, and a `schema` assertion is skipped with the reason. Before, the step line could read
  `OAS: 0 warning(s)`, and a `schema` assertion failed with an empty message.
- `aat generate` writes the graph's `oas:` reference relative to the graph file's directory, so a spec kept elsewhere
  resolves. It used to write only the spec's file name.
- `aat generate` types object body properties `object` and inserts them as JSON literals instead of quoted strings,
  and no longer writes a node-level `name:` line.
- `aat run plan` and `aat run batch` redact the access token they authenticated with, such as an OAuth2 token,
  wherever it appears in an archive. Before, it was masked only in credential headers.

## [0.1.0] - 2026-09-12

The first release with release archives, a Homebrew cask, and a Docker image. v0.0.1 through v0.0.4 are
retracted, so `go install` and `go get` skip them.

### Added
- `aat-sandbox`, a second binary that serves an offline e-commerce demo API (`aat-sandbox serve`:
  shop API on :8765 with OAuth2 tokens, payments API on :8766 with an API key, `us`/`eu` regions
  with their own currency, tax, tiers, and coupons, an order state machine, simulated latency, and
  two chaos hooks for retry demos) and extracts the `examples/shop` project (`aat-sandbox init`).
  The contract lives in `examples/shop/openapi.yaml`; the server tests validate every response
  against it. `make build` builds both binaries; `make sandbox` builds only the demo server.
  The sandbox binds `127.0.0.1` unless `--host` says otherwise.
- `examples/shop`, the offline quick-start project for `aat-sandbox`: a 17-operation graph, Quick
  Purchase and Checkout workflows with slots and addons, 12 layers, 7 plans (full order lifecycle,
  retries, a negative state-machine walk, `addItem` mutations), a declined-card overlay, a receipt
  visualizer, `us`/`eu` environments with payments routed to their own host and credential, and MCP
  configuration for AI coding tools. Its graph describes the API for the AI tools that read it through MCP:
  each operation lists its error codes in the order they are checked, and each input and output says what
  it must be, where it comes from, and what it means.
- The shop example ships an integration kit, the part of an AAT project that an API's integrators get.
  `aat-kit.yaml` names the graph, templates, OpenAPI spec, domain file, workflows, sandbox environment,
  and three reference plans. `sh package-kit.sh` copies them into a directory and a tarball, with the kit
  manifest as its `aat-project.yaml`. The `shop-api` server in `.mcp.json` reads the kit manifest, so it
  shows what an integrator's AI tool would see. `make example-shop` packages the kit, then validates and
  runs the unpacked copy.
- Multi-environment files: one environment file holds named environments with shared settings, inheritance
  (`extends`), `${var}` substitution in every string, abstract environments (names starting with `_`) to
  inherit from, and `include:` to split out files such as secrets. `--env NAME`, `AAT_ENV_NAME`, an overlay's
  `environment:`, or the manifest's `defaultEnvironment` selects one, in that order. `defaultEnvironment`
  applies only to the manifest's own environment file, and a single-environment file still loads, with
  `--env` an error for it. `aat env list` lists the environments, and `aat validate` checks each one.
- `--var KEY=VALUE` (repeatable) on `aat run plan`, `aat run batch`, `aat prompt`, `aat validate`,
  `aat env list`, and `aat mcp serve` sets a var of a multi-environment file for one invocation, for
  example to point `examples/shop` at a sandbox on other ports or in a container. A key the file never
  declares or references is an error.
- An environment's `values:` map holds per-environment data that plan values, graph input defaults, and
  layers read with `{{env.KEY}}`; an OS environment variable `KEY` wins over it.
- Overlays (`--overlay FILE`, `.aat-overrides.yaml`, and now `aat-overrides.yaml`) can set `auth` and
  `headers` for every request, and `environment:` to choose the environment. Overlay headers win over the
  credential on every route. `overrides:` entries in environment files and overlays accept `values:` and
  `expectFailure:`, so a happy-path plan reruns as a negative test without edits; the archive records each
  override value as the input's resolution, with the source `override_value`.
- OAuth2 auth accepts `grantType` and `extraParams` for providers such as Auth0. `--verbose-auth` logs token
  requests and responses to stderr, with the access token masked and at most half of a short password or
  client secret shown.
- Step `mutations:` expand a step into independent sibling steps when the plan is instantiated, each with
  `set:` input overrides or a `rawBody:` for a malformed payload. `mutationScope: isolated` gives each
  sibling its own copy of the steps it depends on, and `--no-mutations` drops mutations for a quick run. The
  `schema` assertion type validates a response against the OpenAPI spec; it was a stub.
- Checkpoints: `aat run plan --stop-after STEP` stops after a step and skips cleanup, so what the run created
  stays alive, and `--dump-state FILE` (`-` for stdout) exports the live session for another tool: the
  environment's base URL and auth headers, each step's base URL and request headers, unredacted, and the
  step outputs. The file is written with mode `0600`. With `--json`, the summary nests the state under
  `state` and reports `stopped_at`.
- Ctrl+C ends `aat run plan` and `aat run batch` as `aborted` (exit code `130`), even during a request or a
  retry wait. Cleanup still runs, within a 30-second budget, and the archive is written. The web UI styles
  the `aborted` and `stopped` outcomes.
- `aat mcp serve --http` serves MCP over Streamable HTTP, with a `/healthz` endpoint. `--log` logs each tool
  call as JSON to stderr.
- `--host` on `aat web`, `aat web view`, `aat web viewtrace`, and `aat mcp serve` chooses the interface
  to bind (default `127.0.0.1`; the `AAT_HOST` environment variable sets it when the flag is absent).
  The Docker image sets `AAT_HOST=0.0.0.0`.
- Plan-level `execution.cleanup` steps execute after the main flow, honoring `runOn: always|success|failure`.
  A step that names a node paired with a graph `cleanup:` does not run on its own: its `runOn` decides
  whether the pairings run, once per created resource, newest first, and never for a create step that
  failed. Other cleanup steps run first, in declaration order.
- Plan `execution.verification` steps execute after the main flow and before cleanup, with their
  assertions counting toward the run outcome. They appear in archives as `verify_<node>` steps.
- Workflow slot options and addons can declare `verification:`. Composition merges it into the base
  workflow's, and a node they verify replaces the base's verification of that node.
- A step that succeeds after retrying shows `retried Nx: <category>` in run output and on the web UI's
  timeline and step page. Archives record the category of each retried attempt in `retriedOn`, and the
  `--json` step summary in `retried_on`.
- Run output lists each failed assertion under its step (`status: expected status 200, got 201`), and the
  `--json` step summary includes them as `failed_assertions`.
- `status` assertions accept a status class such as `expect: 2xx` or `expect: 4xx`.
- `retry.on` and `retry.failOn` accept HTTP status codes (`on: [503]`) alongside category names;
  `aat validate plan` rejects unknown rules.
- `--oas-validate strict` (and `settings.oasValidation: strict`) now fails a step whose request or
  response violates the OpenAPI spec; it previously behaved like `auto`. `expectFailure` steps are
  exempt, and skipped validations or schema compilation warnings never fail a step.
- `aat validate` checks the layers directory: parse errors, duplicate layer names, and layer input keys
  that match no node input (which layers silently ignored).
- `aat validate` checks the domain file and `visualizers.yaml` (Domain and Visualizers sections), and
  names each error's file relative to the working directory.
- Template conditional and iteration blocks accept keys with hyphens, such as
  `{{?X-Request-Id}}…{{/X-Request-Id}}` for a header parameter.
- The web UI's step request view copies the request as a `curl` command.
- `aat run clean` deletes auto-generated run and batch archives and keeps named ones.
  `tools/aat-to-junit.py` converts a run archive to JUnit XML for CI test reports.
- Workflows accept a `selectionHint` that `aat prompt` shows the model when it chooses a workflow. The
  trace viewer's Copy Conversation button copies a planning call's model settings and conversation as JSON.
- `aat web` reports a clear error (exit code 2) when the frontend bundle is not embedded, such as a
  plain `go install` build; the CLI, MCP server, and CI features still work in that build.
- `aat --version` reports the module version for `go install …@vX.Y.Z` builds.
- Release pipeline: versionless archive names for stable `releases/latest/download/…` URLs, a
  Homebrew cask in a tap (`brew install gburgyan/tap/aat`), and a multi-arch image at
  `ghcr.io/gburgyan/aat`. Release archives and the Homebrew cask include `aat-sandbox` next to `aat`, and
  the release notes are this file's section for the version.
- `make example-shop` runs `examples/shop` against a local sandbox the way the CI `example-shop` job does,
  and `make cli` builds `aat` without rebuilding the web UI.
- A documentation site built from `docs/user` with Material for MkDocs (`mkdocs.yml`), deployed to GitHub
  Pages by a new Docs workflow that fails on broken links, broken anchors, and pages missing from the
  navigation. `make docs` runs the same strict build locally.
- Documentation pages for installing, checkpoints, archives, `aat generate`, `aat docs generate`, the
  examples, and the airline case study; the Lua transforms page is written out. The quickstart (on the
  public Petstore API) and the tutorial (built by hand against `aat-sandbox`) are rewritten and were run
  verbatim; the previous versions did not load.
- Docs: *Share Your API with Integrators* describes an integration kit for any API: what to ship and what
  to keep, a kit manifest beside the project manifest, internal environments that `include:` the shipped
  file, packaging and checking the kit in CI, and what an integrator's AI tool can read through the `api`
  persona. It publishes the prompt, the setup, and the results of the shop's single-prompt Python and Go
  client runs, so the claim that a kit lets an AI tool write a working client from one prompt can be
  checked. The docs home page, the examples index, Project Setup, and MCP Server link to it.
- `make demos` regenerates the docs site's recordings and screenshots against a fresh `aat-sandbox`: VHS
  recordings of `aat run plan full-lifecycle` and a parallel layer-group batch, Playwright screenshots of
  the run timeline, a step's request with Copy as cURL, and the batch matrix, plus an MP4 of the plan run
  and the repository's social preview (`demos/`). It checks that both recorded runs passed and that each
  GIF stays within its size budget before writing anything. The docs site's home, matrix-testing, and web UI
  pages show the results.
- Repository scaffolding: issue and pull request templates, `SECURITY.md`, and `ROADMAP.md`.

### Changed
- **BREAKING:** the environment file flag `--env FILE` is now `--env-config FILE`, and `--env NAME` selects a
  named environment. `--env-overlay` is now `--overlay`.
- **BREAKING:** exit codes follow one rule on every command:
  - `0` passed.
  - `1` a test or validation ran and found a failure.
  - `2` AAT could not do what was asked. That covers an unknown flag, argument, or subcommand, and a project,
    environment, or `--var` error. Most commands exited `1` for these.
  - `130` aborted.

  Command by command:
  - `aat validate` exits `1` when it finds a problem, and `2` when there is no manifest to validate or
    `--var` is bad.
  - `aat prompt` exits with its run's outcome code, instead of `1` for any run that did not pass.
  - `aat import`, `aat generate`, and `aat docs generate` exit `2` on an error.
- **BREAKING:** project YAML is decoded strictly. A key that no field accepts — in the manifest,
  environment files and their includes, overlays, the graph, templates, the domain file, visualizers,
  workflows, layers, plans, recipes, and plan YAML given to the MCP plan tools — is an error naming the
  file, the line, and the likely intended key (`plans/smoke.yaml: line 12: unknown key "fromSelecton"
  in step value (did you mean "fromSelection"?)`). Such keys were silently ignored, so a typo produced a
  plan that loaded and did something else. A second YAML document in a file (`---` followed by content)
  is an error too. `aat prompt` model output (JSON) is unaffected. To migrate, run `aat validate` and fix
  what it lists.
- **BREAKING:** a manifest that exists but fails to load is an error for every command that discovers
  it; it was skipped, so commands fell back to a lower-priority project or to none. A missing manifest
  is still skipped, and a higher-priority manifest that loads still wins.
- **BREAKING:** request templates escape each substituted value for where it lands. Values used to go in
  raw.
  - **Path:** a value is URL-encoded as one path segment before the first `?`, and as a query component after
    it. `a/b` stays one segment, and `&` or `#` in a value can no longer add a parameter or cut the URL.
  - **JSON body:** a value inside quotes is JSON-escaped, so a quote, backslash, or newline in a field such
    as `notes` no longer breaks the body. A value outside quotes is written as JSON: arrays and objects as
    JSON, numbers in plain digits, and `null`.
  - **Form body:** values are URL-encoded.
  - **Headers and other bodies:** unchanged.

  To send a malformed payload on purpose, use a step's `rawBody`.
- **BREAKING:** JSON keys follow the convention of the document they are in:
  - The `aat run batch --json` summary's `batchId` is now `batch_id`. It is left out when the batch stopped
    before it started.
  - In run archives, a step's `duration_ms` is now `durationMs`. AAT and `tools/aat-to-junit.py` still read
    archives that use the old key.
  - A plan's `auth` in `metadata.plan` and `metadata.instantiatedPlan` uses camelCase keys (`tokenUrl`,
    `credentials`) instead of Go field names (`TokenURL`, `Credentials`).

  The `state` object that `--json` nests with `--dump-state -` keeps the camelCase keys of the `--dump-state`
  file.
- **BREAKING:** a recipe's `overrides` must name steps of the composed plan. An override for any other step ID,
  such as an addon step without its `inc0_` prefix, used to be ignored; now the recipe fails to load, and the
  error lists the plan's steps. A value override on an input that the workflow template wires with `from`,
  `fromSelection`, or `fromInput` now replaces that wiring and sends the override. It used to have no effect.
- **BREAKING:** Lua transforms can no longer load code or reach the host process. The `package` library is gone,
  and so are the base library's `dofile`, `loadfile`, `load`, `loadstring`, `require`, `module`, `getfenv`,
  `setfenv`, `collectgarbage`, and `newproxy`. An integration kit's templates run on its users' machines, so a
  transform must not read their files. Also, `dofile()` and `loadfile()` with no argument read stdin, which under
  `aat mcp serve` is the MCP connection.
- **BREAKING:** `aat run batch <filter>` selects plans by whole path segments. `orders` selects `orders.yaml`
  and every plan under `orders/`, and `orders/refund` selects one plan. The filter matched the start of each
  plan's path, so `smoke` also ran `smoke-eu.yaml`.
- **BREAKING:** a batch that finds no plans exits `2`, and the message names the filter and the plan
  directories. It used to pass with nothing run, so a mistyped filter passed in CI. An absolute path that does
  not exist gets the same error.
- **BREAKING:** `aat run batch --json` reports an error that stops the batch before any plan runs in a top-level
  `error` field, with an empty `runs` array. It used a run entry with no plan name.
- **BREAKING:** only `aat run plan` and `aat run batch` take the execution flags: `--env`, `--env-config`,
  `--graph`, `--templates`, `--domain`, `--override`, `--overlay`, `--var`, `--retries`, `--layer`,
  `--no-auto-overrides`, `--oas-validate`, `--verbose-auth`, and `--no-mutations`. `aat run clean` and
  `aat run rebuild-summaries` accepted and ignored them; they now reject them.
- Template headers no longer replace the auth credential, an override's own headers, or overlay headers. They
  still replace environment and plan headers, such as a per-operation `Content-Type`. Header names compare
  case-insensitively when headers merge.
- Run output names each step by its step ID, which `--stop-after`, `dependsOn`, the archive, and the web
  UI use, with the node in parentheses when the two differ and the column has room
  (`addProduct (addItem)`); it printed the node, so two steps on one node looked alike. Engine errors do
  the same (`step "addSocks" (addItem) returned status 409`), and so do the parallel batch display and the
  MCP `execute_plan` table.
- Durations are wall-clock: a retried step's duration runs from its first attempt to the end of its last,
  so retry waits count, and a run's duration (the `PASSED` line, the `--json` `summary.duration_ms`,
  `batch.json` run entries, the web UI, and MCP) is the time the run took, recorded in the archive as
  `result.durationMs`. Both were sums of the last attempt of each step, so `full-lifecycle` printed `955ms`
  for a three-second run. Archives written before keep showing the sum. Step durations of a second or more
  read `1.4s`.
- A step that fails after retrying prints its error followed by the same `retried Nx: <category>` note as
  a step that recovers; the note used to take one of two other forms depending on the terminal width.
- `aat run` progress output marks OpenAPI violations on each step (`OAS: 1 warning(s)`) and totals them
  after the outcome, as documented; only an unused summary path printed them before.
- `aat validate` and `aat validate workflow` show OpenAPI and workflow-compatibility warnings as a `WARN`
  section without `--strict` instead of reporting `OK`, and counts read "1 file" rather than "1 files".
- The sequential batch header no longer prints `mode=strict`, a leftover of the runtime modes removed in
  0.0.2.
- `aat web` and `aat mcp serve --http` listen on `127.0.0.1` by default instead of every interface.
  Pass `--host 0.0.0.0` (or set `AAT_HOST`) to accept connections from other machines.
- Steps composed from workflow templates (recipes, `aat prompt`) get a default `status: 2xx`
  assertion instead of `status: 200`, and none when they declare `expectFailure`. On a step with
  `expectFailure` (including one added by an overlay), a `status` assertion that expects success is
  reported as skipped, since the expected-failure status list is the status check; one that agrees
  with it, such as `409` or `4xx`, is evaluated.
- The `--json` step summary's `name` is the step ID, as documented, instead of the node name, so
  mutation siblings and repeated nodes are distinguishable.
- `aat generate --oas` places optional query parameters, headers, and body properties in conditional
  blocks, orders body properties as the spec does, and writes integer, number, boolean, and array body
  values as JSON literals. With `--output-graph -` it writes no files unless `--output-templates` is
  given. A template header that resolves to an empty conditional is not sent.
- A layer that sets a value source (`value`, `pool`, `from`, `fromResolved`) replaces the graph
  default's source instead of merging with it, so a layer value is no longer shadowed by a default's
  `from`.
- Requesting layers (`--layer`, `--layer-group`, or a recipe's `selection.layers`) without a
  `layers:` directory in the manifest is an error; the layers were silently ignored before.
- An override that declares its own `auth` no longer sends the inherited credential header
  (`Authorization`, or the top-level API key header) to its host.
- Override precedence: among glob (and among exact) overrides the last registered match now wins, so
  `.aat-overrides.yaml`, `--overlay`, and `--override` take precedence over `env.yaml` overrides as
  documented. Exact names still beat globs.
- Cleanup input matching scans earlier steps in execution order (it was map order).
- MCP operation details and `explain_field` label a graph input's default "Test default": it is data AAT
  sends in tests, not a value the API fills in. Read as a plain default, the shop's `quantity: 1` and
  `method: card` made required fields look optional.
- CLI description and `--help` text describe AAT as graph-based API workflow testing; the LLM is
  optional and authoring-time only, and execution never calls one.
- Documentation covers features that had none: the Ctrl+C `aborted` outcome, `--oas-validate`, the batch
  matrix view, archive import and export, and `aat run rebuild-summaries`.
- libopenapi-validator v0.14.0 and libopenapi v0.38.7.
- Minimum Go version is 1.25.7 (the OpenAPI libraries require it).

### Removed
- **BREAKING:** YAML keys that nothing read, which strict decoding now rejects: step `fallback`,
  `assertions.semantic`, the environment settings `maxRunDuration`, `defaultRetries`, and
  `archiveFormat` (retry with a step's `retry:` or `--retries`), template `response.validate`, the
  `prompt` field of selections and graph default `select`, and recipe `overrides.descriptions`.
- **BREAKING:** the `llm` selection strategy, which plan validation already rejected but `aat prompt`
  accepted from the model, and the `warn` OpenAPI validation mode, which behaved exactly like `auto`.
- The MCP `execute_plan` tool no longer accepts the obsolete `mode` parameter (the runtime
  strict/lean/adaptive modes were removed in 0.0.2).
- Repository leftovers from the private airline project (`setup.sh`, a root-level plan, IDE run
  configurations, an empty case-study stub).

### Fixed
- Building the web UI (`make frontend`, `make build`) no longer rewrites a tracked file, and `make clean` no
  longer breaks `go build` by deleting one. Vite writes the bundle to `server/web/dist/app`, which git ignores,
  and the embed is satisfied by the tracked `server/web/dist/placeholder.txt`.
- An `errorDetection` `equals` rule with a number (`value: 0`) matches the JSON number. The YAML integer and the
  JSON number used to compare as different types, so the rule never matched. A map or list `value` is now a
  validation error; at run time it crashed `aat run` and the MCP server.
- A request path value with an encoded `/` (`%2F`) keeps it inside its segment; the executor used to decode it
  into a real `/`. The archive records the URL as the executor joins it, instead of concatenating the base URL
  and the path.
- A number of a million or more fills a placeholder in plain digits instead of exponent form (`1.2e+06`), and a
  map fills one as JSON instead of Go syntax (`map[k:v]`).
- Archive references stay inside the archive directory:
  - `aat import --name` must be a single directory name. A name that starts with `run-` or `batch-` gets the
    `!` prefix, as a name derived from the file does. `--name ../x` used to import outside the archive
    directory.
  - The web server answers `404` for a run, batch, or trace ID that is not a single directory name. An ID of
    `..` read the parent directory's `archive.json`. `PUT /api/runs/{id}/name` renamed any directory in the
    archive directory; it now renames only runs and batches.
  - The MCP archive tools reject a `run_id` that is not a directory name. The plan tools reject absolute plan
    names and names that climb out of the plans directory.
- An unknown subcommand is an error (exit `2`), with a suggestion when the name is close. This covers
  `aat run bogus` and unknown subcommands of `aat plan`, `aat env`, `aat mcp`, and `aat docs`. They printed
  help and exited `0`, so a mistyped subcommand passed in CI. Commands that take no arguments, such as
  `aat validate` and `aat web`, now reject stray arguments instead of ignoring them.
- `aat mcp serve` reports a manifest that fails to load with the load error; it said the manifest was not
  found. `aat import` fails on such a manifest instead of importing into `_output/runs`.
- `aat run plan --json` and `aat run batch --json` print the error document for every error that stops them
  before a plan runs. Before, a manifest that failed to load, a bad `--var`, or an overlay environment that
  could not be resolved left stdout empty.
- Graph-level cleanup deletes the resource each step created. Cleanup looked up the creating node's
  outputs by node name, but outputs are stored by step ID, so a step with its own `id` fell through to the
  first step with an output of that name: two `createCart` steps with their own IDs deleted the first cart
  twice and left the second. Cleanup now reads the registering step's outputs, then the most recent step
  with a matching output.
- A request that fails before any response reports `executing HTTP request: …` once, not
  `executing request: executing HTTP request: …`.
- `aat mcp serve` starts when a relative `--manifest` names an `oas:` spec. The spec path was joined onto
  the graph's directory a second time (`examples/shop/examples/shop/openapi.yaml`), so a project loaded
  from another directory failed with "no such file or directory".
- The MCP `get_sample_response` tool returns the newest successful response for an operation, and a
  failed one, marked as such, only when no run succeeded. It took the newest response of any status, so a
  negative test's `409` could pass for the sample. It also searches the runs inside batch directories,
  and a manifest without `archives` gets the expected output shape instead of an error. The archive tools
  accept the ID of a run inside a batch.
- A run archive that cannot be redacted is not written. `aat run`, batch runs, and the MCP `execute_plan`
  tool report the error; before, the error was ignored and the archive was written with its secrets in
  place. Redaction fails only on a value JSON cannot hold, such as a NaN from a Lua transform.
- `aat mcp serve` without `--persona` registers `get_data_flow`, `get_response_shape`, and `explain_field`,
  which only the `api` persona had, so it has every tool: 39 with an OpenAPI spec, 32 without.
- The web UI's run timeline shows a step's assertion count only when the step has assertions, not
  `0 / 0` on every step.
- Run archives redact known secrets from every string they hold: request URLs and query parameters,
  request and response bodies, outputs and display outputs, error, assertion, and OpenAPI messages,
  and plan step values, as well as headers, inputs, and resolved values; `batch.json` entries too. In
  JSON bodies only string values change. Before, a credential used as an input was redacted in `inputs`
  but kept in the body of the same request.
- Archive redaction no longer mangles ordinary data: the oauth2 `username` and `clientId` are not
  treated as secrets, and a secret shorter than eight characters is redacted only where a whole value
  equals it. The shop sandbox's `demo` credentials had turned `demo@example.com` into
  `[REDACTED]@example.com` in inputs. Overlapping secrets are redacted completely; map order could
  leave part of one visible.
- Run archives redact an API key sent under a custom `auth.headerName`, the credentials of host overrides
  and overlay overrides (they were never collected as secrets), and the literal credentials and credential
  headers of the plan stored in `metadata.plan` and `metadata.instantiatedPlan`; `aat prompt` archives also
  collect the credentials of `.aat-overrides.yaml`.
- `aat generate --oas` marks a response property the schema does not list as `required` as an optional
  output with an optional extract rule, so a scaffolded step no longer fails when the API omits it.
- A `select` with a `filter` and no `strategy` works like `match` instead of failing at run time with
  `unknown selection strategy`.
- `--manifest` naming a file that does not exist is an error instead of silently falling back to manifest
  discovery.
- `aat run plan` no longer prints a failed or errored run's message a second time on stderr.
- A project's manifest no longer inherits fields it leaves out (such as `domain`, `layers`, or
  `defaultEnvironment`) from a lower-priority project named by `AAT_PROJECT` or the user config's
  `default_project`; the highest-priority manifest found describes the whole project.
- `aat validate`, `aat validate plan`, and the MCP plan tools resolve recipe layers, so a misspelled
  layer is reported and layer-supplied inputs no longer fail validation.
- MCP `execute_plan` applies override values, `expectFailure`, and recipe layers like `aat run plan`.
- `aat plan list` summarizes recipes instead of reporting a parse error for each.
- Workflow compatibility checking (`aat validate`) accounts for slots: an addon `AUTOWIRE` input that every
  option of a slot produces is no longer reported as unfed, an addon that attaches after a slot option's
  node is checked instead of skipped, and slot options are no longer checked as bases of their own.
- The static OpenAPI output check looks each output up at its template extract path, through nested objects
  and array items, instead of requiring a top-level response property named after the output.
- `settings.oasValidation` and `--oas-validate` reject unknown values instead of treating them as `auto`.
- A workflow template whose `verification:` names a node missing from the graph fails to load, as cleanup
  entries already did.
- `aat run batch --parallel N` with runtime OpenAPI validation no longer has a data race: parallel runs share
  one loaded spec, and libopenapi-validator v0.13.1 wrote into the schema model while rendering a response
  schema behind a `$ref`. The upgrade to v0.14.0 removes the race, so validations still run concurrently.
- Workflow composition fills slots in declaration order, so merged cleanup, slot verification, and slot
  `inject` values no longer vary between runs, and neither can batch dedup fingerprints.
- The sequential batch display prints a run's `OAS: N warning(s)` total, as the plan display does.
- Cleanup steps carry a step ID and a start time in archives, so the web UI places them on the timeline;
  their start time was empty.
- The batch By Test matrix no longer clips its rotated permutation labels: the header grows to fit the
  longest, and a wide matrix uses the space beside the page column. The run timeline shows a step's node
  only when it differs from the step ID.
- `--override NODE=URL` routes keep the environment headers, plan headers, overlay headers, and the
  credential, like an `overrides:` entry with that `match` and `baseUrl`; they used to send no headers.
- `aat prompt` rejects layers when the manifest sets no layers directory instead of silently running
  without them.
- Lua transforms: `print()` writes to stderr instead of stdout (where it corrupted `--json` and
  `--dump-state -` output), `return {}` is a valid empty set of outputs, and a template with a transform
  but no `extract` rules runs its transform; outputs a transform computes no longer fail the adapter
  output check.
- `aat generate --oas` extracts an array property of an object response by its name instead of `@this`.
- The static OpenAPI check accepts a required parameter or body property that the template sends itself
  (such as a literal `"photoUrls": []`), which made `examples/petstore` fail `aat validate --strict`.
- `aat validate` checks workflow templates in subdirectories of the workflows directory (such as
  `workflows/slots/`), which it skipped.
- A layer's `fromResolved` applies over an existing graph default instead of being dropped.
- The plan summary resolves `intent.goal` as a step ID; `aat plan list` truncates long goals on character
  boundaries.
- Visualizers receive `--color-text-secondary`, `--color-danger`, and `--color-warning` as documented; the
  web UI sent variable names it does not define.
- `aat validate`, `aat generate --oas`, and the MCP OpenAPI operation details include parameters
  declared on an OpenAPI path item (such as a shared `{cartId}`), not only those on the operation.

## [0.0.4] - 2026-03-04

### Changed
- `CONTRIBUTING.md` describes what a pull request needs and the project's policy on AI-assisted
  contributions.

## [0.0.3] - 2026-03-03

### Added
- MCP server personas: `--persona api` for integrating with an API and `--persona test` for writing and
  running tests. New tools browse the OpenAPI spec, return a sample response from past runs, and show
  templates, and tool errors say what to try next.
- Runtime OpenAPI validation of requests and responses (`--oas-validate`), with schema compilation problems
  reported as warnings and request and response errors shown separately.
- Layer-group batches detect plans that come out identical across layer permutations and run each once. The
  web UI's batch page hides the skipped runs, dims their representative outcome, and adds a By Test matrix
  with a filter for each dimension.
- Visualizer plugins (`visualizers.yaml`): custom views of a step, rendered in a sandboxed iframe in the web
  UI.
- Run and batch summaries and the web UI group issues by category.
- `fromInput` fills an input from another step's input, and an output can be optional when a response may
  omit it.
- `.aat-overrides.yaml` is discovered automatically, for local development overrides.
- Terminal output uses color and fits the terminal width, batch runs show progress and accept `--shuffle`,
  and progress output shows retries.
- A `raw` flag on mechanical assertions evaluates the whole response body.
- Archives and the web UI record when an override routed a step to another URL.
- Archives and the MCP server report the build's version.

### Changed
- The project is renamed from Adaptive API Testing to Adaptive API Toolkit.
- Plan composition has a single entry point, and `aat prompt`'s planning pipeline is simpler, with a plan
  summary written for the user.

### Fixed
- Inputs with graph defaults are no longer classified as supplied by a layer.
- Layers embedded in a recipe load during layer-group batch runs.
- Batch runs no longer get duplicate run group IDs (#1).
- A batch run that errored no longer shows "run not found" in the web UI.
- `aat web view latest` opens a batch when it is the most recent archive.
- Duplicate cleanup step IDs no longer leave the web UI spinning.
- Dependencies from `requires` and `satisfies` tokens stay within an addon or the base workflow, so composed
  plans no longer get dependency cycles across them.

## [0.0.2] - 2026-02-25

### Added
- `aat web view <file>` opens an archive file without a project.
- GitHub Actions CI runs the tests with the race detector, golangci-lint, and a gofmt check; `make check` runs
  the same checks locally.

### Fixed
- `make clean test build` works.
- Data races found by the race detector.

## [0.0.1] - 2026-02-24

The first tagged version.

### Added
- API graphs in YAML: nodes with typed inputs and outputs, auto-wiring, constraints, and value pools from a
  domain file.
- Request templates with conditional (`{{?a|b}}`) and iteration blocks, response extraction, and Lua
  transforms.
- The execution engine: dependency-aware scheduling; values wired from earlier steps, with array selection
  strategies and named selections; constraint-aware fallback; retries by error category; `expectFailure`
  negative tests; response-body error detection; mechanical assertions; and graph-level cleanup.
- Workflow templates composed from slots and addons; plans, compact recipes, and data layers, with
  `--layer-group` permutation matrices.
- `aat run plan` and `aat run batch` (with `--parallel`), plan-level retries, `--json` and `--quiet` output,
  and exit codes for CI.
- `aat prompt`: an LLM drafts a plan from a sentence, with opt-in planning traces.
- OpenAPI support: `aat generate --oas` scaffolds a graph and templates, and `aat validate` checks them against
  the spec.
- `aat validate` for the graph, plans, and workflows, with project manifest discovery (`aat-project.yaml`).
- The MCP server (`aat mcp serve`): tools for the graph, templates, domain, OpenAPI spec, plans, execution, and
  archives, plus resources and prompts.
- `aat docs generate`: Markdown and Mermaid documentation from the graph.
- Run archives with the full decision trail and redacted secrets; export and import, saved names, and fast
  run listing.
- The web UI (`aat web`): run and batch lists, a run timeline, step detail with audit tabs, and a planning
  trace viewer (`aat web viewtrace`).
- Per-node routing to other base URLs (`--override` and environment `overrides:`), plan-level auth and
  headers, and OAuth2 token caching.
- The Petstore example, the user documentation, and the Apache 2.0 license.

[Unreleased]: https://github.com/gburgyan/aat/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/gburgyan/aat/compare/v0.0.4...v0.1.0
[0.0.4]: https://github.com/gburgyan/aat/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/gburgyan/aat/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/gburgyan/aat/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/gburgyan/aat/tree/v0.0.1
