# Launch M6.1 — AAT improvements from an agent discovery run

A coding agent built an AAT project for a public API in one headless attempt. It learned AAT from the docs site, and
the API from its reference and live test mode. Its transcript and the resulting project were then reviewed for what
AAT made hard. M6.1 fixes those findings in three phases:
1. safety and reading results (P17–P19)
2. tests that pass for the wrong reason (P20–P22)
3. expressiveness (P23–P27)

## 2026-09-12 — P17: `--dump-state` redacts credentials by default

**What:** `aat run plan --dump-state` redacts its export the way run archives are redacted.
- **Redaction:** credential header values are masked at the top level and in every step, then every known secret is
  replaced wherever it appears. The export gains `redacted`.
- **The opt-out:** `--dump-state-secrets` keeps live credentials. It warns on stderr when the dump goes to stdout, and
  it is an error without `--dump-state`.
- **Tokens:** the access token a run authenticates with joins the run's known secrets, which archives use as well.

**Decisions:**
- **Redact by default, reversing the stage-3a decision** (`stage-3a-checkpoint-export.md`). Stage 3a left the dump
  unredacted because a harness must replay calls.
  - In the discovery run, the agent used `--dump-state -` to read step outputs, and its first call printed the API
    token into its transcript.
  - A dump is now safe to print, and the replay case opts in.
- **Always a way back (author).** The author accepted the break before 1.0 on one condition: raw credentials stay
  available, through `--dump-state-secrets`.
- **Same rules as archives.** `engine.RedactStateExport` reuses `archive.RedactHeaders` and `archive.Redact`, so a
  run's dump and its archive agree on what a secret is.
  - The outcome, step IDs, and node names carry `redact:"-"`, like the archive's identifiers. Map keys are never
    changed.
  - Base URLs stay redactable.
- **The issued token is a known secret.** `config.RunSecrets` collects configured credentials. An OAuth2 access
  token exists only at run time, and it was masked only inside credential headers.
  - `aat run plan` and `aat run batch` add it, where the token is already in scope. An end-to-end test has the
    sandbox echo the token in a response header that no redaction rule names.
  - `aat prompt` and the MCP server's `execute_plan` don't add it yet. `aat prompt` authenticates in several
    branches, and `execute_plan` has no execution test to prove the change.
- **One warning per invocation.** It prints from `executeRun`, not once per plan-level retry attempt.
- **Consumers moved to the flag:**
  - the CI example-shop script, which now also checks that a default dump is redacted
  - the end-to-end checkpoint test
  - the pytest handoff in `checkpoints.md`
  - the shop README's `curl` recipe, the kit README, and the integration-kit page

**Verification:**
- `make check`, `make docs`, and `make example-shop` pass. The example-shop script checks that a default dump is
  redacted, then replays the live token from a `--dump-state-secrets` dump.
- **Against a live API with a real test token:** a scratch clone of the discovery run's project, running a plan that
  only reads.
  - The default dump held the token 0 times, with `"redacted": true`.
  - The `--dump-state-secrets` dump held it 4 times.
  - The run's archives held it 0 times.

**Open questions:** none.

## 2026-09-12 — P18: `aat run show`

**What:** `aat run show <run>` prints what a run archive recorded:
- the step list
- one step
- one part of a step (the request or response body, inputs, or outputs), narrowed with `--path` or summarized with
  `--shape`

The MCP server's `get_sample_response` gains `path` and `shape`.

**Decisions:**
- **A read-only command, not another dump.** In the discovery run, the agent read step outputs through `--dump-state`
  17 times, and sent a subagent through a multi-megabyte archive with `jq`. Reading what an API returned is the core
  of the authoring loop, so it gets a command that needs no browser and changes nothing.
- **Run lookup moved into `archive`.** `archive.ListRuns` and `archive.FindRun` replace MCP's private lookup.
  - **Unfinished batches.** Any directory without `archive.json` is read as a batch. The runs of a batch that is still
    going, or that stopped before writing `batch.json`, resolve, and `latest` can name one.
  - **Ordering.** Runs order by the timestamp in `summary.json`, which is read and never written. The server's listing
    writes a missing summary, so a read-only command could not reuse it.
  - **References.** A reference is `latest`, a run ID, or a batch ID and run ID. Each part is checked with
    `CheckDirName`, so MCP's lookups stay inside the archive directory. File paths are the CLI's concern.
  - **The server's `LatestRef` is unchanged:** a newer batch still wins there, because the web view opens batches.
- **Step helpers moved into `archive`:** `StepID`, `CleanupStepIDs`, `FindStep`, and `StepPassed`. The server's copies
  are gone, so `--step` IDs match the web UI's step URLs.
- **The shape merges every element of an array, not only the first**, which was the plan.
  - The merge keeps the union of keys, type unions such as `string|null`, and counts of the objects that hold each
    key.
  - A first-element shape hides fields that are sometimes missing or null, and those fields drove most of the
    transform boilerplate in the discovery project.
  - Paths are gjson paths with `#`, so a line goes straight into an extract rule or `--path`.
- **Bodies stay raw.** Shapes and paths read the archived `json.RawMessage` with gjson, and nothing decodes a body
  into maps.
- **Printed parts are capped at 64 KB by default,** with a note on stderr, so an agent does not pour a large body into
  its context by accident. `--max-bytes 0` lifts the cap. The step list and the step overview stay small, so they are
  not capped.
- **Pass or fail follows the run summary:** `StepPassed`, plus a status of 400 or more without `expectFailure`.

**Verification:**
- **Tests:**
  - lookup: batches, saved and unfinished batches, traversal references, and no summary written
  - the shape
  - each command form and its exit codes
  - the MCP parameters
  - a shop end-to-end subtest that reads a real run's response shape and outputs
- **On the discovery run's own archives, read in place:**
  - The step list of a 318 MB archive took 0.36 s, with 661 MB peak memory.
  - The shape of a 38 MB response took 0.34 s and 288 MB, for 292 lines.
  - Nothing under that project was written.

**Open questions:**
- Archives are indented JSON, so a run with large responses writes hundreds of megabytes. Not scheduled.

## 2026-09-12 — P19: docs an agent can read verbatim

**What:**
- The AI assistant primer moved to `internal/primer/llms.md`. `docs/user/llms.md` includes it with a `--8<--` snippet,
  as `changelog.md` includes the changelog.
- `aat docs primer` prints the primer from the binary.
- The docs build publishes the primer as `llms-full.txt` at the site root, beside a static `llms.txt` index of the
  reference pages.
- **The primer gains what an agent had to learn by trial and error:**
  - step value forms for pools, literal lists (`{default: [...]}`), and `{}`
  - one table of expression forms, with where expressions are evaluated and where they are not
  - assertion details (literal comparison, the predicate grammar, array counts), plus two checks that catch tests
    passing for the wrong reason
  - retry rules, defaults, and waits
  - `min` and `max` ties
  - Lua transforms and template iteration blocks
  - a section on layers and batches: key forms, precedence, groups, dedup, and watching a long batch
  - how verification and `inject` compose
  - reading results with `aat run show`, and redacted dumps
- **Verification of the additions.** Each one was checked against the code first; the literal-list form was also run
  against a test server. That work found three docs pages that disagreed with the code, now fixed:
  - `plans.md` on `{}`
  - `value-flow.md` on inline `min` and `max`
  - `validation.md` on assertion types

  It also found code defects, noted for later M6.1 phases.

**Decisions:**
- **Current behavior only.** The primer describes what the code does today, including the limits later phases may
  lift: no expansion in assertion values, no inputs in a transform, and no bare list in a step value.
  - A rerun of the discovery run reads these docs, so they use the shop's vocabulary and say nothing about that run's
    API.
- **Why verbatim access.** In the discovery run, the agent's fetch tool summarized the primer and declined to
  reproduce it. Its summaries reported documented features, such as date expressions and `{{env.KEY}}`, as missing,
  and a guessed `llms-full.txt` returned 404.
- **One source, embedded.** `docs/go.mod` makes `docs/` a module of its own, so Go cannot embed files there. The
  primer's source moved into the main module, as the foundation package `internal/primer`, and the site includes it.
  There is no second copy to keep in sync.
- **A build hook, not a committed copy.** Snippets do not expand in `.txt` files, so `docs/hooks/llms_txt.py` copies
  the source into the site at build time, and it cannot drift. The docs workflow now also runs on changes to
  `internal/primer/**` and `docs/hooks/**`.
- **Links by URL.** A relative link breaks in raw Markdown and in terminal output, so the primer links to pages by
  site URL. mkdocs does not check URLs, so `internal/primer`'s test does: every site link in the primer and in
  `llms.txt` must name an existing page, and every anchor a heading on that page.
- **Two copies of the primer, for two needs.** `aat docs primer` matches the installed binary, while the site deploys
  from `main`, and the primer says so.

## 2026-09-13 — Large specs, form bodies, and generated values (the first pre-Stripe PR)

**What:** Before the next discovery run, whose API publishes a large OpenAPI spec and takes form-encoded requests, a
research pass ran that spec and those request shapes through AAT. It found that AAT could not load the spec, and that
form bodies, idempotency keys, and cleanup broke on the API. This PR fixes the loading, validation, request, and
expression parts; the cleanup and validation-gap work follows in a second, stacked PR.

**Measurements** (the spec: OpenAPI 3.0, 8,028,700 bytes, 594 operations, 593 form bodies, 622 nullable `anyOf`
schemas):
- Before: `aat generate` exited 2 on `infinite circular reference detected`, and `aat run` quietly skipped validation.
- Loading for runtime validation took 51.6 s, almost all of it compiling every operation's schemas. With the nine
  operations a small project names, it takes 3.25 s.
- A response with `null` in a nullable `anyOf` field got 8 errors; it now gets none, and a wrong type still fails.
- `aat generate` over the whole spec gives 552 warnings instead of 663.

**Decisions:**
- **Tolerate circular references instead of skipping the check.** libopenapi builds the model anyway, and its renderer
  and the validator's `$ref` fallback read the cycles it records. Only errors that are all circular references pass;
  anything else still fails the load. Its logger writes JSON to stdout, so it is discarded.
- **Build the validator for the graph's operations.** A copy of the model keeps only the path items that hold them.
  Warm-up keys its cache by schema content, and steps are validated against path items from the full model, so an
  operation left out still validates on first use.
- **Add a null alternative when loading, not after.** The validator's 3.0 transform adds `null` to a schema's own
  `type`, `allOf`, and `enum`, not to `anyOf` or `oneOf`. Appending `{type: "null"}` to those compositions in the YAML
  tree before the model is built fixes validation, generation, and MCP views at once, for 3.0 documents only.
- **Our own form decoder.** The library's form support rejects `@`, `:`, and `,` in decoded values and ignores `anyOf`
  when converting types. The decoder nests bracketed keys, collects `[]` and repeated keys into arrays, and converts
  values by the schema's alternatives. A body type it doesn't read is recorded as skipped, not as valid.
- **`strict` fails on a spec that doesn't load,** with exit code 2 before any request; `auto` warns on stderr, which
  `--quiet` and `--json` no longer hide.
- **Iteration joins by position, not everywhere.** In a form body or a query string, a block that writes a whole pair
  joins with `&`, and one whose body starts or ends with `&` is concatenated. Other blocks keep commas, so hand-written
  `explode: false` lists still work. Only form bodies are trimmed.
- **Resolve a step's inputs once.** Memoizing only generated values would still let pool picks and dates change
  between attempts, and a retry that sends a different idempotency key defeats the key.
- **Generated-value names say their format:** `today` a date, `now` an RFC 3339 time, `unixtime` integer seconds. Offsets
  name a unit, which leaves `+` and `-` free for later arithmetic. `uuid`, `now`, and `unixtime` become reserved words.
- **The timeout message names the limit only when it applies:** a `net.Error` timeout after the whole limit passed,
  with the run's context still live.

**Deferred:** outputs from an expected-failure body (the primer documents two workarounds), a pruned validator beyond
the graph's operations, expanding maps into bracketed pairs when rendering, and `--tag` for `aat generate`, since the
spec has no tags.

## 2026-09-13 — Cleanup skips, selection ties, resolutions, and validation gaps (the second pre-run PR)

**What:** The second PR before the next discovery run takes in what the first two runs and the research pass found:
- A cleanup pairing can say when it isn't needed (`when`, `releasedBy`), and a cleanup is skipped when a later step on
  its node already released the resource.
- `min` and `max` ties print a warning, and `onTie` fails on them or accepts them.
- A step that fails before its request keeps its resolutions, and `aat run show` gains `--resolutions` and `--compact`.
- `inject` values decode like graph defaults, and literal values are checked against their input's shape.
- Assertion values expand expressions, and an unknown assertion type fails validation.
- An optional input whose `from:` output is missing is left out.
- A `{}` fallback evaluates its default's expressions.
- Validation lines name the value and print once.

**Decisions:**
- **`when` reads only the creating step's outputs,** or the previous cleanup step's in a chain. Reading the newest step
  on the resource would, in the shop, read the charge step's status and skip cancelling an order that can still be
  cancelled. A resource that a later step can leave in several states needs a `stateFrom`, which is deferred.
- **A release must match the resource.** A later main step on the cleanup node, or on a `releasedBy` node, releases it
  only when that step succeeded, isn't an expected failure, and resolved the same values for the inputs it shares with
  the cleanup, with numbers compared by value. An unset optional input blocks the release.
- **A tie warns by default.** A plan may take any of several equal elements on purpose; `onTie: fail` is for one that
  must not guess, and `first` takes the first without a warning. The selection cache key gained the compared field,
  since two inputs sorting one array by different fields got the same element.
- **A failed step keeps what resolved before the failure,** with an `error` record for the input that failed.
- **A bare list in `inject` stays a literal list,** because the second discovery run's fix relies on it. A mapping
  decodes strictly, so `{default: [35]}` fails validation instead of sending a map.
- **A list is allowed for a single-value input.** The first shape check rejected it: the private project's validation
  gained 13 lines, and 4 of its recipes would have failed, where a template sends the list as repeated pairs.
- **Selection filters and cleanup `when` conditions stay literal.** Only `fieldEquals` values and quoted predicate
  literals expand expressions. Filters are evaluated while the step's inputs are still being resolved, and `when`
  during cleanup, so neither has the step's inputs to expand.
- **An optional input whose `from:` output is missing is left out,** as `AUTOWIRE?` leaves one unset. The warning for a
  required input that takes an optional output covers graph defaults and plan and workflow files, not recipes, whose
  wiring comes from composition.
- **`{}` over a default that isn't a plain value still leaves a required input out.** A first version made it an error,
  because a template that sends the placeholder unconditionally fails on it. The rewiring check then found all 7 of the
  first discovery run's recipes failing to compose. That project blanks a payment amount with `{}` on orders paid later
  and sends the amount inside a conditional block, and 117 order steps in its archives succeeded that way. The error
  was dropped; only the plain fallback's expression evaluation remains.

**Verification:**
- `aat validate --strict` output, compared as sets of lines before and after each change, is unchanged for the shop and
  the private project, and for the first discovery run's project after the `{}` revision.
- Composing every recipe with the build before this PR and with its last code commit gives the same plans, apart from
  each plan's creation time: 74 recipes across the shop, the petstore example, the private project, and both discovery
  runs' projects.
- Against the next run's API in test mode, from a scratch project: a payment captured on creation skipped its cancel
  through `when`, one captured later skipped it through `releasedBy`, one waiting for customer authentication was still
  cancelled, and an explicit cancel skipped its pairing.

**Deferred:** unused-input and layer-sibling warnings (Phase 3), `stateFrom`, the web UI for skipped cleanups and ties,
`inject` as a resolution source, a warning when `{}` leaves out a required input that its template sends
unconditionally, and the optional-output warnings in the MCP server's validation tools.

## 2026-09-13 — Review fixes for the second pre-run PR

**What:** A code review of the PR reported 15 findings, and all are fixed.
- **Bugs:**
  - A main step could release a chained cleanup when it sent the same value. The cleanup step before it creates that
    resource during cleanup, so the chain's last call was never sent.
  - The value-shape check ran on steps expected to fail, so a plan whose mutation sends a wrong shape on purpose
    stopped at validation.
  - A required input marked `{}` fell back to the graph default and ignored layers.
  - A slot `inject` value was checked against every input with its name, so an unrelated node's input stopped the
    graph from loading.
- **Gaps:**
  - An optional input that reads a missing output through a named selection failed the step.
  - Cleanup `when` checks and the optional-output warnings ran only in `aat validate`.
  - A retry's assertions read a newer clock than the inputs it resent.
  - A main step with the ID `verify_<node>` was left out of cleanup release checks.
  - `PLACEHOLDER` got a shape error on top of its own.
- **Cleanups:**
  - A selection joined an input that shared its name.
  - A cancelled resolution dropped what had resolved.
  - Match and filter selections parsed their predicate for every element.
  - Cleanup inputs resolved twice.
  - Two number helpers disagreed.
  - Map keys were sorted by hand.

**Decisions:**
- **Layers apply to a `{}` fallback (author).** The entry above documented that they didn't. The fallback stands in
  for the default an unset input gets, and layers are how a batch varies that default, so a matrix axis no longer stops
  at a step that marks the input `{}`.
  - The default after layers also decides whether it is plain: a pool layer over a plain graph default leaves the input
    out.
  - `plan.EffectiveDefault` replaces three copies of the layer lookup.
- **Predicates moved to the foundation package `internal/predicate` (author).** `graph` can't import `plan`, so only
  `aat validate` checked `when`. A misspelled field reached a run through `aat run` or the MCP server, and the cleanup
  then ran every time, recording `whenError`.
  - `graph.Validate` now checks `when` whenever a graph loads, and the two validate commands lost their extra call.
  - `plan` keeps the `{{…}}` expansion for assertions, built on `predicate.EvalExpanding`.
  - `predicate.Parse` lets a selection parse its predicate once.
- **No main step releases a chained cleanup,** rather than searching for releases after the parent cleanup step:
  every main step ran before the chain created the resource.
- **Shape checks skip every step expected to fail,** not only mutation siblings. A hand-written negative step may send
  a wrong shape on purpose too, and a mutation's happy-path step is still checked.
- **An inject value fails only when no input with its name takes it.** The graph doesn't say which base workflows an
  option composes with, and the composed plan's steps are checked like any others.
- **The optional-output warnings reach the MCP server** through `validate_plan` and `save_plan`, which the entry above
  deferred. The graph-default warnings stay in `aat validate`.
- **The review overstated the step ID collision.** Verification step IDs are `verify_<node>`, so only a main step with
  that exact ID was affected. The fix still replaced the ID filter with a slice of the main results, which also saves
  two verification instantiations per cleanup run.

**Verification:**
- `make check`, `make docs`, and `make example-shop` pass.
- A scratch copy of the shop with `cleanup: {node: deleteCart, when: 'staus == "open"'}`: `aat validate --strict` and
  `aat validate graph` exit 1, and `aat run plan smoke` and `aat mcp serve` exit 2. Each names `staus` while loading
  the graph.
