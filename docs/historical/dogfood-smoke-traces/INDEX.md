# Dogfood Smoke Traces — Phase-1 Validation Execution Logs (Archived)

**Archival note.** These are the dogfood / smoke-test execution-trace and log dumps from the May 2026 Phase-1 validation runs (≈2026-05-12 … 2026-05-15), captured while harmonik was first proven to drive `claude` end-to-end on a real bead. They are preserved here as an audit trail of how Phase-1 OPERATIONAL GREEN was reached; they are **not** live design docs or specs. For current project state see [STATUS.md](../../../STATUS.md).

The 2026-05-14 operational-green run is the milestone trace (Harmonik runs Claude end-to-end on a bead with zero human input); the dated `bridge-substrate` runs (v2…v11) are the iterative bridge-integration smoke runs that led up to it, and `dogfood-smoke-procedure-bridge.md` is the runbook for that GREEN-target re-run.

## Files

- `dogfood-smoke-run-2026-05-12.md` — original RED baseline (false-positive: clean-EOF close with no task context).
- `dogfood-smoke-run-2026-05-13-bridge-substrate.md` plus `-v2` … `-v11` — iterative bridge-integration smoke runs.
- `dogfood-smoke-run-2026-05-14-operational-green.md` — Phase-1 operational milestone (end-to-end GREEN).
- `dogfood-smoke-run-2026-05-15-bridge-substrate.md`, `-review-loop.md`, `-review-loop-take2.md` — post-milestone validation runs.
- `dogfood-smoke-procedure-bridge.md` — GREEN-target re-run runbook (procedure for the bridge-integration smoke).
- `dogfood-smoke-trace.md` — hk↔Claude subprocess contract trace (reference material for the smoke implementer).
- `smoke-log.md` — terse deploy-smoke log.
