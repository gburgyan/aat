# Launch M6 — Duffel example, built by a recorded discovery run

## 2026-09-12 — Plan: primitives and a release before the discovery run

**What:** M6 publishes an AAT project for the Duffel flights API that a coding agent builds in a clean room from
public sources, running against Duffel's test mode, with no design handed to it. Before the run, AAT ships v0.2.0
with the primitives such a project is likely to need and the fixes already targeted at M6:
- request pacing (`settings.minRequestInterval`)
- `Retry-After` on step retries (F11)
- cleanup chains (P7)
- `AUTOWIRE?` and one wiring pass after addons (P8)
- numbers sent as strings in `min`/`max` selection (P9)
- `aat generate` fixes (F7, F26)

The project lives in its own repository, `aat-duffel`, not under `examples/`.

**Decisions:**
- **The design written before the run grades it; it never instructs it.** A snapshot of the rubric, the discovery
  questions, and the prompt was taken before any of the primitives landed. It is kept locally and untracked as
  `M6-RUBRIC-2026-09-12.md`, SHA-256 `4eb2068e0ea630fd48f00d5f614e747560ba50f9311fc26a2ff34e01c12fad99`.
  Recording the hash here dates it.
- **Sources offered to the agent, through MCP servers it can query as reference.** They are not a design to follow.
  - Duffel's docs site is the API reference. Duffel publishes no OpenAPI spec.
  - The Postman collection in Duffel's hackathon starter kit is left out. It sends `Duffel-Version: beta`, which
    the API now rejects with `400 unsupported_version`, as it does `v1`. It also sets `Accept-Encoding: gzip`.
  - A third-party profile, `api-evangelist/duffel`, has a v2 spec and a Postman collection generated from it.
    Both are offered, pinned at commit `343353d4dce3e3374e77c54ca194cf5311679c69` and fetched 2026-09-12:

    | File in that repository | SHA-256 |
    |---|---|
    | `openapi/_original/duffel-openapi.yml` (OpenAPI 3.0.1, 31 paths, few response schemas) | `e3dda5415a26688af2e335cdc310198f83fb0a6ab120759d24192ac0f6c57db9` |
    | `collections/duffel.postman_collection.json` (18 requests, partial example bodies) | `431c3525617b0891699cebbc495c5c46eb56f9af8202cee184b21643491110ed` |
- **A spec is offered, so the `aat generate` fixes land before the release.** The agent may scaffold from it.
- **The clean room holds nothing to read but what the run creates.**
  - It is a new repository with only a `.gitignore`.
  - Its MCP servers are registered at local scope, so no local paths enter its history.
  - Auto memory is off.
  - Reads outside the directory are blocked with `permissions.blockReadsOutsideWorkingDirectories`. Without it,
    read-only shell commands such as `cat` and `ls` read any path without a prompt.
  - The run uses the tagged release from Homebrew, never a source build.
- **The docs are the agent's only source for AAT, so the new pages teach with the shop's vocabulary.** Nothing in
  them comes from the rubric.

**Open questions:**
- Whether the session transcript is published (author).
- Where the Duffel project's nightly CI lives. Most likely in `aat-duffel`, installing the release.

## 2026-09-12 — A2: request pacing

**What:** `settings.minRequestInterval` spaces the starts of requests. `aat run plan`, `aat run batch`, `aat prompt`,
and the MCP server's `execute_plan` all honor it.

**Decisions:**
- **One interval per command, not per host.** A rate limit usually belongs to a credential, and a per-host gate would
  have to know which overrides share one. Everything a command sends waits for the same pacer: the plans of a parallel
  batch, plan-level retry attempts, step retries, verification, and cleanup. The MCP server keeps one pacer for as long
  as it runs.
- **The engine paces, not the executor router.** `ExecutorRouter.Resolve` returns a concrete executor whose base URL
  callers read, so a decorating executor would have changed that type. `Engine.send` waits and then executes, at both
  places the engine sends a request. Cleanup became an engine method to reach it.
- **The setting is a duration string.** `${var}` substitution rewrites only strings. A bare `250` is rejected rather
  than read as some unit.
- **OAuth2 token requests are not paced.** The auth provider sends them outside the engine, once per run context.
- **Pacing waits count toward step durations,** as retry waits do.

## 2026-09-12 — A3: step retries honor Retry-After

**What:** A step retry waits at least as long as the failed response asks, from its `Retry-After` header or, on a 429
without one, its `RateLimit-Reset` header. A request for more than 60 seconds ends the retries.

**Decisions:**
- **`RateLimit-Reset` counts on a 429.** A rate-limited API may send it without `Retry-After`; Duffel sends one as an
  HTTP date. Both headers are read as seconds or as an HTTP date (RFC 9110).
- **The server's wait is a floor.** The step waits the longer of its backoff and the server's delay, with no jitter
  below the server's value.
- **Past 60 seconds the step fails fast.** Retrying before the server's time only spends attempts, and waiting an hour
  stalls the run. `errorClassification.detail` says how long the server asked for.
- **`Retry-After` does not hold the shared pacer.** That would need a key per host or credential, and it would stall
  unrelated runs: the shop sandbox's 503 is about one shipment.
- **Unchanged:** plan-level `--retries` keeps its fixed two seconds, and cleanup still never retries.

## 2026-09-12 — A4 and A5: cleanup chains

**What:** A cleanup node's own `cleanup:` now runs right after it succeeds, taking its outputs first, so an API that
releases a resource in two calls is cleaned up completely. Graph validation rejects cleanup pairings that loop back.

**Decisions:**
- **Depth-first, right after the parent.** The chain finishes before the next resource is released, so a stack of
  `[deleteCart, requestRefund]` runs `requestRefund`, `confirmRefund`, `deleteCart`.
- **Chained outputs stay in memory, never in the run state.** Cleanup inputs fall back to the newest step with an
  output of the same name. A refund's generic `id` stored in the run state could reach an unrelated cleanup. A chained
  step looks at the cleanup steps before it (nearest first), then the step that registered the chain, then that
  fallback.
- **A cleanup step succeeds only without an error, below 400, and with no error detection rule triggered.** Cleanup
  responses are now checked against the graph's error detection rules; the run outcome still never changes.
- **Unique cleanup step IDs, seeded with the plan's step IDs.** A node's second cleanup step is `deleteCart_2`, the ID
  the web server already derived. Seeding from the plan leaves `runCleanup`'s nine callers unchanged. `cleanupFor`
  links each cleanup step to what it cleans up after; archives and `--json` output only gain a field.
- **Cycles are rejected at load, and guarded at run time.** `graph.Parse` validates, so no command runs a looping
  graph; the engine still stops a chain that revisits a node, for graphs built in code.
- **A listed chained node follows its `runOn`.** Composed plans list direct pairings; a plan that lists a chained node
  gates that link of the chain rather than running it on its own.

## 2026-09-12 — A6: AUTOWIRE after addons, and `AUTOWIRE?`

**What:** A final pass wires the AUTOWIRE markers that the slot and addon passes leave, once every step is in place.
A base or slot step can now take an output that only an addon adds, and a base without slots or addons resolves its
markers. `AUTOWIRE?` marks an optional input: wired when a step produces the output, left unset otherwise.

**Decisions:**
- **The existing passes keep their rule; only the new pass uses "nearest earlier".** The plan called for moving every
  pass to the nearest earlier producer. Slot and addon markers resolve today from the last producer anywhere, and
  projects rely on that: the private airline project has 58 markers in 39 templates. Changing that rule would rewire
  working plans to add a feature that needs only the markers left over.
- **The final pass skips a producer that depends on the consumer,** so it never creates a dependency cycle.
- **An unmatched `AUTOWIRE?` becomes an empty value, not a deleted key.** The engine already treats `{}` on an
  optional input as unset. A deleted key would let a graph default `from:` a step the plan lacks fail validation.
- **Rewiring check.** Every recipe, every base, and every base with each addon was composed before (f978482) and after
  the change, for the shop and the private airline project. Results:
  - None of the private project's 53 recipes changed.
  - 25 of 390 base and addon compositions gained `dependsOn` entries and lost none. Recipes already get those
    entries in post-processing.
  - No existing wiring changed.
  - The shop composed identically.
