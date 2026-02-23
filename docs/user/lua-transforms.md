# Lua Transforms

<!-- Advanced guide: post-processing extracted outputs with inline Lua scripts. -->

## Overview

<!-- What Lua transforms are: inline scripts in the template's response.transform field -->
<!-- When to use them: complex cross-referenced API responses that need flattening or enrichment -->
<!-- Relationship to extraction: transforms run AFTER extract, receiving the extracted outputs -->

## The `response.transform` Field

<!-- Where it lives in the template YAML -->
<!-- Inline string (YAML block scalar) containing a Lua script -->
<!-- Runs after extract, before outputs are stored -->

## Runtime Environment

<!-- Sandboxed Lua 5.1 via gopher-lua -->
<!-- 5-second execution timeout -->
<!-- No file I/O, no network, no OS access -->

### Available Globals

<!-- `outputs` — mutable Lua table containing the extracted outputs from response.extract -->
<!-- `json_path(path)` — function that queries the full raw response body using gjson syntax -->

### Safe Libraries

<!-- base, table, string, math -->
<!-- NOT available: io, os, debug, coroutine -->

## Return Value

<!-- Script MUST return a table (the modified outputs) -->
<!-- Returning nil or a non-table value is an error -->

## Type Conversion

<!-- Go → Lua: nil→LNil, string→LString, numbers→LNumber, bool→LBool, []any→table, map→table -->
<!-- Lua → Go: reverse mapping; tables detected as sequential (array) or keyed (map) -->

## Use Case: Cross-Reference Resolution

<!-- GDS APIs return reference lists at the top level, with main results containing only reference IDs -->
<!-- The Lua script builds lookup tables from reference lists and enriches the extracted offerings -->
<!-- Example: Travelport ReferenceListProduct/ReferenceListFlight resolution -->

## Examples

### Enriching Array Outputs

<!-- From searchFlights.yaml: resolve productRef → flight details (carrier, times, stops, cabin) -->
<!-- Walk through the script logic step by step -->

### Simple Field Derivation

<!-- Hypothetical: compute a derived field from existing outputs -->

## Error Handling

<!-- Script errors surface as step execution errors -->
<!-- Timeout (5s) kills the Lua VM and reports a timeout error -->
<!-- Missing json_path results: check for nil before using -->

## Debugging Tips

<!-- Use the web UI's step detail to inspect pre-transform and post-transform outputs -->
<!-- Run archives capture the final (post-transform) outputs -->

---

*Source: New document — covers `adapter/lua.go` implementation.*
