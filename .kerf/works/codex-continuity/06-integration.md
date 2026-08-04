# Integration review

## Checks performed

- `specs/harness-contract.md`: the first vertical applies only to registered
  interactive targets. It does not add a harness dispatch seam. A managed-run
  integration needs a later harness-contract amendment.
- `specs/hitl-decisions.md`: the canonical JSONL projection stays the one
  decision source. `DecisionGatePort.WithDeliveryFence` uses its append
  serialization and adds no second decision record.
- `specs/agent-input.md` and `specs/handler-contract.md`: structured input uses
  `InputPort` as an outer adapter. It maps its existing `(run ID, input
  sequence)` observation and does not claim an effect ID on that protocol.
- `specs/process-lifecycle.md`: attached delivery uses the existing tmux adapter
  and its `PL-021d` temp-file, buffer, cleanup, and audit rules.
- `specs/event-model.md`: the first vertical keeps controller transitions in its
  durable record and journal. It uses existing input events. It adds no new
  cross-bus `continuity_*` event.

## Resolutions

The keeper now distinguishes a closed continuity lease from work completion.
Only the shared git and dispatch layer can provide a `WorkTerminalFact` for a
dispatched scope. An operator close writes `LeaseReleased` only.

The decision gate now uses a callback fence. The port holds canonical decision
append serialization while the controller performs its own compare-and-swap.
A conflict retries the complete fold and compare-and-swap.

A manual pause releases the lease and abandons all pending claims. A later
manual turn cannot settle an old successor wait. An authorized new lease is
required before automation resumes.

Structured input has explicit delivered, rejected, and ambiguous paths. Early
input events are buffered or replayed until the exact durable tuple binds.

## Scenario and exploratory gates

- `hk-ssifd`: attached Codex continuation replay scenario.
- `hk-656tk`: operator attach, status, and release exploration.

## Assessment

The drafted specifications are coherent with their existing contracts. The
reviewer approved this integration pass.
