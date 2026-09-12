# Lua Transforms

A template's `response.transform` field holds a Lua script that reshapes a step's outputs after extraction. Use it when the outputs you need are not sitting at any single JSON path: joining a list of references to the records they point at, computing a value from several fields, or building an output from parts of the body that `extract` rules cannot combine.

Most templates never need one. Reach for a transform only after [`extract` rules](templates.md#response-extraction), including [array `fields`](templates.md#array-extraction), have run out.

A transform runs with the same privileges as `aat` itself. It is not a security boundary: treat template scripts like any other code in your project, and review changes to templates you did not write. See [Limits](#limits).

## Where the Script Lives

The script is an inline YAML block scalar under `response`, next to `extract`:

```yaml
response:
  extract:
    subtotal: subtotal
    discount: discount
  transform: |
    -- Amount due after the coupon, in minor units
    outputs.amountDue = outputs.subtotal - outputs.discount
    return outputs
```

With a response of `{"subtotal": 12797, "discount": 1280}`, the step's outputs are `subtotal`, `discount`, and `amountDue: 11517`. The graph node declares all three outputs as usual.

A template may have `extract` rules, a `transform`, or both. With both, the extract rules run first and the script receives what they produced. With only a `transform`, the script starts from an empty `outputs` table and builds everything from `json_path`.

## When It Runs

For steps in the main flow and verification steps, AAT runs extraction, and then the transform, only when the response status is below 400. An error response (including the expected error of an `expectFailure` step) skips both, so the step records no outputs.

Within a step, the order is:

1. The request is sent and the response arrives.
2. `extract` rules run. They need a JSON body: a non-JSON body fails the step with `response body is not valid JSON` before the script starts. A transform-only template runs on any body; its `json_path` calls return `nil` when the body is not JSON.
3. The script runs and its return value replaces the step's outputs.
4. The graph's `errorDetection` rules check the raw response body, not the transformed outputs.
5. Assertions without `raw: true` see the transformed outputs (see [Assertions](plans.md#assertions)), and later steps read them through `from` references.

Cleanup steps run their template's transform too, on every cleanup response whatever its status. A script error there is ignored: the cleanup step still counts as passed and records no outputs.

## The Runtime

Scripts run in [gopher-lua](https://github.com/yuin/gopher-lua) v1.1.1, a Lua 5.1 implementation in Go. Each run gets a fresh interpreter, so nothing carries over between steps or between runs of the same step.

### Globals

| Global | What it is |
|--------|------------|
| `outputs` | A table of the extracted outputs, keyed by output name. An array output with `fields` is a list of tables keyed by field name. Empty when the template has no `extract` rules. |
| `json_path(path)` | Looks up `path` in the full raw response body and returns the value, or `nil` when the path does not exist or holds `null`. |
| `print(...)` | Writes its arguments, tab-separated, as one line to stderr. |

`json_path` takes the same [path syntax](templates.md#path-syntax) as `extract`: a leading `$.` is dropped, `[0]` becomes `.0`, and `$` alone returns the whole body. The rest of [gjson](https://github.com/tidwall/gjson) syntax works as well:

| Call | Result on the shop's cart response |
|------|------------------------------------|
| `json_path("products")` | The products array, as a table |
| `json_path("products.#")` | The number of products |
| `json_path("products.#.name")` | A list of every product name |
| `json_path('products.#(sku=="SKU-1006").name')` | `"Wool Socks"` |
| `json_path("missing.path")` | `nil` |

`print` goes to stderr so that it never mixes with `--json` or `--dump-state -` output on stdout. It prints under `--quiet` too, and nothing it prints is stored in the run archive.

### Libraries

AAT opens these libraries: the base library, `table`, `string`, and `math`. It does not open `io`, `os`, `debug`, `coroutine`, `package`, or gopher-lua's `channel`, so scripts cannot open files, run programs, or read environment variables.

From the base library, AAT removes the functions that load code or reach the host process: `dofile`, `loadfile`, `load`, `loadstring`, `require`, `module`, `getfenv`, `setfenv`, `collectgarbage`, and `newproxy`. The rest stays, including `pcall`, `error`, `pairs`, `ipairs`, `select`, `tonumber`, and `tostring`. See [Limits](#limits).

Numbers follow Lua 5.1: there is no separate integer type, and every number is a double-precision float.

### Return Value

The script must `return` a table keyed by output name. That table becomes the step's outputs in full: a key you remove is no longer an output, and a key you add becomes one.

| Script ends with | Result |
|------------------|--------|
| `return outputs` | The (possibly modified) outputs |
| `return { total = outputs.a + outputs.b }` | Only `total` |
| `return {}` | No outputs |
| no `return`, or `return nil` | Error: `lua script must return a table (got nil)` |
| `return "done"` | Error: `lua script must return a table (got string)` |
| `return { "a", "b" }` | Error: `lua script must return a table keyed by output name (got a list)` |

When a script returns several values, only the last one is checked, so `return outputs, nil` fails.

### Type Conversion

Values cross between JSON and Lua twice: once into the script (`outputs` and `json_path` results) and once back out (the returned table).

| JSON value | In Lua | Back in the step's outputs |
|------------|--------|----------------------------|
| string | string | string |
| number | number (a float) | number, as a float |
| `true` / `false` | boolean | boolean |
| `null` | `nil`: the key is absent from `outputs` | dropped |
| array | table with keys `1`..`n` | array |
| object | table with string keys | object |

Converting a table back follows these rules:

- A table whose keys are exactly `1`..`n` becomes an array.
- An empty table becomes an empty array, `[]`, even where the response had an empty object `{}`.
- A table with gaps in its integer keys becomes an object with those keys as strings. An array whose middle element was `null` comes back as `{"1": ..., "3": ...}`.
- A table with any string key becomes an object; its integer keys become strings.
- A Lua function or other non-data value becomes `null`.

Every output of a template that has a transform makes this round trip, including outputs the script never touches. Integers above 2^53 lose precision: `9007199254740993` comes back as `9007199254740992`, so keep large numeric IDs out of templates with transforms. A whole number that fills a later `{{placeholder}}` is written in plain digits, however large.

## Worked Example: Joining Cart Lines

The [shop example](examples/shop.md)'s cart API returns each line as a SKU and a quantity only. Names and prices sit in a separate top-level `products` array. The `getCart` template joins them so every line reads on its own. This is `examples/shop/templates/getCart.yaml` in full:

```yaml
adapter: getCart
protocol: http

request:
  method: GET
  path: /carts/{{cartId}}

response:
  extract:
    cartId: cartId
    status: status
    lineCount: lineCount
    subtotal: subtotal
    subtotalDisplay: subtotalDisplay
    lines:
      path: lines
      fields:
        sku: sku
        quantity: quantity
  # Cart lines carry only a SKU and a quantity; names and prices sit in the
  # top-level products array. Join them so every line reads on its own.
  transform: |
    local catalog = {}
    for _, product in ipairs(json_path("products") or {}) do
      catalog[product.sku] = product
    end
    for _, line in ipairs(outputs.lines or {}) do
      local product = catalog[line.sku]
      if product then
        line.name = product.name
        line.price = product.price
      end
    end
    return outputs
```

A real response from the sandbox, trimmed to the two arrays:

```json
{
  "lines": [
    {"sku": "SKU-1001", "quantity": 1},
    {"sku": "SKU-1006", "quantity": 2}
  ],
  "products": [
    {"sku": "SKU-1001", "name": "Trail Backpack", "price": 8999, "priceDisplay": "$89.99"},
    {"sku": "SKU-1006", "name": "Wool Socks", "price": 1899, "priceDisplay": "$18.99"}
  ]
}
```

What happens, step by step:

1. The `extract` rules run. `outputs.lines` is a list of tables with only `sku` and `quantity`, because the `fields` mapping keeps only those two keys.
2. The first loop reads the whole `products` array with `json_path("products")` and indexes it by SKU. `or {}` keeps the loop safe if the array is missing, since `json_path` returns `nil` then.
3. The second loop looks up each line's SKU and adds `name` and `price` to the line table. Tables are references in Lua, so changing `line` changes `outputs.lines` in place.
4. `return outputs` hands back the same table, now with the joined lines.

`products` never becomes an output. It has no `extract` rule, so it does not have to be declared in the graph (see [Static Validation](#static-validation)), and the script reads it directly from the body instead.

Run it against the sandbox and read the step's outputs from the archive. From the shop example's directory (`examples/shop` in a clone of the repository, or the directory `aat-sandbox init shop` creates):

```bash
aat-sandbox serve --latency 0 &
archive=$(aat run plan full-lifecycle --json | jq -r .archive_path)
jq '.steps[] | select(.node == "getCart") | .outputs.lines' "$archive"
```

```json
[
  {
    "name": "Trail Backpack",
    "price": 8999,
    "quantity": 1,
    "sku": "SKU-1001"
  },
  {
    "name": "Wool Socks",
    "price": 1899,
    "quantity": 2,
    "sku": "SKU-1006"
  }
]
```

The graph's `getCart` node declares the joined shape, so plans and selections can use `name` and `price` like any other element field (an excerpt of its outputs):

```yaml
outputs:
  - name: lines
    type: cartLine[]
    description: Cart lines joined with product name and unit price
    elementFields:
      - name: sku
        type: string
      - name: quantity
        type: integer
      - name: name
        type: string
      - name: price
        type: integer
```

## Static Validation

`aat validate` (its Adapter outputs section), `aat validate graph --templates`, and the pre-flight check at the start of every run compare each template with its graph node. A transform relaxes some of these checks, because AAT does not run or analyze the script ahead of time:

| Check | Without a transform | With a transform |
|-------|---------------------|------------------|
| The graph declares an output the template does not extract | Error, unless the output is `optional` | Accepted: the script is trusted to set it |
| The template extracts an output the graph does not declare | Error | Still an error |
| An array output's `fields` mapping leaves out one of the graph's `elementFields` | Error | Accepted |
| An array output's `fields` mapping has a field that is not among the graph's `elementFields` | Error | Still an error |
| The OAS output check (validate commands only; a warning, an error under `--strict`) | Looks for each output in the 2xx response schema at its extract path | Skips outputs that have no extract rule |

In the shop, `name` and `price` pass only because `getCart` has a transform. Delete the `transform:` block and `aat validate` reports:

```text
Adapter outputs:        FAILED
  adapter output validation failed:
    - node "getCart" output "lines": graph elementField "name" has no corresponding template field
    - node "getCart" output "lines": graph elementField "price" has no corresponding template field
```

Because an extract rule for an undeclared output is still an error, read data the script only needs along the way with `json_path` rather than extracting it.

Validation does not parse the script, so a Lua syntax error passes `aat validate` and fails the step at run time. Nothing checks at run time that the script set every declared output either: a later step that reads a missing one fails while resolving its inputs, for example `from reference "getCart.itemCount": output "itemCount" not found for node "getCart"`.

## Errors and Timeouts

Any script failure fails the step: a syntax error, a runtime error, a call to `error()`, a return value that breaks the rules above, or the timeout. Every message starts with `extracting outputs: transform:`. Errors raised while the script runs continue with `lua script error:` and a position whose line number counts from the first line of the script. If the shop's script called `error("expected at most one line")` on its line 13, the run would show:

```text
  [ 6/15] getCart              ERROR: extracting outputs: transform: lua script error: <string>:13: expected at most one line
stack traceback:
	[G]: in function 'error'
	<string>:13: in main chunk
	[G]: ?

  cleanup:
    deleteCart             204  0ms
```

What a failed transform does to the run:

- The run's outcome is `error` and `aat run` exits with code `2` (see [Exit Codes](running.md#exit-codes)).
- Cleanup still runs for the steps that completed. Above, `deleteCart` removes the cart, and no `deleteOrder` is sent because checkout never ran.
- All of the failing step's outputs are discarded, including the IDs its own cleanup would need. If that step's request created something (a transform error on `createCart`, say), the resource is left behind.
- Step-level `retry:` classifies a transform error as `adapter`, which is not retried unless `on` lists it (see [Retry](plans.md#retry)). Plan-level `--retries` reruns the whole plan after an `error` outcome.

Each script run has a 5-second timeout, which fails the step with a `context deadline exceeded` error at the line that was running. The interpreter checks the deadline before every Lua instruction, so wrapping a loop in `pcall` does not keep the script alive: the next instruction outside it fails again. A single long call into a Go-implemented function (a `string.rep` with a huge count, a `gsub` over a very large string, or `json_path` itself) is not interrupted, and the deadline is noticed only after it returns. AAT sets no memory limit on scripts.

## Inspecting Transform Results

- **Run archives.** Each step whose transform succeeded stores its outputs after the transform and the script source (`transformScript`). Cleanup steps record their outputs but not the script. The outputs before the transform are not stored, but the raw response body is, so you can redo the join by hand. See [Run Archives](archives.md).
- **Web UI.** In `aat web`, a step that ran a transform gets a **Lua Output** tab showing the step's final outputs; the **Extractions** tab lists the same outputs with the steps that consumed them. See [Web UI](web-ui.md).
- **MCP tools.** `inspect_template` (`inspect_request_template` in the API persona) shows the full script. `describe_node`, `describe_operation`, and `get_response_shape` flag nodes whose outputs a transform changes, using the script's leading `--` comment lines as a summary, so start each script with one. A YAML `#` comment above `transform:` is not part of the script and is not used. See [Lua Transform Indicators](mcp-server.md#lua-transform-indicators).
- **`print`.** Output goes to stderr during the run and is not archived.

## Limits

These are current limits of the transform runtime, not guarantees to rely on:

- **Not a sandbox against hostile scripts.** A script cannot open files, run programs, or load code, but it runs inside the `aat` process with its permissions. Review the transforms in templates you did not write, such as an integration kit's.
- **Numbers are floats.** Integers above 2^53 lose precision (see [Type Conversion](#type-conversion)).
- **Nulls are lost.** A `null` value disappears from objects, turns an array with a `null` inside into an object, and cannot be told apart from a missing path in `json_path`. Empty objects come back as empty arrays.
- **The timeout is partial.** The 5-second limit does not interrupt a long Go-side library call, and there is no memory limit.

---

*Source: `adapter/lua.go`, `adapter/template.go`, `engine/engine.go`, `engine/cleanup.go`, `engine/validate_adapters.go`, `graph/oas/validator.go`, `archive/types.go`, `server/web/src/routes/StepDetail.svelte`, `mcp/tools_template.go`, `mcp/tools_graph.go`.*
