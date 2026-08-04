# Implementation tasks

## T1 — finalize the normative contract

Spec traceability: `continuity-keeper.md` CK-001 through CK-015.

Copy the approved continuity specification and registry reservation into
`specs/`. Keep dependent specifications unchanged. Do not include POC shell
code.

Acceptance: registry lint accepts `CK`.

Depends on: none.

## T2 — build the harness-neutral continuity domain

Spec traceability: CK-001, CK-004 through CK-008, CK-011, CK-014.

Create `internal/continuity` for opaque identifiers, base and effective state,
lease profile, claims, sealed delivery variants, typed inputs, declarations,
and the pure reducer. Inject time. Do not import tmux, Codex, filesystem,
daemon, or edge adapters.

Add L0 table tests for every state-event pair. Cover status count and elapsed
limits, the decision gate, close, declaration correlation, rejection, and
manual pause. Add a failing reducer mutation test.

Acceptance: package tests pass. An import test or depguard rule proves the core
cannot import harness edges.

Depends on: T1.

## T3 — durable controller record, journal, and outbox

Spec traceability: CK-003, CK-005, CK-005a, CK-008, CK-012 through CK-014.

Build the runtime outside the pure domain. It owns one versioned envelope per
instance. The envelope contains record, journal, and outbox. Use atomic durable
writes and compare-and-swap semantics. Define storage, clock, decision-fence,
source, and delivery ports.

Implement `DecisionGatePort.WithDeliveryFence`. It holds canonical decision
append serialization while the controller performs its own conditional commit.
A conflict retries the full fold and conditional commit. The port never writes
continuity state.

Add L1 replay tests for duplicate, stale, reordered, truncated, and rotated
inputs. Test every crash cut and recovery precedence. Test the delivery fence:
a decision before the first `May` commit cancels, a decision after it permits
only the in-flight effect, and no successor follows. Test source silence, lease
expiry, status-deadline escalation, exact and wrong status declaration, typed
external block, awaiting assignment, and close during a pending claim. Add a
failing replay mutation.

Acceptance: replay gives the same record and pending effect set. A pause,
close, or decision gate prevents new delivery. An architecture test proves the
controller, record, and journal packages also have no Codex, tmux, source-codec,
or delivery-adapter dependency.

Depends on: T2.

## T4 — controller commands and typed declarations

Spec traceability: CK-004, CK-006, CK-012, CK-013.

Add `harmonik continuity` operations for attach, status, report-active,
block-external, await-assignment, pause, and close. Attach creates an
interactive instance and authorized lease. Close writes `LeaseReleased` only.
Bind a per-instance capability to peer identity, generation, and crew.

Acceptance: wrong generation, capability, status claim, and self-close calls
have no state effect. A controller-owned input capability test proves that only
the registered delivery adapter can dispatch a claim. Pause revokes that right
and abandons every claim.

Depends on: T3.

## T5 — attached Codex lifecycle source

Spec traceability: CK-001, CK-002, CK-011, CK-015.

Create a registered attached-session source adapter. It decodes only stable
Codex session and turn lifecycle fields. It emits no assistant text. It binds
source instance, pane, session, generation, turn ID, and ordinal. It requires a
valid `TurnStarted` and `TurnEnded` pair.

Acceptance: tests reject ambiguous source selection, rotation, wrong session,
prior turn, duplicate, stale, and reordered events. A source payload test proves
no assistant body enters the domain.

Depends on: T3.

## T6 — attached tmux delivery adapter

Spec traceability: CK-009, CK-014, CK-015.

Implement the attached `ContinuityDeliveryPort` through
`internal/lifecycle/tmux.Adapter`. Reuse `PL-021d` temp-file, deterministic
buffer owner, cleanup, and `daemon_pane_write` audit behavior. Keep tmux targets
and command strings outside the domain and controller.

Implement `PayloadMayBePasted`, `PayloadPasted`, `SettleDue`,
`EnterMayBeSent`, and `EnterSent`. A crash in either `May` state becomes
delivery uncertainty. It sends no second blind paste or Enter.

Acceptance: L2 fake-port tests prove ordering and pause revocation before an
external dispatch. A failing L2 mutation proves no delivery follows a revoked
claim. L3 owned-tmux tests cover each crash cut. A mutation that retries Enter
after `EnterMayBeSent` fails.

Depends on: T3, T4.

## T7 — structured input adapter

Spec traceability: CK-008, CK-010, CK-011, CK-015.

Implement the `handler.InputPort` adapter. Persist `(run ID, Ack.Seq)` before
accepting input events. Buffer or replay early `agent_input_acked` and
`agent_input_stale` events until binding exists. Map only exact tuples.

Model `HandoffMayHaveRun`, `HandoffKnown`, and `HandoffRejected`. A crash in
the `May` state is delivery uncertainty. Rejected input starts no successor
wait. Do not add an effect-ID retry claim to `InputPort`.

Acceptance: tests cover early event, stale tuple, wrong generation, rejected
input, and crash after `SubmitInput`. No ambiguous input retries.

Depends on: T3.

## T8 — compose interactive Codex vertical

Spec traceability: CK-001 through CK-015.

Wire controller, decision fence, attached source, attached delivery, and
commands in the daemon composition root. Apply it only to registered interactive
targets. Do not modify `internal/keeper.Watcher` or add a managed-run harness
branch.

Register attached tmux and structured `InputPort` delivery adapters behind a
typed transport selection. The first canary selects attached tmux. The composed
fixture also selects structured input so T7 is production-composed code, not a
dead helper.

Add startup recovery. Re-arm only safe pending delivery. Escalate every
ambiguous delivery state.

Acceptance: an integration fixture runs typed turn stops, decision wait,
external block, awaiting assignment, status request and deadline, source
silence, manual pause, and lease release. It uses no assistant-text fixture
field.

Depends on: T4, T5, T6, T7.

## T9 — scenario gate

Bead: `hk-ssifd`.

Spec traceability: CK-015.

Run the owned attached Codex scenario. Verify the journal, decision gate, and
close behavior named in the bead.

Depends on: T8.

## T10 — exploratory operator gate

Bead: `hk-656tk`.

Spec traceability: CK-004, CK-006, CK-013, CK-015.

Exercise attach, status, and close as an operator. Verify close is not work
completion and leaves no active claim.

Depends on: T8.

## T11 — capped live Codex canary

Spec traceability: CK-015.

Run one registered interactive Codex target with an external test-run time and
trial bound. That bound stops the canary runner only. It MUST NOT become a
controller lifetime continuation cap. The controller uses only per-claim
delivery attempts, status-epoch count and elapsed limits, a status-declaration
deadline, and an absolute lease review deadline. Observe typed lifecycle,
journal, and controller state only. Release the lease on any review state.

Acceptance: the run advances through typed successor events or stops in a typed
safe state. No second blind tmux action occurs. A deliberate L4 canary mutation
that permits an ambiguous retry must fail before the canary gate is accepted.

Depends on: T9, T10.

## Dependency graph

```text
T1 -> T2 -> T3 -> T4 -> T6 -> T8 -> T9 -> T11
                   |                 -> T10 -> T11
                   +-> T5 -----------^
                   +-> T7 -----------^
```

## Parallel work

After T3, T4, T5, and T7 can proceed in parallel. T6 starts after T4 because
it needs the target contract. T8 waits for T4 through T7. The test gates wait
for the composed vertical. T7 is required before this work is ready.
