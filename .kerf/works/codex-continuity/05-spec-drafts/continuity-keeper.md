# Continuity Keeper

```yaml
---
title: Continuity Keeper
spec-id: continuity-keeper
requirement-prefix: CK
status: draft
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.1.0
spec-template-version: 1.1
owner: continuity-keeper-author
last-updated: 2026-08-02
depends-on:
  - agent-input
  - event-model
  - handler-contract
  - hitl-decisions
  - process-lifecycle
---
```

## 1. Purpose

This spec defines the continuity keeper. The keeper continues assigned crew
work between typed lifecycle stops. It does not decide what agent prose means.
It does not decide that work is complete.

The keeper has one controller-owned record for each registered crew instance.
It receives typed lifecycle events and typed agent declarations. It can issue a
typed input claim through a harness delivery adapter. It pauses and asks for
review when its evidence is incomplete.

## 2. Scope

### 2.1 In scope

- Instance identity, source registration, generation, work lease, and authority.
- Base work state, decision gate, review state, and terminal fact.
- Typed lifecycle input and typed agent declarations.
- A pure policy reducer, one durable controller record, journal, and outbox.
- Transport-specific delivery states for attached tmux and structured input.
- Lease, source, delivery, and status-epoch limits.
- Replay, crash recovery, and conformance cases.

### 2.2 Out of scope

- Claude context compaction, handoff, `/clear`, and session resume.
- Agent output parsing, pane scraping, or completion inference.
- A decision service. The existing decision projection remains its owner.
- Selection of the next work item.
- A terminal fact emitted by a crew.
- A retryable structured input transport. It needs a later effect-ID contract.
- Managed dispatch runs. This first vertical applies only to registered
  interactive targets outside the `Harness` contract.

## 3. Glossary

- **Instance**: one registered continuity target with an opaque instance ID.
- **Generation**: a monotonic binding version for one instance.
- **Work lease**: an authorized grant for one work scope until a review deadline.
- **Base state**: `Active`, `BlockedExternal`, `AwaitingAssignment`, or `Closed`.
- **Decision gate**: a pause derived from the canonical open-decision projection.
- **Claim**: one controller-owned continuation or status input request.
- **May state**: a delivery state where an external action may have happened.
- **Causal successor**: a lifecycle event that names one delivered claim.
- **Status epoch**: a bounded group of settled continuation cycles.

## 4. Normative requirements

### CK-001 — typed cognitive boundary

The keeper MUST NOT receive, store, parse, classify, search, or branch on
assistant prose. Its core types MUST contain only typed identities, references,
state, timestamps, and fixed enums. A source adapter MUST remove assistant body
content before it emits lifecycle input.

Tags: safety
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-002 — exact source binding

A source registration MUST bind an instance and generation to crew identity,
harness session identity, stable target identity, source instance identity, and
source event identity rules. It MUST NOT select a source from current directory,
newest file, timestamp, or text.

Ambiguous, rotated, stale, duplicate, or reordered input MUST NOT create a new
claim. A source rebind MUST be an authorized controller action and MUST create
a new generation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-003 — single writer

One `ContinuityController` MUST serialize operations for one instance. It MUST
be the sole writer of the versioned record, transition journal, and outbox. A
commit MUST compare-and-swap the record version and persist the new record,
journal entry, and pending effects as one unit.

The event bus MAY receive derived observations. It MUST NOT decide prompt
delivery or become the authoritative record.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-003a — harness-neutral core

The continuity reducer, record, journal, and controller types MUST be
harness-neutral. They MUST NOT import or depend on Codex, tmux, source codecs,
or delivery adapters. Only registered source and delivery edge adapters MAY
depend on a harness implementation.

Tags: structure
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-004 — authority and base state

The controller MUST represent exactly these base states: `Active`,
`BlockedExternal`, `AwaitingAssignment`, and `Closed`.

Only an authorized controller actor MAY grant a work lease, rebind a source, or
record `Closed`. A crew MUST NOT record `Closed` or release its own lease.
A crew MAY request `BlockedExternal`, `AwaitingAssignment`, or an active renewal
through a typed authenticated request.

`BlockedExternal` MUST contain an external reference type that cannot contain a
decision ID.

Tags: safety
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-005a — decision delivery fence

`DecisionGatePort.WithDeliveryFence` MUST share the canonical decision-log
append serialization. It MUST fold the canonical decision log and pass
`{open, watermark}` to a controller callback while it holds the fence. The
controller alone MUST compare-and-swap its record and commit the first delivery
`May` state with that watermark. On a record-version conflict, it MUST release
the fence and retry the fold and controller compare-and-swap. The port MUST NOT
write the controller record or create a second decision projection or state.

Tags: safety
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-005 — decision gate

The controller MUST read the canonical open-decision projection at a durable
cursor. An open decision MUST form an effective `WaitingDecision` gate. The
gate MUST NOT be stored as a continuity base state.

The controller MUST recheck the gate before the first delivery `May` state. An
open decision before that commit cancels an undispatched claim. An open decision
after that commit MAY let that in-flight effect finish. It MUST prevent all
successor claims. Resolving a decision MUST NOT infer a continuation request.

Tags: safety
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-006 — work lease and status epoch

An authorized actor MUST grant an `ActiveWorkLease` before the controller can
issue a continuation. The lease MUST name the instance, generation, crew,
work-scope reference, profile, grant time, and absolute review deadline.

The profile MUST persist separate bounds for one claim delivery, source
freshness, continuation count in one status epoch, elapsed time in one status
epoch, and a status-declaration deadline. It MUST NOT use one lifetime
continuation cap.
An accepted `report-active` request resets only the status epoch. It MUST NOT
reset the lease deadline, source freshness, or claim delivery state.

Tags: policy
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-007 — pure policy

The policy reducer MUST accept only typed input, record state, decision-gate
state, and an injected clock. It MUST return typed actions only:
`ClaimPrompt`, `Pause`, `StatusRequest`, or `Escalate`. It MUST perform no I/O.
It MUST NOT receive model output.

At a status-epoch limit, the controller MUST issue one `StatusRequest`. It MUST
pause and escalate if no accepted correlated declaration arrives by the status
deadline. It MUST NOT infer completion, blockage, or failure.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-008 — claim identity and outbox

Each claim MUST have a claim ID, purpose (`Continue` or `StatusRequest`),
attempt number, and effect ID. An effect ID MUST derive from instance,
generation, claim, purpose, and attempt.

An outbox effect MUST be `Pending`, `Dispatching`, `Acknowledged`, or `Failed`.
A committed `Pending` effect MAY be dispatched after restart. A `Dispatching`
effect MUST recover by its transport state. An attached delivery observation
MUST name the exact effect ID that it acknowledges. A structured adapter MUST
persist the bound run ID and `Ack.Seq`. It MUST map `agent_input_acked` or
`agent_input_stale` only when its `(run ID, input sequence)` exactly matches.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-009 — attached tmux delivery

An attached tmux delivery adapter MUST own all tmux mechanics. The core MUST
NOT receive a pane target, buffer name, shell command, or pane text. The
adapter MUST use `internal/lifecycle/tmux.Adapter` and follow `PL-021d`
temp-file, deterministic-buffer-owner, cleanup, and `daemon_pane_write` audit
rules.

The attached claim state MUST be:

```text
PayloadMayBePasted -> PayloadPasted -> SettleDue
-> EnterMayBeSent -> EnterSent
```

The controller MUST commit each `May` state before its matching external action.
It MUST commit `PayloadPasted` and `EnterSent` only after their typed delivery
observations. Recovery from `PayloadMayBePasted` MUST send neither a second
paste nor Enter. Recovery from `EnterMayBeSent` MUST send no second Enter.
Either case MUST become `DeliveryUncertain` and require review. Pane capture
MUST NOT acknowledge input.

Tags: safety
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### CK-010 — structured input delivery

The structured adapter MUST map one claim to `handler.InputPort`. Its claim
state MUST be:

```text
HandoffMayHaveRun -> HandoffKnown | HandoffRejected
HandoffRejected -> ReviewRequired
```

The controller MUST commit `HandoffMayHaveRun` before it calls `SubmitInput`.
If a crash leaves that call ambiguous, the controller MUST record
`DeliveryUncertain` and MUST NOT retry it. `Ack{Delivered}` MAY commit
`HandoffKnown`. `Ack{Rejected}` MUST commit `HandoffRejected`, start no
successor wait, and require review. The adapter MUST buffer or replay an early
`agent_input_acked` or `agent_input_stale` observation until it persists the
matching `(run ID, input sequence)` binding. It MAY update only that exact
pending claim.

The structured adapter MUST NOT claim paste, Enter, atomicity, or idempotent
replay that `InputPort` does not expose. A later transport may retry only after
it defines a durable controller effect ID and idempotent replay contract.

Tags: safety
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### CK-011 — purpose-specific settlement

After a known delivery result, a `Continue` claim MUST enter
`AwaitingSuccessor`. It MAY settle only from a typed source successor with the
same instance, generation, and source identity and a `(turn ID, ordinal)`
strictly after the claim source watermark. The source MUST have a valid
`TurnStarted` and `TurnEnded` pair for that turn ID. A prior, missing, duplicate,
stale, or reordered turn MUST NOT settle a claim.

A registered interactive target MUST have one controller input owner. An
operator or other system MUST pause continuity before manual input injection.
That pause MUST release the lease and abandon every claim, including one in
`AwaitingSuccessor`. An authorized new lease is required before automation
resumes.

After a known delivery result, a `StatusRequest` claim MUST enter
`AwaitingDeclaration`. It MAY settle only from an exact correlated typed
declaration. Its status deadline MUST begin on entry to `AwaitingDeclaration`.
A source event MUST NOT settle a `StatusRequest` claim.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-012 — declaration correlation

Every crew declaration MUST name its instance, generation, lease, crew
capability, request ID, and, when one is outstanding, status-request claim ID.
The controller MUST validate process identity, authority, shape, and all named
identity values. It MUST NOT interpret an external reference.

An ordinary active renewal while a status request is outstanding MUST be
rejected unless it names that exact status-request claim. A stale declaration,
wrong generation, wrong claim, or wrong capability MUST NOT settle or change a
claim.

Tags: safety
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-013 — closed lease and work-terminal fact

A stopped turn, process stop, or absent source event MUST NOT prove work
completion. For a dispatched work scope, the shared git and dispatch layer MUST
be the only source of a `WorkTerminalFact`. An authorized controller command
MAY write `LeaseReleased`, which closes continuity but MUST NOT assert work
completion. The controller MUST record either close fact idempotently and MUST
cancel any claim that has not begun delivery.

A lifecycle `TurnEnded` event MUST NOT be a handler terminal event,
`outcome_emitted` event, process-exit signal, or harness completion signal.

Tags: safety
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-014 — recovery precedence

The controller MUST apply this precedence: closed lease or accepted pause; open
decision; delivery uncertainty; source stale; absolute lease expiry; status
deadline; status epoch limit. Equal cases MUST use the earlier persisted event
ID. A known active turn MUST not become source stale.

Tags: policy
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### CK-015 — conformance cases

The implementation MUST replay and test at least these cases:

| Case | Required result |
|---|---|
| Long valid work with renewals | No lifetime prompt cap ends the lease. |
| Open decision | No new delivery occurs. |
| Typed external block | Base state pauses without prose parsing. |
| Awaiting assignment | No continuation occurs until an authorized lease action. |
| Crash before or after tmux paste or Enter | No second blind paste or Enter occurs. |
| Crash after `SubmitInput` before commit | No structured retry occurs. |
| Structured rejection or early input event | No successor wait starts from rejection. Early events replay only after their exact tuple binds. |
| Wrong or stale source event | It cannot settle or create a claim. |
| Wrong status declaration | It cannot settle `AwaitingDeclaration`. |
| Missed status deadline | Controller records review and escalates. |
| Source rotation or silence | Controller fails closed and requires review. |
| Closed lease during pending claim | No new delivery begins. |
| Manual pause during successor wait | The claim is abandoned and a later manual turn cannot settle it. |

The test suite MUST include these gates:

- L0 reducer and constructor tests for every state-event pair.
- L1 durable replay tests for duplicate, stale, reordered, truncated, and
  rotated inputs.
- L2 controller tests with a fake delivery port.
- L3 an owned attached-tmux fixture.
- L4 one capped live Codex canary that observes typed lifecycle progress only.

Each gate MUST demonstrate one relevant failing mutation before it is accepted.

Tags: conformance
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
