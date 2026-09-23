# Fuzzing

`--fuzz` sends generated values to one step inside a real flow. The plan builds the state the step needs — a cart
with an item in it, a booking, an order — and the fuzzer tries the step with values the graph allows, values it
forbids, and values it says nothing about. Each case runs as its own step, and a case the API mishandles is reported
without stopping the run.

Spec-driven fuzzers such as Schemathesis fuzz one operation at a time. That finds a lot, but it can't reach an
operation whose inputs have to come from earlier calls: an `addItem` needs a cart that exists. In AAT the plan gets
there first.

Nothing about fuzzing goes in the graph. The cases come from what the project already declares: each input's `type`
and `constraints`, the domain file's types and [value pools](domain.md#value-pools), and, when the graph has an
OpenAPI spec, the spec.

## Quick start

With the shop sandbox running (see [the shop example](examples/shop.md)):

```bash
cd examples/shop/
../../aat run plan smoke --fuzz addItem
```

```
  [15/23] addItem              201  0ms
  [16/23] addItem--fuzz-quant~ 400  0ms  fuzz quantity.fraction (negative)
  [17/23] addItem--fuzz-quant~ 400  0ms  fuzz quantity.overflow (negative)
  [18/23] addItem--fuzz-quant~ 400  0ms  fuzz quantity.wrong-type (negative)
  [19/23] addItem--fuzz-quant~ 400  0ms  fuzz quantity.zero (edge→negative)
  [20/23] addItem--fuzz-quant~ 400  0ms  fuzz quantity.negative (edge→negative)
  [21/23] addItem--fuzz-quant~ 409  0ms  fuzz quantity.large (edge)
  [22/23] checkout             201  0ms
...
PASSED (23/23 steps, 5ms)
Fuzz: 6 cases: 6 as expected
```

The happy path runs as usual. Each case runs after its target, on its own copy of the steps the target depends on,
so a case can't change what the rest of the plan sees. Those copies, such as `listProducts__fuzz-quantity-zero` and
`createCart__fuzz-quantity-zero` for the case `quantity.zero`, run before the target and are left out above.

Only `quantity` was fuzzed: `cartId` and `sku` are wired from earlier steps, and fuzzing those would only test that
the step can't find its cart.

`--fuzz` takes step IDs or node names, comma-separated. A node name fuzzes every step of that node.

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

Values are sent exactly as generated, like a step value with [`raw: true`](value-flow.md#raw-values): `{{…}}` in a
value is not evaluated, and `"12"` stays a string. A `wrong-type` value for a number or a boolean is a JSON string,
quotes included, so it arrives as a string even in an unquoted template slot such as `{"quantity": {{quantity}}}`.

### The spec is the referee

When the step's node has an [OpenAPI operation](running.md#oas-validation), its request is checked against the spec.
A case whose request breaks the spec is judged as negative, whatever its own mode: the spec is the API's own word on
what it accepts. In the run above, `quantity.zero` is an edge case, since the graph declares no minimum, but the shop's
spec says `minimum: 1`, so the 400 it got was right. The progress line shows this as `edge→negative`, and the
archive keeps the spec violations on the case. Spec violations on a fuzz step's request are the point of the case,
so they don't count as OAS warnings.

## Findings

Each case's response gets one finding, or none when it was what the case called for:

| Finding | Meaning | Fails the run by default |
|---------|---------|:---:|
| `server-error` | A 5xx | yes |
| `no-response` | The request was sent and got no response: a timeout, a dropped connection | yes |
| `schema-violation` | The response breaks the OpenAPI spec | yes |
| `accepted-invalid` | A success for a negative case | no |
| `rejected-valid` | A 4xx for a positive case | no |
| `not-sent` | AAT could not build the request, such as a value a gRPC message can't hold | no |

`--fuzz-fail` lists the findings that fail the run: `--fuzz-fail server-error,accepted-invalid` makes an API that
takes forbidden values a failure. The others are warnings. A fuzz step never stops the run, and it doesn't retry.

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
| `--fuzz-input` | inputs not wired | Fuzz only these inputs. A wired input, such as an ID from an earlier step, is fuzzed only when named here |
| `--fuzz-cases` | `0` (all) | At most this many cases per step, picked by the run's [seed](value-flow.md#replaying-a-runs-picks) |
| `--fuzz-case` | — | Run only these case IDs |
| `--fuzz-scope` | `isolated` | `isolated` gives each case its own copy of the steps the target depends on. `shared` runs every case on the target's own; it is faster, but a case the API accepts can change what later steps see |
| `--fuzz-fail` | `server-error,no-response,schema-violation` | Findings that fail the run |

The flags work on `aat run batch` too, where `--fuzz addItem` fuzzes every plan that has an `addItem` step and runs
the others as written.

With `--fuzz-cases`, the seed picks which cases run, and the run prints the seed, so `--seed N` runs the same ones
again.

## What it doesn't do yet

- Fuzzing reaches a step's inputs, not fields a template fills in itself: a nested body field that isn't an input
  isn't fuzzed.
- A missing required input isn't a case: the template can't be rendered without it.
- A case that finds something is not yet written out as a plan to keep as a regression test; replay it with
  `--fuzz-case`, or copy the value into a [mutation](plans.md#negative-testing-expectfailure).
