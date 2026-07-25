# Decompose review

## Round 1 — REQUEST_CHANGES

Reviewer: `kerf_decompose_final_review`

Findings:

1. C5 invented an event-publication step between checkpoint persistence and ACK that is not present in
   CHB-023 or EM-015d.
2. C2 placed review-loop auxiliary observer lifetimes under handler-contract even though handler-contract
   owns only the authoritative handler watcher.
3. C1 prescribed a decision-table document form instead of a semantic requirement.

Disposition:

- C5 now preserves capture/receipt → durable checkpoint/baseline → connection-accept ACK and forbids an
  additional publication stage absent a normative owner.
- Auxiliary observer cancellation/join and callback unregistration moved to C4 process/phase lifetime.
- C1 now requires canonical complete semantics without prescribing spec text shape.

## Round 2 — APPROVE

Reviewer confirmed all findings resolved, spec ownership remains minimal, every problem-space goal is
covered, dependencies are correct, and the no-new-subsystem decision conforms to architecture AR-052,
AR-053, and AR-016.

