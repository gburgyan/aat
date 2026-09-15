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
