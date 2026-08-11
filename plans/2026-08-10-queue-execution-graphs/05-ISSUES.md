# Issues

Track only confirmed gaps. Each item must name the failed claim, the lowest failing layer, the durable reproduction, the owner, and the current state.

Do not turn open research questions into implementation issues.

## Confirmed research gaps

### Parent-derived integration branches were not wired

- Failed claim: child beads under one epic automatically land on one parent-derived integration branch.
- Evidence: `WM-006` requires the derivation. `workspace.IntegrationBranchName` has no production caller. `daemon.resolveBranchingFrom` has no parent-edge input.
- Lowest failing layer: daemon run-plan composition.
- State: fixed and proven in delta. The run plan reads the parent edge, applies parent-derived defaults by field, and creates the branch with an atomic Git ref update. The real five-child graph passed on the derived branch. The fix also preserves the full Bead record during queue-path hydration so its edges reach the run plan.

### Dispatch skill text disagrees with current stream behavior

- Failed claim: the operator guide is a reliable description of stream concurrency.
- Evidence: both loaded dispatch skills say streams dispatch one item at a time. `QM-035`, `EM-NOTE-STREAM-CONCURRENCY`, and `streamEligible` allow later pending items while earlier items are dispatched.
- Lowest failing layer: generated skill documentation.
- State: fixed in delta after confirmation by contract, code trace, and a real socket-driven fan-out run. The embedded source and generated mirror now match.

### Clean daemon stop cancelled the active queue instead of preserving a drain pause

- Failed claim: a daemon restart can return a dependency graph to its pre-restart queue state without a supervisor rebuilding the queue.
- Evidence: `ON-027` and `QM-054` say a graceful stop moves an active queue to `paused-by-drain`. `QM-055` says that state survives restart. Production `daemon.drainCancelledQueue` instead calls `queue.CancelQueueOnShutdown`, archives the canonical queue as `*.cancelled-*`, and clears it from memory. The focused production tests `TestQueueCancel_TransitionsToCancelled` and `TestQueueCancel_NamedQueue_ArchivedOnShutdown` pass and pin this behavior.
- Consequence: the durable queue state and its stop reason are lost. Open beads remain in Beads, but queue order, group shape, and the submitted set are no longer active. An agent or operator must submit them again.
- Lowest failing layer: daemon clean-exit queue transition. The implementation and its tests disagree with the current queue and operator contracts.
- State: fixed and proven in delta. Shutdown persists the canonical queue as a restart drain. A second real daemon continued it without resubmission.

### Restart rules disagreed about drain-pause recovery

- Failed claim: preserving `paused-by-drain` at shutdown is enough to let the same graph continue after restart.
- Evidence: `QM-055` says the state survives restart but remains paused. It names fresh submit after operator action as the v0.1 recovery path. `QM-002b Class D` marks pending and deferred items in that queue failed during startup because it treats the queue as abandoned. The live operator-resume consumer changes the queue back to active, but it does not re-arm those failed items.
- Lowest failing layer: queue restart contract. The shutdown transition and startup reconciliation cannot be fixed as one mechanical code change until the desired recovery rule is selected.
- State: fixed and proven in delta. Clean shutdown sets a one-shot restart intent. Startup resumes it before reconciliation. Explicit operator pause stays paused and keeps pending items. The real two-daemon scenario continued one pending bead without resubmit or queue resume.

## Open research gaps

- The queue accepts named bead IDs. It does not expand an epic into children. Confirm whether this is intentional in the queue contract.
- Alpha's durable dispatch replay producer and startup replay paths are not on the integration branch yet. Reconcile the abrupt-crash scenario with their final contract when they land.
