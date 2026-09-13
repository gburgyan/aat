# Launch M6.1 — AAT improvements from an agent discovery run

A coding agent built an AAT project for a public API in one headless attempt. It learned AAT from the docs site, and
the API from its reference and live test mode. Its transcript and the resulting project were then reviewed for what
AAT made hard. M6.1 fixes those findings in three phases:
1. safety and reading results (P17–P19)
2. tests that pass for the wrong reason (P20–P22)
3. expressiveness (P23–P27)

## 2026-09-12 — P17: `--dump-state` redacts credentials by default

**What:** `aat run plan --dump-state` redacts its export the way run archives are redacted.
- **Redaction:** credential header values are masked at the top level and in every step, then every known secret is
  replaced wherever it appears. The export gains `redacted`.
- **The opt-out:** `--dump-state-secrets` keeps live credentials. It warns on stderr when the dump goes to stdout, and
  it is an error without `--dump-state`.
- **Tokens:** the access token a run authenticates with joins the run's known secrets, which archives use as well.

**Decisions:**
- **Redact by default, reversing the stage-3a decision** (`stage-3a-checkpoint-export.md`). Stage 3a left the dump
  unredacted because a harness must replay calls.
  - In the discovery run, the agent used `--dump-state -` to read step outputs, and its first call printed the API
    token into its transcript.
  - A dump is now safe to print, and the replay case opts in.
- **Always a way back (author).** The author accepted the break before 1.0 on one condition: raw credentials stay
  available, through `--dump-state-secrets`.
- **Same rules as archives.** `engine.RedactStateExport` reuses `archive.RedactHeaders` and `archive.Redact`, so a
  run's dump and its archive agree on what a secret is.
  - The outcome, step IDs, and node names carry `redact:"-"`, like the archive's identifiers. Map keys are never
    changed.
  - Base URLs stay redactable.
- **The issued token is a known secret.** `config.RunSecrets` collects configured credentials. An OAuth2 access
  token exists only at run time, and it was masked only inside credential headers.
  - `aat run plan` and `aat run batch` add it, where the token is already in scope. An end-to-end test has the
    sandbox echo the token in a response header that no redaction rule names.
  - `aat prompt` and the MCP server's `execute_plan` don't add it yet. `aat prompt` authenticates in several
    branches, and `execute_plan` has no execution test to prove the change.
- **One warning per invocation.** It prints from `executeRun`, not once per plan-level retry attempt.
- **Consumers moved to the flag:**
  - the CI example-shop script, which now also checks that a default dump is redacted
  - the end-to-end checkpoint test
  - the pytest handoff in `checkpoints.md`
  - the shop README's `curl` recipe, the kit README, and the integration-kit page

**Verification:**
- `make check`, `make docs`, and `make example-shop` pass. The example-shop script checks that a default dump is
  redacted, then replays the live token from a `--dump-state-secrets` dump.
- **Against a live API with a real test token:** a scratch clone of the discovery run's project, running a plan that
  only reads.
  - The default dump held the token 0 times, with `"redacted": true`.
  - The `--dump-state-secrets` dump held it 4 times.
  - The run's archives held it 0 times.

**Open questions:** none.

## 2026-09-12 — P18: `aat run show`

**What:** `aat run show <run>` prints what a run archive recorded:
- the step list
- one step
- one part of a step (the request or response body, inputs, or outputs), narrowed with `--path` or summarized with
  `--shape`

The MCP server's `get_sample_response` gains `path` and `shape`.

**Decisions:**
- **A read-only command, not another dump.** In the discovery run, the agent read step outputs through `--dump-state`
  17 times, and sent a subagent through a multi-megabyte archive with `jq`. Reading what an API returned is the core
  of the authoring loop, so it gets a command that needs no browser and changes nothing.
- **Run lookup moved into `archive`.** `archive.ListRuns` and `archive.FindRun` replace MCP's private lookup.
  - **Unfinished batches.** Any directory without `archive.json` is read as a batch. The runs of a batch that is still
    going, or that stopped before writing `batch.json`, resolve, and `latest` can name one.
  - **Ordering.** Runs order by the timestamp in `summary.json`, which is read and never written. The server's listing
    writes a missing summary, so a read-only command could not reuse it.
  - **References.** A reference is `latest`, a run ID, or a batch ID and run ID. Each part is checked with
    `CheckDirName`, so MCP's lookups stay inside the archive directory. File paths are the CLI's concern.
  - **The server's `LatestRef` is unchanged:** a newer batch still wins there, because the web view opens batches.
- **Step helpers moved into `archive`:** `StepID`, `CleanupStepIDs`, `FindStep`, and `StepPassed`. The server's copies
  are gone, so `--step` IDs match the web UI's step URLs.
- **The shape merges every element of an array, not only the first**, which was the plan.
  - The merge keeps the union of keys, type unions such as `string|null`, and counts of the objects that hold each
    key.
  - A first-element shape hides fields that are sometimes missing or null, and those fields drove most of the
    transform boilerplate in the discovery project.
  - Paths are gjson paths with `#`, so a line goes straight into an extract rule or `--path`.
- **Bodies stay raw.** Shapes and paths read the archived `json.RawMessage` with gjson, and nothing decodes a body
  into maps.
- **Printed parts are capped at 64 KB by default,** with a note on stderr, so an agent does not pour a large body into
  its context by accident. `--max-bytes 0` lifts the cap. The step list and the step overview stay small, so they are
  not capped.
- **Pass or fail follows the run summary:** `StepPassed`, plus a status of 400 or more without `expectFailure`.

**Verification:**
- **Tests:**
  - lookup: batches, saved and unfinished batches, traversal references, and no summary written
  - the shape
  - each command form and its exit codes
  - the MCP parameters
  - a shop end-to-end subtest that reads a real run's response shape and outputs
- **On the discovery run's own archives, read in place:**
  - The step list of a 318 MB archive took 0.36 s, with 661 MB peak memory.
  - The shape of a 38 MB response took 0.34 s and 288 MB, for 292 lines.
  - Nothing under that project was written.

**Open questions:**
- Archives are indented JSON, so a run with large responses writes hundreds of megabytes. Not scheduled.
