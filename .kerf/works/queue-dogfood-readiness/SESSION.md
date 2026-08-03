# Queue dogfood readiness — Session

## Current pass

The work is **Ready**. Problem Space, Decompose, Research, Change Design, Spec
Draft, Integration, and Tasks are complete. Two independent re-reviews approved
the corrected Integration pass and two approved the task plan. `kerf square`
passes with all 49 expected artifacts. The task plan has twelve tasks and names
all sixteen required test beads. Fourteen research records and fifteen design
records are on disk.

## Decisions made

- This work is a narrow queue-readiness gate. It does not restart the fleet.
- An assessor is a separate audit role. It does not consume normal queue work.
- The first live evidence uses an isolated scratch daemon and a fresh, limited
  mission.
- The first canary is one local repeat-safe stream at concurrency one. It has no
  remote, Pi, cross-repository, or wave work.
- The confirmed blocker set is DOT shutdown drain, failed-item recovery,
  reservation durability, durability-proof defects, daemon-test reliability,
  Step 9 and core-loop proof, and bead and event hygiene.
- `hk-v4wer`, `hk-o4sgg`, `hk-fmere`, and `hk-b4xf2` are verified stale closure
  candidates. Verify their current source paths before closing them. Do not plan
  code from their old descriptions.
- Alpha owns daemon code. Bravo owns queue, queue wiring, CLI, proof repairs,
  and backlog triage. Release-path contracts are shared.
- Alpha owns every daemon change and daemon test, including
  `evaluateGroupAdvanceWithOutcome`. Bravo must add an explicit recovery-wiring
  task because `queue-status-writer` does not wire failed-item resume.
- Registry and mission operations are read-first. The operator authorizes each
  reset, retirement, or creation before it happens.
- Failed-item recovery is a synchronous, per-queue transaction. It re-arms all
  failed items in one `paused-by-failure` queue. It is not operator drain
  release and it does not use a handler resume.
- Recovery is durable before its RPC reports success. It emits a distinct
  post-commit queue recovery event. A failed write or quarantine reopens
  nothing and reports failure.
- Shutdown drain resolves the committed tip with a live context, synchronizes a
  remote branch before merge, and reopens on sync or merge failure. A no-change
  close requires that synchronization.
- The first canary uses an explicit local, non-Pi, single-item invocation. It
  saves the batch artifact and event capture before scratch cleanup.
- A normal command watchdog must not bypass graceful drain. Immediate stop is a
  separate, explicit operator action.
- Recovery preflights every failed item's Beads status. It mutates nothing when
  any record is missing, unreadable, or non-open.
- Recovery retains the prior run ID and review-loop count. It resets attempts
  and the last failure reason only.
- The readiness target has named required inputs, retained artifact paths, and
  a validator that makes no Beads or fleet-daemon call.

## Open questions

1. Pick a safe, repeatable first mission batch.
2. Decide whether daemon test reliability needs code changes after a controlled
   idle-box trial, or whether the single-suite operating rule is enough.
3. Define the exact evidence required to close a stale graph finding.

## Suggested next steps

1. Implement T1–T9 from `07-tasks.md` in lane order.
2. Finalize documents with T10 after implementation evidence exists.
3. Run T11 then T12 on a controlled local fixture.
4. Keep the fleet daemon stopped.

## Reading order

1. `01-problem-space.md`
2. `plans/2026-07-27-delete-and-rewrite/LANES.md` sections 3, 7, and 8
3. `plans/2026-07-27-delete-and-rewrite/STEP-4-RESERVATION-TRANSACTION.md`
4. `docs/scratch-daemon-runbook.md`
5. `specs/assessor-handoff-schema.md`
6. `decompose-review.md`
7. Current bead records for the blockers named in `01-problem-space.md`
