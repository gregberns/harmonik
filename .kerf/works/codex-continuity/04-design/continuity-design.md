# Design: typed continuity keeper

## Current state

The Claude keeper owns context-pressure recovery. Its `Watcher` and `Cycle`
are not a crew-work continuity protocol.

The decision subsystem already owns human decision blocks. It emits durable
decision events and gives a blocked agent a typed wait path.

The Codex shell pilot was an isolated experiment. It reads Codex rollout JSON,
interprets assistant text, keeps a text state file, and calls tmux directly.
It is stopped and is not an implementation base.

## Target structure

```text
source codec -> typed lifecycle event -> pure continuity reducer -> typed effect
                                         ^                         |
                                         |                         v
                              controller record and outbox <--- controller
```

The source codec and delivery adapter are harness-specific edges. The reducer
and record are harness-neutral. No policy type carries assistant text.

## Domain and identity

The domain defines opaque `ContinuityInstanceID`, `Generation`, `CrewIdentity`,
and `HarnessSessionID`. A source event has `SourceInstanceID` and monotonic
`SourceEventID`. An attached rollout registration binds a source instance to
one file instance and one Codex session. It does not use CWD, a newest-file
choice, timestamp, or model text as identity.

The controller base state is `Active`, `BlockedExternal`,
`AwaitingAssignment`, or `Closed`. The controller is the only writer.
`BlockedExternal` uses an external-reference type that cannot hold a decision
identifier. Only the existing open-decision projection represents a human
decision block.

`WaitingDecision` is an effective delivery gate, not stored continuity state.
The controller reads the canonical decision projection at its recorded cursor.
It rechecks that gate immediately before delivery. An open decision cancels a
claim that has not started delivery. A decision resolution does not infer a
continuation prompt.

The reducer reads typed lifecycle input, base-state snapshot, decision gate,
claim record, and an injected clock. It returns `ClaimPrompt`, `Pause`,
`StatusRequest`, or `Escalate`. It performs no I/O and cannot receive model
output.

## Work lease and iteration policy

An authorized dispatcher, captain, or operator grants an `ActiveWorkLease`.
It binds the instance, work-scope reference, policy profile, grant time, and
absolute review deadline. The controller applies the chosen profile. It does
not choose one from a turn count or agent prose.

The profile contains three separate persisted bounds:

- delivery-attempt limit for one prompt claim;
- lifecycle source freshness deadline after an input expects a successor;
- continuation claim and time limit for one status epoch.

The status-epoch limit is not a lifetime prompt cap. A long-running crew renews
its epoch with `harmonik continuity report-active`. The typed request contains
lease identity, generation, crew identity, and an idempotency request ID. The
controller records the accepted renewal. It resets only the status epoch. It
does not reset delivery retry, source health, or the absolute review deadline.

At status-epoch exhaustion the controller sends one bounded `StatusRequest`.
It is not a continuation claim. If no accepted declaration arrives by the
profile deadline, the controller records `ContinuityReviewRequired`, pauses
new delivery, and escalates. It does not infer completed, blocked, or failed
work.

At the absolute review deadline the controller records the same review state,
even if the crew continues to report active. Only an authorized actor grants a
new lease.

An agent can request only `BlockedExternal`, `AwaitingAssignment`, or an active
renewal. The request enters through a typed daemon RPC. The controller validates
instance, generation, crew identity, authenticated process identity, request
shape, and authority. It never interprets a reference. An agent cannot close or
release itself when controller policy reserves release.

## One writer and durable ordering

One daemon-local `ContinuityController` serializes all operations per instance.
It is the sole writer of one versioned continuity record, claim journal, and
durable outbox. Together they are authoritative. The bus receives derived
cross-subsystem observations. It is never prompt-gating authority.

The record holds base state, generation, decision cursor, last accepted source
event, outstanding claim, attempt count, delivery stage, cooldown deadline,
status epoch, source expectation, review state, and lease deadline. Each source
event and request has an idempotency key. The controller commits a new record
version and matching journal entry before it exposes an outbox effect.

A base-state pause, review state, decision gate, or closed record cancels a
claim that has not begun delivery. It cannot retract a delivery already in
progress. The invariant is that no new delivery begins after the pause is
accepted.

### Attached tmux delivery

The continuity runtime declares a consumer-owned `ContinuityDeliveryPort`. The
attached implementation uses `internal/lifecycle/tmux.Adapter` internally. It
owns one payload paste, settle, and one Enter operation. It follows the
`PL-021d` temp-file, deterministic-buffer-owner, cleanup, and `daemon_pane_write`
audit contract. Continuity core never sees a pane target, buffer name, or tmux
command.

The controller records `PayloadMayBePasted` before the adapter begins a paste.
If a crash follows that record, recovery sends neither another paste nor Enter.
An Enter could submit unrelated pre-existing input when the paste never ran.
The controller records `DeliveryUncertain` and requires review. This prevents
duplicated, concatenated, or unrelated input. The controller records
`EnterMayBeSent` only after a typed successful paste result. An attached adapter
returns a typed handoff result, not model acceptance.

The attached contract is at-most-once payload paste and at-most-once Enter. A
crash before a paste or after an uncertain Enter can lose a prompt. That
produces delivery uncertainty and review rather than an unsafe blind retry.
Pane capture is never an acknowledgement source.

Structured Codex supplies the same delivery port through `handler.InputPort`.
The existing port has no controller effect ID or replay-safe idempotency
contract. Therefore a dispatch that is ambiguous after a crash becomes
`DeliveryUncertain`. It is not retried. The core imports neither adapter.

## Source and claim lifecycle

The first source is a registered attached Codex rollout codec. It validates
crew, generation, stable pane identity, Codex session, file instance, and
monotonic source event identity before it emits `TurnEnded`. It contains no
assistant-text decoder. Rotation or ambiguity fails closed and requires a
controller-owned rebind that increments generation.

A later source event can settle a continuation claim only if it has the same
instance, generation, and source identity and its `(turn ID, ordinal)` is
strictly after the source watermark captured with that claim's known delivery
result. The source codec emits a `TurnStarted` and `TurnEnded` pair for that
turn ID. It rejects a missing pair, duplicate, stale, or reordered event. A
registered interactive target has one controller input owner. An operator or
another system MUST pause continuity before it injects manual input. That pause
releases the lease and abandons every claim, including one awaiting a successor.
An authorized new lease is required before automation resumes. A prior turn can
never settle a claim.

Source freshness starts only after a delivered input expects a successor. A
long active turn does not false-stale. An accepted active renewal does not make
the source healthy by implication. A missed successor records source stale and
requires review. The controller does not prompt into an unknown source state.

Codex notify and App Server adapters later map supported lifecycle signals to
the same domain events. Claude context reset is outside this work.

## Controller close authority

A stopped turn never proves completion. A crew can report awaiting assignment.
The shared dispatch and git layer is the sole source of a work-complete fact
for a dispatched work scope. An authorized controller command can close a
continuity lease without asserting work completion. A `Closed` record includes
either `WorkTerminalFact` or `LeaseReleased`. Both cancel an undispatched claim.
Continuity is an interactive target protocol. It is outside the managed-run
`Harness` contract in this first vertical. A later managed-run integration must
amend that contract before it adds a harness seam.

## Exact controller contract

### Atomic unit and outbox

`ContinuityStore.Commit` is the only persistence operation. It compare-and-
swaps one versioned record envelope. The envelope contains the current record,
one transition journal entry, and all pending outbox effects. The controller
never writes these parts separately.

Each outbox effect has an `EffectID` built from instance identity, generation,
claim identity, purpose, and attempt. Its dispatch state is `Pending`,
`Dispatching`, `Acknowledged`, or `Failed`. A committed but undispatched effect
is retried after restart. A dispatching effect is recovered by its delivery
stage rule. Attached delivery observations acknowledge one exact `EffectID`.
The structured adapter persists its `InputPort` `Ack.Seq` and its bound run ID
before it accepts an `agent_input_acked` or `agent_input_stale` observation.
It maps only that exact `(run ID, input sequence)` tuple to the effect ID.

The authoritative record is the envelope. The journal is part of that envelope
for replay and audit. The event bus receives only derived observations.

### Decision linearization

The daemon feeds accepted decision changes and continuity inputs through one
ordered controller ingress. `DecisionGatePort.WithDeliveryFence` shares the
canonical decision-log append serialization. It folds the canonical log and
passes `{open, watermark}` to a controller callback while it holds the fence.
The controller alone compare-and-swaps its record and commits the first delivery
`May` stage with that watermark. On a record-version conflict it releases the
fence and retries the whole fold and controller CAS. The port adds no decision
state and never writes the controller record.

- A decision accepted before the first transport `May` stage commits cancels the claim.
- A decision accepted after that commit can let the in-flight effect finish.
- An accepted decision prevents every successor claim.

This is the controller linearization rule. The controller does not claim it can
undo a paste that began before a later decision.

### Delivery state machine

Every agent input uses an `InputClaim`. Its purpose is `Continue` or
`StatusRequest`. The generic claim stores its identity, purpose, effect ID,
attempt, and terminal outcome. Its delivery state is a sealed transport variant:

```text
Claimed -> AttachedTMUX | StructuredInputPort

AttachedTMUX:
  PayloadMayBePasted -> PayloadPasted -> SettleDue
  -> EnterMayBeSent -> EnterSent

StructuredInputPort:
  HandoffMayHaveRun -> HandoffKnown | HandoffRejected

Continue: EnterSent | HandoffKnown -> AwaitingSuccessor
AwaitingSuccessor -> Settled on matching source successor

StatusRequest: EnterSent | HandoffKnown -> AwaitingDeclaration
AwaitingDeclaration -> Settled on exact correlated typed declaration
AwaitingDeclaration -> ReviewRequired on status deadline

AwaitingSuccessor -> DeliveryUncertain | ReviewRequired
HandoffRejected -> ReviewRequired
```

The controller commits each `May` arrow before its effect. It commits a known
result only after the matching typed observation. The only shared stage rule is
that any transport-specific `May` state is ambiguous after a crash and becomes
`DeliveryUncertain`. The controller does not replay it.

For attached tmux, `PayloadMayBePasted` becomes `DeliveryUncertain` without an
Enter or a second payload paste. `PayloadPasted` means the adapter returned a
typed paste success. Only then can the controller commit `EnterMayBeSent`.

`EnterMayBeSent` is ambiguous after crash. For an attached tmux adapter it
becomes `DeliveryUncertain` without a second keypress. For structured input,
the controller commits `HandoffMayHaveRun` before it calls `SubmitInput`. A
crash in that state becomes `DeliveryUncertain`. A typed result commits
`HandoffKnown` only for `Ack{Delivered}`. `Ack{Rejected}` commits
`HandoffRejected`, starts no successor wait, and requires review. The adapter
buffers or replays an `agent_input_acked` or `agent_input_stale` event that
arrives before the controller commits its exact `(run ID, input sequence)`
binding. It applies that event only after the binding exists. The existing
`InputPort` cannot retry until its contract carries a controller effect ID and
defines durable idempotent replay. An attached
`EnterSent` or a structured `HandoffKnown` starts `AwaitingSuccessor` for a
continuation claim. A matching typed successor settles that claim. It starts
`AwaitingDeclaration` for a status request. Only an exact correlated typed
declaration settles that claim. No source event settles a status request.

`ContinuityDeliveryPort` executes typed delivery steps. Its attached adapter
owns the tmux implementation. Its structured adapter maps one claim to
`handler.InputPort`. It does not claim atomicity, idempotent replay, paste, or
Enter stages that the structured transport cannot observe.

The port owns continuity claim staging and recovery only. It does not define a
second acceptance meaning. The structured adapter delegates handoff to
`handler.InputPort` and maps its `Ack`, `agent_input_acked`, and
`agent_input_stale` observations into the claim state. The attached adapter
maps only honest tmux handoff stages and never scrapes a pane as evidence.

### Budget rules

The delivery-attempt count belongs to one input claim. The first vertical
permits one attempt only for every transport. Attached tmux delivery has one
paste and one Enter attempt only. A later transport may use a larger bound only
after its own contract defines controller effect-ID idempotency and durable
correlation. A failed continuation does not consume the status epoch. A status
request has its own input claim and delivery budget.

The status epoch counts settled continuation cycles without an accepted active
renewal. It starts with a granted or renewed lease. It increments only when a
matching stopped-turn successor settles a continuation claim. It resets only on
an accepted `report-active` request or a controller lease action.

Source freshness starts when `EnterSent` or a known structured handoff result
commits and a successor is expected. It clears only on a matching typed
lifecycle event. It does not run during a known active turn.

Precedence is closed lease or accepted pause, then open decision, then delivery
uncertainty, then source stale, then absolute lease expiry, then status request
deadline, then status epoch limit. Ties choose the earlier persisted event ID.

An outstanding `StatusRequest` has its own input-claim ID. Its deadline starts
only when it enters `AwaitingDeclaration`. A declaration can satisfy it only
when it names that ID and matches instance, generation, lease, crew capability,
and request identity. An ordinary active renewal is rejected while a status
request is outstanding unless it carries that correlation. A turn-ended event
never settles a status request.

### Capability and close fact

Attach or launch creates a random capability bound to instance, generation, and
crew identity. It is made available only to the managed agent environment. The
continuity CLI sends it with every declaration request. The daemon validates it
with the Unix peer identity and rejects stale, missing, or mismatched requests.

The authorized `harmonik continuity close` controller command records an
idempotent `LeaseReleased` fact that names the work scope and lease. It does not
assert work completion. A future dispatcher integration can supply a
`WorkTerminalFact` through a `TerminalFactPort`, sourced by the shared git and
dispatch layer. Either close fact wins a pending undispatched claim.

## Specification changes

Add `specs/continuity-keeper.md`. It defines identity, authority, work lease,
policy profile, delivery stages, source contract, controller record, and
conformance behavior. It depends on the existing event model, lifecycle,
handler, input, and decision contracts. The first vertical does not amend those
specifications. It uses their existing boundaries and emits derived controller
observations only.

## Test gate

- L0 tests constructors and every reducer state-event pair.
- L1 replays duplicate, delayed, reordered, truncated, stale, and rotated
  source events. It covers every stated crash cut.
- L2 uses a fake `ContinuityDeliveryPort`. It proves claim ordering, bounded
  retries, and no new delivery after a pause.
- L3 uses an owned tmux fixture. It proves paste, settle, and Enter as one
  attached delivery operation.
- L4 runs one capped Codex canary. It observes only typed lifecycle or notify
  progress after delivery.

The corpus includes normal long work with active renewal, decision wait,
awaiting assignment, prose-only block, lost Enter, paste-success Enter-unknown,
crash before and after Enter dispatch with no second keypress,
crash-after-`SubmitInput`-before-commit with no retry, stale and reordered
acknowledgements from a prior claim or generation, source rotation, source
silence, operator steer,
decision opening between claim and delivery, wrong-claim source event, repeated
active reports at lease expiry, exact and wrong-correlated status declarations,
missed status request deadline, pause during an active effect, and closed lease
with a pending claim.

Each tier has a deliberate failing mutation before it gates work.
