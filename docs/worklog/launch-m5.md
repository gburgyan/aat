# Launch M5 — Release v0.1.0

## 2026-09-11 — Plan: the findings targeted at M5 come first

**What:** M5's task list covers the release itself: CHANGELOG, a snapshot build, release notes, install docs,
demos, and repository metadata. The findings deferred from M2 to M4 also point 14 items at M5. Three read-only
exploration passes confirmed each one and read the code around them for problems the release would otherwise
ship.

**Decisions:**
- **Every finding targeted at M5 lands before the tag.** It happens in three phases:
  - **Contracts:** exit codes, the batch filter, environment selection, JSON and archive keys, and file
    paths built from IDs and names.
  - **Requests and transforms:** placeholder escaping, header precedence, and the Lua sandbox.
  - **The rest:** the remaining fixes, then the release.

  v0.1.0 is the first version people will script against, so anything that changes a contract goes in first.
- **Exit codes follow one rule on every command:**
  - 0: passed.
  - 1: a test or validation ran and found a failure.
  - 2: AAT could not do what was asked. That covers bad flags, an unknown subcommand, a manifest, environment,
    or `--var` it cannot use, and a batch with no plans.
  - 130: aborted.

  CI can then read 1 as "the API or the tests are wrong" and 2 as "the job is wrong".
- **Placeholders are escaped for where they sit, with no raw opt-out.** A JSON string gets JSON escaping; a path
  segment and a query value get URL encoding. `rawBody` stays the way to send a malformed payload.

**Found while exploring (fixed in the phases):**
- `aat run bogus` and `aat run batch` with a filter that matches nothing both exit 0.
- `aat import` ignores a manifest that fails to load. `aat mcp serve` reports such a manifest as "not found" and
  exits without saying why.
- Several file paths are built from IDs and names without checks: archive IDs in the web server, `run_id` and
  plan names in MCP, and `aat import --name`.
- `errorDetection` `equals` with a map or list value panics.
- Archives write `metadata.plan.auth` with PascalCase keys.
- A Lua transform can call `dofile()` with no argument, which reads stdin. Under `aat mcp serve`, stdin is the
  MCP protocol stream.

**Open questions:**
- Edge cases of the cleanup change in f21e825, deferred to M9:
  - A listed paired node with `runOn: success` suppresses its pairings on failure.
  - The cleanup-input fallback can pick another step's output.
  - `--json` cleanup entries repeat node names.
- MCP list tools show only `run-*` directories. Deferred to M7.
- Expression evaluation and type coercion for overlay values. Deferred to M9.

## 2026-09-11 — A1: one exit-code rule on every command

**What:** Every command now follows the M5 exit-code rule. Several fixes came with it:
- Unknown subcommands and stray arguments are errors.
- `aat mcp serve` and `aat import` report manifest load errors.
- The execution flags moved off `run clean` and `run rebuild-summaries`.
- Every `--json` setup error prints a JSON document.

A table test runs the real CLI in child processes to check the codes.

**Decisions:**
- **A plain error exits 2.** `main` maps any error that is not an `exitError` to 2. Bad flags, missing
  arguments, setup errors, and each command's I/O errors therefore exit 2 without every call site choosing a
  code. Exit 1 stays explicit: `validate` findings, and the outcomes of `run`, `batch`, and `prompt`.
- **Command groups reject unknown subcommands.** Cobra shows help and returns nil when a command with no
  `Run` gets an argument, before any argument validation, so `aat run bach` exited 0. `groupRunE` on `run`,
  `plan`, `env`, `mcp`, and `docs` returns an error instead, with Cobra's suggestion. Cobra sets its default
  suggestion distance only on the root command, so `groupRunE` sets it. Commands that take no arguments
  declare `cobra.NoArgs`.
- **`validate` exits 2 when it has nothing to validate or was invoked wrongly, and 1 for findings.**
  - A manifest that exists but fails to load is a finding, shown as a FAILED section.
  - A `--manifest` that does not exist matches `config.ErrManifestNotFound` and exits 2.
  - A `--var` the environment file cannot use matches `config.ErrUnusableVars` and exits 2. The error type
    keeps both existing messages.
- **Execution flags belong to `run plan` and `run batch`.** `run` keeps only the flags that locate the
  project and the archives (`--manifest`, `--output`). `run clean` and `run rebuild-summaries` had inherited
  14 flags they ignored.
- **A `--json` setup error always prints a document.** `run batch` puts the reason in a new top-level `error`
  field. It used to report it as a run entry with no plan name.
- **The exit-code test runs the real CLI.** `TestMain` calls `main()` when `AAT_TEST_RUN_MAIN=1`. Each case
  is a child process with its own flag state and its own `os.Exit`, isolated from the user config and
  `AAT_PROJECT`. Cobra keeps flag values between `Execute` calls, so an in-process table would leak flags from
  one case to the next.

**Open questions:** none.

## 2026-09-11 — A2: batch filter and environment selection

**What:** Two changes to how runs pick their input:
- `aat run batch <filter>` compares whole path segments, and a batch that finds no plans fails (F12, F31).
- One helper chooses the environment for `run plan`, `run batch`, and `prompt`. The manifest's default applies
  only to the manifest's own environment file (F33).

**Decisions:**
- **A filter names a directory or a plan.** `orders` selects `orders.yaml` and every plan under `orders/`.
  `orders/refund` selects one plan, with or without `.yaml`. Names are compared after `filepath.Clean` and
  `ToSlash`, so `orders/`, `./orders`, and Windows separators behave alike. A path relative to the working
  directory, such as `plans/smoke.yaml`, is still read as a plan name. It now fails with the plan directories
  in the message instead of silently selecting nothing.
- **No plans is an error, filtered or not.** In CI, an empty plan directory means a misconfigured job, not a
  pass. The check moved into `discoverBatchPlans`, so `batchCommand` no longer has a zero-plan path that
  passes.
- **The manifest's default belongs to the manifest's environment file.** An `--env-config` file holds a
  different set of environments, so it does not inherit a name it may not define.
- **A single-environment file ignores names it was not given explicitly.** It has no names to choose from. It
  ignores `AAT_ENV_NAME`, an overlay's `environment:`, and the manifest default; only an explicit `--env` is
  an error. `AAT_ENV_NAME` counts as a default here because it is usually set for a whole CI job.

**Open questions:** none.

## 2026-09-11 — A3: JSON keys

**What:** Three JSON keys changed:
- The `--json` batch summary's `batchId` became `batch_id` (F8).
- An archived step's `duration_ms` became `durationMs` (F36).
- A plan's `auth` in archive metadata uses camelCase keys instead of Go field names.

**Decisions:**
- **Each document keeps one convention.** `--json` summaries are snake_case. Archives (`archive.json`,
  `batch.json`) are camelCase, like the YAML they record, so `batch.json`'s `metadata.batchId` stays
  camelCase.
- **Old archives stay readable, without a custom unmarshaler.** Archives persist. `archive.Read` and `.aar`
  import decode through `decodeArchive`, which fills a missing `durationMs` from `duration_ms`. That second
  pass runs only when the old key appears.
  - The first attempt was a `StepRecord.UnmarshalJSON`, and it broke redaction. A custom unmarshaler decodes
    its fields without the caller's `UseNumber` setting, so a large integer input came back rounded.
    `TestRedact_UnmatchedSecretChangesNothing` caught it.
  - `tools/aat-to-junit.py` reads both keys.
  - The PascalCase auth keys need no fallback, because encoding/json matches keys case-insensitively.
- **`state` stays the dump file.** With `--json --dump-state -`, the summary nests the `--dump-state` document
  verbatim, camelCase keys included. A harness parses one format whether the dump comes from a file or from
  stdout.
- **An error document has no batch ID.** `batch_id` is omitted when empty, so a batch that failed before it
  started does not report `"batch_id": ""`.

**Open questions:** none.

## 2026-09-11 — A4: IDs and names used as paths

**What:** AAT now checks every name from outside input before joining it into a file path (F4):
- `aat import --name`
- the web server's run, batch, and trace IDs, for reads, exports, and renames
- the MCP `run_id` and MCP plan names

**Decisions:**
- **One rule for archive directory names.** `archive.CheckDirName` accepts only a single directory name:
  - not empty, `.`, or `..`
  - no path separators or NUL bytes
  - local by `filepath.IsLocal`

  `archive.SavedName` adds the `!` prefix to `run-` and `batch-` names. `aat import --name` and web renames
  share it, as the import name derived from the file already did.
- **A bad ID is reported as not found.** The web server gives an ID that is not a directory name the same
  error as a missing run, batch, or trace, so handlers answer `404` without a new status path. The checks
  (`checkRef`) sit in the service methods that build paths, not in router middleware, so every caller of the
  service is covered.
- **A rename moves only runs and batches.** `renameSource` requires `archive.json` or `batch.json` in the
  directory. Before, the rename route could move any directory in the archive directory, notes and version
  control directories included.
- **Plan names may have subdirectories.** MCP plan tools accept `negative/state-machine` but reject absolute
  paths and `..` (`filepath.IsLocal`). The CLI still accepts absolute plan paths, because a person types them
  on purpose; an MCP client passes whatever a model produced.
- **Duplicates removed.** The import code's private copy of the run/batch prefix pattern and the web server's
  own name validator are gone. The web server's `isNamed` now calls `archive.IsNamed`.

**Open questions:**
- MCP `list_archives` and `list_recent_failures` still list only `run-*` directories. Deferred to M7.

## 2026-09-11 — B1: placeholder escaping and the template env fallback

**What:** Two changes to request templates:
- Every substituted value is escaped for where it lands (P13).
- The dead fallback that let a template read environment `values:` directly is gone (F2).

**Decisions:**
- **Escape by position, read from the template's own text.** `substitutePlaceholders` expands blocks, then
  walks the literal text between placeholders:
  - A path value is URL-encoded as a segment until the first literal `?`, and as a query component after it.
  - In a JSON body, the scanner tracks quote and backslash state. A value inside a string literal is
    JSON-escaped; a value outside one is written as a JSON value.
  - Form bodies are URL-encoded. Headers and other bodies are text.

  Only literal template text moves the scanner. Every inserted value is balanced for its position, so it
  cannot move the scanner.
- **A body's context comes from its Content-Type.** A type containing `json` means JSON,
  `x-www-form-urlencoded` means form, and anything else means text. With no Content-Type, a body that starts
  with `{` or `[` counts as JSON.
- **Strings outside JSON quotes still go in as they are.** A template may build JSON text from a value, and
  quoting the string would break it. Every other value outside quotes is written as JSON: `null`, plain
  digits, arrays, and objects.
- **Iteration values are escaped too.** An iteration block leaves a placeholder for each element value, so the
  same pass escapes them.
- **`JoinURL` keeps percent-encoding.** It joins the escaped paths and sets `RawPath`, so `%2F` stays inside
  its segment on the wire. The archive records the URL through the same function.
- **The template env fallback is removed, not wired up.** `adapter.EnvironmentConfig.Values` was always
  empty in production. Environment values already reach requests through input defaults
  (`default: "{{env.KEY}}"`), which keep them visible to validation, archives, and MCP. A second path would
  bypass all three.

**Open questions:** none.

## 2026-09-11 — B2: header precedence

**What:** Template headers no longer overwrite the credential, an override's own headers, or overlay headers
(F1). Three related fixes:
- Header names compare case-insensitively.
- Overlay headers beat the credential on routed nodes too.
- `aat prompt` keeps dotfile headers when a plan brings its own auth or headers.

**Decisions:**
- **Protected headers apply after the template's.** `adapter.EnvironmentConfig.Protected` holds the headers a
  template may not replace. `BuildRequest` merges config headers, then template headers, then protected
  headers. A template can still set a per-operation `Content-Type` over an environment default.
- **The route carries its own layers.** `config.APIConfig` gained `Protected` and `Overlay`. The credential
  joins `Protected` when the route is built, and `AddOverlayHeaders` adds overlay headers to both. `Headers`
  stays the full merged set, so `--dump-state` and existing callers see the same map.
- **Overrides start from the base route, not a header map.** `BuildOverrideConfigs*` take `*APIConfig`, so a
  routed node reapplies the base route's overlay headers after its own credential. That keeps "overlay beats
  credential" true on every route. Before, an override's credential won over an overlay `Authorization`.
- **Header names compare case-insensitively.** HTTP header names are case-insensitive, and Go's transport
  canonicalizes them. When two map keys differed only in case, map iteration order decided the winner.

**Open questions:** none.

## 2026-09-11 — B3: the Lua sandbox

**What:** Lua transforms lost every function that loads code or reaches the host process (F3):
- the `package` library
- `dofile`, `loadfile`, `load`, `loadstring`, `require`, and `module`
- `getfenv` and `setfenv`
- `collectgarbage`, `newproxy`, and `_printregs`

**Decisions:**
- **Nil the globals after opening the base library.** gopher-lua registers `require` and `module` in the base
  library itself, so leaving out the `package` library would still leave them callable.
- **Why now.** Integration kits mean people run templates that someone else wrote. Also, `dofile()` and
  `loadfile()` with no argument read stdin, which under `aat mcp serve` is the protocol stream.
- **Still not a sandbox.** Scripts run in-process with no memory limit, and the timeout does not interrupt a
  long call into a Go library function. The docs and SECURITY.md say so. JSON nulls and empty objects keep their
  documented conversion limits (M9).

**Open questions:** none.

## 2026-09-11 — C1: errorDetection equals

**What:** An `errorDetection` `equals` rule now compares numbers by value, and a map or list `value` fails
validation (F9).

**Decisions:**
- **Reuse the assertion comparison.** `validate.ValuesEqual`, exported from `valuesEqual`, already compares a
  gjson result with a YAML value. It compares numbers as numbers, strings and booleans strictly, and anything
  else by its text. `equals` rules and `fieldEquals` assertions now behave the same way.
- **Reject values the rule cannot mean.** A map or list `value` used to panic, because `==` on two maps
  panics. Validation now rejects such a value, and if one gets through anyway, the comparison no longer panics.

**Open questions:** none.

## 2026-09-11 — C2: recipe overrides and overlay values

**What:** Two changes to recipe overrides (F14):
- A value override on a wired input replaces the wiring.
- An override for a step that is not in the composed plan is an error.

Override `values:` from an environment, the dotfile, or an overlay now replace the input's resolution record,
with the source `override_value` (F30).

**Decisions:**
- **Only in `Reconstitute`.** `applyTargetedResponse` is shared with the LLM path, where skipping wired inputs
  and unknown steps keeps the model from shadowing auto-wired edges. A recipe is written by a person, so a key
  that does nothing is a typo. `checkOverrideSteps` runs before the overrides apply, and `replaceWiring` clears
  `from`, `select`, `fromSelection`, `fromInput`, and `fromResolved` after. Post-processing then recomputes
  `dependsOn` from the remaining references. The error reaches every caller of `Reconstitute`: run, batch,
  validate, and MCP.
- **Replace the record, don't add one.** The archive shows one resolution per input. The override's record
  takes the place of the one for the value it overwrote, so the decision trail matches the request.
- **Overlay values stay literal.** Evaluating `{{...}}` in overlay values and applying the graph type would
  change what negative-test overlays send today. Both move to M9.

**Open questions:** none.

## 2026-09-11 — C3: frontend build output

**What:** Vite builds into `server/web/dist/app`, which git ignores. `server/web/dist/index.html` is no longer
tracked, and a tracked `server/web/dist/placeholder.txt` keeps the embed compiling (F21).

**Decisions:**
- **A subdirectory, not a tracked build file.** Every frontend build rewrote the tracked `index.html`, which
  holds hashed asset names, so `make build` left the tree dirty and a stray commit could ship a stale entry
  point. `//go:embed web/dist` needs at least one file, and Vite's `emptyOutDir` deletes every file in its
  output directory. Building into `dist/app` leaves the placeholder alone.
- **A `.txt` placeholder.** go:embed skips files whose names start with `.` or `_`, so a `.gitkeep` would not
  satisfy the pattern.
- **Anchor `/dist/`.** The unanchored goreleaser `dist/` rule also ignored `server/web/dist`, so the old
  placeholder had to be force-added. The CI artifact paths now name `dist/app` too.

**Open questions:** none.

## 2026-09-12 — C4: release prep

**What:**
- The CHANGELOG has a `[0.1.0]` section and history back to 0.0.1.
- The release workflow publishes the tag's CHANGELOG section as the release notes.
- `go.mod` retracts v0.0.1–v0.0.4.
- The pre-release notes are gone from the README, the install, home, and CI/CD pages, the Petstore README,
  and the roadmap.
- `plans.md` says what happens when a plan lists one paired cleanup node more than once.

**Decisions:**
- **0.1.0 is measured against v0.0.4.** *Unreleased* started at M0, so nineteen feature commits from March to
  June had no entries. Among them:
  - multi-environment files
  - overlay auth, headers, values, and `expectFailure`
  - mutations
  - checkpoints
  - Ctrl+C handling
  - MCP over HTTP
  - the renames of `--env` to `--env-config` and `--env-overlay` to `--overlay`

  They are now listed, and the renames are marked BREAKING, since v0.0.4 had `--env FILE` and `--env-overlay`.
  A Fixed entry for a bug introduced and fixed after v0.0.4 either became part of its feature's Added entry
  (such as `--env-config` ignoring `defaultEnvironment`, or overlay values in the resolution record) or was
  dropped (such as the shop kit descriptions, the inverted child-environment override order, or the
  `--verbose-auth` token). `git grep` at v0.0.4 decided which features existed then.
- **Release notes come from CHANGELOG.md.** goreleaser's generated changelog would publish every commit
  subject; its exclude filters match prefixes this repository never uses. The workflow extracts the section
  with awk and fails when the tag has none. The goreleaser footer still adds the install lines.
- **Retract rather than re-tag.** The v0.0.x tags point at rewritten commits, and proxy.golang.org holds the
  originals. A retraction in v0.1.0's `go.mod` hides them from `@latest` and `go list -m -versions`.
- **Backfill from the tags.** 0.0.1 is summarized by area from its 150 commits. 0.0.2–0.0.4 come from their
  ranges and the local notes file for v0.0.2..v0.0.4, which is deleted. The 0.0.1 link points at the tree,
  since no GitHub releases exist.

**Open questions:** the `[0.1.0]` date is 2026-09-12; change it if the tag lands on another day.

## 2026-09-12 — C5: demo assets at 0.1.0

**What:** `make demos VERSION=0.1.0`, run from a clean tree after C4, regenerated the two GIFs and three
screenshots in `docs/user/assets`:
- The run and batch pages show `0.1.0` as the tool version.
- The run timeline no longer shows `0 / 0` on steps without assertions.
- The recorded runs passed: `full-lifecycle` with 15 of 15 steps, and the layer-group batch with 27 of 63 runs
  and 36 skipped as duplicates.

**Decisions:** none. `demos/out/social-preview.png` is not committed; the author uploads it as the repository's
social preview.

**Open questions:** none.
