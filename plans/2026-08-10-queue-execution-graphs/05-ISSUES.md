# Issues

Track only confirmed gaps. Each item must name the failed claim, the lowest failing layer, the durable reproduction, the owner, and the current state.

Do not turn open research questions into implementation issues.

## Confirmed research gaps

### Parent-derived integration branches were not wired

- Failed claim: child beads under one epic automatically land on one parent-derived integration branch.
- Evidence: `WM-006` requires the derivation. `workspace.IntegrationBranchName` has no production caller. `daemon.resolveBranchingFrom` has no parent-edge input.
- Lowest failing layer: daemon run-plan composition.
- State: fixed in delta. The run plan reads the parent edge, applies parent-derived defaults by field, and creates the branch with an atomic Git ref update. Focused tests pass. The real fan graph is updated but its proof run is blocked by the daemon disk watermark.

### Dispatch skill text disagrees with current stream behavior

- Failed claim: the operator guide is a reliable description of stream concurrency.
- Evidence: both loaded dispatch skills say streams dispatch one item at a time. `QM-035`, `EM-NOTE-STREAM-CONCURRENCY`, and `streamEligible` allow later pending items while earlier items are dispatched.
- Lowest failing layer: generated skill documentation.
- State: fixed in delta after confirmation by contract, code trace, and a real socket-driven fan-out run. The embedded source and generated mirror now match.

### Clean daemon stop cancels the active queue instead of preserving a resumable drain

- Failed claim: a daemon restart can return a dependency graph to its pre-restart queue state without a supervisor rebuilding the queue.
- Evidence: `ON-027` and `QM-054` say a graceful stop moves an active queue to `paused-by-drain`. `QM-055` says that state survives restart. Production `daemon.drainCancelledQueue` instead calls `queue.CancelQueueOnShutdown`, archives the canonical queue as `*.cancelled-*`, and clears it from memory. The focused production tests `TestQueueCancel_TransitionsToCancelled` and `TestQueueCancel_NamedQueue_ArchivedOnShutdown` pass and pin this behavior.
- Consequence: `harmonik queue resume` cannot recover the work after restart because it needs a live canonical queue in `paused-by-drain`. Open beads remain in Beads, but queue order, group shape, and the submitted set are no longer active. An agent or operator must submit them again.
- Lowest failing layer: daemon clean-exit queue transition. The implementation and its tests disagree with the current queue and operator contracts.
- State: confirmed by spec trace, production code trace, focused tests, and a real daemon stop and start scenario. The scenario proves the second daemon has no canonical queue to resume and performs no dispatch.

## Open research gaps

- The queue accepts named bead IDs. It does not expand an epic into children. Confirm whether this is intentional in the queue contract.
- Crash recovery still needs a graph-specific run with an abrupt process death. A clean stop is a separate confirmed gap.
