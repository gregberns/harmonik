# Change Design — Event model

## Current state

Section 8.10 has eight queue lifecycle events. `queue_paused` has only
`group_failure` and `operator_drain`. `operator_resuming` has no failed-item
recovery facts. `queue_resumed` is reserved with no payload.

## Target state

Add `queue_recovered` through an `EV-027` amendment. Do not use
`queue_resumed`, which could mean drain release or handler resume. Its payload
has queue ID, normalized name, re-armed bead IDs, re-armed count, and recovery
time.

The event is class O. The durable queue candidate is authority. Successful
transaction and memory installation precede the single event attempt. The event
attempt precedes the dispatch wake. Restart neither synthesizes nor retries it.
Add its payload type, registration, compatibility entry, and coverage tests.

## Rationale

The event records a real recovery action that the old operator event cannot
express. Class O preserves queue persistence as the authority.

## Requirements traceability

- `02-components.md`: event lifecycle requirements.
- `03-research/event-model/findings.md`.
- Session decision requiring a distinct post-commit recovery event.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Event model change design

#### Current state

Queue events show group completion and pause. Queue status cannot prove a
failed recovery request or result.

#### Target state

Add Class-F failed-recovery requested and completed or rejected events. Their
payload includes queue, group, item count, prior and current state, recovery
receipt, retired run identities, and reason. Emit only after durable commit.
Add one correlation field from terminal recovery to the existing workspace and
run observations. Keep `run_*` as the only item terminal events.

#### Rationale

An assessor needs audit evidence without treating event log as authority.

#### Requirements traceability

Addresses recovery observation and replay-proof requirements.
