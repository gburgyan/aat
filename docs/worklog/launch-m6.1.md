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
