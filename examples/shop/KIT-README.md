# Shop API integration kit

This kit describes the shop API for integrators and their AI coding tools: 18 operations with their
request templates, the OpenAPI contract, the order calls run in and the data each one needs from the
calls before it, the rules and error codes behind them, integration flows, and three reference plans that
run against the API. `package-kit.sh` builds it from the shop's AAT test project. Through the MCP server
below, an AI coding tool reads all of it, including this README, and can write a working client in your
language from a single prompt.

## What you need

- `aat` and `aat-sandbox`: see [Install](https://gburgyan.github.io/aat/install/). The release archives and
  the Homebrew cask ship both.
- The API. This kit targets the offline sandbox that `aat-sandbox serve` starts.

## Connecting

| | Shop API | Payments API |
|---|---|---|
| Base URL | `http://localhost:8765/{region}/v1` | `http://localhost:8766/{region}/v1` |
| Credential | `Authorization: Bearer <token>` | `X-API-Key: pay-demo-key` |
| Operations | everything except payments | `paymentCharge`, `paymentRefund` |

`region` is `us` (USD, sales tax added at checkout) or `eu` (EUR, VAT included). Get the bearer token
from `POST http://localhost:8765/oauth/token` with the form-encoded body
`grant_type=password&username=demo&password=demo&client_id=aat-shop&client_secret=aat-shop-secret`; the
response carries `access_token` and `expires_in` (3600 seconds). Never send the bearer token to the
payments API.

## Flows

**Buy**, the core of every integration:

```
listProducts → createCart → addItem (once per product) → [applyCoupon] → checkoutCart → paymentCharge
```

- `addItem` takes the `cartId` from `createCart`, a `sku` whose `inStock` is true in `listProducts`, and a
  `quantity` of at least 1.
- `checkoutCart` needs `shippingTier` and `postalCode`. It returns the `orderId`, `total`, and `currency`
  that `paymentCharge` must send, exactly, along with a `method` and that method's field: `cardNumber`,
  `giftCardCode`, or `paypalEmail`.

**Fulfil and return:**

```
paymentCharge → shipOrder → getShipment (retry while 503) → deliverShipment → createReturn → paymentRefund
```

**Cancel:**

```
checkoutCart → [paymentCharge] → cancelOrder → paymentRefund (only if it was paid)
```

**Clean up:** `deleteOrder` and `deleteCart` delete in any state.

## Rules that matter

- Money is an integer in minor units (cents). An order's amounts have `*Display` twins for people; order
  lines carry only `unitPrice` and `lineTotal`.
- Errors under `/{region}/v1` are `{"error": {"code": "...", "message": "..."}}`. Branch on `code`:
  `INVALID_TRANSITION` for an out-of-order call, `OUT_OF_STOCK`, `AMOUNT_MISMATCH`, `CARD_DECLINED`, and so
  on. A wrong base URL gets a plain-text 404 instead.
- Orders move created → paid → shipped → delivered → returned; cancel works from created or paid.
  Cancelling or returning never refunds; `paymentRefund` does, in any state while money is captured.
- `paymentCharge` and `shipOrder` check the order again after their delay. If a charge's response is lost,
  read the order before charging again: a second charge answers 409 `INVALID_TRANSITION`.
- Retry exactly two things: `checkInventory` answering 200 with `status: ERROR` (`STALE_READ`), and
  `getShipment` answering 503 `TRACKING_UNAVAILABLE` (wait for its `Retry-After` seconds).
- Apply a coupon before checkout; totals are fixed when the order is created.

## Give your AI coding tool the API

Unpack the kit into your repository, for example as `vendor/shop-kit/`, and register the MCP server in your
`.mcp.json`:

```json
{
  "mcpServers": {
    "shop-api": {
      "command": "aat",
      "args": ["mcp", "serve", "--manifest", "vendor/shop-kit/aat-project.yaml", "--persona", "api"]
    }
  }
}
```

The `api` persona is read-only. It serves the operations and their exact requests, integration flows step
by step, the data flow between calls, the domain's rules, OpenAPI schemas, and sample responses.

## See the real exchanges

From the kit directory, with the sandbox running:

```bash
aat run plan full-lifecycle   # one order through every state, then cleanup
aat web view latest           # every request and response, with Copy as cURL
```

After a run, `get_sample_response` returns the responses it recorded. To continue from live state in your
own client, `aat run plan smoke --stop-after paymentCharge --dump-state state.json --dump-state-secrets`
leaves a paid order in place and writes its IDs and the live bearer token to `state.json`.

| Plan | Flow |
|------|------|
| `smoke` | Browse, open a cart, add an in-stock item, check out, and pay by card |
| `full-lifecycle` | One order from browsing through payment, shipment, delivery, return, and refund |
| `registered-paypal-coupon` | A registered customer applies a coupon, pays with PayPal, and returns the order |

## Not included

The shop's negative tests, resilience tests, test-data layers, and any internal environments stay in the
producer's project.
