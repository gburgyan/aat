# Graph Authoring Guide

This guide walks through the recommended workflow for creating an AAT graph from scratch. The process starts with automated scaffolding from an OpenAPI spec and progresses through iterative refinement with validation feedback.

## Overview

An AAT graph defines the topology of an API: which operations exist, what data they accept and produce, and how data flows between them. The authoring workflow is:

1. **Scaffold** — Generate a starting-point graph and templates from an OpenAPI spec
2. **Refine nodes** — Adjust types, add descriptions, mark optional inputs, tune outputs
3. **Add edges** — Define data flow between nodes (the scaffold intentionally omits edges)
4. **Validate** — Run `aat graph validate` to catch structural and OAS alignment issues
5. **Iterate** — Repeat steps 2–4 until the graph accurately models your API workflow

> **No OpenAPI spec?** You can write graph and template files directly — see [Getting Started: Manual Authoring](getting-started.md#path-b-without-a-spec-manual-authoring) for a walkthrough, then come back here for the [YAML reference](#graph-yaml-reference) and edge authoring guidance. The scaffold step is optional; everything from Step 2 onward applies regardless of how you created your initial graph.

## Step 1: Scaffold from an OpenAPI Spec

If you have an OpenAPI 3.x spec, use `aat generate` to create a starting point:

```bash
aat generate \
  --oas api-spec.yaml \
  --output-graph graph.yaml \
  --output-templates templates/
```

This produces:
- **`graph.yaml`** — One node per operation (operationId), with inputs from parameters + request body, and outputs from the first 2xx response schema
- **`templates/<operationId>.yaml`** — One template per node, with `{{placeholder}}` substitution in paths, headers, and request bodies

The scaffold is intentionally rough. It gives you correct HTTP methods, paths, and parameter names, but won't know which operations connect to each other or which outputs matter for your test flow.

### What the scaffold includes

| Element | Source |
|---------|--------|
| Node names | `operationId` from the spec |
| Inputs | Path/query/header parameters + request body properties |
| Input types | Mapped from OAS types (string, integer, number→float, boolean, date, datetime) |
| Optional/required | From OAS `required` field |
| Outputs | Properties from the first 2xx `application/json` response |
| Array outputs | Detected automatically with `elementFields` from item schema |
| OAS links | Each node gets `oas.operationId` for validation |
| Templates | HTTP method, path with `{{params}}`, headers, JSON body with placeholders |

### What the scaffold does NOT include

- **Edges** — You add these based on your workflow (which output feeds which input)
- **Descriptions** — Only copied if present in the spec's `summary` field
- **Cleanup nodes** — You mark which nodes need cleanup
- **Custom types** — Domain-specific types like `airportCode` need manual annotation
- **Selective outputs** — The scaffold includes all response properties; you'll typically pare down to just the ones your workflow needs

### Stdout mode

To preview the generated graph without writing files:

```bash
aat generate --oas api-spec.yaml --output-graph -
```

## Step 2: Refine Nodes

Open the generated `graph.yaml` and curate it for your test workflow.

### Remove unused nodes

The scaffold creates a node for every operation. Delete the ones your workflow doesn't need. If you're testing a booking flow, you probably don't need `listUsers` or `getSystemHealth`.

### Improve types

The scaffold maps OAS types mechanically. Replace generic types with domain-specific ones where appropriate:

```yaml
# Scaffold output:
inputs:
  - name: origin
    type: string
  - name: departureDate
    type: string

# After refinement:
inputs:
  - name: origin
    type: airportCode
    description: "Departure airport IATA code"
  - name: departureDate
    type: date
    description: "Outbound departure date"
```

Domain types integrate with the [domain knowledge layer](value-flow.md#domain-knowledge) for LLM-assisted value resolution.

### Trim outputs

The scaffold includes every property from the response schema. Keep only the outputs your workflow actually uses — either as inputs to downstream nodes or as assertions in your plan.

```yaml
# Scaffold includes all 15 response fields
outputs:
  - name: id
    type: string
  - name: status
    type: string
  - name: createdAt
    type: datetime
  # ... 12 more

# After refinement: just what the workflow needs
outputs:
  - name: id
    type: string
    description: "Created resource ID, passed to update/delete steps"
  - name: status
    type: string
    description: "Resource status for assertion"
```

### Configure array outputs

If a list endpoint returns objects that downstream steps need to select from, configure `elementFields` on the array output:

```yaml
outputs:
  - name: offerings
    type: offering[]
    description: "Available offerings to select from"
    elementFields:
      - name: offeringId
        type: string
        path: "id"           # gjson extraction path (if different from name)
      - name: price
        type: float
      - name: carrier
        type: string
```

The `path` field is only needed when the JSON field name differs from the logical name you want in your plan.

### Mark cleanup nodes

If an operation creates a resource that should be cleaned up after the test (even on failure), set the `cleanup` field on the creating node:

```yaml
createWorkbench:
  description: "Create a booking session"
  adapter: createWorkbench
  cleanup: deleteWorkbench    # runs after plan completes
  inputs: [...]
  outputs: [...]
```

## Step 3: Add Edges

Edges define how data flows between nodes. This is the most important manual step — the scaffold can't infer which outputs feed which inputs because that depends on your specific workflow.

### Edge format

```yaml
edges:
  # Scalar: pass output directly as input
  - from: createResource.resourceId
    to: updateResource.resourceId

  # Select: choose from an array output
  - from: listItems.items
    to: processItem.itemId
    select: true
```

### How to identify edges

For each node's inputs, ask: "Where does this value come from?"

- **From an upstream output** → Add an edge
- **From the plan/user** → No edge needed (the plan provides the value)
- **A constant** → Set a `default` on the input

A useful heuristic: if two nodes share an input/output name with the same type, they probably need an edge.

### Example: building a booking flow

Starting from a scaffold with these nodes: `searchFlights`, `createWorkbench`, `addOffer`, `addTraveler`, `commitBooking`

```yaml
edges:
  # Search results feed into addOffer
  - from: searchFlights.catalogOfferingsId
    to: addOffer.catalogOfferingsId
  - from: searchFlights.catalogOfferings
    to: addOffer.offeringId
    select: true
  - from: searchFlights.catalogOfferings
    to: addOffer.productRef
    select: true

  # Workbench ID flows to all steps that need it
  - from: createWorkbench.workbenchId
    to: addOffer.workbenchId
  - from: createWorkbench.workbenchId
    to: addTraveler.workbenchId
  - from: createWorkbench.workbenchId
    to: commitBooking.workbenchId

  # Commit needs confirmation from both addOffer and addTraveler
  - from: addOffer.offerStatus
    to: commitBooking.offerStatus
  - from: addTraveler.travelerId
    to: commitBooking.travelerId
```

### AI-assisted edge authoring

If you're using Claude Code with AAT's MCP server, Claude can analyze API documentation, response schemas, and your graph nodes to suggest edges. The typical workflow:

1. Generate the scaffold and refine nodes
2. Share the graph with Claude Code and ask it to add edges based on the API docs
3. Validate the result with `aat graph validate`
4. Review and adjust

Even without MCP, you can share your graph YAML and API documentation with any LLM and ask for edge suggestions.

## Step 4: Validate

Run structural and OAS validation after each round of edits:

```bash
aat graph validate --graph graph.yaml
```

### Flags

| Flag | Description |
|------|-------------|
| `--graph` | Path to graph YAML file (required) |
| `--oas` | Override OAS spec path (replaces graph-level `oas` field) |
| `--templates` | Path to templates directory (cross-validates outputs vs extract keys) |
| `--strict` | Treat warnings as errors |

### Structural validation

- All edge references (`from`, `to`) point to existing nodes and fields
- No orphan nodes (every node is reachable)
- No cycles (unless broken by `cycleBreaker: true`)
- Input/output names are unique per node

### OAS validation

If your graph references an OAS spec (via the `oas` field or per-node `oas.spec`), validation also checks alignment:

```bash
# Spec path from graph's oas field
aat graph validate --graph graph.yaml

# Or override with a specific spec
aat graph validate --graph graph.yaml --oas api-spec.yaml

# Treat warnings as errors
aat graph validate --graph graph.yaml --strict
```

For nodes with `oas.operationId`:
- **Errors**: operationId not found in spec, missing spec path
- **Warnings**: graph input not in OAS params, required OAS param missing from graph, graph output not in OAS response

Warnings are informational — the graph is the source of truth, and intentional divergence from the OAS spec is normal (e.g., you might omit optional parameters or extract only specific response fields).

### Adapter output validation

When `--templates` is provided, validation checks that each node's declared outputs match the extract keys in its template:

```bash
aat graph validate --graph graph.yaml --templates templates/
```

This catches two kinds of mismatch:
- **Graph output not extracted by template** — the node declares an output but the template has no extract entry for it, so the output will always be nil at runtime
- **Template extracts undeclared output** — the template extracts a key the graph doesn't declare, which is dead extraction (likely a typo or stale rename)

Nodes with no outputs (cleanup/void operations) and non-template adapters are skipped. This check also runs automatically at the start of `aat run`.

## Step 5: Iterate

Graph authoring is iterative. A typical session looks like:

```
generate scaffold
  └─ remove unused nodes
  └─ refine types and descriptions
  └─ validate → fix issues
  └─ add edges for first workflow
  └─ validate → fix issues
  └─ write a test plan
  └─ run → observe failures
  └─ fix graph/templates based on real API behavior
  └─ validate → clean
```

The Travelport booking graph went through several iterations as real API quirks were discovered (see [travelport-example.md](travelport-example.md#travelport-api-notes)).

## Template Refinement

The generated templates are functional but minimal. Common refinements:

### Add response extraction paths

The scaffold extracts top-level fields. For nested responses, add gjson paths:

```yaml
response:
  extract:
    resourceId: "data.resource.id"
    status: "data.resource.status"
```

### Fix request body structure

The scaffold creates a flat JSON body from request body properties. Real APIs often need nested structures:

```yaml
# Scaffold output:
body: |-
  {
    "name": "{{name}}",
    "type": "{{type}}"
  }

# After refinement (nested structure):
body: |-
  {
    "resource": {
      "name": "{{name}}",
      "type": "{{type}}"
    }
  }
```

### Add static headers

```yaml
headers:
  Content-Type: application/json
  Accept: application/json
  X-Api-Version: "2"
```

Environment-level headers (like auth tokens) are configured in the [environment file](environments.md), not in templates.

## Graph YAML Reference

### Node

```yaml
nodeName:
  description: "What this operation does"
  adapter: adapterName              # matches template's adapter field
  cleanup: cleanupNodeName          # optional: runs after plan completes
  cycleBreaker: true                # optional: breaks dependency cycles
  oas:                              # optional: link to OAS operation
    operationId: getResource
    spec: other-spec.yaml           # optional: override graph-level spec
  inputs:
    - name: resourceId
      type: string
      description: "Resource identifier"
      optional: true                # default: false (required)
      default: "default-value"      # used when no edge or plan value provides it
  outputs:
    - name: result
      type: object[]
      description: "List of results"
      elementFields:                # for array outputs
        - name: id
          type: string
          path: "nested.id"         # gjson path, defaults to name
```

### Edge

```yaml
edges:
  - from: sourceNode.outputName
    to: targetNode.inputName
    select: true                    # optional: array element selection
    preferred: true                 # optional: prefer this edge over alternatives
```

### Types

| Type | Description |
|------|-------------|
| `string` | Text value |
| `integer` | Whole number |
| `float` | Decimal number |
| `boolean` | true/false |
| `date` | Date (YYYY-MM-DD) |
| `datetime` | Date and time |
| `money` | Currency amount |
| `enum[a, b, c]` | Enumeration |
| `X[]` | Array of X |
| `customName` | Domain-specific type |
