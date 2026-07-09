# Tasks — test-daemon-harness (dispatchable bead breakdown)

> Context for the captain: the CORE harness is already landed and closed
> (init/build/up/down/cycle/batch/feedback + hermetic smoke). These five tasks
> close ONLY the remaining verification-durability gap. All are small, independent,
> and fleet-safe. Labels: `codename:test-daemon-harness`. None should be pre-assigned
> or pre-set in_progress (daemon owns claiming).

## T1 — Golden signature-corpus regression (spec S-C4.1 / N1)  [P2, bug-guard]
Add `scripts/testdata/scratch-signatures/`: one `.ndjson` event stream + expected
`.sig` per `oneline` redaction class (timestamps, UUID, 0x-addr, goroutine, worktree-
agent id, run/wt id, pid/port, duration, git SHA, tmpdir root) PLUS a false-merge
guard pair. Extend `scripts/scratch-daemon-smoke.sh` with a hermetic phase that folds
each via `batch --from-events` and byte-diffs the signature against the golden.
Acceptance: phase runs by default, exits 0 on match; a corrupted golden exits 1.
Deps: none. Size: S-M.

## T2 — CI regression gate for the scratch-daemon smoke (spec S-C4.2 / N2)  [P2]
Add a path-scoped CI job running `scratch-daemon-smoke.sh` (hermetic) on changes to
`scripts/scratch-daemon*.sh` or the corpus. If no CI runner is available, instead land
a build-practices "run `make scratch-daemon-smoke` before merging changes to this
script" rule. Acceptance: a signature-breaking change is caught before merge.
Deps: T1 (corpus should exist first), T5 (make target) if using the make entry point.
Size: S.

## T3 — `gc`/`reset` subcommand on scratch-daemon.sh (spec S-C5.2 / N3)  [P2, feature]
Add `gc <scratch> [--hard]`: prune scratch-clone worktrees + `batch-*.json`; `--hard`
also resets the scratch beads DB. Inherit guard_path + assert_not_supervised; refuse on
the fleet path and on a live daemon; emit `GC_SUMMARY ...`. Wire into usage() + runbook
table. Add smoke coverage for the refusal guards.
Acceptance: reclaims disk on a scratch clone; refuses on fleet/live-daemon; fleet
untouched. Deps: none. Size: M. NOTE decision: default is soft (worktrees+artifacts);
`--hard` resets DB — flag to operator if they want always-clean default.

## T4 — Recorded live end-to-end run + runbook evidence (spec S-C4.3 / N5)  [P3, verify]
Run the gated `scratch-daemon-smoke.sh --full` once against a scratch clone with a real
claude binary; capture log + BATCH_SUMMARY; add "Last verified live: <date>/<commit>/
<summary>" to `docs/scratch-daemon-runbook.md`. No new code.
Acceptance: runbook cites a real green live run; log retained. Deps: none (harness is
landed). Needs: real claude + passwordless ssh localhost. Size: S (but wall-clock heavy).

## T5 — `make scratch-daemon-smoke` target + runbook standing-capability section (S-C5.1/S-C5.3 / N4)  [P3]
Add a Makefile target (name must NOT collide with existing `smoke-scratch`) running the
hermetic smoke, with a `## ` help comment; add a "Durable standing capability" section
to the runbook (when to `gc`, the regression-gate contract, the make entry point).
Acceptance: `make help` lists it; runbook documents gc + gate + target. Deps: T3 (for
the gc doc). Size: S.

## Dispatch shape
- Parallel now: T1, T3.
- After T1: T2. After T3: T5. T2 may also depend on T5 if the gate uses the make target.
- T4 any time (independent; wall-clock heavy, needs real claude).

## Genuine decisions the operator may want to revisit
1. **This plan is retroactive.** The core landed 2026-06-25/26; these 5 tasks are the
   durability/verification remainder, not the original build. If the operator considers
   "documented + landed + hermetic smoke" sufficient, the whole work can go straight to
   `ready`/close and T1-T5 become optional hardening.
2. **`gc --hard` default** (soft vs always-reset beads DB) — spec picks soft; flip if
   preferred.
3. **CI vs documented pre-merge gate** (T2) — depends on whether a PR CI runner exists;
   spec prefers CI.
