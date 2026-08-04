# Change Design — Queue model

## Current state

`QM-032`, `QM-052`, `QM-055`, and Appendix A.3 defer failed-queue recovery.
They require restart plus fresh submit. `internal/queue/resume.go`
`ResumeFromFailure` exists but has no production caller. It re-arms every
failed item, clears attempts and failure fields, reopens failed groups, and
changes `paused-by-failure` to active.

## Target state

Promote recovery into the current queue contract. Add a queue-recover request
and response with name or queue-ID selection, normalized queue name, re-armed
bead IDs and count, and final active status. Name takes precedence. No selector
means main.

Replace deferred recovery text. `complete-success` remains terminal. A failed
group may re-enter active only inside a successful recovery transaction. The
operation accepts only one `paused-by-failure` queue. It uses one
`QueueStore.Transact` operation to re-arm all failed items, reopen failed
groups, set active, and durably install the candidate. After `Transact`
returns success, it emits `queue_recovered` once and then wakes dispatch.

Every reservation release and undo path uses a named transaction operation.
Each operation changes status and run ID in one candidate. A failed replacement
write leaves memory and persistent state at the old candidate, returns failure,
and retains quarantine. Raw setters and direct persistence do not implement
recovery, release, or undo.

Quarantine, failed replacement write, stale snapshot, missing queue, or another
status returns failure. It does not clear quarantine or change handler state.
Recovery success means durable queue mutation, not started work.

## Rationale

This removes false success from the drain-resume command. One mutation owner
keeps disk and memory aligned. Re-arming all failed items is deterministic and
matches the existing pure mutation.

## Requirements traceability

- `02-components.md`: queue model recovery requirements.
- `03-research/queue-model/findings.md`.
- Session decisions: per-queue synchronous recovery, all failed items, durable
  success, and separate recovery event.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Queue model change design

#### Current state

Failed-item rearm is a pure mutation with no production caller. Reservation
release uses raw writes that can clear quarantine and diverge from disk.

#### Target state

Add one `RecoverFailedQueue` transaction. It accepts only a quarantined
`paused-by-failure` queue. In one durable write it re-arms failed items,
reopens only failed groups, clears each retired run ID, and records a recovery
receipt. It leaves completed items unchanged. Repeating the request returns the
same receipt and cannot re-arm a second time. All reservation undo, terminal
release, adoption, and recovery writes use the same transaction owner.

#### Rationale

This separates failed recovery from drain resume and preserves the one-writer
and durable-boundary principles.

#### Requirements traceability

Addresses queue-model requirements in `02-components.md` and research in
`03-research/queue-model/`.
