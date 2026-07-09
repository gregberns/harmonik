# SPEC — test-daemon-harness (durable scratch-clone test-daemon harness)

## Status
The core capability is LANDED (beads hk-4tdlw, hk-6vr02/batch, hk-1gkc8/feedback,
hk-6eqv9/smoke — all closed on origin/main). This spec closes the remaining
verification-durability gap so the harness is a truly DURABLE, rot-protected,
documented standing capability.

## What the harness IS (as built)
`scripts/scratch-daemon.sh` runs a SECOND, fully-isolated harmonik daemon on a
separate git clone (own socket, tmux session, binary, beads DB), rebuildable in
seconds, that NEVER touches the live fleet daemon. Subcommands:
`init | build | up | status | down | cycle | batch | feedback` (+ new `gc`).
`batch` submits a named bead batch to the scratch queue and emits a structured
pass/fail artifact; `feedback` turns FAIL items into deduped OPEN beads on the fleet
DB with a stable `prov:<hash>` provenance key. Docs: `docs/scratch-daemon-runbook.md`
(reference) + `docs/remote-test-daemon-methodology.md` (field manual).

## Normative requirements (this plan)
- **N1 (golden corpus).** A hermetic regression corpus asserts `fail_signature`
  stability AND distinctness across every `oneline` redaction class; run by the
  default smoke; a signature regression fails the smoke.
- **N2 (CI gate).** Changes to `scripts/scratch-daemon*.sh` or the corpus are
  gated by the hermetic smoke before merge (CI job preferred; documented pre-merge
  rule as fallback).
- **N3 (`gc`).** A `gc <scratch> [--hard]` subcommand reclaims scratch-clone disk
  (worktrees + batch artifacts; `--hard` also resets the scratch beads DB), inheriting
  every fleet-safety guard, refusing on the fleet path and on a live daemon.
- **N4 (make target).** `make scratch-daemon-smoke` runs the hermetic smoke and is
  listed in `make help`.
- **N5 (live evidence).** One recorded green live end-to-end run
  (`init->up->batch(real bead)->feedback->down`) cited in the runbook.
- **N6 (fleet-safety, standing).** Every subcommand — including `gc` — continues to
  satisfy: guard_path scratch != fleet, assert_not_supervised, argv-verified kill,
  proof-gated tmux teardown. The only deliberate fleet write remains `feedback`
  (OPEN beads, never assigned, never in_progress).

## Success criteria (from problem-space, reconciled)
- [x] One command spins up a scratch daemon, runs a named batch, returns structured
  pass/fail. (LANDED: `batch`.)
- [x] A failing batch item produces/updates a provenance-stamped fleet bead without
  duplicating on re-run. (LANDED: `feedback` + smoke phase D.)
- [~] Documented general on-demand capability + enforced never-touch-fleet guarantee.
  (Runbook + methodology LANDED; N3/N4/N5/N6 close the "durable standing" remainder.)
- [~] Demonstrated end-to-end on a trivial batch (LANDED, offline) + the
  remote-substrate scenario batch (N5 provides the recorded LIVE run).
