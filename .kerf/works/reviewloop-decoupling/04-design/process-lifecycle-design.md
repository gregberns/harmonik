# Interactive process and phase lifetime design

## Current state

`specs/process-lifecycle.md` owns parentage, provenance, cancellation, kill/reap, tmux, and orphan recovery
but has no phase-lifetime barrier. Logical outcomes and cycle events can occur while callbacks,
heartbeats, event subscriptions, auxiliary observers, sessions, and reviewer worktrees remain live until
the entire review loop returns.

PL-014/016 also conflate lifecycle watcher authority with exclusive `cmd.Wait` ownership, and blanket
child-of-daemon prose conflicts with the established direct and tmux regimes.

## Target state

Add terminology for phase lifetime, phase-owned resource, logical phase outcome, quiescent phase
completion, and explicit typed ownership transfer.

Add PL-014b: each implementer/reviewer phase establishes one ownership scope before its first asynchronous
acquisition. Logical outcome may be published before close, but cannot authorize next dependent launch,
workspace reuse/removal, or review-loop return. Close follows this partial order:

1. seal new input and nonterminal callbacks;
2. cancel heartbeat and auxiliary observers;
3. seal the authoritative lifecycle source and initiate session termination under existing policy;
4. await authoritative watcher and all auxiliary completion, then complete process/pane destruction and
   reap;
5. preserve/consume required terminal hook state, then close registration;
6. release phase workspace only after every possible user is quiescent.

Close is bounded, idempotent, concurrency-safe, and best-effort across all steps. Timeout uses the existing
failure path, continues later cleanup attempts, and retains a still-dependent workspace. Resources outlive
the phase only through explicit ownership transfer; existing independent-session and retained-failure
cases remain valid when they satisfy it.

Amend PL-014/016 to require one wait owner and one reap for direct processes without requiring the
authoritative lifecycle watcher to own `cmd.Wait`. Preserve direct/tmux provenance and all kill policy.

Cross-reference Handler Contract: lifecycle watcher authority remains exactly one; commit, verdict,
artifact, budget, delivery, and heartbeat tasks are auxiliary and cannot publish competing terminal
events. Every asynchronous resource exposes cancellation and completion through its phase owner.

Make callback unregistration a quiescence barrier. It prevents new admission and accounts for callbacks
already admitted before returning. Terminal-hook retention may require “seal nonterminal delivery” before
final close.

The exact middle order is substrate-specific but must preserve one invariant: the watcher cannot outlive
destruction of the lifecycle source it observes. Direct execution may signal/close process pipes and then
join the watcher before final reap; interactive execution consumes/seals terminal hook state and joins its
authoritative observer before destructive pane release. Tmux sessions with no current authoritative
observer remain a C2 conformance defect and must not be modeled as permission to leak an observer.

Existing phase/cycle events remain logical-outcome events. C4 adds an invisible advancement barrier and
does not add or reorder events.

Conformance covers success/failure/timeout/cancellation/approval/`REQUEST_CHANGES`, callback sealing and
task joins before advancement, reap/watcher completion before workspace removal, reviewer cleanup before
next iteration, repeated close, timeout retention, event compatibility, and race/leak cases.

## Rationale

The phase is the smallest ownership unit that prevents cross-phase callbacks, observer leaks, premature
workspace deletion, and process/watcher races while preserving observable behavior.

## Traceability

- Component: C4.
- Owners: PL-014/016/017/021b and HC-018.
- Goals: phase-owned imperative shells, bounded teardown, leak-free cleanup.
- Dependencies: C2 watcher completion, C3 placement, C6 observable event definitions.
- Preserved: provenance, spawn regimes, escalation, orphan sweep, tmux policy, ownership transfer.
- Non-goals: no new process class, generic runtime service, event, input mechanism, or timing change.
