# Fuzzing

`--fuzz` sends generated values to one step inside a real flow. The plan builds the state the step needs — a cart
with an item in it, a booking, an order — and the fuzzer tries the step with values the graph allows, values it
forbids, and values it says nothing about. Each case runs as its own step, and a case the API mishandles is reported
without stopping the run.

Spec-driven fuzzers such as Schemathesis fuzz one operation at a time. That finds a lot, but it can't reach an
operation whose inputs have to come from earlier calls: an `addItem` needs a cart that exists. In AAT the plan gets
there first.

Nothing about fuzzing goes in the graph. The cases come from what the project already declares: each input's `type`
and `constraints`, the domain file's types and [value pools](domain.md#value-pools), the node's template, and, when
the graph has an OpenAPI spec, the spec.

## Quick start

With the shop sandbox running (see [the shop example](examples/shop.md)):

```bash
cd examples/shop/
../../aat run plan smoke --fuzz addItem
```

```
  [ 1/23] listProducts         200  0ms
  [ 2/23] createCart           201  0ms
  [ 3/23] addItem              201  0ms
  [ 5/23] addItem__fuzz_quant~ 201  0ms  fuzz quantity.at-min (positive)
  [ 7/23] addItem__fuzz_quant~ 400  0ms  fuzz quantity.below-min (negative)
  [ 9/23] addItem__fuzz_quant~ 400  0ms  fuzz quantity.fraction (negative)
  [11/23] addItem__fuzz_quant~ 400  0ms  fuzz quantity.overflow (negative)
  [13/23] addItem__fuzz_quant~ 400  0ms  fuzz quantity.wrong-type (negative)
  [15/23] addItem__fuzz_quant~ 400  0ms  fuzz quantity.missing (negative)
  [17/23] addItem__fuzz_quant~ 400  0ms  fuzz quantity.null (negative)
  [19/23] addItem__fuzz_quant~ 409  0ms  fuzz quantity.large (edge)
  [21/23] addItem__fuzz_body_~ 201  0ms  fuzz body.extra-property (edge)
  [22/23] checkout             201  0ms
  [23/23] paymentCharge        201  0ms
...
PASSED (16/16 steps, 4ms)
Fuzz: 9 cases: 9 as expected · setup: 2 fresh, 7 reused
```

The happy path runs as usual, and each case runs as a step of its own. A case is a dead end: no later step reads
anything from it. Everything after the target reads the original step, which ran with the plan's own values. So a
case that stops `createCart` from making a cart can't take the cart away from `addItem`. Each case varies one step,
and everything else is the happy path.

Only `quantity` was fuzzed as an input: `cartId` and `sku` are wired from earlier steps, and fuzzing those would only
test that the step can't find its cart. The shop's graph gives `quantity` a `min: 1` constraint, which is where
`at-min` and `below-min` come from.

`--fuzz` takes step IDs or node names, comma-separated. A node name fuzzes every step of that node that is meant to
succeed: a negative step written for one bad request, with `expectFailure` or a `rawBody`, is left out, and naming
one by its ID is an error, since its cases would be refused, or sent with its raw body, whatever they sent. A case's
step is named after its target and case, as in `addItem__fuzz_quantity_below_min`; when two case IDs make the same
name, the second gets `_2`.

## What a case runs on

A case that the API accepts changes something: an item goes into a cart, a traveler onto a reservation, a booking is
made. If every case ran on the happy path's cart, the cart would fill up. The rest of the plan would see a different
cart, and an API with a limit, such as a few travelers per reservation, would start refusing cases for the wrong
reason. So by default a case runs on a copy of the steps its target depends on, wherever the plan lists them. The copy
also includes the earlier steps that build on those, such as the `addItem` a checkout needs even though it reads
nothing from it. A step that
depends only on read-only steps, such as a wishlist made from `listProducts`, changes nothing the target works on and
isn't copied. A target's cases all run before the steps that come after it in the plan.

Making a copy for every case is expensive against a slow, rate-limited API, so the default scope, `reuse`, shares one:

- **Sharing.** A target's cases share a copy of its setup as long as the API refuses them. A refusal (a 4xx, or a
  success whose body the graph's `errorDetection` reads as an error) changes nothing, so the next case can use the
  same cart; nor does a rate limit (`throttled`) or a case that was never sent. A target that only reads, such as
  `getOrder`, never changes its setup, so its cases all share one.
- **When a copy is used up.** When a case is accepted (2xx), fails with a 5xx that may have half-written something, or
  gets no response, the copy may have changed. The next case gets a fresh one.
- **Read-only steps.** A setup step that only reads (a GET, HEAD, or OPTIONS with no cleanup pairing), such as
  `listProducts`, isn't copied at all, as long as it depends on nothing that is copied.
- **Failed setup.** If a copy fails, its case is reported as `not-sent` and the run carries on. After three setups for
  a target fail in a row, often because of a rate limit, that target's remaining cases aren't tried. A failure a
  [`knownIssue`](plans.md#known-issues-a-failure-with-a-deadline) lets the happy path carry on past, such as a failed assertion, doesn't fail
  the copy either.

In the quick start, `at-min` was accepted, so the refused cases after it got a fresh cart and then shared it: `setup:
2 fresh, 7 reused`. The copies' own lines are hidden unless one fails. They are in the archive with `fuzzSetup` set.
Other scopes:

| Scope | What a case runs on |
|-------|---------------------|
| `reuse` (default) | A copy of the setup the target's cases share while the API refuses them |
| `isolated` | A fresh copy for every case: for an API that changes state even when it refuses a request |
| `shared` | The happy path's own setup: no copies, but every case the API accepts changes what the rest of the plan sees. A run warns when a target that isn't read-only uses it |

Set it with `--fuzz-scope` or `scope:` in the step's [`fuzz:` block](#the-fuzz-block).

## Cases

A case sends one value to one input, and its ID names both: `quantity.above-max`. IDs don't change between runs, so
`--fuzz-case quantity.above-max` replays one case. Each case is in one of three modes:

| Mode | What the value is | What the API should do |
|------|-------------------|------------------------|
| `positive` | Allowed by the input's type and constraints: a bound, an enum member, a pool value | Accept it |
| `negative` | Forbidden by the type or constraints: past a bound, not in the enum, the wrong type, a malformed date | Refuse it with a 4xx |
| `edge` | Neither allowed nor forbidden by what the project declares: empty, very long, Unicode, control characters, injection-shaped strings | Anything but fail |

What each type gets:

| Input type | Positive | Negative | Edge |
|------------|----------|----------|------|
| `integer`, `float`, `money` | `at-min`, `at-max`, `zero` (when bounded) | `below-min`, `above-max`, `wrong-type`, and for integers `fraction` and `overflow` | `zero` and `negative` (unbounded), `large` |
| `string` or a custom type | up to three `pool-N` values from its pool or the domain type's pool, `at-min-length`, `at-max-length` | `below-min-length`, `above-max-length`, `pattern-mismatch` (from `constraints.pattern` or the domain type's `validation`) | `empty`, `whitespace`, `unicode`, `right-to-left`, `control-chars`, `long`, `sql-quote`, `markup`, `path-traversal`, `format-string`, `template` |
| `enum[...]` | `enum-<member>` for each member | `not-in-enum`, `enum-wrong-case`, `empty` | |
| `boolean` | `true`, `false` | `wrong-type` | |
| `date`, `datetime` | today, next year | an invalid date, text | `far-past` |
| `T[]` | | | `empty-list` |
| Any input the template sends in the body, the query, or a header | `missing` for an optional input with a value | `missing` and `null` (body only) for a required input | `null` for an optional one |

An input the template sends only inside a `{{?input}}` block gets no `missing` case: leaving it out is the template's
own choice. An input that is only in the path gets none either, since leaving it out changes the route.

A case is only made when the request can carry its value as itself:

- **No empty value where empty means absent.** An input inside its own `{{?input}}` block, or the whole value of a
  header or a `request.form` field, is left out of the request when it is `""`. Its `empty`-valued cases would send
  what leaving it out sends, so they aren't made.
- **No value a header can't hold.** A header refuses a control character, and gRPC metadata anything but printable
  ASCII (unless its key ends in `-bin`); the client would refuse the request before sending it.
- **Positive means allowed by everything declared.** `at-min-length` and `at-max-length` are made of a character the
  input's pattern allows (`AAA` for `^[A-Z]{3}$`), or aren't made. A pool value the pattern refuses is an edge case.
  A length over 65,536, such as the 2,147,483,647 some spec generators write for "no limit", gets no length cases,
  and an integer bound past what a 64-bit integer or a float's precision holds gets no boundary cases.
- **Wired inputs are left alone**, whether wired with `from` or by a default such as `{{createCart.cartId}}` that
  reads an earlier step's output.

### The template's own fields

A template also writes values of its own: `"channel": "web"`, a nested object, a literal query parameter. Nothing
says whether the API needs them, so each is fuzzed as an edge case, where only a 5xx, no response, or a broken
response is a finding:

| Field | Cases |
|-------|-------|
| A string, number, or boolean in the body | `body.<path>.remove`, `.null`, `.wrong-type` (a string becomes `12345`, a number `"x"`, a boolean `"yes"`) |
| An object or array in the body | `body.<path>.remove`, `.empty` |
| The body itself | `body.extra-property`, a property the template never sends |
| A literal query parameter | `query.<name>.remove` |

A gRPC message gets no `body.extra-property`, `wrong-type`, `fraction`, or `overflow` cases: a protobuf message has no
room for a field its type doesn't declare, and its codec refuses a value of the wrong type before anything is sent.

Fields inside `{{?…}}` and `{{#…}}` blocks, array elements, elements after a block in the same array (a block can send
any number of elements, so their index isn't known), and values under a key an input picks are left alone. A form
body or one that isn't JSON gets none of these cases.

These cases, and `missing` and `null`, change the request after the template builds it, so everything else in it is
what the plan would send. A query or header patch changes its own parameter or header and leaves the rest as the
template wrote it.
Values are sent exactly as generated, like a step value with [`raw: true`](value-flow.md#raw-values): `{{…}}` in a
value is not evaluated, and `"12"` stays a string. A `wrong-type` value for a number or a boolean is a JSON string,
quotes included, so it arrives as a string even in an unquoted template slot such as `{"quantity": {{quantity}}}`.

### The spec is the referee

When the step's node has an [OpenAPI operation](running.md#oas-validation), its request is checked against the spec.
A case whose request breaks the spec is judged as negative, whatever its own mode: the spec is the API's own word on
what it accepts. If the graph gave `quantity` no `min`, 0 would be an edge case, but the shop's spec says
`minimum: 1`, so the case is judged as negative and a 400 is the right answer. Removing a body field the spec
requires works the same way. The progress line shows the change as `edge→negative`, and the archive keeps the spec
violations on the case. Spec violations on a fuzz step's request are the point of the case, so they don't count as
OAS warnings. A violation the target's own request has too, such as a SKU format the spec is stricter about than the
API, is the spec's quarrel with the happy path, so it neither makes a case negative nor is listed on it.

A positive value the step's [`constraint`](value-flow.md#constraints) rules out, such as a destination equal to the
origin, is judged as edge: the plan would never send it, so a refusal isn't `rejected-valid`.

### Without an OpenAPI spec

Everything above works without a spec except the parts that read it: judging by the spec, `schema-violation`, and
`undocumented-status`. What decides a case's mode is then what the project declares:

- **Bounds and formats come from the graph.** `constraints` on an input (`min`, `max`, `minLength`, `maxLength`,
  `pattern`), `enum[...]` types, and a domain type's `validation` make the positive and negative cases. They describe
  the API, not the fuzzing, so they belong in the graph, where `aat validate`, the generated docs, and MCP use them
  too. [`aat generate --oas`](generate.md) writes an operation's bounds into the graph for you.
- **The structure comes from the template.** Body, query, and header cases need no spec.
- **Everything else is an edge case**, which only fails on a 5xx or no response.

An input with no constraints still gets its type's negative cases (`wrong-type`, `fraction`, `missing` when it is
required) and all of its edge cases.

## Findings

Each case's response gets one finding, or none when it was what the case called for:

| Finding | Meaning | Fails the run by default |
|---------|---------|:---:|
| `server-error` | A 5xx | yes |
| `no-response` | The request was sent and got no response: a timeout, a dropped connection | yes |
| `schema-violation` | The response breaks the OpenAPI spec | yes |
| `accepted-invalid` | A success for a negative case | no |
| `rejected-valid` | A 4xx for a positive case | no |
| `undocumented-status` | A status the node's OpenAPI operation doesn't list, with no `default` response | no |
| `throttled` | A 429, or gRPC `RESOURCE_EXHAUSTED`, after the retries the target allows: the API turned the request away before judging its value | no |
| `not-sent` | The case couldn't be sent: its setup failed (counted as `failed` in the setup line), or the request couldn't be built or its client refused it, such as a value a gRPC message can't hold | no |

A case gets the most serious finding that applies, in the order of the table: a status the spec doesn't list never
hides a forbidden value the API took.

`--fuzz-fail` lists the findings that fail the run: `--fuzz-fail server-error,accepted-invalid` makes an API that
takes forbidden values a failure. The others are warnings. A fuzz step never stops the run. It retries only a rate
limit, and only when its target's `retry` block would retry a 429; any other finding stands.

After the run, the output counts the cases by finding and lists each case that had one, with the value and the
status:

```
FAILED: fuzzing found 1 server-error
Fuzz: 6 cases: 5 as expected, 1 server-error
  server-error     quantity.negative  quantity=-1 -> 500
```

`aat run show` prints the same summary under the run's header. `--json`, the archive, and `summary.json` record each
case: its ID, mode, input, value, the mode it was judged by, the spec violations, and the finding.

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `--fuzz` | — | Steps to fuzz, by step ID or node |
| `--fuzz-mode` | all | `positive`, `negative`, `edge`, comma-separated |
| `--fuzz-input` | inputs not wired | Fuzz only these inputs, of whichever targets have them. A wired input, such as an ID from an earlier step, is fuzzed only when named here |
| `--fuzz-cases` | `0` (all) | At most this many cases per step, picked by the run's [seed](value-flow.md#replaying-a-runs-picks) |
| `--fuzz-case` | — | Run only these case IDs. Each must be a case of a step `--fuzz` names; the cap then picks among them |
| `--fuzz-scope` | `reuse` | What a case runs on: `reuse`, `isolated`, or `shared` (see [What a case runs on](#what-a-case-runs-on)) |
| `--fuzz-fail` | `server-error,no-response,schema-violation` | Findings that fail the run |
| `--fuzz-save` | — | Write a regression plan for each failing case to this directory ([Keeping a finding](#keeping-a-finding)) |
| `--fuzz-save-all` | `false` | With `--fuzz-save`, save the warnings too |
| `--no-fuzz` | `false` | Ignore the plans' `fuzz:` blocks |

The flags work on `aat run batch` too, where `--fuzz addItem` fuzzes every plan that has an `addItem` step and runs
the others as written. A name in `--fuzz`, `--fuzz-input`, or `--fuzz-case` only has to match in one plan, but one
that matches in none fails the batch with exit code 2, so a misspelled target doesn't pass as a batch with nothing
fuzzed.

`--stop-after` names a step of the plan, not a fuzz case or a copy made for one. A run that a fuzz finding has already
failed stops `failed`, not `stopped`, so the finding isn't lost.

With `--fuzz-cases`, the seed picks which cases run, and the run prints the seed, so `--seed N` runs the same ones
again.

## The fuzz block

A step can carry its fuzzing with it. A step with a `fuzz:` block is fuzzed on every run, with or without `--fuzz`:

```yaml
- id: addItem
  node: addItem
  fuzz:
    mode: [negative, edge]       # --fuzz-mode
    inputs: [quantity]           # --fuzz-input
    skip: [note]                 # inputs never to fuzz
    cases: 20                    # --fuzz-cases
    only: [quantity.below-min]   # --fuzz-case
    scope: isolated              # --fuzz-scope
    fail: [server-error, accepted-invalid]   # --fuzz-fail
    accept: [409]                # statuses no case is faulted for
    pinned:                      # cases sent exactly as written
      - id: quantity.below-min
        mode: negative
        input: quantity
        value: 0
        found: server-error
      - id: body.channel.remove
        mode: edge
        patch: [{where: body, path: channel, op: remove}]
```

Every key is optional.

- **`accept`** is where a judgement goes that only fuzzing needs and the graph doesn't hold. An API that answers
  `409` to any request it can't serve right now isn't faulted for it: a status in `accept` is never
  `accepted-invalid`, `rejected-valid`, or `undocumented-status`. A 5xx can't be accepted.
- **`pinned`** cases are sent as written, without the generator. A case sets either `input` and `value`, or `patch`.
  With only `pinned`, nothing else is generated. `mode`, `inputs`, `skip`, `only`, and `cases`, and the `--fuzz`
  flags, choose among pinned cases as they do among generated ones.
- **`only`** must name cases the step has; a misspelled ID is an error, not a block that sends nothing.
- **`found`** records what a case found when it was saved. It isn't checked.

`--fuzz` flags win over the block for the steps they name. `--no-fuzz` ignores every block and runs the plan as
written. `aat validate` checks the block against the step's node: modes, findings, inputs, and each pinned case. A
block on a step with `expectFailure` or a `rawBody` is an error.

The MCP server's [`generate_fuzz_cases`](mcp-server.md) tool lists the cases `--fuzz` would send to a step, as a
`fuzz:` block of pinned cases, without sending anything. An assistant can show them and keep the ones worth keeping.

## Keeping a finding

`--fuzz-save DIR` writes a plan for each case whose finding failed the run (`--fuzz-save-all` adds the warnings):

```bash
aat run plan smoke --fuzz addItem --fuzz-save plans/fuzz/
# Saved fuzz regression plan: plans/fuzz/smoke--addItem--quantity.below-min.yaml
```

The saved plan is the one that ran, with a recipe written out in full. Its target step has a `fuzz:` block pinning the
one case, with the `scope`, `accept`, and `fail` the case ran with, and `fail` includes the finding. On every run it
sends that value, fails while the API still mishandles it, and passes once the API is fixed. Kept in a plan directory, it becomes part of `aat run batch`. A plain plan can't
apply layers, so a plan found with layers says which ones in its description, along with the run's seed. In a batch,
a file is named after the plan's path within its directory, as in `us-smoke--addItem--quantity.below-min.yaml`, so
plans of one name in different subdirectories keep their own.

## What it doesn't do yet

- Properties the spec describes but the template never sends are not fuzzed, beyond `body.extra-property`.
