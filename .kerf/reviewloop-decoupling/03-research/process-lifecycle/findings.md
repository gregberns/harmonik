# Process and phase lifetime findings

## Questions

1. What does process completion mean today?
2. Which resources are phase-owned versus run-owned?
3. What must be quiescent before the next phase or workspace release?
4. Which current contracts and tests are insufficient?

## Executive finding

The review loop generally protects the main worktree while the primary process is visibly alive, but it has no real per-phase lifetime boundary. Process exit, logical outcome, event publication, hook closure, callback quiescence, heartbeat termination, observer termination, watcher completion, and workspace release are distinct moments that current code partially conflates.

`specs/process-lifecycle.md` should gain one narrow requirement: each implementer or reviewer phase owns an explicit resource scope, and the coordinator closes that scope to quiescence before launching the next dependent phase, releasing a dependent workspace, or returning.

## Current ownership and ordering

- `beadRunOne` owns the main run worktree, run registry, remote tunnel/slot, and final cleanup.
- Each implementer phase owns a session, hook registration, event tap subscription, ready callback, input delivery, heartbeat, and commit/hang/budget observers.
- Each reviewer phase additionally owns a detached reviewer worktree and verdict observers.
- `DispatchSegment.Run` returns when launch/readiness/input reach `Working`; it does not mean the agent phase has completed.
- `implementer_phase_complete` is emitted after the primary wait, but before hook callback quiescence, heartbeat stop, observer joins, and accumulated force teardown.
- Reviewer worktrees and phase teardown are deferred until the entire review loop returns. On `REQUEST_CHANGES`, earlier phase resources survive into later iterations.

The accumulated LIFO defers keep resources bounded by the iteration cap, but they are run-scoped rather than phase-scoped.

## Contract and implementation gaps

1. Process Lifecycle has startup, shutdown, parentage, and orphan rules but no per-phase completion barrier.
2. Direct execution gives `handler.Session.WaitOwner` exclusive `cmd.Wait`; parts of PL-014/PL-016 assign it to the watcher.
3. Tmux owns child parentage and escalation, so blanket child-of-daemon language and simplified tmux kill text are stale.
4. Tmux has no authoritative stdout watcher; auxiliary hook observers must not be promoted into competing lifecycle watchers.
5. Deleting a hook session cannot quiesce a ready callback already copied for invocation; repeated ready signals can invoke it repeatedly.
6. `PerRunEventTap` has no unsubscribe/close operation, and naïvely closing subscriber channels would race snapshotted sends.
7. Heartbeats and most observers accept cancellation but expose no join handle. Some tests sleep before restoring globals to accommodate unjoinable goroutines.
8. The post-commit watchdog can spawn a nested goroutine not covered by joining only its parent.
9. Reviewer cleanup is delayed, uses an unbounded cancellation-detached context, and has a non-concurrency-safe idempotency flag.
10. `ForceTeardownSession` documents a reap guarantee stronger than tmux exposes through an observable join.

## Recommended boundary

Add a phase-owned lifetime scope before the first asynchronous phase resource is acquired. The scope owns:

- primary session/process and authoritative watcher when present;
- hook callback registration;
- input-delivery workers;
- heartbeat and auxiliary observers;
- event-tap subscriptions;
- any workspace used only by the phase.

Every asynchronous member must have bounded stop/cancel and observable completion. Closing the scope must:

1. seal new input and callback delivery;
2. cancel heartbeat and auxiliary work;
3. terminate/reap the session under existing Handler/Process Lifecycle policy;
4. await authoritative watcher and auxiliary completion;
5. preserve the required terminal hook outcome before closing the hook session;
6. release the phase workspace only after all possible users are quiescent.

Close must be bounded, idempotent, concurrency-safe, and best-effort across all steps. Failure to quiesce routes through the existing failure path and retains any still-dependent workspace. A resource may outlive a phase only by an explicit typed ownership transfer.

This is an internal close/done barrier, not a new public subsystem or event.
`implementer_phase_complete` is explicitly a logical work-result event: it reports that the primary
implementer outcome is known, not that phase resources are quiescent. Existing public result-event order
therefore remains stable; advancement and workspace release occur only after the invisible barrier.

## Functional core and adapters

Keep the ordering policy pure: given acquired resources, logical outcome, terminal hook state, and teardown results, compute the remaining close actions and whether advancement/release is allowed. Adapters implement process kill/reap, hook sealing, task cancellation/join, event-tap unsubscribe, and local/remote workspace cleanup.

Do not use `DispatchSegment`'s context for this scope because it is canceled at `Working`. Use a child of the run context plus a fresh bounded cleanup context.

## Tests needed

Characterize ordered traces for success, launch failure, ready timeout, cancellation, no-commit, approval, `REQUEST_CHANGES`, malformed/missing verdict, and budget kill. Prove:

- logical result publication may precede close;
- callback/input sealing precedes advancement;
- heartbeat, observers, watcher, and session are joined;
- reviewer verdict is read/archived before reviewer workspace cleanup;
- iteration N is quiescent and its reviewer worktree removed before iteration N+1 launches;
- outer main-worktree cleanup remains last.

Race tests should cover callback invocation versus close, repeated ready delivery, tap emit/subscribe/unsubscribe/close, watchdog cancellation versus terminal detection, concurrent workspace cleanup, teardown timeout versus natural exit, and terminal hook outcome versus close.

Leak tests should repeat many cycles with a deterministic resource ledger and assert zero hook sessions, subscriptions, tasks, leaked spawn slots, and reviewer worktrees after every phase. Preserve the explicit independent-session ownership-transfer and retained-failure exceptions.

## Split hazards

- Moving existing defers into phase helpers is intended but unsafe until completion handles exist.
- Hook close likely needs two stages: seal nonterminal callbacks, consume terminal state, then remove.
- A generic phase scope must not invent reviewer heartbeat activity or a second tmux watcher.
- Session persistence/ACK remains a separate transaction and must finish before work proceeds.
- Ready latch replay must not be lost or duplicated.
- Remote implementer and local reviewer workspaces require location-aware cleanup adapters.
