# Components: review-loop decoupling

## Decision: amend existing owners; do not create a review-loop subsystem spec

`workflow_mode=review-loop` is already normatively owned by `specs/execution-model.md` EM-015d/e.
This work changes internal decomposition and conformance coverage, not the product model, event vocabulary,
or actor/process geometry.

A dedicated runtime-subsystem spec would duplicate EM ownership and trigger the subsystem-envelope and
Go-package obligations of architecture AR-052, AR-053, and AR-016. Pure functions, ports, packages, and
reducers are implementation design, not normative system behavior.

`specs/architecture.md` remains a cited guardrail and is not amended.

## C1 — Review-cycle semantics

Primary owner: `specs/execution-model.md`

Affected surface:

- EM-015d and EM-015e;
- `Run.context` review-loop fields and completion-reason grammar;
- execution-model conformance obligations.

Requirements after the change:

- Canonical, complete semantics cover iteration initialization/increment, verdict routing,
  no-progress detection, cap handling, needs-attention outcomes, and terminal completion reasons.
- Those semantics remain authoritative over any pure implementation projection.
- Session selection/resume/freshness and the persisted context baseline remain explicit.
- Conformance tests exhaustively cover the decision table and current result/payload mapping.

Dependencies:

- event-model review-loop schemas/order;
- handler and Claude launch/session facts;
- workspace verdict artifacts;
- existing control-point budget vocabulary where applicable.

Goals covered: deterministic/testable decisions, behavior parity, eventual mechanical relocation.

## C2 — Handler session and observer lifecycle

Primary owner: `specs/handler-contract.md`

Affected surface:

- handler lifecycle watcher, progress stream, ready, cancellation and heartbeat requirements;
- handler capability/session-ID negotiation;
- handler conformance obligations.

Requirements after the change:

- Ready remains a prerequisite for live input delivery.
- The authoritative handler lifecycle watcher remains exactly one per session.
- Handler- and daemon-emitted heartbeat remain equivalent for session liveness per current HC-057; this
  work does not add a wire distinction.

Dependencies: process-lifecycle resource ownership; agent-input substrate contract for delivery.

Goals covered: bounded watcher lifetime, no cross-phase callback leakage, lifecycle truthfulness.

## C3 — Review artifacts and placement

Primary owner: `specs/workspace-model.md`

Affected surface:

- review-target, review verdict, archived verdict, feedback and budget-artifact paths;
- local/remote worktree and branch-transfer semantics;
- workspace conformance obligations.

Research required before amendment:

- Current specs place review artifacts in “the run's worktree.”
- Current implementation appears to execute the reviewer and read its control artifacts on box A even
  when the implementer workspace is remote.
- Determine whether box A owns a reviewer control workspace/mirror, or whether current implementation
  diverges from the normative run-worktree contract.

Requirements after evidence resolves the model:

- Every artifact operation has an explicit location; nullable-runner inference is not normative.
- Implementer work-product operations and reviewer-control artifacts cannot be accidentally executed in
  the other's location.
- Branch transfer is explicit.
- Workspace removal cannot precede observer/session completion.
- A local/remote conformance matrix covers the chosen current behavior.

Dependencies: C1 review-cycle phase facts; C4 process lifetime.

Goals covered: explicit placement and safe cleanup.

## C4 — Interactive process and phase lifetime

Primary owner: `specs/process-lifecycle.md`

Affected surface:

- subprocess/tmux parentage and teardown;
- composition-root wiring;
- cancellation, kill/reap and orphan behavior;
- process-lifecycle conformance obligations.

Requirements after the change:

- A phase cannot be considered complete while a live process, session, callback, or observer still depends
  on its workspace, unless ownership is explicitly transferred.
- Auxiliary commit, verdict, artifact, and budget observers are distinguished from the authoritative
  handler watcher and have bounded cancellation plus completion/join before dependent teardown.
- Callback unregistration prevents late delivery into a later phase.
- Teardown is bounded, ordered, and idempotent.
- Watchers have cancel/join semantics and do not outlive destroyed panes or removed worktrees.
- Existing provenance and bounded-kill requirements remain unchanged.
- No new process class or tmux policy is introduced.

Dependencies: C2 handler-session lifecycle; C3 workspace placement.

Goals covered: phase-owned imperative shells and leak-free cleanup.

## C5 — Session checkpoint and protocol acceptance

Primary sequence owner: `specs/execution-model.md`, citing existing handler-contract and
Claude hook/launch/session specifications.

Affected surface:

- EM-015d captured-session clause and durable context baseline;
- existing handler capability and version-selection ACK requirements;
- existing CHB-023 checkpoint-before-connection-accept contract.

Research required:

- Confirm the exact current error/ACK matrix rather than “fixing” a discrepancy during refactor.
- Confirm how the persisted context commit SHA becomes the no-commit comparison baseline.
- Confirm Captured versus Minted session policies and remote behavior.

Requirements after the change:

- One authoritative ordering sequence is cited consistently:
  capture/receipt → durable context checkpoint/baseline → connection-accept ACK.
- No additional event-publication stage is inserted unless research finds an existing normative owner
  requiring it.
- The persisted checkpoint result remains available to the no-commit guard.
- Failure and remote behavior remain unchanged unless separately amended.

Dependencies: existing execution-model durability, handler wire contract and CHB-023.

Goals covered: cohesive transaction boundary and ACK/baseline correctness.

## C6 — Event truth and ordering

Primary owner: `specs/event-model.md`

Affected surface:

- existing review-loop event schemas and ordering;
- event conformance traces.

Requirements after the change:

- Existing event names, payloads, identities, durability and causal order remain authoritative.
- Conformance traces cover launch, ready, delivery, phase completion, verdict, no-progress/cap and cycle
  completion.
- Logical reviewer-launch identity and actual handler-session identity remain distinct if current specs
  intend them to be; the refactor does not unify them mechanically.
- Add no new event type unless research proves an observable lifecycle fact lacks representation.

Dependencies: C1 decision table; C5 session ordering.

Goals covered: observable parity.

## C7 — Reviewer budget and activity evidence

Conditional owner: `specs/control-points.md` only if research confirms the reviewer budget is a
ControlPoint budget; otherwise retain ownership in the review-loop execution amendment.

Affected surface:

- reviewer allowance/activity-reset semantics;
- conformance tests for liveness versus work activity.

Research required:

- Determine whether the current reviewer budget is CP state or daemon artifact/watchdog policy.
- Characterize which tap events currently extend the allowance.

Requirement if an amendment is needed:

- Heartbeat events, regardless of handler or daemon emitter, are session-liveness evidence and are not by
  themselves reviewer-work activity for an activity-reset allowance.

This preserves HC-057's heartbeat equivalence and avoids a new event payload discriminator.

Goals covered: truthful liveness and deterministic budget outcome.

## Existing specs cited but not amended

- `specs/architecture.md`: dependency direction, composition root, mechanism/cognition boundary.
- agent-input substrate spec: authoritative live-input delivery and ACK surface.
- Claude launch/session specs: Captured/Minted session policy.
- Claude hook bridge: handler capabilities and CHB-023 durability.

## Dependency order

1. Characterize C3 locality, C5 checkpoint behavior, C2/C4 cleanup ownership, and C7 activity evidence.
2. Consolidate C1's existing semantics and conformance table.
3. Land only evidence-backed C2–C7 amendments; use citations where requirements already exist.
4. Implement pure policy projection and ordered trace oracle.
5. Implement owner APIs/adapters under the existing contracts.
6. Split phase orchestration in place.
7. Retry L8 only after behavioral parity and dependency gates pass.

## Implementation-only acceptance criteria

These belong in kerf design/tasks rather than normative specs:

- zero daemon-private undefined symbols in the relocation probe;
- `internal/runloop` does not import daemon;
- port/interface method counts and package layout;
- freeze/depguard ratchets;
- worktree/commit sequencing.

## Scope-creep guards

- No new review-loop subsystem spec.
- No new actor/process class or controller geometry.
- No generic runtime/services port.
- No DOT conversion, LIFT L12/L13, or full reducer/effect interpreter.
- No altered event identity/order, ACK timing, remote persistence, kill/watchdog timing, routing, budget
  threshold, or iteration cap without a separately reviewed amendment.
