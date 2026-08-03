# Queue dogfood readiness — Session

## Current pass

The work is shelved at **Decompose**. The Problem Space pass is complete. The
component map is complete and awaits its normal Decompose review.

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

## Open questions

1. Pick a safe, repeatable first mission batch.
2. Decide whether daemon test reliability needs code changes after a controlled
   idle-box trial, or whether the single-suite operating rule is enough.
3. Define the exact evidence required to close a stale graph finding.

## Suggested next steps

1. Resume this work.
2. Read the problem-space record.
3. Review `02-components.md` against the problem-space record and the affected
   existing specs.
4. Resolve review findings before advancing to Research.

## Reading order

1. `01-problem-space.md`
2. `plans/2026-07-27-delete-and-rewrite/LANES.md` sections 3, 7, and 8
3. `plans/2026-07-27-delete-and-rewrite/STEP-4-RESERVATION-TRANSACTION.md`
4. `docs/scratch-daemon-runbook.md`
5. `specs/assessor-handoff-schema.md`
6. Current bead records for the blockers named in `01-problem-space.md`
