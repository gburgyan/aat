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
  [21/32] addItem              201  0ms
  [22/32] addItem--fuzz-quant~ 201  0ms  fuzz quantity.at-min (positive)
  [23/32] addItem--fuzz-quant~ 400  0ms  fuzz quantity.below-min (negative)
  [24/32] addItem--fuzz-quant~ 400  0ms  fuzz quantity.fraction (negative)
  [25/32] addItem--fuzz-quant~ 400  0ms  fuzz quantity.overflow (negative)
  [26/32] addItem--fuzz-quant~ 400  0ms  fuzz quantity.wrong-type (negative)
  [27/32] addItem--fuzz-quant~ 400  0ms  fuzz quantity.missing (negative)
  [28/32] addItem--fuzz-quant~ 400  0ms  fuzz quantity.null (negative)
  [29/32] addItem--fuzz-quant~ 409  0ms  fuzz quantity.large (edge)
  [30/32] addItem--fuzz-body-~ 201  0ms  fuzz body.extra-property (edge)
  [31/32] checkout             201  0ms
...
PASSED (32/32 steps, 6ms)
Fuzz: 9 cases: 9 as expected
```

The happy path runs as usual. Each case runs after its target, on its own copy of the steps the target depends on,
so a case can't change what the rest of the plan sees. The copy also includes the earlier steps that build on those,
such as the `addItem` a checkout needs even though it reads nothing from it. If a copied step fails, its case is
reported as `not-sent` and the run carries on. Those copies, such as `listProducts__addItem--fuzz-quantity-null`
and `createCart__addItem--fuzz-quantity-null` for the case `quantity.null`, run before the target and are left out
above.

Only `quantity` was fuzzed as an input: `cartId` and `sku` are wired from earlier steps, and fuzzing those would only
test that the step can't find its cart. The shop's graph gives `quantity` a `min: 1` constraint, which is where
`at-min` and `below-min` come from.

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
| Any input the template sends in the body, the query, or a header | `missing` for an optional input with a value | `missing` and `null` (body only) for a required input | `null` for an optional one |

An input the template sends only inside a `{{?input}}` block gets no `missing` case: leaving it out is the template's
own choice. An input that is only in the path gets none either, since leaving it out changes the route.

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

A gRPC message gets no `body.extra-property`: a protobuf message has no room for a field its type doesn't declare.

Fields inside `{{?…}}` and `{{#…}}` blocks, and array elements, are left alone. A form body or one that isn't JSON
gets none of these cases.

These cases, and `missing` and `null`, change the request after the template builds it, so everything else in it is
what the plan would send.
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
OAS warnings.

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
| `not-sent` | The case couldn't be sent: its copy of a setup step failed, or AAT couldn't build the request, such as a value a gRPC message can't hold | no |

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

- Properties the spec describes but the template never sends are not fuzzed, beyond `body.extra-property`.
- A case that finds something is not yet written out as a plan to keep as a regression test; replay it with
  `--fuzz-case`, or copy the value into a [mutation](plans.md#negative-testing-expectfailure).
