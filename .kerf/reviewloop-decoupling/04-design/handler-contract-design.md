# Handler session and observer lifecycle design

## Current state

`specs/handler-contract.md` gives one session watcher authority over lifecycle ordering, redaction,
publication, readiness, terminal classification, and silent-hang detection. HC-011/HC-INV-001 require
exactly one watcher, HC-INV-007 makes it the authoritative lifecycle publisher, HC-039/041/056 require
genuine ready before input, and HC-026a/057 make handler and daemon heartbeat equivalent for liveness.

Three conflicts remain: HC-INV-004 places ready before `Launch` returns; interactive tmux sessions bypass
the progress-stream watcher and have no conforming authoritative observer; and HC-INV-007 does not state
the HC-057 daemon-heartbeat carve-out or an observable stop/completion contract.

## Target state

Amend terminology and HC-011/011a to define one substrate-neutral **authoritative lifecycle observer**
(retaining “watcher” as shorthand) per active session. Its source may be the conventional progress stream
or a substrate-specific lifecycle source such as the hook bridge. It remains the sole ordering,
deduplication, redaction, and lifecycle-publication boundary. Auxiliary commit, verdict, artifact, budget,
heartbeat, and delivery observers are not lifecycle watchers. Process wait/reap remains separate.

The watcher exposes idempotent cancellation and observable completion. Completion means its source is
closed, final terminal publication is resolved, and no later lifecycle event can be published.

Amend HC-039/041/056 and HC-INV-004:

- each successful launch produces exactly one genuine, session-specific `agent_ready`;
- pane existence, launch success, first output, heartbeat, or input ACK cannot synthesize it;
- repeated substrate notifications are deduplicated;
- ready timeout retains existing timing and kill/reap behavior;
- the invariant is `successful spawn/Launch return -> launch_initiated -> genuine ready -> first input`,
  with capability/session-log/skills facts still preceding ready.

Clarify HC-057/HC-INV-007 that daemon heartbeat need not traverse the watcher, remains equivalent for
session liveness, is not reviewer work activity, and does not create a second authoritative watcher.
Its producer has cancellation and observable completion; Process Lifecycle owns the phase join.

Amend interface/conformance text without prescribing Go packages. Cover conventional and interactive
substrates, exactly-one watcher/ready/terminal result, repeated-ready and timeout races, blocked-source
cancellation, watcher completion before ownership release, heartbeat completion, and ready-before-input.

## Rationale

This preserves the existing authority boundary while making it compatible with both substrates, corrects
an impossible ordering statement, and supplies the completion fact needed by the phase lifetime owner.

## Traceability

- Component: C2.
- Owners: HC-011/011a, HC-026a, HC-039/041/056/057, HC-INV-001/004/006/007.
- Goals: bounded watcher lifetime, no cross-phase publication, truthful lifecycle.
- Dependencies: Process Lifecycle owns cancellation/join; Agent Input owns delivery/ACK; C5 owns
  checkpoint ordering.
- Non-goals: no second watcher, event type, heartbeat discriminator, ACK change, or resume-ready change.

