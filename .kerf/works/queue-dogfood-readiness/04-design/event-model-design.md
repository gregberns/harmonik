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
