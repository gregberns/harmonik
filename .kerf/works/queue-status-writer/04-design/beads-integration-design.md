# Beads integration change design

## Current state

`specs/beads-integration.md` defines the ledger facts used for item deferral and
recovery. Queue code reads those facts. It does not own Beads writes.

## Target state

No normative Beads integration text changes in this work.

The queue transition surface keeps deferred-for-ledger-dependency non-terminal.
It changes an item back to pending only when the existing ledger checks show
that every blocker resolved. It emits no new event for that transition.

## Rationale

The status API must not alter claim, close, reopen, or reset ownership in the
Beads adapter. It only makes the existing queue-side state change explicit.

## Requirements traceability

| Requirement | Design response |
| --- | --- |
| BI-006 deferred meaning | Preserve the current blocker checks. |
| No Beads ownership change | Keep all ledger writes outside the queue surface. |
| Deferred is non-terminal | Retain the group-advance gate. |
