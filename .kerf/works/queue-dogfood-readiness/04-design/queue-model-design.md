# Queue model change design

## Current state

Failed-item rearm is a pure mutation with no production caller. Reservation
release uses raw writes that can clear quarantine and diverge from disk.

## Target state

Add one `RecoverFailedQueue` transaction. It accepts only a quarantined
`paused-by-failure` queue. In one durable write it re-arms failed items,
reopens only failed groups, clears each retired run ID, and records a recovery
receipt. It leaves completed items unchanged. Repeating the request returns the
same receipt and cannot re-arm a second time. All reservation undo, terminal
release, adoption, and recovery writes use the same transaction owner.

## Rationale

This separates failed recovery from drain resume and preserves the one-writer
and durable-boundary principles.

## Requirements traceability

Addresses queue-model requirements in `02-components.md` and research in
`03-research/queue-model/`.
