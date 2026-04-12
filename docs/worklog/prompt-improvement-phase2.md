# Prompt Improvement: Value Fill (Phase 2) — Implementation

**Date:** 2026-03-07
**Status:** Complete (Sessions 1-4). Session 5 (travelport domain) deferred to aat-travelport repo.

## Context

A review of the Call 2 LLM prompt (targeted value fill) identified overuse of negative instructions (~18 "DO NOT" directives), missing few-shot examples, conflicting pool guidance, and unnecessary token overhead. This work rewrites the prompts for positive framing, adds a few-shot example, consolidates rules, and updates all tests.

---

## Session 1: System Prompt Overhaul — `intent/prompt.go` lines 114-176

### Changes Made

**Opening + output format:**
- "Your ONLY job" → "Your job" (less aggressive)
- "Respond with a JSON object (no markdown fencing, just raw JSON)" → "Return a JSON object (no markdown fencing)"
- "Omit empty categories" → "Include a category only if it contains entries" (positive framing)
- Added few-shot example (API-agnostic: `search.origin`, `search.date`, `search.results`)

**Date rules consolidated:**
- Two separate rules ("user's prompt takes priority" + "default to at least 7 days") → single precedence rule: "Date precedence: user prompt → template current value → default 7+ days in the future"

**Values section:**
- Removed "DO NOT use from, fromSelection, select" → "Values must be plain scalars — no object or reference syntax"
- "OMIT the key entirely unless the user explicitly specifies" → "include a pool input only when the user specifies a concrete value"
- Added "prefer simple, deterministic values" (determinism directive)
- "Hard constraints MUST be met" → "Hard constraints must be met" (less shouting)
- "If Current value: is shown, keep it unless..." → "Keep the Current value: unless..." (simpler)

**Optional Configuration section:**
- 3 bullet points → single sentence: "Set a value only when the user explicitly or implicitly requests it; omit to use the default."

**Selections section:**
- "Only add selection overrides when" → "Leave selections unchanged unless the user's intent requires an override"
- "do NOT add any selection overrides" + "DO NOT filter on fields not in" + "DO NOT re-filter on values" → "Filters may only reference fields in the element fields list. Search inputs already constrain results; selection filters are for additional preferences."

**Assertions section:**
- "Do NOT add assertions unless the user explicitly says" → "Add assertions only when the user says"
- Removed "— only when explicitly requested" subtitle suffix

**Wrong Workflow section:**
- 11 lines → 4 lines
- 4 "DO NOT" rules → single positive statement: "Adapting values, pools, and layer data to match user intent is your normal job — that is not a workflow mismatch."
- Removed "REFERENCE SAMPLES" shouting, "NOT mean the workflow only supports", etc.

### Token impact (estimated)
- Before: ~510 tokens system prompt
- After: ~400 tokens (+example, -verbosity)
- Net: ~20% reduction with a free few-shot example added

---

## Session 2: User Prompt Sections — `intent/prompt.go` lines 298-488

### Pool Section (`writePoolInputsSection`)
- Header: "## Pool Inputs — DO NOT provide values unless the user specifies" → "## Pool Inputs — auto-selected at runtime"
- 8 lines of guard-rails → 4 lines with positive precedence rule
- "DO NOT include these in your values response" → "Include a pool input only when the user's prompt specifies a concrete value"
- "do NOT pick random values from them" → "Pool values below are reference samples for mapping user intent to valid values"

### Auto-Wired Section (`writeAutoWiredSection`)
- Header: "## Auto-Wired Inputs — override only when user intent conflicts" → "## Auto-Wired Inputs — derived at runtime"
- "Do NOT provide values for these UNLESS the user's intent explicitly requires a different value" → "Include one only when the user's intent requires a value different from what auto-wiring produces"

### Layer-Handled Section (`writeLayerHandledSection`)
- Header: "## Layer-Handled Inputs — skip unless user intent conflicts" → "## Layer-Handled Inputs — managed by data layers"
- "DO NOT include these unless the user's prompt EXPLICITLY mentions" → "Include one only when the user's prompt specifies a concrete value"
- "do NOT pick random values" → "Pool values below are reference samples for mapping user intent to valid values"

### Negative instruction count
| Location | Before | After |
|----------|--------|-------|
| System prompt | 12 | 0 |
| Pool section | 3 | 0 |
| Auto-wired section | 1 | 0 |
| Layer-handled section | 2 | 0 |
| **Total** | **18** | **0** |

---

## Session 3: Workflow Selection Prompt — `intent/prompt.go` lines 16-58

Light consistency pass on Call 1 system prompt:
- "Include addons ONLY when" → "Include addons only when" (less shouting, same constraint)
- "Do not speculatively include addons. Omit 'addons' if no addons are needed." → "Omit 'addons' if no addons apply." (redundant sentence removed)
- "Omit 'layers' if no layers are needed." → "Omit 'layers' if none apply."

The Call 1 prompt was already clean from Phase 1 — minimal changes needed.

---

## Session 4: Test Updates

### Files modified
| File | Assertions updated |
|------|-------------------|
| `intent/targeted_test.go` | 8 assertions |
| `intent/template_test.go` | 2 assertions |
| `intent/format_test.go` | 0 (unaffected — tests FormatGraph/FormatWorkflowMenu, not prompt text) |

### Specific assertion changes in `targeted_test.go`

1. Pool section header: `"## Pool Inputs — DO NOT provide values..."` → `"## Pool Inputs — auto-selected at runtime"`
2. Pool section index: `"## Pool Inputs — DO NOT"` → `"## Pool Inputs — auto-selected"`
3. Pool values guard-rail: `"do NOT pick random values"` → `"reference samples for mapping user intent"`
4. System pool guidance: `"OMIT the key entirely unless the user explicitly specifies"` → `"include a pool input only when the user specifies"`
5. System pool guidance: `"Pool Inputs"` → `"Pool inputs auto-select at runtime"`
6. Auto-wired header: `"override only when user intent conflicts"` → `"derived at runtime"`
7. Layer-handled guard-rail: `"DO NOT include these unless the user's prompt EXPLICITLY mentions"` → `"Include one only when the user's prompt specifies a concrete value"`
8. Wrong plan guardrails: `"Pool values and layer data shown below are REFERENCE SAMPLES"` + `"Data layers restrict the random pool"` → `"Adapting values, pools, and layer data to match user intent is your normal job"` + `"fundamental domain mismatches"`

### Specific assertion changes in `template_test.go`

1. Addon rule: `"Include addons ONLY"` → `"Include addons only when the user explicitly requests"`
2. Addon speculative: `"Do not speculatively include addons"` → `"Omit \"addons\" if no addons apply"`

### Verification
- `make check`: all tests pass (fmt + test-race + lint), 0 issues

---

## Session 5: Travelport Domain Refinements (deferred)

Deferred to `aat-travelport` repository:
- Clarify `airportCode` type: prefer airport codes over city codes (ORD not CHI, CDG not PAR)
- Review `airportCodes` pool for city code entries
- Integration validation with `aat prompt --trace`

---

## Key Decisions

1. **All negative framing removed.** Every "DO NOT" / "do NOT" directive replaced with positive "include only when" / "leave unchanged unless" / "values must be" framing. Same constraints, better LLM compliance.

2. **Few-shot example is API-agnostic.** Uses `search.origin`, `search.date`, `search.results` — generic enough for any API, concrete enough to demonstrate format.

3. **Pool precedence unified.** The old conflicting "Pick from sample values" + "OMIT pool inputs" is now a single rule: "include a pool input only when the user specifies a concrete value."

4. **"from/fromSelection" rule simplified.** The old explicit "DO NOT use from, fromSelection, select" was replaced with "Values must be plain scalars — no object or reference syntax." The targeted prompt never shows these concepts to the LLM anyway; `validateValuesAreLiteral` catches violations mechanically.

5. **Wrong Workflow section dramatically simplified.** 11 lines with 4 negative rules → 4 lines with one positive statement. The LLM doesn't need to be told what's NOT a mismatch in 4 separate ways.

6. **Tests updated by string only.** No test logic, fixtures, or structural assertions changed — only the expected prompt text strings in `assert.Contains` calls.
