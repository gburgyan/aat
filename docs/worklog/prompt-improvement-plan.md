# Prompt Improvement Plan: Workflow Selection (Phase 1)

**Date:** 2026-03-07
**Source:** OpenAI Prompt Guidance review of `buildWorkflowSelectionPrompt`

## Context

The workflow selection prompt (`intent/prompt.go:16-44`) is the first LLM call in the two-call Interpret pipeline. It classifies user intent and selects a workflow + choices + addons + layers. Review identified 10 improvement areas. Some changes are purely in the `aat` codebase (prompt structure, rules); others require project-specific content (layer rules, few-shot examples, defaults) that belongs in the travelport project's `graph.yaml` or layer files.

Current prompt structure:
- **System**: Role + JSON schema + 5 rules (mixed together)
- **User**: `FormatWorkflowMenu()` output + user intent

---

## Session 1: Structural Prompt Improvements (aat repo)

Changes to `intent/prompt.go` `buildWorkflowSelectionPrompt()` and related helpers.

### 1a. Separate instructions from context (Issue 1)

Currently the system prompt mixes task definition, output schema, and rules. Restructure:

```
SYSTEM:
  - Task framing (classification task, exactly one workflow)
  - Output schema (JSON format)
  - Decision procedure (step-by-step)
  - Rules (addon, layer, choices)
  - JSON output enforcement

USER:
  - Available workflows / addons / layers (from FormatWorkflowMenu)
  - User intent
```

The user prompt structure is already close to this — the main work is reorganizing the system prompt into clearly delimited sections.

**Files:** `intent/prompt.go` (lines 16-44)

### 1b. Strengthen task framing (Issue 2)

Replace the opening line:
```
You are an API testing assistant. Given available workflows and a user's testing intent, select the appropriate workflow.
```
With explicit classification framing:
```
You are an API testing workflow classifier. Your task is to classify the user's testing intent and select exactly one workflow configuration.
```

**Files:** `intent/prompt.go`

### 1c. Strengthen JSON output enforcement (Issue 3)

Replace:
```
Respond with a JSON object (no markdown fencing, just raw JSON):
```
With:
```
Return ONLY valid JSON. Do not include markdown fencing, explanations, commentary, or additional fields.
```

**Files:** `intent/prompt.go`

### 1d. Add decision procedure (Issue 7)

Add an explicit step-by-step process after the output schema:

```
Follow this process:
1. Identify the user's testing goal
2. Select the best matching workflow
3. Fill choice slots (or use defaults)
4. Include addons only if the user explicitly requests matching capabilities
5. Apply layers based on route, payment, cabin, or date clues
6. Return the JSON result
```

**Files:** `intent/prompt.go`

### 1e. Tighten addon selection rule (Issue 8)

Change:
```
Include addons when the user mentions capabilities matching an addon's description
```
To:
```
Include addons ONLY when the user explicitly requests capabilities matching an addon's description (e.g., "add seat", "add baggage", "modify traveler"). Do not speculatively include addons.
```

**Files:** `intent/prompt.go`

### 1f. Remove date generation rule (Issue 9)

The workflow selection call does not generate dates — that's phase 2's job. Remove:
```
Today's date is YYYY-MM-DD. When generating dates, default to at least 7 days in the future or past depending on context.
```
The date is still needed in phase 2 (`buildTargetedPlanPrompt`) — leave that unchanged.

**Files:** `intent/prompt.go`

### 1g. Specify description format (Issue 10)

Change `"brief description"` in the JSON schema to:
```
"description": "1-2 sentence summary of the test scenario"
```

**Files:** `intent/prompt.go`

### Testing

- Update `intent/template_test.go` tests (lines 755-806) to match new prompt text
- Run `make check` to verify no regressions
- Manual test: `cd ../aat-travelport && ../aat/aat prompt "book a flight from rome to new york"` — verify selection still works

---

## Session 2: Project-Level Prompt Extensions (aat + travelport repos)

Some improvements require project-specific content that doesn't belong hardcoded in `aat`. This session adds a mechanism for projects to contribute prompt hints, then uses it in travelport.

### 2a. Add `selectionHint` field to Workflow type

Add an optional `selectionHint` field to `graph.Workflow`:
```go
SelectionHint string `yaml:"selectionHint,omitempty"`
```

When present, append it after the workflow description in `FormatWorkflowMenu`. This lets projects annotate workflows with selection guidance (e.g., "deprecated - do not select") without polluting the description field that's used elsewhere.

**Files:** `graph/types.go`, `intent/format.go` (`formatBaseWorkflows`, `formatAddonWorkflows`)

### 2b. Add `selectionHint` field to Layer type

Add to `graph.Layer`:
```go
SelectionHint string `yaml:"selectionHint,omitempty"`
```

Append after the layer description in `formatLayersSection`. This lets layers carry explicit selection rules (e.g., "Use when travel crosses global regions").

**Files:** `graph/layer.go`, `intent/format.go` (`formatLayersSection`)

### 2c. Add `Deprecated` field to Workflow (Issue 5)

Add to `graph.Workflow`:
```go
Deprecated bool `yaml:"deprecated,omitempty"`
```

In `formatBaseWorkflows` and slot option rendering: when `Deprecated` is true, append `(deprecated - do not select)` and/or skip the workflow entirely from the menu. Skipping is safer — removes the option so the LLM can't select it.

In `validateWorkflowSelection`: reject deprecated workflows with a clear error message.

**Files:** `graph/types.go`, `intent/format.go`, `intent/interpret.go` (validation)

### 2d. Surface slot defaults in the prompt (Issue 6)

The slot default is already tracked in `SlotDef.Default` and rendered with a `(default)` marker. Strengthen by adding an explicit "Defaults" summary after the choices block in `formatBaseWorkflows`:

```
  Defaults: trip-search=One-Way, traveler=Single Traveler, payment=Cash
```

**Files:** `intent/format.go` (`formatBaseWorkflows`)

### 2e. Travelport: annotate layers with selection hints

Update each layer YAML in `aat-travelport/layers/` to add `selectionHint`:

```yaml
# amex.yaml
selectionHint: "Use when payment mentions American Express or Amex"

# international.yaml
selectionHint: "Use when travel crosses global regions (e.g., US to Europe)"

# european.yaml
selectionHint: "Use when all routes are within Europe"

# ndc.yaml
selectionHint: "Use only if user explicitly requests NDC content"

# near-term.yaml
selectionHint: "Use when travel is within 2-5 days"

# premium.yaml
selectionHint: "Use when user requests Business or First class"
```

**Files:** `aat-travelport/layers/*.yaml`

### 2f. Travelport: mark broken workflows as deprecated

In `aat-travelport/graph.yaml`, add `deprecated: true` to the four broken Full-Payload slot options (lines ~67-80). Remove "(don't use, broken)" from their descriptions. Clean up the descriptions.

**Files:** `aat-travelport/graph.yaml`

### Testing

- Add unit tests for `selectionHint` rendering in `format_test.go`
- Add unit test for deprecated workflow filtering in `format_test.go`
- Add unit test for deprecated workflow rejection in `interpret_test.go`
- Run `make check`
- Manual test with travelport to verify layer hints appear and broken options are excluded

---

## Session 3: Few-Shot Examples (aat + travelport repos)

### 3a. Add `examples` field to Graph type

Add to `graph.Graph`:
```go
Examples []WorkflowExample `yaml:"examples,omitempty"`
```

Where:
```go
type WorkflowExample struct {
    Input  string `yaml:"input"`
    Output string `yaml:"output"` // raw JSON string
}
```

### 3b. Render examples in workflow selection prompt

In `buildWorkflowSelectionPrompt`, if the graph has examples, append an `## Examples` section to the user prompt (after the workflow menu, before user intent):

```
## Examples

Input: book a one way flight from nyc to lax
Output: {"workflow": "Booking", "description": "Book a one-way domestic flight.", "choices": {"trip-search": "One-Way"}}

Input: round trip chicago to paris for two, pay with amex, add seats
Output: {"workflow": "Booking", "description": "Round-trip international flight for two with seat selection.", "choices": {"trip-search": "Round-Trip", "traveler": "Two Travelers", "payment": "Card"}, "addons": ["Seat Selection Addon"], "layers": ["international", "amex"]}
```

This requires passing the graph (or just the examples) through to `buildWorkflowSelectionPrompt`. Currently it only receives the pre-formatted menu string. Options:
- Pass `[]WorkflowExample` as an additional parameter
- Include examples in `FormatWorkflowMenu` output

The cleaner approach is adding examples to `FormatWorkflowMenu` since it already takes the graph.

**Files:** `graph/types.go`, `intent/format.go` (`FormatWorkflowMenu`), `intent/prompt.go`

### 3c. Travelport: add 2-3 few-shot examples

Add examples to `aat-travelport/graph.yaml`:

```yaml
examples:
  - input: "book a one way flight from nyc to lax"
    output: '{"workflow": "Booking", "description": "Book a one-way domestic flight for one traveler.", "choices": {"trip-search": "One-Way"}}'
  - input: "round trip from chicago to paris for two travelers, pay with amex, add seats"
    output: '{"workflow": "Booking", "description": "Round-trip international flight for two with seat selection and Amex payment.", "choices": {"trip-search": "Round-Trip", "traveler": "Two Travelers", "payment": "Card"}, "addons": ["Seat Selection Addon"], "layers": ["international", "amex"]}'
  - input: "exchange a booking to a different flight"
    output: '{"workflow": "Exchange", "description": "Exchange an existing booking for a different flight."}'
```

**Files:** `aat-travelport/graph.yaml`

### Testing

- Unit tests for example rendering in `format_test.go`
- Verify examples appear in prompt output
- Manual end-to-end test with travelport
- Run `make check`

---

## Summary: Issue Coverage

| Issue | Fix | Session |
|-------|-----|---------|
| 1. Instructions mixed with context | Restructure system prompt sections | 1 |
| 2. Weak task framing | Explicit classification framing | 1 |
| 3. Weak output enforcement | Stronger JSON-only rule | 1 |
| 4. Implicit layer selection | `selectionHint` on Layer | 2 |
| 5. Deprecated workflow options | `deprecated` field + filtering | 2 |
| 6. Defaults not explicit | Defaults summary in menu | 2 |
| 7. No decision process | Step-by-step procedure | 1 |
| 8. Addon selection too open | Tighten addon rule | 1 |
| 9. Unnecessary date logic | Remove from phase 1 | 1 |
| 10. Description underspecified | Format spec in schema | 1 |
| Bonus: Few-shot examples | `examples` on Graph | 3 |

## Key Files

| File | Changes |
|------|---------|
| `intent/prompt.go` | System prompt restructure (Session 1) |
| `intent/format.go` | selectionHint rendering, deprecated filtering, defaults summary, examples (Sessions 2-3) |
| `intent/interpret.go` | Deprecated workflow validation (Session 2) |
| `graph/types.go` | `Deprecated`, `SelectionHint`, `WorkflowExample` fields (Sessions 2-3) |
| `graph/layer.go` | `SelectionHint` field (Session 2) |
| `intent/template_test.go` | Update prompt text assertions (Session 1) |
| `intent/format_test.go` | New tests for hints, deprecated, defaults, examples (Sessions 2-3) |
| `intent/interpret_test.go` | Deprecated rejection test (Session 2) |
| `aat-travelport/graph.yaml` | Mark deprecated, add examples (Sessions 2-3) |
| `aat-travelport/layers/*.yaml` | Add selectionHint (Session 2) |
