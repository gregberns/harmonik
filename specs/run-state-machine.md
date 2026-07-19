# Run State Machine

```yaml
---
title: Run State Machine
spec-id: run-state-machine
requirement-prefix: RSM
status: draft
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.2.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-07-14
depends-on:
  - replay-substrate
  - event-model
  - process-lifecycle
  - handler-contract
  - queue-model
  - agent-input
---
```

## 1. Purpose

This spec defines the normative contract for the harmonik daemon's **per-bead run lifecycle**
— the logic historically carried by the `beadRunOne` god-function. It is normative for: the
consumer-owned ports the run lifecycle runs over; the two pure `Step(state, event) → (state,
[]action)` reactors (a per-session **Dispatch** machine and a per-run **Run** machine) that
express the lifecycle; the explicit **merge queue** that serialises only the merge critical
section; the single factored **terminal spine**; and the **bounded-liveness** invariants that
forbid a resumed run from hanging silently. It is the second production instantiation of the
replay-substrate seam ([replay-substrate.md §1]) and the daemon peer of the session-keeper
reactor ([session-keeper.md §1]); it consumes — and does not redefine — the agent-input
`InputPort`/`Ack` seam ([agent-input.md] AIS-001/AIS-003/AIS-004).

## 2. Scope

### 2.1 In scope
- The run-lifecycle **ports** (LedgerPort, EmitterPort, WorktreePort, MergePort, LaunchPort,
  ClockPort, WorkerPort, GatePort) as consumer-owned narrow interfaces.
- The pure **Dispatch** and **Run** reactors: their named states, event vocabulary, action
  vocabulary, and total transition tables.
- The **ClockPort** determinism seam across the run path.
- The **merge queue** and the merge critical section it serialises.
- The **terminal spine** (the factored launch→gate→merge→close tail) and elimination of the
  `runSucceeded` out-parameter.
- The **bounded-liveness** invariants (the resume-hang fix), including the normative home of
  the `run_stale` event.
- Depguard enforcement of the `runexec` / `mergequeue` package boundaries.

### 2.2 Out of scope
- The full daemon package decomposition (the ≥8-subsystem breakup) — that is a later work;
  this spec carves the run-lifecycle + merge boundary only.
- The agent-input channel itself — owned by [agent-input.md]; this spec CONSUMES its
  `InputPort.SubmitInput` / `Ack` contract (§9).
- The remote worker-resident execution interface — a later work depends on this spec's merge
  queue but owns its own execution seam.
- Interactive-operator-session input and the keeper/CLI paste carve-out ([agent-input.md]
  AIS-012).
- Terminal bead-transition ownership — the daemon (not this machine) owns close/reopen ledger
  writes; this spec factors their invocation, it does not move ownership
  ([beads-integration.md §4.4]).

## 3. The run-lifecycle reactors

### 3.1 Structure
**RSM-001.** The run lifecycle MUST be expressed as two pure reactors in a package that MUST
NOT import the flat `internal/daemon` package: a **Dispatch** machine (one instance per agent
session) and a **Run** machine (one instance per bead run). Each reactor's `Step` MUST be a
total function of `(state, event)` returning `(state, []action)` with no I/O, no clock reads,
and no identifier minting; every timestamp in state MUST derive from an event's stamped `At`.

**RSM-002.** A daemon **shell** (in `internal/daemon`) MUST own all effects: it samples I/O
into events, executes actions via an effector, owns the ClockPort and the timer deadlines, and
drives each reactor to a terminal. The former `beadRunOne` MUST become a thin driver that
constructs the reactors and runs the shell loop.

**RSM-003.** Every `(state, event)` pair MUST have a defined transition; pairs with no
semantic effect MUST be explicit no-ops. Each reactor MUST reach exactly one terminal state per
instance, and terminal states MUST have no outgoing transitions.

### 3.2 The Dispatch machine
**RSM-004.** The Dispatch machine MUST express one agent session as the states
`Idle → Launching → AwaitingReady → Briefing → Working → {Completed | Exited | Stalled |
ReadyTimeout→Failed | Failed | Aborted}`. A session launched under a completion-by-process-exit
harness (no readiness handshake) MUST transition `Launching → Working` directly, skipping
`AwaitingReady` and `Briefing`.

**RSM-005.** In `AwaitingReady`, expiry of the `agent_ready` timer MUST transition to
`ReadyTimeout` with an outgoing action set (kill + reap-timer + `agent_ready_timeout` emission)
— it MUST NOT be a silent wait. Readiness MUST be signalled by an `agent_ready` event carrying
the run identifier, on both first launch and resume. The `agent_ready` timer is armed at the
`Idle → Launching` entry and stays live across `Launching`; a hung launch that never yields a
`launched` or `launch_failed` event (e.g. `tmux_new_window_timeout`) and lets that deadline
expire in `Launching` MUST ride the SAME `ReadyTimeout` edge (kill + reap-timer +
`agent_ready_timeout` emission), never a silent wait (RSM-INV-002). This closes the SR9
silent-wedge on the launch phase as well as `AwaitingReady`.

**RSM-006.** In `Working`, an `agent_heartbeat` event (daemon-goroutine liveness) MUST NOT be
treated as agent progress. Only agent-derived signals — a run-attributed `agent_ready`, an
observed worktree-HEAD advance, an agent-input ack (§9), or a tool/outcome event — MUST advance
or sustain the working state.

### 3.3 The Run machine
**RSM-007.** The Run machine MUST express one bead run as the states
`Resolving → Provisioning → Dispatching → [Guarding] → Gating → Merging → Finalizing →
Done{closed | reopened}`, with the workflow-mode fork (review-loop, DOT cascade, single-shot)
driving one or more Dispatch instances from the `Dispatching` state.

**RSM-008.** The single-shot path's post-exit guards — the escaped-worktree check and the
no-commit-guard — MUST run in a `Guarding` state between `Dispatching` and `Gating`, and MUST
execute mutually exclusively with the merge critical section (§6). The review-loop and DOT
paths MUST NOT enter `Guarding` (they do not run those guards).

**RSM-009.** Every pre-launch failure (configuration, branching, remote setup, worktree
creation) MUST route to `Finalizing(reopen)`; the terminal spine (§7), not scattered returns,
MUST own every reopen/emit pairing.

## 4. Run-lifecycle ports

**RSM-010.** The run lifecycle MUST consume its daemon dependencies through consumer-owned
narrow interfaces, declared beside their consumers and satisfied structurally by the daemon
shell: **LedgerPort** (bead close/reopen/history-trim + intent log), **EmitterPort** (the event
bus), **WorktreePort** (local and remote worktree create + base-sync), **MergePort** (the merge
queue submit surface, §6), **LaunchPort** (launch-spec build, agent spawn, harness/adapter
registries, hook store, agent-ready timeouts, sandbox), **ClockPort** (§5), **WorkerPort**
(worker registry + remote code-sync — **M4-deferred**: the `runner != nil` execution seam it
collapses is an M4/C1 concern, so M3 implements the other seven ports and leaves WorkerPort's
extraction to the M4 execution-seam collapse), and **GatePort** (DOT gate-node evaluation).

**RSM-011.** Only run-lifecycle fields MUST be promoted to ports. Cross-goroutine shared state
(the run registry, the local-in-flight counter, the TID generator, the spawn semaphore) MUST
remain shared by reference. Periodic-maintenance value fields MUST NOT be carried on the run
dependency bundle. The bead-queue store MUST NOT become a run port; its single run-path use
(the review-loop-failure budget) MUST be exposed as a one-method budget port, and the run
outcome MUST be surfaced to the dispatch side as a terminal event rather than by direct store
access.

**RSM-012.** Port constructors MUST preserve the current nil-means-default behavior: a nil
launch-spec builder, worktree factory, worker registry, hook store, harness registry, or gate
registry MUST resolve to today's production default or documented no-op, with no behavior
change.

## 5. ClockPort and determinism

**RSM-013.** The run lifecycle MUST read time only through `substrate.ClockPort`
([replay-substrate.md]). A `ClockPort` dependency MUST be threaded through the run ports and the
reactor shell, defaulting to the system clock when unset; tests MUST inject a fake clock. This
seam MUST be the single time source for the run path: the existing mutable timeout variables,
the per-subsystem `Now` function fields on the run path, and the wall-clock `context.WithTimeout`
run-path deadlines MUST reconcile onto it.

**RSM-014.** Every run-path wall-clock select-deadline (`agent_ready`, kill-reap, post-agent-ready
hang, resume-ready, and the commit watchdog budgets) MUST be expressed as a reactor timer event
(§3, §8), not a direct `time.After`. Interval reads (`Now`/`Since`) MUST use the ClockPort. No
run-path blocking wait MUST read the wall clock directly.

## 6. The merge queue

**RSM-015.** The global merge mutex MUST be replaced by an explicit merge queue that serialises
merges to a given target branch through a single owner. Submission MUST be serialised per target
branch, MUST accept a submission whose context outlives the per-run context (shutdown-drain), and
MUST impose a FIFO ordering among concurrent submissions.

**RSM-016 (the critical section).** The merge queue MUST hold its exclusive section over only
the local ref + working-tree mutations, per target branch, split across the phases the push
relocation (RSM-019, M4-C5) opens: **Phase A** — re-validate the fast-forward check against a
freshly read target tip → advance the target ref (`git update-ref`); **Phase C** (on push
success) — restore the index → reset the working tree → the conditional `br sync` reconciliation;
**Phase D** (on push failure) — the compare-and-swap rollback of the local ref → `git fetch` →
re-advance the local target to the fresh origin tip. The network `git push origin <target>`
(**Phase B**) MUST run OUTSIDE the exclusive section (see RSM-019). The rebase, the `go build` /
`go vet` gate, and the format run MUST likewise execute OUTSIDE the exclusive section,
speculatively, and MUST be re-validated inside it. A re-validation that loses the race MUST
re-rebase and re-run the build/format gate before re-attempting the ref advance.

**RSM-017.** No build-class command — `go build`, `go vet`, `gofumpt`/`gci`, or `git rebase` —
MUST run while the merge queue's exclusive section is held, and the network `git push` MUST NOT
run inside it (it is relocated OUTSIDE per RSM-019, M4-C5). Conformance MUST be checkable by a
test asserting the critical-section executor performs no build-class command and no `git push`.
The local ref-advance (`git update-ref`), the working-tree reset, the push-failure `git fetch` +
CAS-rollback, and the post-merge `br sync` reconciliation DO run inside the exclusive section
(Phases A/C/D); the push (Phase B) does not.

**RSM-018 (preserved exclusions).** The escaped-worktree check MUST remain mutually exclusive
with the ref-advance→working-tree-reset window (via the same queue, as a read-only
tree-quiescent slot). The remote base-sync + worktree-add MUST retain an equivalent exclusion
against concurrent creators and against the main-checkout working-tree reset.

**RSM-019.** Merge outcomes MUST preserve the current taxonomy and retry semantics: a retryable
failure (rebase conflict, non-fast-forward, format failure) below its per-mode retry cap MUST
re-prepare and re-attempt; an exhausted or fatal failure MUST emit a rejected outcome, reopen the
bead, and emit a failed run terminal.

The network `git push origin <target>` MUST run OUTSIDE the exclusive section (Phase B). M4 (M4-C5)
performed this relocation: the invariant the exclusion domain protects is serial mutation of the
local target ref and working tree, not serial publication to origin, so holding the section across
the network push needlessly serializes unrelated merges behind network I/O. Correctness on a lost
race comes from RE-VALIDATING inside the section on conflict, not from holding the lock across the
push: a non-fast-forward push rejection (origin advanced under the relocated push) MUST
**re-enter the exclusive section**, compare-and-swap-roll-back the local ref advance (regressing it
only when it still points at the tip this run set — a sibling merge may have advanced and published
it in the Phase-B window), `git fetch` the fresh origin tip, re-advance the local target to it, and
then re-prepare (rebase OUTSIDE the section) and re-attempt, up to the same per-mode retry cap.
Exhaustion → the same rejected outcome + reopen + failed run terminal, with byte-identical reason
strings to the pre-relocation form.

## 7. The terminal spine

**RSM-020.** The launch→gate→merge→close logic MUST exist once, as the Run machine's
`Gating → Merging → Finalizing` tail plus a single close-ladder effector — not as the four
open-coded blocks and the duplicated close ladder it replaces. Every behavioral divergence among
the former blocks MUST survive as an explicit parameter (summary label, gate-runner presence,
pre-merge-sync flag, trailer-verdict and per-retry re-amend, merge-retry count, rebase-dropped
fall-through, context selection, outcome-emission flag, needs-attention flag, and close/reopen
reason templates). All observable summary and reason strings MUST be preserved.

**RSM-021.** The exit-0 auto-close path MUST remain a distinct terminal entry (it is the
terminal path for completion-by-process-exit harnesses that emit no stop-hook outcome), sharing
the spine tail. The shutdown-drain path MUST remain a distinct terminal edge (background context,
no gate, no pre-merge-sync, no outcome emission, direct run-completed emission, and its
requeue-recovery reopen reason).

**RSM-022.** The `runSucceeded` out-parameter MUST be eliminated: run success MUST be a terminal
state of the Run machine, read by the shell after the reactor returns (for group advancement,
staged-generator evaluation, and worktree-retention decisions). Terminal reopens MUST use a
background context so a mid-merge-cancelled run's reopen does not silently no-op.

**RSM-023.** The formal session-lifecycle machine ([handler-contract.md] HC-065) MUST be driven
as a downstream projection via a lifecycle-transition action, preserving its transition emissions
and its external readers; the Run machine MUST NOT require it.

## 8. Bounded liveness (the resume-hang invariant)

**RSM-INV-001 (resume liveness).** For every run `r` that emits `implementer_resumed(r, i)`,
exactly one run-correlated terminal event (`review_loop_cycle_complete(r)` with outcome,
`run_completed(r)`, or `run_failed(r)`) or failure-class event (`agent_ready_timeout(r)`,
`agent_input_stale(r)`, or `run_stale(r)`) MUST follow within the bounded window (RSM-024). A run
that produces neither is a conformance failure. **Silence is forbidden.**

**RSM-INV-002 (structural non-wedge).** Every timer-fired transition in the Dispatch machine MUST
land in a state with an outgoing action. No reachable `(state, timer-fired)` pair MUST be a
silent no-op.

**RSM-024 (the bound).** The resume window MUST be bounded by the composed timer stack, all
ClockPort-timed:
- the agent-input output-or-stale bound on the resume seed (§9), which resolves the seed
  submission to an `Ack` or an `agent_input_stale` terminal within the agent-input bounded
  window ([agent-input.md] AIS-INV-001; the window value is owned by the agent-input seam);
- the ready sub-bound: resume to ready-or-fail MUST NOT exceed the effective agent-ready timeout
  (the tight headline guarantee that replaces the former fixed 2-second resume grace);
- the post-agent-ready progress bound (`post_ready_hang`); and
- the absolute commit-watchdog ceiling.
The former fixed resume grace MUST be removed; the resume-ready decision MUST dissolve into the
ready-timer edge.

**RSM-025 (fail-closed).** On a liveness-timeout edge the run MUST kill the agent and reopen the
bead, riding the existing review-loop-failure budget for anti-thrash. The run MUST NOT silently
proceed past an unconfirmed resume.

**RSM-026 (`run_stale`).** The `run_stale` event is a run-lifecycle failure-class event owned by
this spec. It MUST be emitted, run-attributed, when a run's liveness bound (RSM-024) elapses with
no terminal or other failure-class event. (This spec is its normative home; prior citations to a
non-existent event-model section are superseded.)

## 9. Consuming the agent-input seam

**RSM-027.** The run lifecycle MUST consume the agent-input contract ([agent-input.md]
AIS-001, AIS-003, AIS-004, AIS-INV-001); it MUST NOT define its own input port, acceptance
type, stale terminal, or input-ack timer. Specifically:
- The reactor MUST request input via submit actions (a resume-seed submit and a brief submit),
  each carrying an `InputRequest`; the shell effector MUST call the agent-input port
  `InputPort.SubmitInput(ctx, InputRequest) (Ack, error)` ([agent-input.md] AIS-001).
- The reactor MUST honour the three-valued acceptance class of `Ack` ([agent-input.md]
  AIS-003): `Accepted` (positively confirmed) MUST advance the dispatch; `Rejected`
  (protocol refusal) MUST route to the fail-closed liveness edge (RSM-025); `Degraded`
  (written but not positively confirmed — the interim tmux/paste case) MUST NOT be treated as
  confirmation — the reactor MUST continue to require an agent-derived readiness or progress
  signal and MUST rely on the liveness bound (RSM-024) to terminate a Degraded submission that
  never confirms.
- The shell MUST convert `SubmitInput`'s synchronous `Ack` and the dual-delivered durable
  `agent_input_acked` / `agent_input_stale` events ([agent-input.md] AIS-004) into reactor
  events; correlation MUST use the `Ack`'s driver-internal monotonic input-sequence id, and a
  duplicate for an already-correlated submission MUST be dropped by `Step`.
- The bounded output-or-stale window and the acceptance definition belong to the agent-input
  seam ([agent-input.md] AIS-INV-001); the reactor MUST NOT re-implement them.
- The per-submission output-or-stale guarantee ([agent-input.md] AIS-INV-001) composes into
  RSM-INV-001: a stale (or `Rejected`, or never-confirmed `Degraded`) resume seed MUST feed the
  run's fail-closed liveness edge (RSM-025), never silence.

## 10. Enforcement

**RSM-028.** The reactor package and the merge-queue package MUST have depguard entries
forbidding imports from the flat `internal/daemon` package, so run-lifecycle and merge logic
cannot leak back into it.

## 11. Conformance

**RSM-029 (parity).** The extraction MUST preserve observable dispatch behavior — same events,
same order, same bead transitions, same terminal outcomes — except the sanctioned divergences:
the resume-hang liveness fix (the resume bound replaces the fixed grace; DOT back-edge resumes
gain the bound; a formerly-hung run now terminates or emits a failure-class event), the
run-identifier attribution on the synthetic ready, the shrunk escape-check window, and the
absence of a transient ref advance during a build failure.

**RSM-030 (tests).** Conformance MUST be demonstrated by: pure per-transition tests of both
reactors (every row, including no-ops) and the structural properties (terminal exclusivity;
RSM-INV-002); a finalizing replay checker, keyed per run, that flags any `implementer_resumed`
with no terminal or failure-class event (RSM-INV-001) and any terminal-exclusivity breach; a
fake-clock fault-injection test that stalls the agent on relaunch and asserts a terminal or
failure-class signal within the virtual-time bound, never silence; the existing incident-pinned
regression suite green per commit; and an out-of-band oracle (N=10 clean relaunch cycles plus a
replay-log check that the seeded hung-run gap is flagged and absent post-fix). The state-machine
path MUST meet the measured coverage floor from the coverage audit.

## 12. Amendment A1 — single-mode failure mapping (2026-07-14)

> **Amendment.** Added after the RT5/RT6 machines landed, to close the RT7 spec gap: the Run
> machine had no edge for a failed single-mode Dispatch, and its reopen/outcome strings were
> static `RunConfig` templates that cannot reproduce single-mode's per-sub-branch reason strings
> byte-equal against the event-stream goldens. RSM-031..035 are normative for the RT7 re-drive.

**RSM-031 (the failed-Dispatch → reopen edge).** A single-mode Dispatch instance that reaches a
failure-class terminal (`Failed`, `Stalled`, `Exited`-with-abort, or `Aborted`) MUST be mapped
onto the Run machine's reopen spine by the shell synthesizing a mode-outcome event —
`EvModeOutcome{ModeOutcome: failure}` — exactly as the review-loop and DOT sub-drivers surface
their returns (RSM-011: "the run outcome MUST be surfaced … as a terminal event"). Single-shot
is a workflow mode; its sub-driver is one Dispatch instance, and its failure is a mode failure.
The Run machine MUST NOT gain a separate dispatch-failure event kind, and the Dispatch machine
MUST NOT gain knowledge of the Run machine. In `Dispatching`, `EvModeOutcome{failure}` is
therefore a defined edge for ALL modes, and RSM-025's "reopen the bead" is reachable for
single-mode through this edge.

**RSM-032 (event-sourced terminal strings).** The reopen reason and the failed-terminal summary
for an event-classified failure MUST be carried on the triggering event's payload (`Reason` =
the `ReopenBead` reason string; `Detail` = the run-terminal summary string), NOT derived from
static `RunConfig`. The `RunConfig` templates (`ReopenReason`, `CloseSummary`,
`BrUnavailableSummary`, `NoMergeCloseSummary`) remain the fallback when the event carries no
string, preserving RT6 behavior for the review-loop/DOT paths until their own re-drives. This
applies to: `EvModeOutcome{failure}`, `EvGateFailed`, `EvEscapeDetected`,
`EvNoCommitGuardReopen`, `EvMergeResult{fatal|exhausted}`, `EvCloseResult{error}`, and
`EvProvisionFailed`. The last covers every pre-launch/provisioning failure that
`stepRunResolving`/`stepRunProvisioning` route to the reopen spine — the launch-spec build error
(`build launch spec error: %v`, workloop.go ~:4346), the D2 API-key refusal (`remote run:
ANTHROPIC_API_KEY in spawn env (D2 fail-closed)`, ~:4376), worktree-create failures, and the
prepareRun guard failures — all of which interpolate runtime strings that today's
`finalizeReopen(cfg, s, nil)` → static `cfg.ReopenReason` cannot reproduce; `EvProvisionFailed`
MUST therefore carry its `Reason`/`Detail` payload like the other failure events. The machine
remains pure: the strings are event data, composed shell-side or latched from prior events
(RSM-033); the machine mints none of them.

**RSM-033 (the single-mode path label).** The single-mode dispatch-terminal events
`EvAgentCompleted` and `EvCleanExit` MUST latch a path label into Run state
(`agent_completed` and `auto-close` respectively; the shell-synthesized noChange-subsumed close
carries `noChange-subsumed`), because the downstream close/merge/sync strings are
label-parameterized (`merge-failed (agent_completed): …` vs `merge-failed (auto-close): …`;
`close-transient-merged (<label>)`) and the branch is only known at event time, never at
config-construction time. Merge-window failures MUST additionally be staged: an `EvMergeResult`
failure carries a stage discriminator (`code_sync` vs `merge`) so the pre-merge code-sync
failure reproduces its distinct `code-sync failed (<label>): …` reopen reason and
`code-sync-failed (<label>): …` terminal summary, byte-equal. Event-classified failure reasons
on the P13/P18 spine are exactly: `agent_ready_timeout`, the code-sync failure, the merge
failure, the gate failure, the escaped-worktree guard, the no-commit guard, `noChange-timeout`,
and the never-spawned-reaper abort (`never_spawned_reaper: launch_initiated but agent_ready not
received within deadline`, workloop.go ~:5071) — the last surfaced mechanically via RSM-031's
`Aborted` dispatch-terminal class carrying its reason on payload, no distinct edge required.

**RSM-034 (rejected-outcome pairing).** The gate-failure, code-sync-failure, and merge-failure
reopens MUST be preceded by an `outcome_emitted=rejected` emission carrying the classified
reason (the P18 golden pairing); the `agent_ready_timeout`, escaped-worktree, no-commit, and
`noChange-timeout` reopens MUST NOT emit an outcome. The machine expresses this as the reopen
prefix (the same mechanism as the existing escaped-worktree and merge-rejected prefixes).

**RSM-035 (noChange-subsumed → approved close).** A single-mode run whose Dispatch stalls on
the no-change timeout but whose bead is found already subsumed in the target branch MUST close
— not reopen — with an `outcome_emitted=approved` emission and the subsumed close summary,
sharing the RSM-020 close ladder. The shell performs the subsumption check (it is I/O) and
synthesizes the subsumed mode-outcome event carrying the close summary and the emit-approved
flag; the not-subsumed case rides RSM-031 with reason `noChange-timeout`. This supersedes the
RT6 assumption that a subsumed close never emits an outcome (that remains true for the DOT
subsumed path, which passes no flag).

## 13. Cross-references

- [replay-substrate.md] — the generic reactor seam (`EventSource`/`Effector`/`Run`), `ClockPort`,
  the fault-injection Twin, and the replay-checker harness this spec instantiates.
- [session-keeper.md] — the first reactor instantiation and the template for RSM-INV-001 (its
  SK-INV-005 / SK-015 bounded-liveness invariant).
- [agent-input.md] — the `InputPort`/`Ack` seam this spec consumes (§9); AIS-001, AIS-003,
  AIS-004, AIS-INV-001. This spec's run-lifecycle changes co-land with agent-input (a hard
  dependency: agent-input introduces the `InputPort` that RSM-027 consumes).
- [event-model.md] — the durable event registry; the run-lifecycle events named here
  (`run_started`, `run_completed`, `run_failed`, `run_stale`, `implementer_resumed`,
  `agent_ready`, `agent_ready_timeout`, `lifecycle_transition`) are consumed and, for `run_stale`,
  normatively homed here (RSM-026).
- [handler-contract.md] — the session-lifecycle machine (HC-065) driven as a projection (RSM-023).
- [queue-model.md] — the bead-queue store, kept out of the run ports (RSM-011).
- [beads-integration.md] — the daemon owns terminal bead transitions (§2.2).
