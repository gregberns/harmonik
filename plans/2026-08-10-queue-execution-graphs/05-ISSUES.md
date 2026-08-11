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

### Failed dependencies were released toward dispatch

- Failed claim: a dependent of a failed item never crosses into the dispatch path.
- Evidence: after A failed validation and reopened, deferred B moved to pending and reached `ClaimBead`. The Beads blocked-claim guard refused it, so B did not launch, but the queue relied on its last safety layer.
- Lowest failing layer: queue deferred-item re-evaluation and group completion.
- State: fixed and proven in delta. Failed group completion now propagates through only that item's dependency descendants. They become failed with a typed reason before reservation. Independent chains remain deferred or runnable. The live failed-gate graph pauses without a dependent claim.

### Restart rules disagreed about drain-pause recovery

- Failed claim: preserving `paused-by-drain` at shutdown is enough to let the same graph continue after restart.
- Evidence: `QM-055` says the state survives restart but remains paused. It names fresh submit after operator action as the v0.1 recovery path. `QM-002b Class D` marks pending and deferred items in that queue failed during startup because it treats the queue as abandoned. The live operator-resume consumer changes the queue back to active, but it does not re-arm those failed items.
- Lowest failing layer: queue restart contract. The shutdown transition and startup reconciliation cannot be fixed as one mechanical code change until the desired recovery rule is selected.
- State: fixed and proven in delta. Clean shutdown sets a one-shot restart intent. Startup resumes it before reconciliation. Explicit operator pause stays paused and keeps pending items. The real two-daemon scenario continued one pending bead without resubmit or queue resume.

## Open research gaps

- Alpha's durable dispatch replay producer and startup replay paths are not on the integration branch yet. Reconcile the abrupt-crash scenario with their final contract when they land.

## Confirmed policy boundaries

### Epic integration branch promotion is external

- Claim: the daemon should automatically merge a completed parent-derived branch onward.
- Evidence: `workspace-model` `WM-007` ends Harmonik's contract when the integration branch holds one commit per task. `daemon.maybeEmitEpicCompleted` emits a fact and does not mutate Git. The project branch rule requires a human pull request into `main`.
- State: not an implementation issue under the current contract. The live fan graph proves the derived branch advances while its configured base stays unchanged.
- Future design need: distinguish derived epic branch to project integration from project integration to `main`. Define the target and conflict contract before code performs either merge.

### Submission-set completeness belongs to the planning agent

- Claim: queue dry-run can confirm that a submitted epic child set is complete.
- Evidence: the queue contract checks dependency edges only between items in the submitted group. A real dry run that omitted A from `A -> [B, C, D] -> E` marked B, C, and D pending and reported only their edges into E.
- State: accepted current boundary. The user said automatic epic expansion is not a priority. The crew must submit every child ID.
- Future option: add an explicit completeness audit. Do not silently expand the queue because that changes the requested work set.

### Parent epic closure is a supervisor decision

- Claim: the daemon closes the parent when every submitted child finishes.
- Evidence: the live fan graph closed all five children, kept the parent open, and emitted one run-scoped `epic_completed`. `QuiesceArbiter.handleEpicCompleted` wakes the captain.
- State: accepted current boundary. The event can include a set with tombstoned children, and the current contract also leaves onward branch promotion outside the core. Closing the epic can therefore require a result judgment.
- Economy: this is one wake after the graph, not work between child transitions. The captain should inspect the result once and then close, revise, or escalate the epic.
