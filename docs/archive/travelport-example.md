# Travelport Booking Example

This walks through the Travelport booking flow — a real end-to-end API test that searches for flights, creates a reservation workbench, adds an offer and traveler, and commits the booking to produce a PNR. The Travelport graph also demonstrates addon workflows for seat selection, ancillary services, and traveler modifications.

## Prerequisites

The Travelport graph and templates live in a separate repository. Clone it into the `travelport/` directory inside your AAT checkout:

```bash
cd aat
git clone https://github.com/gburgyan/aat-graph-travelport.git travelport
```

You also need valid Travelport API credentials. The environment config (`travelport/env.yaml`) references secrets via environment variables — set them before running.

## Files

| File | Description |
|------|-------------|
| `travelport/graph.yaml` | API graph: nodes, edges, base and addon workflows |
| `travelport/templates/` | Request/response templates for each operation |
| `travelport/workflows/` | Workflow plan templates (base and addon) |
| `travelport/env.yaml` | Pre-production environment config |

## Running

```
make build
```

The travelport directory has an `aat-project.yaml` manifest, so from inside it you only need the plan path:

```bash
cd travelport

# Run a pre-written plan
aat run plan workflows/oneway-fullpayload.yaml

# Or generate a plan from a prompt (requires LLM config in env)
aat prompt "Book a one-way flight from Denver to San Francisco"
```

You can also run from the repo root with explicit paths:

```
./aat run plan travelport/workflows/oneway-fullpayload.yaml \
  --env travelport/env.yaml \
  --graph travelport/graph.yaml \
  --templates travelport/templates/
```

Or point `AAT_PROJECT` at the travelport directory:

```
AAT_PROJECT=./travelport aat run plan travelport/workflows/oneway-fullpayload.yaml
```

## Booking Flow

```
searchFlights ──────┬──────────────────── priceOffer (informational)
  │                 │
  │  catalogOfferingsId, offeringId, productRef
  │                 │
  ▼                 ▼
createWorkbench ── addOffer ──┐
  │                           │
  ▼                           ▼
addTraveler ───────────── commitBooking
                              │
                              ▼
                         PNR locator
```

### Step Details

**1. searchFlights** — Searches the Travelport catalog for flights.
- Inputs: origin, destination, departureDate (from plan values)
- Outputs: `catalogOfferings` (array of flight offerings), `catalogOfferingsId` (session ID)

**2. priceOffer** — Prices a selected offering (informational only, not required for booking).
- Inputs: `catalogOfferingsId` (from search), `offeringId` and `productRef` (selected from search offerings)
- Outputs: `offerListId`, `offerId`, `totalPrice`, `currencyCode`
- Note: This step confirms pricing but the booking itself doesn't depend on it.

**3. createWorkbench** — Creates a reservation workbench (booking session).
- Inputs: none
- Outputs: `workbenchId`
- Cleanup: `ignoreWorkbench` runs after the plan completes to discard the workbench if the booking fails.

**4. addOffer** — Adds the selected offer to the workbench using `BuildFromCatalogProductOfferings`.
- Inputs: `workbenchId` (from createWorkbench), `catalogOfferingsId`, `offeringId`, `productRef` (from search)
- Outputs: `offerStatus` (offer identifier confirming success)
- Endpoint: `/air/book/airoffer/reservationworkbench/{id}/offers/buildfromcatalogproductofferings`

**5. addTraveler** — Adds passenger details to the workbench.
- Inputs: `workbenchId`, `surname`, `givenName`, `birthDate`, `gender` (from plan values)
- Outputs: `travelerId`

**6. commitBooking** — Finalizes the reservation.
- Inputs: `workbenchId`, `offerStatus`, `travelerId`
- Outputs: `locator` (PNR), `locatorSource` (e.g., "1G")

## Travelport API Notes

These are lessons learned from debugging against the real PP environment:

### Endpoint Selection for Adding Offers

There are three endpoints for adding offers to a workbench:

| Endpoint | Description | Status |
|----------|-------------|--------|
| `buildfromcatalogproductofferings` | References search catalog IDs directly | Works in PP |
| `buildfromproducts` | Full payload with SpecificFlightCriteria | Works in PP |
| `buildfromofferlist` | References a price response by GUID | 404 in PP |

We use `buildfromcatalogproductofferings` because it works with simple template substitution — the offering ID and product ref from the search response are passed directly.

### PersonName vs PersonNameDetail

The addTraveler endpoint requires `"@type": "PersonNameDetail"` (not `"PersonName"`). Using `PersonName` will cause validation errors.

### Required Traveler Fields

The addTraveler request must include `Telephone` and `Email` arrays. These are hardcoded in the template with placeholder values since they're required by the API but not significant for test purposes.

### PNR Location in Response

The PNR locator is nested at:
```
ReservationResponse.Reservation.Receipt[0].Confirmation.Locator.value
```
Not at `LocatorCode` or `Reservation.Identifier.value` as other documentation might suggest.

### Workbench Cleanup

After a successful commit, the `ignoreWorkbench` cleanup step returns 400 (workbench already consumed). This is expected behavior — the cleanup is only meaningful when the booking fails partway through.

### Required Headers

The environment config includes these headers for proper API gateway routing:

```yaml
headers:
  XAUTH_TRAVELPORT_ACCESSGROUP: "<access-group-id>"
  Accept: application/json
  Accept-Version: "11"
  Content-Version: "11"
```

## Customizing the Plan

To book a different route, edit `workflows/travelport_booking.yaml`:

```yaml
    - node: searchFlights
      values:
        origin: "ORD"
        destination: "DEN"
        departureDate: "2026-04-01"
```

To change the traveler:

```yaml
    - node: addTraveler
      values:
        surname: "Doe"
        givenName: "John"
        birthDate: "1985-06-15"
        gender: "Male"
```

## Selection Strategies

The plan uses named selections when multiple inputs need fields from the same array element. Both `priceOffer` and `addOffer` need `offeringId` and `productRef` from the same offering:

```yaml
    - node: addOffer
      selections:
        catalogOffering:
          from: searchFlights.catalogOfferings
          strategy: first
      values:
        offeringId:
          fromSelection: catalogOffering.offeringId
        productRef:
          fromSelection: catalogOffering.productRef
```

The `selections` block picks one element from the array, then each `fromSelection` extracts a field from that element. This guarantees both values come from the same offering.

Other strategies: `last`, `random`, `index` (specific position), `min`/`max` (by field value), `match` (by predicate filter), `llm` (LLM-assisted choice).

See [value-flow.md](value-flow.md) for the full guide on how values move between steps.

## Addon Workflows

The Travelport graph includes three addon workflows that can be composed with any base booking workflow:

| Addon | After Node | Description |
|-------|-----------|-------------|
| **Seat Selection** | `priceOfferFullPayload` | Searches the seat map and adds a seat offer to the workbench |
| **Ancillary Booking** | `addTraveler` | Searches for and adds ancillary services (bags, meals, etc.) |
| **Traveler Modification** | `addTraveler` | Two-step traveler update (modify then verify) |

### Using Addons with `aat prompt`

When you mention addon functionality in your prompt, the LLM automatically selects the appropriate addons:

```bash
# Base booking only
./aat prompt "Book a one-way flight from Denver to San Francisco" ...

# Base booking + seat selection addon
./aat prompt "Book a one-way flight from Denver to San Francisco with seat selection" ...

# Base booking + multiple addons
./aat prompt "Book a flight from DEN to SFO with seat selection and ancillary services" ...
```

The composition system:
1. Loads the base workflow template (e.g., Full-Payload Booking)
2. For each addon, splices its steps into the base plan at the `after:` insertion point
3. Auto-wires `AUTOWIRE` values to matching outputs from the base workflow
4. The LLM then fills in remaining literal values (dates, traveler info, etc.)
