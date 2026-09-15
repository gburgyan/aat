# Launch M6.6 — the `aat-duffel` package and the AAT primitives it needs

M6.6 builds a real Duffel package in its own repository, with full access to AAT and the author. The discovery runs in
M6 built working projects under deliberately adversarial rules, and this is the version without those rules.

Each gap the package runs into becomes its own AAT branch and PR. In order:
1. lists in step values
2. extract `default:`
3. response-header extraction
4. `repeat` with `until`
5. `repeat` with `next`
6. visualizer `bodyPath`

## 2026-09-14 — Lists in step values (P23)

**What:** a step value can be a YAML list, which is the list itself.
- **Items:** a list of maps feeds a template's `{{#lineItems}}…{{.sku}}…{{/lineItems}}`.
- **`value:`:** `{value: …}` in a step value reads as `{default: …}`.
- **Expressions:** they are evaluated inside list and map items, at any depth.
- **Validation:** their syntax is checked there, with the item named.
- **Round trip:** a saved plan with a list or map value reads back.

**Decisions:**
- **A bare list is the list itself, not a pool.** This follows `inject`, where a bare list was already literal. A step
  value already had `pool:` for alternatives, so a bare list had no other sensible meaning. In graph defaults and layers
  a bare list stays a pool, which would be a breaking change to alter.
- **`value:` as a synonym for `default:`**, which is P23's original design.
  - **Why:** graph defaults, layers, and `inject` write a literal as `value:`. Discovery attempt 2 reached for it in a
    step value and got `unknown key "value" in step value`. With the synonym, one form reads the same in all four
    places.
  - **How:** yaml.v3's callback form decodes the node that `yamlx.Node` captured (`callObsoleteUnmarshaler` passes the
    same `*Node`). So `readValueKeyAsDefault` renames the key in place before the strict decode, and every other key is
    still checked. A mapping with both keys is an error, reported at the `value` key's line.
- **Expressions in items are evaluated into a copy.**
  - `EvalExpr` builds a new list or map only when an item holds an expression, so the plan keeps its expressions for the
    next run or permutation.
  - Map keys are walked in sorted order, so generated values are drawn in the same order every run.
  - Graph defaults under `{}`, pools, layers, `inject`, mutations, recipe overrides, and `fieldEquals` values all go
    through `EvalExpr`, so they gain the same behavior.
- **Round trip.** `StepValue.MarshalYAML` wrote any default-only value bare.
  - A list was then rejected by the parser.
  - A map was read as the step value's own keys.
  - Lists stay bare, since they now parse, and maps are written under `default:`.
- **Error text:** `evaluating graph default` keeps quoting a string and prints a list or map with `%v`.
  `ValueResolution.Expression` stays a string, set only for a string value; `RawValue` carries a list.

**Open questions:**
- **Name zipping:** pairing an API's per-passenger IDs with plan-supplied names by index still needs a Lua transform,
  because placeholders read flat inputs. A primitive for it would need evidence beyond one API.
- **JSON input:** a JSON-encoded step value (the web UI, MCP) has no `value` synonym, because only YAML decoding reads it.

## 2026-09-14 — Extract `default:` (P27)

**What:**
- **The default:** an extract rule's `default:` is the output's value when the path is missing or holds `null`.
- **Rules:** it can't be combined with `optional: true`, and a rule with `fields` takes a list default. `aat validate`
  checks the default's shape against the output type.
- **Error text:** the `not found in response` error names both remedies.
- **Docs:** they teach gjson counts and queries, and a test pins every form they show.

**Decisions:**
- **Null counts as missing, for a rule with a default only.** Paging cursors and optional objects come back as
  explicit `null`, and a default is written for exactly that case. Without a default, a `null` still extracts as a
  present `nil`, as before.
- **The default isn't mapped through `fields`.** It is written in the output's shape. The common case, `[]`, is the
  same either way.
- **No `null` default.** `default: null` decodes the same as no default, and `optional: true` already covers leaving
  the output out.
- **No copy of list defaults.** An output's value is read downstream, never changed in place, so the rule's `[]` is
  handed out as is.
- **A gjson trap found while writing the docs.** A probe showed that `#(field==null)#` matches nothing. gjson compares
  `null` there as a string, so a guard such as "no unshipped orders" built on it would always pass. `==~null` (null or
  missing) and `!=~null` are the working forms. The docs and primer say so, and a test pins the broken form as 0.

**Open questions:**
- **Empty bodies.** A rule with a default still needs a JSON body. A 204 with no body fails extraction as before. PR 3
  (header extraction) takes up bodies that only header rules read.

## 2026-09-14 — Response-header extraction

**What:**
- **Header rules:** an extract rule's `header:` reads a response header. `header(name)` gives Lua transforms the same.
- **Types:** header outputs are converted to their graph output's integer, float, or boolean type.
- **Empty bodies:** a template whose rules all read headers needs no JSON body.
- **MCP:** the server shows a header rule as `header <name>`.
- **Sandbox:** the shop sandbox reports `RateLimit-*` headers per bearer token. The shop reads them on `listProducts`,
  and Quick Purchase asserts on them.

**Decisions:**
- **Credential headers aren't refused,** a change from the gap design.
  - **Why:** some APIs return a session token in a response header, and later steps need it. Refusing `X-Auth-Token` or
    `Set-Cookie` would block that flow.
  - **Redaction:** a token an API issues in a body isn't redacted from archives either, since only configured
    credentials and the run's OAuth token are known secrets. So a header value is treated the same, and the docs say so.
  - **Dependencies:** a refusal would also have copied `archive`'s header list into the leaf `adapter` package.
- **The engine converts types, not the adapter.** The adapter doesn't know graph types. `convertHeaderOutputs` reuses
  `coerceValue`, and it runs after a transform, so a transform that replaces a header output with a string is converted
  too.
- **Matching in any case,** with a fallback for header maps whose keys aren't canonical. Test doubles and custom
  executors can build such maps, where `http.Header.Values` would miss a key.
- **Joined values.** A header sent more than once gives its values joined with `, `, the HTTP list form. That keeps an
  output a string, where a list would need a selection.
- **OAS static check.** Header outputs map to `""` in `OutputExtractPaths`, so rule 7 doesn't look for them in the body
  schema.
- **The sandbox counts but never refuses.**
  - **Why not refuse:** a 429 would throttle matrix batches.
  - **What the shop's check proves:** `rateLimitRemaining < rateLimitLimit` shows the header was read as a number and
    that the request was counted.
  - **The counts:** they're per token per minute, from the injected clock, so tests are deterministic.

**Open questions:**
- **`Link` cursors:** a pagination cursor in a `Link` header needs a transform to parse out `rel="next"`, or a dedicated
  rule later.
- **429s:** extraction doesn't run on a 429, so `RateLimit-Reset` there is only read by retry, as before.

## 2026-09-14 — `repeat` with `until` (PR 4a)

**What:**
- **The block:** `repeat: {until, collect, interval, max, timeout}` on a main or verification step sends the request
  until `until` holds over a response's outputs.
- **Inputs and retries:** every request resends the first request's resolved inputs, and each request goes through the
  step's retries.
- **`collect`:** it appends lists and adds numbers across the responses.
- **Assertions:** they run once, on the last response's outputs plus the collected ones.
- **Archive:** it records `iterations` and `repeatStop`, and OpenAPI counts cover every request.
- **Output:** the progress line, `aat run show`, and MCP step details show the request count.

**Decisions:**
- **Split in two.** This PR carries the engine, plan, archive, CLI, and docs. PR 4b carries the web UI's Iterations tab,
  `aat run show --iteration N`, and a sandbox job endpoint with a shop plan, which changes the example-shop counts. That
  keeps each PR reviewable.
- **A poll that never converges fails the step; it isn't an error.** Reaching `max` or `timeout`, or an `until` that
  can't be evaluated, adds a `repeat` assertion result (`validate.AssertRepeat`). So the run is `failed`, as for a check
  that didn't hold, and `ContinueOnAssertionFailure` applies. `OutcomeError` stays for failures to execute.
- **A failed request ends the repeats.** An error, a status of 400 or more, or an `errorDetection` match returns the
  result as a single step would. `Run` reports it with its status, and assertions don't run. A poll that expects a 404
  until something exists isn't supported; `expectFailure` is rejected with `repeat`.
- **Assertions are split out of `executeStepWith`.** `runStepAssertions` runs them. The loop runs its requests with a
  copy of the step whose `Assertions` is nil, then calls it once. This needed no flag on `stepInputs`, and single steps
  behave exactly as before.
- **Shared inputs.** `executeStepWithRetry` takes the `*stepInputs` the loop owns, so a retry inside a request and every
  later request resend the first resolution: generated values, pool picks, dates.
- **`until` reads each response's own outputs, through JSON.** That is the same round trip as assertions, so an extracted
  `json.Number` compares as a number. The first engine tests caught `cannot compare type json.Number` on raw outputs.
  The collected values are for the assertions and later steps.
- **Waits.** `interval` defaults to 1 s. A `Retry-After` lengthens it, capped at the 60 s retry cap. A `timeout` stops
  the step before a wait that would pass it, rather than sleeping past it.
- **Reads only.** A node with a cleanup pairing is rejected, since the engine registers one cleanup per step, not per
  request. Verification steps may repeat, which is where PR 5's full-listing audits belong.
- **Counts.** `collect` gives an `int` when both values are whole numbers, and a float otherwise.
  `archive.StepOASValidations` makes the summary count each request's validation instead of the step's copy of the last.

**Open questions:**
- **`next`.** PR 5 adds it to the same loop.
- **Retry-After on success.** A server that sends `Retry-After` on a 200 to pace polling is honored up to 60 s. A longer
  request is capped, not treated as a failure as retries treat it, since the poll hasn't failed.

## 2026-09-14 — Visualizer `match.bodyPath` (PR 6)

**What:** `match.bodyPath` is a gjson path that must reach a value other than `null` in the response body. It is
combined with `bodyContains` and `node` using AND, and it counts as a match criterion.

**Decisions:**
- **Exists and not null.** An enveloped API often returns the same object shape with a field `null` until it applies,
  such as a booking reference. A visualizer for the booking view shouldn't open on those responses.
- **A new key.** `bodyContains` keeps its top-level meaning. A nested reading would change which tabs existing projects
  show, and a dotted `bodyContains` would be ambiguous with a key that contains a dot.
- **gjson as written, with no `$.` normalization.** The path is data that the matcher checks, so it follows gjson syntax
  exactly, as the docs say.

## 2026-09-15 — Blocks of the same key nest (PR 7)

**What:**
- **The bug:** a conditional that wraps an iteration of the same list, as in the templates guide, didn't render. An
  absent list failed with `unresolved placeholders: /ids`, and a present one rendered `["p"],"q"]` without an error.
  The aat-duffel price action found it in Phase 2; the package sent `[]` as a workaround.
- **The fix:** `findBlockClose` finds the closing tag that matches a block, counting the `{{?key}}` and `{{#key}}` blocks
  opened inside it for the same key. Rendering, `withoutBlocks`, input classification, and a form value's check use it.
- **The package:** aat-duffel's `priceOffer` leaves `intended_services` out when no services are given, with no default.

**Decisions:**
- **Both kinds of block count.** A conditional and an iteration close with the same `{{/key}}`, so a block of either
  kind opened inside for that key takes the next closing tag first. Tags for other keys are skipped, so a template
  without same-key nesting renders as before.
- **A compound key is one name.** `{{?a|b}}` closes with `{{/a|b}}`, so an `{{#a}}…{{/a}}` inside it doesn't change its
  depth.
- **The tests fail on the old lookup.** A `go test -overlay` build with the first-tag lookup fails the rendering,
  `withoutBlocks`, and templates-guide tests. Input classification already treated the list as optional; its test
  passes both ways and stays as a guard.

**Open questions:**
- **Iteration inside iteration.** An inner block still isn't repeated for each element of the outer one ("Blocks don't
  nest" in the templates guide). Duffel's seat-per-passenger pairing stays in Lua.

## 2026-09-15 — `repeat` with `next` (PR 5)

**What:**
- **The block:** `repeat.next` maps an input of the step's node to one of its outputs, as in `after: nextCursor`. Each
  request after the first sends the previous response's output as that input, so a step reads every page of a listing.
- **Stops:** the step passes when every cursor comes back missing, `null`, or `""` (`exhausted`), or when `until`, now
  optional, holds. It fails when `max` or `timeout` comes with a cursor left, or when a response gives cursors an
  earlier request already sent (`loop`).
- **Archive:** each iteration of a paging step records its `inputs`, and `repeatStop` gains `exhausted` and `loop`.
- **Shop:** the sandbox serves `GET /orders` a page at a time, the graph reads it as `listOrders`, and the state-machine
  plan's verification pages through its customer's orders at `limit: 1`.

**Decisions:**
- **Running out of pages passes, and `until` stops early.** Assertions over the collected outputs decide the result. That
  serves both "find one" (`until: count > 0`, then assert the collected count) and "none left" audits, where failing on
  exhaustion would serve only the first.
- **A listing cut off by a limit fails.** Reaching `max` or `timeout` with a cursor left fails even without `until`, so
  an audit can't pass on part of a listing.
- **A repeated cursor stops as `loop`, not `error`.** `error` reads as a failed request in `aat run show`. A cursor the
  plan gave, to start a listing partway, counts as sent by the first request.
- **The step keeps its first page's inputs.** `Inputs`, `Resolutions`, and what `fromInput` reads are the query, not a
  cursor that is stale once the pages run out. Each iteration records the inputs it sent instead, and `until` literals
  read the inputs of the request being evaluated.
- **No wait by default.** `interval` defaults to none with `next`, while `Retry-After` and `minRequestInterval` still
  apply. Each page's inputs are prepared before its first attempt, so a retry resends that page's cursor.
- **Cursors are sent as extracted.** A `json.Number` renders as its digits, so no conversion is needed. `cursorKey`
  compares values through `%v`, which reads `json.Number("2")` and `2` alike.
- **The shop's demo adds no plan.** A verification step in an existing plan leaves the plan, matrix, and kit counts as
  they were; only the node count moves to 18. A full page always returns a cursor, so a listing that fills its last
  page ends on an empty one, as many cursor APIs do.

**Open questions:**
- **`Link` headers and page numbers.** A cursor inside a `Link` header, or a page number that needs arithmetic, isn't an
  output `next` can send; those still need chained steps or a transform.
- **Web UI.** A paging step shows its last page only, until PR 4b's Iterations tab.

## 2026-09-15 — `{{step.output}}` in assertions (PR 8)

**What:**
- **References:** a `fieldEquals` value, a quoted literal in a predicate, and a quoted literal in `repeat.until` can read
  an earlier step's output as `{{step.output}}`, as in `amount == "{{checkout.total}}"`.
- **Ordering:** `< > <= >=` compare two decimal numbers written as text as numbers, so `"1000.00" > "999.50"`.
- **Validation:** `aat validate` checks that a reference names a main step and a single-value output its node declares.
  It rejects a reference to the step's own output, to a step that expects failure, from a verification step to another
  verification step, and in a step value, where the message points to `from:`.
- **Wiring:** a reference implies `dependsOn`. Addon composition renames references to the addon's own steps, and
  isolated mutation clones rename theirs.
- **Shop:** the full-lifecycle plan checks that checkout keeps the cart's subtotal, and that the refund and the returned
  order match checkout's total.

**Decisions:**
- **Assertions only.** Step values already have `from:`, which records its resolution and implies the dependency, and a
  second wiring syntax would bypass both. Cleanup `when` stays a plain predicate, since a graph pairing can't name a
  plan's step IDs.
- **`{{env.NAME}}` wins.** The environment form is matched first, so a step with the ID `env` can't be read this way.
  A hyphenated step ID, such as a mutation's `pay--declined`, gets a hint.
- **Values keep their type.** An extracted `json.Number` becomes an int64 or a float64, so a number output compares
  with a number field. A null, list, or object output fails the assertion instead of comparing as text.
- **A missing value fails the assertion, not the run.** A step that stored no outputs, or an optional output the
  response left out, fails with a message that says which, as an unresolved `{{input}}` already did.
- **Decimal text orders as numbers; equality stays text.** Two strings that are both decimal numbers compare exactly,
  through `big.Rat`. `==` still compares text, so `"1.0" == "1.00"` stays false and no existing equality can flip, and
  dates keep text order. The change reaches selection filters, constraints, and `when` too, where numeric text in
  lexicographic order was never meant, so the CHANGELOG lists it under Changed.
- **No arithmetic.** The Duffel checks compare totals the API returns, and sums stay in transforms, as integer cents.
- **Verification reads main steps.** Verification steps run after the main steps, each on its own, so reading another
  verification step can wait for a case that needs it.

**Open questions:**
- **Arithmetic,** such as a held total plus a change fee, if a check appears that no returned total covers.
