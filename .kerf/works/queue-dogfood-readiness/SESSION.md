# Queue dogfood readiness — Session

## Current pass

The work is **Ready**. Problem Space, Decompose, Research, Change Design, Spec
Draft, Integration, and Tasks are complete. Two independent re-reviews approved
the corrected Integration pass and two approved the task plan. The later
release-claim amendment needs a cross-boundary review. `kerf square` passed
before that amendment. The task plan now has thirteen tasks and names all
sixteen required test beads. Fourteen research records and fifteen design
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
- Restart release recovery needs an immutable Git-backed release claim. The
  final pre-release transition records the dispatch head, resolved merge target,
  and optional remote endpoint. T5a writes the claim. T6 reads the claim and
  current Bead state without JSONL or daemon-local registry fallback.
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

1. Implement T1–T9 plus T5a from `07-tasks.md` in lane order.
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

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. Its text is kept below because it names targets
and records this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree, the section above wins.

### Queue dogfood readiness — Session

#### Current pass

The work is shelved at **Ready**. `kerf square` passed with all expected
artifacts. The work is ready for finalization and implementation. The fleet
daemon was not started.

#### Decisions made

- Failed-item recovery becomes a durable queue transaction. It is distinct from
  drain resume and returns a durable receipt.
- A committed but unmerged run gets one durable recovery record and one
  terminal action. It retains its original worktree until recovery completes.
- A readiness gate is assessor-only. Schema version 3 pins its candidate,
  canary profile, decision owner, and proof artifacts.
- The first canary remains one repeat-safe local stream item at concurrency one.
  It excludes append, remote, Pi, cross-repository, and wave work.

#### Ledger triage

The four historical finding IDs are absent from this machine ledger. No closure
was made. Current source has their named regression tests. See
`ledger-triage.md` for the evidence boundary.

#### Suggested next steps

1. Review and finalize the ready work before implementation.
2. Implement `07-tasks.md` in dependency order. Alpha owns daemon and workspace
   work. Bravo owns queue, CLI, schema/runbook, and triage.
3. Do not alter registry or mission state without operator authority.

#### Reading order

1. `07-tasks.md`
2. `05-changelog.md`
3. `05-spec-drafts/`
4. `ledger-triage.md`
