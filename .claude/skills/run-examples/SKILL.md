---
name: run-examples
description: Build aat and aat-sandbox, then run the shop, gRPC payments, and petstore examples against the local offline sandbox, the way CI does. Use to see a change working end to end, reproduce a CI example job, or exercise layers, environments, --stop-after/--dump-state, and the web viewer.
---

# Running the AAT examples

Build both binaries and run the offline shop example (`examples/shop`, served by `aat-sandbox`):

```bash
# Build (injects version/commit/date automatically): aat and aat-sandbox
make build
# Or without rebuilding the web UI (`aat web` then needs a frontend build):
# make cli sandbox

# Shop example against the local sandbox (no network, no credentials)
cd examples/shop/
../../aat-sandbox serve --latency 0 &   # shop API :8765, payments :8766
../../aat validate --strict
../../aat run plan full-lifecycle
../../aat run batch --oas-validate strict
../../aat run batch --layer-group shipping-standard,shipping-express --layer-group basket-gear,basket-apparel --parallel 4
../../aat run plan smoke --env eu
../../aat run plan smoke --stop-after paymentCharge --dump-state -   # state for another tool, credentials redacted
../../aat run plan smoke --var apiHost=localhost:9765                # a sandbox on other ports
../../aat web view latest
../../aat validate --strict --manifest aat-kit.yaml                  # the integration kit on its own
sh package-kit.sh                                                     # package it: _output/shop-kit/ and .tar.gz

# What CI runs against the shop (starts its own sandbox; needs curl, jq, free ports 8765/8766)
make example-shop

# The gRPC example: a cart over HTTP, then charge and refund over gRPC, against
# the same sandbox (shop :8765, payments :8766, gRPC payments :8767)
cd examples/grpc-payments/
../../aat validate --strict                # checks gRPC nodes against payments.protoset, offline
../../aat run plan charge-and-refund       # three HTTP steps, then two gRPC ones
../../aat run plan declined-card           # expectFailure naming a gRPC status
make example-grpc                          # what CI runs

# Petstore example (public API, no credentials), from the repository root
cd examples/petstore/
../../aat run plan plans/create-and-verify.yaml

# Explicit paths instead of manifest auto-discovery:
./aat run plan examples/petstore/plans/create-and-verify.yaml \
  --env-config examples/petstore/env.yaml \
  --graph examples/petstore/graph.yaml \
  --templates examples/petstore/templates/
```

Every flag is documented in `aat run plan --help`, `aat run batch --help`, and `aat prompt --help`.
