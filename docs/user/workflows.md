# Workflows and Composition

Workflows are reusable plan templates that capture common test patterns. Instead of writing the same ten steps for every booking test, you write the pattern once as a workflow and instantiate it through [recipes](plans.md). Workflows reduce duplication and let you focus on what varies between tests.

## Overview

A workflow is a YAML template that looks like a [full plan](plans.md#full-plans) but contains placeholders instead of final values. Workflows live in the `workflows/` directory and are declared in the [graph file](graphs.md) so AAT knows about them.

AAT supports three workflow kinds:

| Kind | Purpose | Example |
|------|---------|---------|
| *(none)* | **Base workflow** — a complete test skeleton with steps, cleanup, and optional slots | "Booking", "Exchange", "Create Order" |
| `slot` | **Slot option** — an interchangeable fragment that fills a choice point in a base workflow | "One-Way Search", "Round-Trip Search", "PayPal Payment" |
| `addon` | **Addon** — a bolt-on extension that splices steps into a base workflow at a declared insertion point | "Seat Selection", "Loyalty Points Redemption" |

A typical test composition: start with a base workflow, fill its slots with the desired options, then attach zero or more addons. The result is a standard plan that the engine executes without any special handling.

## Declaring Workflows in the Graph

Workflows are declared in the `workflows:` section of the graph YAML. Each entry gives the workflow a name, a kind, and a path to its template file.

### Base Workflow Declaration

A base workflow has no `kind` field (or kind is empty). It declares the overall test pattern, optional slots for variation, and a template file:

```yaml
workflows:
  - name: Create Order
    description: "Place an order with product selection, payment, and confirmation"
    template: workflows/order-base.yaml
    slots:
      - name: payment
        description: "Payment method"
        options: [Credit Card, PayPal, Bank Transfer]
        default: Credit Card
      - name: shipping
        description: "Shipping method"
        options: [Standard, Express]
        default: Standard
```

An airline booking workflow uses a similar structure with two slots:

```yaml
workflows:
  - name: Booking
    description: "Book a flight. Choose trip type and payment method."
    template: workflows/booking-base.yaml
    slots:
      - name: trip-search
        description: "How to search for and price flights"
        options: [One-Way, Round-Trip, Multi-City 3-Leg, Multi-City 4-Leg]
        default: One-Way
      - name: payment
        description: "Payment method"
        options: [Cash, Card]
        default: Cash
```

### Slot Definitions

Each slot in a base workflow defines a choice point with named options:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Slot name — referenced by recipes and used as a step marker in the template |
| `description` | string | no | Human-readable description, shown when choosing a workflow and in documentation |
| `options` | list | yes | Workflow names (each must be a `kind: slot` workflow) |
| `default` | string | no | Option used when the recipe doesn't specify a choice |

### Slot Option Declaration

A slot option is a workflow with `kind: slot` and a template:

```yaml
  - name: Credit Card
    kind: slot
    description: "Credit card payment method"
    template: workflows/slots/payment/credit-card.yaml

  - name: PayPal
    kind: slot
    description: "PayPal payment method"
    template: workflows/slots/payment/paypal.yaml
```

Slot options have no `after`, `wire`, or `slots` fields. Their template contains the steps that replace the slot marker in the base workflow.

A slot option may also carry an `inject` map. After every slot has been filled, each `inject` entry is applied to **every step of the base workflow and the chosen slot options**. Any step whose graph node declares an input with that name receives the value, unless the step already sets that input:
- A value, reference, selection, pool, or constraint counts as set, and so does a locked value.
- An empty `{}` counts as unset.

Addon steps are spliced in afterwards, so they do not receive injected values. `inject` on a base workflow or an addon is a validation error. This is how a choice made in one slot reaches steps that live outside it:

```yaml
  - name: Two Travelers
    kind: slot
    description: "Search and book for two passengers"
    template: workflows/slots/travelers/two.yaml
    inject:
      passengers: 2          # every step with a `passengers` input gets 2
```

Because injected values are defaults, a recipe's `values:` override still wins, and steps that do not declare the input are untouched.

An injected value takes the forms of a graph [input default](graphs.md#input-defaults), with one difference: a bare list is the list itself. `quantities: [2, 1]` injects that list, where a graph default would read it as a pool.
- A mapping takes the default's keys, such as `{value: [2, 1]}`, `{pool: [us, eu]}`, or `{from: createCart.cartId}`.
- The node name in `from` is translated to the composed step's ID.
- Any other key, such as `default:`, is an error.

`aat validate` checks each literal value against the type of every input it sets, so a single number injected for an `integer[]` input fails.

### Addon Declaration

An addon is a workflow with `kind: addon`. It declares where to insert (`after`), how to wire inputs (`wire`), and optional ordering (`priority`):

```yaml
  - name: Seat Selection
    kind: addon
    after: [priceOfferFull, priceOfferByRef]
    description: "Search seat map and add seat offer"
    template: workflows/addons/seat-selection.yaml
    wire:
      offerListIdentifier: $after.offerRef
      offerId: $after.offerId
      productId: $after.productId

  - name: Cancel Booking
    kind: addon
    after: confirmBooking
    priority: 90
    description: "Cancel the reservation after booking"
    template: workflows/addons/cancel-addon.yaml
    wire:
      locator: confirmBooking.locator
```

### Workflow Field Reference

| Field | Applies to | Type | Description |
|-------|-----------|------|-------------|
| `name` | all | string | Unique workflow name |
| `description` | all | string | Human-readable description |
| `kind` | slot, addon | string | `"slot"` or `"addon"` (empty = base workflow) |
| `template` | all | string | Path to the template YAML, relative to the graph file |
| `slots` | base only | list | Slot definitions (choice points) |
| `inject` | slot only | map | Input name → value set on every composed base and slot step whose node has that input, unless the step sets it itself. Values take the input default forms, except that a bare list is a literal list |
| `after` | addon only | string or list | Node name(s) to insert after; first match wins |
| `wire` | addon only | map | Explicit input wiring overrides (see [The Wire Map](#the-wire-map)) |
| `priority` | addon only | int | Composition ordering — lower values compose first (default: 0) |
| `selectionHint` | all | string | Extra guidance shown to the LLM when `aat prompt` chooses a workflow |
| `deprecated` | all | bool | Hides the workflow from `aat prompt`'s menu and rejects it when the LLM selects it; recipes that name it still compose |

## Workflow Templates

### Template File Format

A workflow template is a YAML file with the same structure as a plan's `execution` block. It contains `steps` (and optionally `cleanup`) but no `metadata` or top-level `kind`:

```yaml
# workflows/order-base.yaml
execution:
  steps:
    - node: createCart
      description: "Initialize a shopping cart"

    - slot: shipping

    - node: applyDiscount
      dependsOn: [shipping]
      values:
        cartId: {from: createCart.cartId}
        discountCode: AUTOWIRE

    - slot: payment
      dependsOn: [shipping]

    - node: confirmOrder
      dependsOn: [applyDiscount, payment]
      isGoal: true
      values:
        cartId: {from: createCart.cartId}
        paymentToken: AUTOWIRE

  cleanup:
    - node: cancelCart
      runOn: always
```

Steps use `node:` to reference graph nodes, just like plan steps. They also support `dependsOn`, `values`, `selections`, `assertions`, and all other step fields.

### Graph Default Pools and Workflow Templates

Notice that the template above only provides values for inputs that need explicit wiring (`from` references and AUTOWIRE). Other inputs, such as a quantity or a shipping address, are absent — the engine fills them from **graph default pools** declared on the node's inputs.

For example, a graph node might declare:

```yaml
nodes:
  searchProducts:
    inputs:
      - name: category
        type: string
        default: ["electronics", "clothing", "books", "home"]
      - name: maxPrice
        type: float
        optional: true
```

The pool shorthand `["electronics", "clothing", "books", "home"]` tells the engine to randomly pick one value at execution time. This means a workflow template doesn't need to specify `category` at all — every run gets a valid, varied value automatically.

This is a key design pattern: **graph default pools handle routine inputs, so workflow templates only wire structural data flow.** A well-designed graph with pools on its configurable inputs means:

- Workflow templates stay small and focused on step ordering and data wiring
- Every run uses different realistic data without any recipe overrides
- Recipes only need overrides when the test requires *specific* values (e.g., "search for flights from Nashville")

Pools can also contain expressions for dynamic values like dates:

```yaml
      - name: departureDate
        type: date
        default: ["{{today + 7 days}}", "{{today + 14 days}}", "{{today + 30 days}}"]
```

When a recipe or plan *does* provide a value for an input, it takes priority over the graph default pool. This is the override mechanism — graph pools provide sensible defaults, plans override when needed. See [Value Resolution](value-flow.md#resolution-priority) for the full priority chain.

### Slot Markers

A step with `slot:` instead of `node:` is a **slot marker** — a placeholder that gets replaced at composition time:

```yaml
    - slot: payment
      dependsOn: [shipping]
```

The slot marker carries `dependsOn` entries that are propagated to the root steps of whichever option fills the slot. This ensures the option's steps run at the right point in the execution order.

Slot markers have no `values`, `assertions`, or other step fields. They exist purely to mark where an option's steps get inserted.

### AUTOWIRE Placeholders

Steps in workflow templates often need inputs that come from the base workflow's other steps. Instead of hardcoding the source, use the `AUTOWIRE` placeholder:

```yaml
    - node: addSeatOffer
      values:
        bookingId: AUTOWIRE
        seatOfferingId: {fromSelection: seat.seatIdentifierValue}
```

`AUTOWIRE` tells the composition pipeline to resolve this input from another step's output with a matching name. The resolution order is:

1. **Explicit Wire map** — if the addon's `wire` map provides a mapping for this input name, use it.
2. **Output name match** — a step already in the plan produces an output with the same name as the input; the last such step wins.
3. **Final pass** — once slots and addons are in place, a marker still unresolved takes the nearest earlier step that produces the output (skipping any step that depends on the marker's own step). A base or slot step can therefore take an output that only an addon adds.
4. **Leave unresolved** — if nothing produces it, the AUTOWIRE marker stays for a recipe's `overrides.values` (or `aat prompt`) to fill. A plan that still holds the marker fails validation instead of sending it, and `aat validate workflow` reports addon AUTOWIRE inputs that cannot be wired.

#### Optional Inputs: `AUTOWIRE?`

Use `AUTOWIRE?` for an optional input that only some compositions can feed. It resolves exactly like `AUTOWIRE`, but when no step produces the output the input is left unset instead of unresolved:

```yaml
# workflows/checkout-base.yaml (excerpt)
    - node: checkout
      values:
        cartId: AUTOWIRE
        giftMessage: AUTOWIRE?     # only the Gift Wrap addon produces giftMessage
```

```yaml
# workflows/addons/gift-wrap.yaml (the addon attaches after createCart)
execution:
  steps:
    - node: addGiftWrap
      values:
        cartId: AUTOWIRE
```

- **With the addon:** `checkout.giftMessage` is wired to `inc0_addGiftWrap.giftMessage`, and `checkout` waits for that step.
- **Without it:** `giftMessage` is not sent.
- **What the input needs:** the graph must mark it `optional: true`, and the node's template should wrap the field in a conditional block (`{{?giftMessage}}…{{/giftMessage}}`) so the field is left out when unset.
- **Defaults:** graph and layer defaults do not apply to an input that `AUTOWIRE?` leaves unset.

### Cleanup in Templates

Templates can include `cleanup` steps. When addons also have cleanup, the composition pipeline merges them:

- Cleanup steps are deduplicated by node name
- If both the base and an addon declare cleanup for the same node, the base's entry wins
- Addon cleanup steps appear after the base's cleanup steps

## Slots

### How Slot Filling Works

When a recipe specifies `choices: {payment: "Credit Card"}`, the composition pipeline:

1. Finds the slot marker step with `slot: payment` in the base template
2. Loads the "Credit Card" workflow's template
3. Replaces the marker with the option's steps
4. Propagates the marker's `dependsOn` to the option's root steps (steps with no internal dependencies)
5. Rewrites any downstream `dependsOn` references to the slot name so they point to the last step of the inserted option

After all slots are filled, the pipeline runs AUTOWIRE resolution across the full plan and ensures all `from` references have corresponding `dependsOn` entries.

### Default Choices

When a recipe doesn't specify a choice for a slot, the slot's `default` option is used. If neither the recipe nor the slot definition provides a choice, composition fails with an error.

### Slot Option Templates

A slot option template is a standard workflow template — just steps and optional cleanup:

```yaml
# workflows/slots/payment/credit-card.yaml
execution:
  steps:
    - node: addCreditCard
      description: "Add credit card payment method"
      values:
        cartId: {from: createCart.cartId}
```

Option steps can reference steps from the base workflow (like `createCart` above) because the option's steps are inserted directly into the base plan at the marker's position. References to base workflow steps are valid.

## Addons

Addons are the primary extension mechanism. They splice additional steps into a composed plan at a declared insertion point.

### The After Field

The `after` field tells the composition pipeline where to insert the addon's steps. It accepts a scalar or a list:

```yaml
# Scalar — insert after this specific node
after: confirmOrder
```

```yaml
# List — try each in order, use first match
after: [priceOfferFull, priceOfferByRef]
```

When `after` is a list, the pipeline checks each node name against the composed plan and uses the first match. This is useful when different slot choices produce different node names but the addon should attach to whichever one is present.

### Addon Chaining

When multiple addons share the same insertion point, they chain automatically. The first addon inserts after the declared node; the second addon inserts after the last step of the first addon; and so on. This preserves sequential execution order between addons.

The chain order is determined by the `priority` field — lower values compose first. Addons with equal priority preserve their declaration order in the graph.

### The Wire Map

The `wire` map provides explicit input wiring for addon steps that can't be auto-wired by output name matching:

```yaml
wire:
  offerListIdentifier: $after.offerRef
  locator: confirmBooking.locator
```

Wire map entries take the form `inputName: stepId.outputName`. Two special syntaxes:

| Syntax | Meaning |
|--------|---------|
| `$after.fieldName` | Resolves to `matchedNode.fieldName` — the actual node that the addon attached to |
| `MANUAL` | Clears the AUTOWIRE marker, leaving the input for a recipe override (or `aat prompt`) to fill |

The `$after.` prefix is essential when the addon's `after` field is a list. The pipeline substitutes the actual matched node at composition time, so `$after.offerRef` might become `priceOfferByRef.offerRef` or `priceOfferFull.offerRef` depending on which slot option was chosen.

### Auto-Wiring Resolution

For each `AUTOWIRE` or `AUTOWIRE?` placeholder in an addon's steps, the composition pipeline tries, in order:

1. **Explicit Wire** — the addon's `wire` map provides a direct mapping.
2. **Output name match** — a step already in the plan (from the base plan, a slot option, or a previous addon) produces an output with the same name as the input; the last such step wins.
3. **Final pass** — after every addon is spliced, the nearest earlier step that produces the output, which can be another addon's step.
4. **Leave unresolved** — no match found. An `AUTOWIRE` marker remains for a recipe override (or `aat prompt`); an `AUTOWIRE?` input is left unset.

### Priority and Ordering

The `priority` field (default: 0) controls composition order. Lower priority values compose first. When multiple addons have the same priority, they compose in declaration order.

Priority matters when addons modify shared state:

```yaml
  - name: Retrieve Booking
    kind: addon
    after: confirmBooking
    priority: 10          # composes first — retrieves the reservation
    wire:
      identifier: confirmBooking.locator

  - name: Cancel Booking
    kind: addon
    after: confirmBooking
    priority: 90          # composes last — cancels the reservation
    wire:
      locator: confirmBooking.locator
```

Here, "Retrieve Booking" runs before "Cancel Booking" because it has a lower priority value — even though both attach after `confirmBooking`.

### Step ID Prefixing

To avoid name collisions between addon steps and the base plan, each addon's step IDs are prefixed during composition:

- Addon 0: `inc0_searchSeatMap`, `inc0_addSeatOffer`
- Addon 1: `inc1_retrieveReservation`
- Addon 2: `inc2_cancelReservation`

All internal references (`dependsOn`, `from`, `fromInput`, and a named selection's `from`) are rewritten to use the prefixed IDs. References to base plan steps remain unchanged.

Cleanup step node names are *not* prefixed because they reference graph node names directly.

## Composition Pipeline

Here is how a recipe becomes an executable plan, illustrated with a booking example using a Round-Trip slot choice and the Seat Selection addon.

### Step 1: Load Base Template

The base workflow's template file is loaded. The result is a plan with slot markers:

```
createBooking → [slot: trip-search] → addTraveler → [slot: payment] →
  addPayment → confirmBooking
```

### Step 2: Fill Slots

Each slot marker is replaced by its chosen option's steps:

- `[slot: trip-search]` with `choices: {trip-search: Round-Trip}` becomes:
  `searchRoundTrip → searchReturnLeg → priceOfferByRef → addOfferByRef`
- `[slot: payment]` with `choices: {payment: Cash}` becomes:
  `addCashPayment`

Downstream `dependsOn` references to `trip-search` are rewritten to `addOfferByRef` (the last step of the Round-Trip option). Each option's `cleanup:` is merged into the base's, and so is its `verification:`: a node the option verifies replaces the base's verification of that node.

### Step 3: Resolve AUTOWIRE (Slots)

After slot insertion, AUTOWIRE resolution runs across the entire plan. Inputs like `bookingId: AUTOWIRE` on slot option steps are resolved to `createBooking.bookingId` by matching the output name.

### Step 4: Splice Addons

Each addon is processed in priority order:

1. Find the insertion point (`after: priceOfferByRef` matches `priceOfferByRef`)
2. Load the addon template
3. Prefix step IDs: `searchSeatMap` → `inc0_searchSeatMap`
4. Resolve `$after.` wire references: `$after.offerRef` → `priceOfferByRef.offerRef`
5. Wire AUTOWIRE placeholders using the explicit Wire map and output name matching
6. Add insertion-point dependency to addon root steps
7. Splice addon steps after the insertion point
8. Merge addon cleanup with base cleanup, and addon verification with base verification (a node the addon verifies replaces the base's check of that node, since the addon knows the state the plan ends in)

Because slots are filled first, addons see slot option steps: an addon can attach after a node that the chosen slot option contributes, and `AUTOWIRE` an output that every option of a slot produces. `aat validate` checks addon compatibility the same way.

### Step 5: Resolve Remaining AUTOWIRE and Fix Dependencies

The pipeline scans all `from` references and ensures corresponding `dependsOn` entries exist. This catches cases where auto-wiring created data dependencies without explicit ordering.

It then wires the markers the earlier passes left. Each takes the nearest earlier step that produces an output of its name, unless that step depends on the marker's own step. An unmatched `AUTOWIRE?` is left unset.

### Step 6: Apply Recipe Overrides

Finally, [recipe overrides](plans.md#value-overrides) are applied — value overrides, selection strategy overrides, and assertion additions. A value override on an input that composition wired with `from` replaces that wiring, and an override for a step the composed plan does not have is an error. Post-processing then settles `dependsOn` again, adds the default `status: 2xx` assertion to steps that declare none (unless they expect failure), and adds a cleanup step for each node whose graph entry declares `cleanup:`.

The result is a standard `plan.Plan` ready for [execution](running.md).

## Repeated Steps

There is no repeat count in a recipe. When a test needs the same operation more than once — two travelers, three cart items — make the count a slot and give each option the steps it needs, with distinct `id`s. An `inject` map on the option carries the count to other steps that take it as an input:

```yaml
workflows:
  - name: Booking
    description: "Book a flight for one or two travelers"
    template: workflows/booking-base.yaml
    slots:
      - name: travelers
        description: "How many travelers to add"
        options: [One Traveler, Two Travelers]
        default: One Traveler

  - name: One Traveler
    kind: slot
    description: "A single passenger"
    template: workflows/slots/travelers/one.yaml

  - name: Two Travelers
    kind: slot
    description: "Two passengers"
    template: workflows/slots/travelers/two.yaml
    inject:
      passengers: 2          # e.g. the search step's passenger count
```

```yaml
# workflows/slots/travelers/two.yaml
execution:
  steps:
    - id: addTraveler1
      node: addTraveler
      values:
        bookingId: AUTOWIRE

    - id: addTraveler2
      node: addTraveler
      dependsOn: [addTraveler1]
      values:
        bookingId: AUTOWIRE
```

A recipe selects the option with `choices: {travelers: Two Travelers}`. Steps that depended on the `travelers` slot now depend on `addTraveler2`, the option's last step, and an addon declared `after: addTraveler` attaches after `addTraveler2`. Each copy's other inputs come from graph default pools, or a recipe can set them per copy (`addTraveler2.surname: Jones`).

## How Recipes Drive Composition

A [recipe](plans.md#recipes) provides the inputs to the composition pipeline:

| Recipe field | Pipeline input |
|-------------|---------------|
| `selection.workflow` | Which base workflow to load |
| `selection.choices` | Slot name → option name mappings for slot filling |
| `selection.addons` | List of addon workflow names to compose |
| `selection.layers` | Layers applied to the composed plan, before any `--layer` flags |
| `overrides.*` | Applied after composition completes |

The pipeline runs in this order: load base → fill slots → splice addons → apply overrides → post-process.

## Choosing a Workflow from a Description

Recipes are usually written by an AI coding tool connected to the [MCP server](mcp-server.md): it lists the workflows with their slots and addons, picks a workflow, choices, and addons for the test you describe, and validates and saves the recipe. The composition pipeline is the same whoever writes the recipe.

`aat prompt` is a built-in shortcut for the same job using the LLM configured in the environment file. Its first call returns a `WorkflowSelection` for a prompt such as `aat prompt "place an order with express shipping"`:

- **workflow**: which base workflow to use (e.g., "Create Order")
- **description**: a one-line summary of the test
- **choices**: which slot options to select (e.g., `{shipping: Express}`)
- **addons**: which addons to include (e.g., `["Loyalty Points"]`)
- **layers**: which layers to apply, when the project has layers

A second call fills literal values and assertions. `selectionHint` and `deprecated` on a workflow shape the menu that the first call chooses from. See [LLM-Assisted Planning](prompt.md) for details.

## Writing Workflows

### Designing Base Workflows

1. **Identify the recurring pattern** — what sequence of steps do multiple tests share?
2. **Define the core steps** — operations that appear in every variation
3. **Place slots at variation points** — where different tests diverge (e.g., payment method, search type)
4. **Use AUTOWIRE for inputs** that come from earlier steps — don't hardcode source references
5. **Add cleanup** for any resources that need teardown (e.g., delete cart, cancel reservation)

### Designing Slot Options

- Each option should be **single-concern** — it fills exactly one slot
- Option steps can reference base workflow steps by node name
- Options should produce outputs that downstream steps expect (match the contract the slot assumes)
- Keep options small — a slot option with ten steps may indicate the slot is too coarse

### Designing Addons

- Each addon should be **single-concern** — one logical extension (e.g., seat selection, loyalty points)
- Use `after` + `wire` to declare the attachment point and explicit wiring
- Use `$after.` syntax when the addon can attach to different nodes (multiple slot outcomes)
- Set `priority` to control ordering when multiple addons attach to the same point
- Keep addon cleanup minimal — let the base workflow handle shared cleanup

## Validation

`aat validate workflow` checks workflow structural correctness:

- Template files exist and parse correctly
- All step and cleanup nodes exist in the graph
- Slot markers reference valid slot definitions
- Addon `after` nodes exist in the graph

See [Validation](validation.md) for the full list of checks.

## Examples

### Base Workflow with Slots

The airline Booking workflow uses two slots — `trip-search` and `payment`:

```yaml
# Graph declaration
- name: Booking
  description: "Book a flight. Choose trip type and payment method."
  template: workflows/booking-base.yaml
  slots:
    - name: trip-search
      description: "How to search for and price flights"
      options: [One-Way, Round-Trip]
      default: One-Way
    - name: payment
      description: "Payment method"
      options: [Cash, Card]
      default: Cash
```

The base template uses slot markers that downstream steps depend on:

```yaml
# workflows/booking-base.yaml
execution:
  steps:
    - node: createBooking
    - slot: trip-search
    - node: addTraveler
      dependsOn: [trip-search]
      values:
        bookingId: {from: createBooking.bookingId}
    - slot: payment
      dependsOn: [trip-search]
    - node: addPayment
      dependsOn: [trip-search, payment, addTraveler]
      values:
        bookingId: {from: createBooking.bookingId}
        totalPrice: AUTOWIRE
        currencyCode: AUTOWIRE
    - node: confirmBooking
      dependsOn: [addPayment]
      isGoal: true
      values:
        bookingId: {from: createBooking.bookingId}
  cleanup:
    - node: discardBooking
      runOn: always
```

### Slot Composition: Cash vs Credit Card Payment

The Cash slot option is a single step:

```yaml
# workflows/slots/payment/cash.yaml
execution:
  steps:
    - node: addCashPayment
      values:
        bookingId: {from: createBooking.bookingId}
```

The Card slot option is also a single step but with different inputs:

```yaml
# workflows/slots/payment/card.yaml
execution:
  steps:
    - node: addCardPayment
      values:
        bookingId: {from: createBooking.bookingId}
```

When the recipe specifies `choices: {payment: Card}`, the `[slot: payment]` marker is replaced by the Card option's step. Downstream steps that depended on `payment` now depend on `addCardPayment`.

### Addon Composition: Seat Selection

The Seat Selection addon attaches after the pricing step and uses wire mapping with `$after.` syntax:

```yaml
# Graph declaration
- name: Seat Selection
  kind: addon
  after: [priceOfferFull, priceOfferByRef]
  template: workflows/addons/seat-selection.yaml
  wire:
    offerListIdentifier: $after.offerRef
    offerId: $after.offerId
    productId: $after.productId
```

The addon template uses AUTOWIRE for inputs that need wiring from the base plan:

```yaml
# workflows/addons/seat-selection.yaml
execution:
  steps:
    - node: searchSeatMap
      values:
        offerListIdentifier: AUTOWIRE
        offerId: AUTOWIRE
        productId: AUTOWIRE
    - node: addSeatOffer
      dependsOn: [searchSeatMap]
      selections:
        seat:
          from: searchSeatMap.seatOfferings
          strategy: first
      values:
        bookingId: AUTOWIRE
        catalogOfferingsIdentifierValue: {from: searchSeatMap.catalogOfferingsIdentifierValue}
        seatOfferingIdentifierValue: {fromSelection: seat.seatIdentifierValue}
        seatAssignment: {fromSelection: seat.seatAssignment}
```

When composed with a Round-Trip booking, `$after.offerRef` resolves to `priceOfferByRef.offerRef`, the AUTOWIRE `bookingId` resolves to `createBooking.bookingId`, and step IDs become `inc0_searchSeatMap` and `inc0_addSeatOffer`.

---

*Source: workflow types in `graph/types.go`, composition logic in `intent/compose.go`, template loading in `intent/template.go`.*
