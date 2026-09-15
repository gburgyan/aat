# Checkpoints

A checkpoint lets AAT drive a system into a particular state and then hand off to another tool. AAT runs a plan only as far as one step, leaves everything it created alive, and writes the session to a JSON file: base URLs, request headers, and every step's inputs and outputs, with credentials redacted unless you ask for them. A pytest suite, a load test, or a manual `curl` session picks up from there.

These `aat run plan` flags do this:

| Flag | Description |
|------|-------------|
| `--stop-after STEP` | Stop after the step whose ID is `STEP` passes. Later steps, verification steps, and all cleanup are skipped, so resources created up to that point stay alive |
| `--dump-state FILE` | Write the run's state to `FILE` (mode `0600`), with credentials redacted. `-` writes it to stdout instead. Works with or without `--stop-after` |
| `--dump-state-secrets` | Keep live credentials in the dump, for a tool that sends requests as the run's session. Needs `--dump-state`; see [Security](#security) |

None of them exists on `aat run batch`.

## Stopping After a Step

```
aat run plan smoke --stop-after paymentCharge --dump-state state.json
```

On the [shop example](examples/shop.md), with `aat-sandbox serve` running:

```
aat: loading environment...
aat: loaded environment "us"
aat: loaded graph (18 nodes)
aat: loaded domain knowledge
aat: loaded 18 templates
aat: loaded 1 OAS spec(s) for runtime validation
aat: reconstituting recipe "Quick Purchase"...
aat: authenticated via oauth2
aat: override: payment*
aat: executing plan (5 steps)...

  [1/5] listProducts         200  0ms
  [2/5] createCart           201  0ms
  [3/5] addItem              201  0ms
  [4/5] checkout             201  0ms
        Order: ord_0001
        Receipt: RCPT-US-0001
        Tax: Sales tax 8.25%
        Total: $103.40
  [5/5] paymentCharge        201  0ms
        Charged: $103.40

STOPPED at "paymentCharge" (5/5 steps, 1ms)
Archive: /path/to/shop/_output/runs/run-20260910-230852-8b2139bc/archive.json
aat: state dumped to state.json
```

How the stop works:

- `STEP` is a step ID: the step's `id:` if it has one, otherwise its node name. The progress lines show step IDs, with the node in parentheses when the two differ and the terminal is wide enough: in the shop's `smoke` recipe, the `checkoutCart` node runs as step `checkout`. The `--json` summary lists them too: `aat run plan smoke --json | jq -r '.steps[].name'`.
- The stop happens only when the step passes, including an `expectFailure` step whose expected error came back. If the run fails or errors before or at that step, it ends the normal way and cleanup runs.
- Only main steps can be stop points. Verification steps run after every main step, so a checkpoint always skips them.
- The outcome is `stopped` and the exit code is `0`. `--quiet` prints `STOPPED (5/5 steps)`, and the `--json` summary carries `"outcome": "stopped"` and `"stopped_at": "paymentCharge"`, with no `cleanup` array.
- The run archive is still written, with outcome `stopped` and no cleanup records. See [Archives](archives.md).
- A stopped run counts as a success, so `--retries` does not retry it.
- An unknown step ID fails the run before any step runs, with outcome `error` and exit code `2`. A node name gets a pointer to the step IDs that run it:

    ```
    ERROR: --stop-after: no step "checkoutCart" in plan (node checkoutCart is step "checkout")
    ```

AAT does not clean up after a checkpoint, then or later. Whatever takes over owns the resources, as the [pytest example](#hand-off-to-pytest) below shows.

## The State Dump

The dump written by `--stop-after paymentCharge --dump-state state.json` above, trimmed to two of its five steps and five of its 22 `values` entries:

```json
{
  "version": "1",
  "redacted": true,
  "outcome": "stopped",
  "stoppedAt": "paymentCharge",
  "baseUrl": "http://localhost:8765/us/v1",
  "auth": {
    "headers": {
      "Accept": "application/json",
      "Authorization": "[REDACTED]",
      "Content-Type": "application/json"
    }
  },
  "steps": [
    {
      "stepId": "checkout",
      "node": "checkoutCart",
      "baseUrl": "http://localhost:8765/us/v1",
      "headers": {
        "Accept": "application/json",
        "Authorization": "[REDACTED]",
        "Content-Type": "application/json"
      },
      "outputs": {
        "orderId": "ord_0001",
        "status": "created",
        "total": 10340
      },
      "inputs": {
        "cartId": "cart_0001",
        "postalCode": "78701",
        "shippingTier": "standard"
      }
    },
    {
      "stepId": "paymentCharge",
      "node": "paymentCharge",
      "baseUrl": "http://localhost:8766/us/v1",
      "headers": {
        "Accept": "application/json",
        "Content-Type": "application/json",
        "X-API-Key": "[REDACTED]"
      },
      "outputs": {
        "amountDisplay": "$103.40",
        "orderStatus": "paid",
        "paymentId": "pay_0001",
        "status": "captured"
      },
      "inputs": {
        "amount": 10340,
        "cardNumber": "4242424242424242",
        "currency": "USD",
        "method": "card",
        "orderId": "ord_0001"
      }
    }
  ],
  "values": {
    "checkout.orderId": "ord_0001",
    "checkout.total": 10340,
    "createCart.cartId": "cart_0001",
    "paymentCharge.paymentId": "pay_0001",
    "paymentCharge.status": "captured"
  }
}
```

| Field | Contents |
|-------|----------|
| `version` | Format version, currently `"1"` |
| `redacted` | `true` when credentials were redacted, the default; `false` with `--dump-state-secrets` |
| `outcome` | The run outcome: `stopped` at a checkpoint, otherwise `passed`, `failed`, `error`, or `aborted` |
| `stoppedAt` | The step ID the run stopped after; absent when it did not stop |
| `baseUrl`, `auth.headers` | The environment's default route: the base URL and every request header of the last request sent to the environment's `apiBaseUrl`. When no request went there, they come from the last request sent anywhere |
| `steps` | One entry per step that ran without an execution error, in order: `stepId`, `node`, and, for steps that sent a request, the `baseUrl` and `headers` that request actually used. `outputs` holds the extracted outputs, `inputs` the resolved inputs |
| `values` | Every step's outputs flattened into `stepId.outputName` keys |

The shop routes `payment*` operations to a second host with its own API key (see [Environments: Multi-Host Routing](environments.md#multi-host-routing)). That is why the top-level `auth.headers` still hold the shop's `Authorization` header even though the last step went to the payments host, and why the `paymentCharge` entry carries `X-API-Key` and its own `baseUrl`. A harness that calls both hosts reads the per-step entries.

Without `--stop-after`, `--dump-state` writes the state after the run finishes, cleanup included, so the resources named in `values` may already be gone. It is still a convenient way to capture outputs.

## Dumping to Stdout

Pass `-` as the path to write the state to stdout instead of a file.

Without `--json`, stdout carries only the state object. Progress, the summary line, and the archive path go to stderr, so the dump pipes cleanly with or without `--quiet`:

```
aat run plan smoke --stop-after checkout --dump-state - --quiet | jq -r '.values["checkout.orderId"]'
```

With `--json`, stdout carries the usual JSON summary (see [CI/CD Integration](ci-cd.md)) with the dump nested under `state`, camelCase keys and all, so a wrapping harness reads one object in the dump file's format. Trimmed:

```
aat run plan smoke --stop-after paymentCharge --json --dump-state -
```

```json
{
  "outcome": "stopped",
  "steps": [ ... ],
  "summary": {
    "total_steps": 5,
    "passed_steps": 5,
    "failed_steps": 0,
    "duration_ms": 1
  },
  "archive_path": "/path/to/shop/_output/runs/run-20260910-230901-122dcc53/archive.json",
  "stopped_at": "paymentCharge",
  "state": {
    "version": "1",
    "redacted": true,
    "outcome": "stopped",
    "stoppedAt": "paymentCharge",
    "baseUrl": "http://localhost:8765/us/v1",
    "auth": { "headers": { ... } },
    "steps": [ ... ],
    "values": { ... }
  }
}
```

## Security

By default, a dump is redacted the way a [run archive](archives.md#what-is-redacted-and-what-is-not) is, and says so with `"redacted": true`:

- The values of credential headers (`Authorization`, `Proxy-Authorization`, `X-API-Key`, `X-Auth-Token`, `Cookie`, and `Set-Cookie`) read `[REDACTED]`, at the top level and in every step.
- Every secret AAT knows about is replaced wherever it appears, inputs and outputs included: the resolved credentials of the environment, its host overrides, the plan, and overlays, and the access token the run authenticated with.
- IDs, base URLs, and other data stay, so a harness can still find what the run created. Data that is not a credential, such as the `cardNumber` input in the sample above, is kept, as it is in archives.

A tool that sends requests as the run's session, like the [pytest example](#hand-off-to-pytest) below, needs the live credentials. `--dump-state-secrets` keeps them:

```
aat run plan smoke --stop-after checkout --dump-state state.json --dump-state-secrets
```

Such a dump says `"redacted": false` and holds live credentials:

- Treat it like a password file: do not commit it, attach it to issues, or leave it in a shared location. The shop example's `.gitignore` already lists `state.json`.
- With `--dump-state -`, the credentials land wherever stdout goes: your terminal, a pipe, a CI log, or an AI assistant's transcript. AAT warns on stderr:

    ```
    aat: warning: --dump-state-secrets writes live credentials to stdout
    ```

- `--dump-state-secrets` without `--dump-state` is an error, with exit code `2`.

Either way:

- A dump file is written with mode `0600`. AAT writes a temporary file in the same directory and renames it into place, so a file that already existed also ends up `0600`.
- If the file cannot be written, AAT prints a warning to stderr even under `--quiet` or `--json`, and the exit code does not change:

    ```
    aat: warning: failed to write state dump: creating state export dir: mkdir /no-such-root: read-only file system
    ```

## Hand Off to pytest

This example runs the shop's `smoke` recipe up to checkout, then lets a pytest module check the order AAT created and delete what AAT left behind. It uses only the Python standard library besides pytest, and assumes `aat-sandbox serve` runs on its default ports (8765 and 8766). With the sandbox on other ports, add `--var apiHost=localhost:PORT --var payHost=localhost:PORT` to the `aat` command; the test reads the base URL from the dump and needs no change.

The test sends requests as AAT's session, so in the shop project directory, dump the state with live credentials:

```
$ aat run plan smoke --stop-after checkout --dump-state state.json --dump-state-secrets --quiet
STOPPED (4/4 steps)
Archive: /path/to/shop/_output/runs/run-20260910-230909-eb7845f6/archive.json
```

The order now exists in state `created`, unpaid, and its cart still exists. Save this as `test_handoff.py` next to `state.json`:

```python
import json
import urllib.request

import pytest

with open("state.json") as f:
    STATE = json.load(f)


def call(method, path):
    """Send a request with the session AAT left behind; return (status, parsed body)."""
    req = urllib.request.Request(
        STATE["baseUrl"] + path, method=method, headers=STATE["auth"]["headers"]
    )
    with urllib.request.urlopen(req) as resp:
        body = resp.read()
        return resp.status, json.loads(body) if body else None


@pytest.fixture
def order_id():
    order_id = STATE["values"]["checkout.orderId"]
    yield order_id
    # --stop-after skipped AAT's cleanup, so the harness deletes what the plan created.
    assert call("DELETE", "/orders/" + order_id)[0] == 204
    assert call("DELETE", "/carts/" + STATE["values"]["createCart.cartId"])[0] == 204


def test_checkout_leaves_an_unpaid_order(order_id):
    status, order = call("GET", "/orders/" + order_id)
    assert status == 200
    assert order["status"] == "created"
    assert order["paymentStatus"] == "unpaid"
```

```
$ pytest -q test_handoff.py
.                                                                        [100%]
1 passed in 0.03s
```

The fixture's teardown runs whether the test passes or fails, so the order and the cart are deleted either way; afterwards both return `404`. The `DELETE /orders/{orderId}` and `DELETE /carts/{cartId}` calls are the same operations the plan's cleanup would have run (`deleteOrder` and `deleteCart` in the shop graph).

---

*Source: `cmd/aat/run_plan_cmd.go`, `cmd/aat/run_shared.go`, `engine/engine.go`, `engine/export.go`, `examples/shop/`.*
