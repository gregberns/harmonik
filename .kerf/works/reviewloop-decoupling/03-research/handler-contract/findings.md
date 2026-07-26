# Handler and phase lifecycle findings

## Questions

1. Who owns the authoritative handler observer?
2. What orders readiness and input?
3. What does heartbeat prove?
4. How are callback and auxiliary-observer lifetimes bounded?

## Evidence and findings

- HC-011/HC-INV-001 assign exactly one authoritative watcher per active handler session to S01.
- HC-INV-007 makes that watcher the lifecycle-event publisher, except the input-ack carve-out.
- HC-039/041/056/070, HC-INV-004 and AIS require genuine ready before first input; input ACK cannot
  replace readiness.
- HC-026a/057 make handler and daemon heartbeat equivalent for session liveness. Heartbeat is not proof
  of reviewer work activity (also consistent with RSM-006).
- Production tmux sessions currently have no progress watcher and publish readiness through a hook
  callback, creating contract drift rather than a model to bless.
- `SetAgentReadyCallback` lacks unregister/quiesce. A copied callback can fire after hook-session close,
  repeated ready can invoke it repeatedly, and it uses `context.Background`.
- `PerRunEventTap` has no unsubscribe/close.
- Heartbeat stop channels provide no join; review-loop heartbeat defers accumulate until the entire loop
  returns.
- Commit/verdict/budget observers use run-wide context and return no completion handle.
- HC-INV-004 prose orders ready and Launch inconsistently with production; the required invariant is
  spawn/Launch return → genuine ready → first input.
- Handler watcher/process reaper ownership is inconsistent between HC/PL prose and `Session.WaitOwner`.

## Ownership conclusions

- Handler-contract owns the authoritative watcher, genuine ready, progress stream, heartbeat liveness and
  handler session.
- Process-lifecycle owns phase-scoped cancellation/join for callbacks, heartbeat loops, auxiliary
  observers, sessions and teardown.
- Agent-input owns delivery and ACK; do not duplicate it.
- Reviewer budget is currently daemon-local watchdog policy, not a registered ControlPoint budget.

## Required amendments

- Correct HC-INV-004 ordering without weakening ready-before-input.
- Clarify that daemon heartbeat need not traverse the progress watcher to retain HC-057 equivalence.
- Reconcile tmux zero-watcher/direct-publisher drift; do not introduce a second watcher.
- PL: phase completion requires bounded cancel/join/quiescence for all dependent resources.
- Reconcile Claude launch/hook prose to the intended pre-exec sequence:
  capabilities → session log → skills → launch initiated → relay ready.

## Test patterns and gaps

Reuse watcher `Done/Err`, fake-clock dispatch, hook latch, event-tap fan-out, teardown and goleak patterns.
Add races for copied callback versus close, exactly-once ready, blocked-reader cancellation, heartbeat
stop/join, phase observer join before workspace removal, and repeated implementer/reviewer transitions.

## Risks

- A lifecycle bundle that spawns another watcher violates HC cardinality.
- Moving callbacks unchanged preserves cross-phase emissions.
- Treating heartbeat as progress can keep a hung reviewer alive.
- Fixing resume-ready or ACK synthesis during a behavior-neutral chunk requires separate contract work.

