# Travelport Booking Example

This walks through the included Travelport booking flow — a real end-to-end API test that searches for flights, creates a reservation workbench, adds an offer and traveler, and commits the booking to produce a PNR.

## Files

| File | Description |
|------|-------------|
| `graph/testdata/valid/travelport_booking.yaml` | API graph: 7 nodes, 11 edges |
| `adapter/testdata/templates/travelport/` | 7 request/response templates |
| `plans/travelport_booking.yaml` | Booking plan: 6 steps with assertions |
| `environments/travelport-pp.yaml` | Pre-production environment config |

## Running

```
go build -o aat ./cmd/aat/

./aat run \
  --plan plans/travelport_booking.yaml \
  --env environments/travelport-pp.yaml \
  --graph graph/testdata/valid/travelport_booking.yaml \
  --templates adapter/testdata/templates/travelport/
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

To book a different route, edit `plans/travelport_booking.yaml`:

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
