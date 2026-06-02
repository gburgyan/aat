# Run Checkpoints & State Export

## 2026-06-02 — Checkpoint a run and export live state for external harnesses

**What:** Added `--stop-after STEP` and `--dump-state FILE` to `aat run plan`, letting AAT
drive a system into a known state and hand off the live session (base URL, auth, accumulated
outputs) to an external/specialized test harness. Motivating case: stop the Travelport
round-trip booking plan after `createWorkbench` and let another script use the live workbench.

**Decisions:**
- **New outcome `OutcomeStopped`** ("stopped") rather than reusing `OutcomePassed` — a partial run
  is honestly distinct from a complete one. Exit code is `0`; archive metadata records it via the
  existing `Outcome.String()` path (no archive changes needed).
- **Skip cleanup on checkpoint.** The whole point is to leave created resources alive, so the
  checkpoint return path discards the cleanup stack instead of executing it.
- **Checkpoint placed at loop-end**, after outputs are stored and per-step assertions run, so the
  named step is fully executed and validated before stopping.
- **Engine surface:** `Engine.WithStopAfter(stepID)` builder + `RunState`-independent
  `BuildStateExport(*RunResult)` / `WriteStateExport` in new `engine/export.go`. Export lives in
  `engine` because it needs `adapter.Request` headers and owns `RunResult`.
- **Auth is UNREDACTED in the dump** (file mode `0600`). Run archives still redact as before;
  the dump deliberately does not, because the external harness must replay calls. In-memory
  `StepResult.Request.Headers` are unredacted (redaction only happens at archive-write time), so
  the export reads them directly. Base URL + headers are taken from the last request-issuing step.
- **Export only — no resume.** Feeding a checkpoint back into AAT to continue a plan is out of
  scope for this change.
- **Single-plan only.** Flags live on `runPlanCmd`, not the shared `runCmd` persistent flags;
  batch checkpointing is not meaningful.
- **Dump failure is non-fatal** — a write error logs a warning but does not change the run's
  exit code.
- **`--dump-state -` means stdout.** With `--json` the export is nested under a `state` key on
  the summary so a wrapping harness gets one coherent JSON object on stdout (obviating the file);
  without `--json` it is printed standalone. The unredacted state is attached to the summary only
  in this stdout mode — file mode keeps secrets out of the `--json` summary.

**Open questions:**
- Resume-from-checkpoint (seed `RunState` from a dump and continue) if a future use case wants
  AAT itself to pick back up rather than an external harness.
