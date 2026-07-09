# Change Spec — C4 Verification harness

## S-C4.1  Golden signature-corpus regression (hermetic)
- Add `scripts/testdata/scratch-signatures/`: N cases, each a small `.ndjson` event
  stream (`run_started` + terminal `run_completed`/`run_failed`) plus an expected
  `.sig` (the ≤200-char `fail_signature`).
- Extend `scripts/scratch-daemon-smoke.sh` with a hermetic phase that, for each case,
  runs `scratch-daemon.sh batch --from-events <case.ndjson>` (offline fold, no daemon)
  and asserts the resulting `fail_signature` == the golden `.sig` byte-for-byte.
- Corpus MUST include, at minimum, one case per redaction class in `oneline`
  (ISO-8601 + epoch timestamps, UUID, 0x-address, goroutine N, worktree-agent-<id>,
  bare run-/wt-id, pid/port, duration literal, git SHA, tmpdir root) AND at least one
  false-merge guard pair (two DISTINCT failures differing only in the identity-bearing
  path tail -> two DISTINCT signatures).
- **Invariant asserted:** stability (volatile-token variants collapse) AND distinctness
  (different logical failures stay separate). Exit non-zero on any mismatch.
- **Done:** phase runs by default, exits 0 on match; a deliberately corrupted golden
  makes the smoke exit 1.

## S-C4.2  CI regression gate
- Preferred: a path-scoped CI job that runs `scripts/scratch-daemon-smoke.sh` (default
  hermetic phases) on any change to `scripts/scratch-daemon*.sh` or the corpus.
- Fallback (no CI runner): a build-practices rule + a `make scratch-daemon-smoke`
  invocation documented as required-before-merge for this script.
- **Done:** a change to the script that breaks a golden signature is caught before
  merge (job red, or the documented gate fails locally).

## S-C4.3  Recorded live end-to-end run (evidence, no new code)
- Run the gated full path (`scratch-daemon-smoke.sh --full` or `SMOKE_SCENARIO_RUN=1`)
  once against a scratch clone with a real claude binary; capture the log and the
  `BATCH_SUMMARY` line.
- Update `docs/scratch-daemon-runbook.md` with a "Last verified live" line: date,
  commit, and the summary line.
- **Done:** runbook cites a real green live run; the log artifact is retained.
