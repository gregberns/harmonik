# Research — C4 Verification harness (golden corpus + CI gate + live-run evidence)

## What already exists
- `scripts/scratch-daemon-smoke.sh` runs phases A/A2/B/C/D hermetically by default,
  exits 0/1, prints `RESULT: N passed, M failed`. It shells out to the real
  `scratch-daemon.sh batch --from-events` and `feedback` (with
  `SCRATCH_FEEDBACK_FLEET_ROOT` -> throwaway git+beads repo), so it tests the REAL
  code paths, not reimplementations.
- Gated phases E-F execute a live daemon lifecycle + the remote-substrate localhost
  e2e ONLY with `--full`/`SMOKE_SCENARIO_RUN=1`; by default they merely assert the
  scenario test compiles (`go test -tags=scenario -run '^$'`).

## Key question: where is the rot risk?
The `oneline` jq normalizer (in `cmd_batch`) is the correctness core of dedup. Its
redaction set must satisfy two invariants simultaneously:
- **Stability**: same logical failure, different volatile tokens -> identical
  signature (feedback UPDATES, not duplicates).
- **Distinctness**: two different logical failures -> different signatures (two real
  bugs become two beads, never one).
A new event format that introduces an un-redacted volatile token silently breaks
Stability (false-split -> duplicate beads); an over-broad new redaction breaks
Distinctness (false-merge -> two bugs collapse to one). Both are SILENT. Today the
only guard is inline synthetic strings in phase A/A2.

## Findings / decisions
1. **Golden corpus format.** A `testdata/scratch-signatures/` set of small NDJSON
   event streams + an expected-signature file. The smoke folds each via
   `batch --from-events` and diffs the resulting `fail_signature` against the golden.
   Adding a redaction class = add one pair; a regression = a one-line diff. Keep it
   in the SAME hermetic phase so CI runs it for free.
2. **CI trigger.** Path-scoped: run the hermetic smoke whenever
   `scripts/scratch-daemon*.sh` or the corpus changes. No committed CI workflow for
   this exists in the tree yet, so the deliverable is EITHER a lightweight CI job OR,
   if the project prefers local gates, a documented pre-merge invocation tracked as a
   build-practices rule. Recommend the CI job: the smoke is already exit-code clean
   and secret-free.
3. **Live-run evidence (D3).** The gated `--full` path is the right home; it just
   needs to be RUN ONCE against a scratch clone with a real claude binary, its log
   captured, and the runbook updated with "last verified live: <date>, <commit>,
   BATCH_SUMMARY line". No new code -- a verification task with a recorded artifact.
4. **No Go port.** Rejected: the shipped path is bash+jq; testing a Go
   reimplementation would test the wrong thing (drift risk).

## Constraints carried forward
- Smoke stays hermetic-by-default (no secrets, no network, no claude) so CI runs it
  unconditionally.
- Golden streams use REALISTIC volatile tokens (real-looking timestamps, UUIDs,
  worktree-agent ids, git SHAs, `/var/folders/...` tmp roots) so each redaction rule
  is actually exercised.
