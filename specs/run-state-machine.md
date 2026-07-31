# Run State Machine

```yaml
---
title: Run State Machine
spec-id: run-state-machine
requirement-prefix: RSM
status: draft
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.3.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-07-31
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
Done{closed | reopened}`, with the workflow-mode fork (DOT cascade, single-shot)
driving one or more Dispatch instances from the `Dispatching` state.

**RSM-008.** The single-shot path's post-exit guards — the escaped-worktree check and the
no-commit-guard — MUST run in a `Guarding` state between `Dispatching` and `Gating`, and MUST
execute mutually exclusively with the merge critical section (§6). The DOT path MUST NOT enter
`Guarding` (it does not run those guards).

> **OPEN — RSM-008 guard coverage. This needs an operator decision (raised 2026-07-30).**
>
> The obligation above is unchanged and stays as written. The only edit was to the pointer: the
> retired `review-loop` mode was removed from the list of paths, because that mode no longer
> exists. The question the rule raises is still open, and a citation sweep must not settle it.
>
> **What the rule means in production.** RSM-008 makes two safety checks — "did the agent write
> files outside its workspace" and "did the agent commit anything" — run only on the single-shot
> path. The graph path skips both. Almost every run takes the graph path. A count of the local
> event log on 2026-07-30 found 866 runs that started on `dot` against 2 that started on
> `single`. So both checks are close to dead code in practice, even though the spec presents them
> as protection.
>
> **What the code does today.** `WireSpine` in `internal/runloop/runbridge.go` binds the
> `CheckEscape` effect to a function that returns the `EvGuardsPassed` event and nothing else. The
> `Guarding` state therefore records a pass without running a check. The action kind
> `ActCheckEscape` still exists in `internal/runexec/vocab.go`. The real guards stay imperative in
> `beadRunOne` in `internal/daemon/workloop.go` on the single path.
>
> **Option A — amend the rule so the guards run on the graph path.** This restores the protection
> the spec claims. The cost is that runs which pass today would start to fail, and nobody can say
> how many until it ships.
>
> **Option B — leave the rule alone and stop describing these checks as protection.** This keeps
> today's behavior. The cost is real. The escape guard was built for a real observed failure: an
> implementer wrote files into the main working tree instead of its own worktree (hk-6zylj). That
> failure mode is not prevented on the graph path today.
>
> **Evidence that argues against shipping option A unchanged.** The escaped-worktree check has a
> known false-failure mode. A working tree that stays dirty for an unrelated reason trips the
> `implementer_escaped_worktree` detector, and the detector then fails dispatched beads that did
> nothing wrong (hk-yru). AGENTS.md repeats this as a standing trap. Extending that check as it
> stands would put a guard with a known false-positive mode onto the path that carries nearly all
> the traffic. If option A is chosen, the false-failure mode should be fixed first.

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

## 4a. Run resource discipline

A run takes nine resources in three lifetimes and a run registry entry on the same axis. Before
this section the rules for giving them back were open-coded at each release site. The obligations
here move that to one shape. They constrain HOW a run holds and gives back a resource. They do not
change WHICH resources a run takes.

**RSM-036.** Every resource a run takes MUST be held as a lease: one value that carries both the
resource and the single call that gives it back. That call MUST run at most once, whichever
goroutine asks and however many times. A release site MUST NOT test whether the release already
happened, and MUST NOT retry a release that failed — a give-back call that cannot succeed does not
succeed on a second try.

**RSM-037.** A run MUST decide ONE disposition for the whole run, and every release site MUST read
that one value. A skip flag per resource MUST NOT be used. The disposition MUST be a total pure
function of the run's exit facts, and those facts MUST be a value the function takes as input, not
state it reads.

The three dispositions are:

| Disposition | What the run gives back |
|---|---|
| reclaim | everything |
| survive | everything except the agent session, the worktree, the run registry entry, the hook session, and the tunnel |
| retain-evidence | everything except the worktree |

Survive requires BOTH of its facts: the agent runs in a session of its own that outlives this
process, AND the run is ending because the daemon is stopping. Survive wins over retain-evidence
when both apply. Only a surviving run leaves its bead in progress for a later boot to adopt; every
other disposition settles the bead by the run's own outcome.

> **Survive is what a run ASKS for. The system does not deliver it today.** The boot orphan sweep
> kills every tmux session carrying the project prefix that is not in its exclusion set, with no
> liveness test, and it runs before the pass that looks for a surviving run. The bead still
> recovers, because adoption then classifies the run as dead and resets it. Nothing in this spec
> or in the code MUST be written as if the agent is still there at the next boot. Making survival
> real needs a way to tell a live surviving session from a genuine orphan, and that is separate
> work.

**RSM-038.** Leases MUST be held in a scope that gives them back in the reverse of the order they
were taken. Per-launch resources MUST be held in a scope nested inside the run's scope: a graph run
takes and gives back one hook session and one agent session per node while it holds one tunnel and
one worktree for the whole run, so one flat scope cannot express both lifetimes.

Three ordering edges are load-bearing and MUST hold. The rest of the order is reverse-of-acquisition
by construction and nothing else depends on it.

1. The agent session MUST die before the worktree is removed. Otherwise `git worktree remove
   --force` races a live process inside the directory and the run is misrecorded as having produced
   no commit.
2. The agent session MUST die before the Pi log capture runs, because reading the session outcome
   blocks until the session is waited on.
3. The Pi log capture MUST run before the worktree is removed, because it writes into it.

**Where this lives.** `internal/runlease` holds the lease, the scope, and the disposition. It is a
pure leaf: it takes the give-back call as a value and knows nothing about what any resource is. Its
depguard rule allows the standard library and itself, and nothing more. A resource added to that
package's closed list without a disposition decision is a lint failure, which is how the polarity
question stays answered.

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
  submission to synchronous `Ack{Rejected}` or, after `Ack{Delivered}`, to a correlated
  `agent_input_acked` / `agent_input_stale` terminal within the agent-input bounded window
  ([agent-input.md] AIS-003, AIS-004, AIS-INV-001; the window value and the timer that measures
  it are owned by the agent-input seam). A `Delivered` return is a delivery handoff, so it
  does not by itself satisfy this sub-bound;
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
- The reactor MUST honour the BINARY delivery outcome of `Ack` ([agent-input.md] AIS-003).
  `Ack{Delivered}` records a successful handoff of the input to the driver; it MUST NOT by
  itself advance a transition that requires positive agent acceptance — it leaves positive
  acceptance PENDING on the correlated asynchronous terminal. `Ack{Rejected}` (a protocol-level
  refusal — structured drivers only) MUST route to the fail-closed liveness edge (RSM-025).
  There is no `Accepted` and no `Degraded` outcome, and no acceptance class or tier: a
  successful write on the tmux/paste path is a delivery handoff, never positive acceptance.
- The reactor MUST take the correlated asynchronous events ([agent-input.md] AIS-004) as the
  acceptance verdict: `agent_input_acked` IS the positive-acceptance signal and MUST advance
  the transition that the `Delivered` handoff left pending; `agent_input_stale` MUST route to
  the fail-closed liveness edge (RSM-025). The four input-seam routes are therefore TOTAL —
  `Delivered` → await the correlated asynchronous terminal; `Rejected` → fail closed;
  `agent_input_acked` → acceptance path; `agent_input_stale` → fail closed — and none of
  them is a silent no-op (RSM-INV-002).
- The shell MUST convert `SubmitInput`'s synchronous `Ack` and the dual-delivered durable
  `agent_input_acked` / `agent_input_stale` events ([agent-input.md] AIS-004) into reactor
  events. Correlation MUST use the `Ack`'s driver-internal monotonic input-sequence id
  ([agent-input.md] AIS-003b; its serialized `input_seq` payload field name is owned by
  [event-model.md §6.3], not by this spec). For one `input_seq`, `Step` MUST consume the first
  synchronous outcome exactly once; after a `Delivered`, the FIRST correlated asynchronous
  terminal (`agent_input_acked` or `agent_input_stale`) wins. `Step` MUST drop only repeated
  or late observations for an already-resolved `input_seq`; it MUST NOT drop the first
  `agent_input_acked` merely because `Delivered` was already observed.
- The bounded output-or-stale window and the acceptance definition belong to the agent-input
  seam ([agent-input.md] AIS-INV-001); the reactor MUST NOT re-implement them and MUST NOT add
  a second input timer of its own. The run-level backstop for a `Delivered` submission whose
  correlated asynchronous terminal never arrives is the ALREADY-composed RSM-024 timer stack
  (the ready sub-bound, `post_ready_hang`, and the absolute commit-watchdog ceiling), which
  routes to RSM-025 — not a new input timer.
- The per-submission output-or-stale guarantee ([agent-input.md] AIS-INV-001) composes into
  RSM-INV-001: a `Rejected` resume seed, or a `Delivered` resume seed whose correlated
  asynchronous terminal is `agent_input_stale`, MUST feed the run's fail-closed liveness edge
  (RSM-025), never silence.

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
`EvModeOutcome{ModeOutcome: failure}` — exactly as the DOT sub-driver surfaces its returns
(RSM-011: "the run outcome MUST be surfaced … as a terminal event"). Single-shot
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
string, preserving RT6 behavior for the DOT path until its own re-drive. This
applies to: `EvModeOutcome{failure}`, `EvGateFailed`, `EvEscapeDetected`,
`EvNoCommitGuardReopen`, `EvMergeResult{fatal|exhausted}`, `EvCloseResult{error}`, and
`EvProvisionFailed`. The last covers every pre-launch/provisioning failure that
`stepRunResolving`/`stepRunProvisioning` route to the reopen spine — the launch-spec build error
(`build launch spec error: %v`, `internal/daemon/workloop.go` `beadRunOne`), the D2 API-key
refusal (the `d2APIKeyRefusal` constant in the same file), worktree-create failures, and the
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
received within deadline`, the `abortReason` constant in `internal/daemon/workloop.go`
`beadRunOne`) — the last surfaced mechanically via RSM-031's `Aborted` dispatch-terminal class
carrying its reason on payload, no distinct edge required.

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

## 14. Revision history

> This table starts at v0.2.1; earlier versions of this spec predate it and are recoverable
> from Git history.

| Date | Version | Author | Change |
|------|---------|--------|--------|
| 2026-07-31 | 0.3.0 | agent (delete-and-rewrite step 6) | **New §4a, run resource discipline: RSM-036, RSM-037, RSM-038.** The spec had no rule about how a run holds a resource or gives it back, and the daemon therefore open-coded the answer at each release site. One condition — an agent in its own session plus a stopping daemon — reached four sites, spelled three different ways, and missed the hook session and the tunnel. RSM-036 requires a lease, whose give-back call runs at most once and is never retried. RSM-037 requires ONE disposition value for the whole run, decided by a total pure function of the run's exit facts, and forbids a skip flag per resource; it names the three dispositions and what each keeps. RSM-038 requires a scope closed in reverse order, requires the per-launch resources to sit in a nested scope, and names the three ordering edges that are load-bearing. §4a also records that survival is what a run asks for and NOT something the system delivers today, because the boot orphan sweep kills such sessions before anything looks for them. `internal/runlease` is named as the owner and is fenced to the standard library. No existing rule is renumbered and no production behaviour changes: the types land unwired, and the migration of each release site onto them is separate work. |
| 2026-07-30 | 0.2.2 | agent (spec citation cleanup) | **Rotted pointers repaired. No obligation changed.** The workflow mode `review-loop` was retired and its driver deleted, so `core.WorkflowMode.Valid()` now accepts only `single` and `dot`. The retired mode is removed from the mode lists in RSM-007, RSM-008, RSM-031 and RSM-032. What each of those rules requires is unchanged. Three approximate line-number citations into `workloop.go` are replaced by symbol names (`beadRunOne`, the `d2APIKeyRefusal` constant, the `abortReason` constant), per the repo convention to cite symbols and never line numbers. RSM-008 also gains an OPEN note that records a question the sweep found but must not settle: the rule confines both post-exit guards to the single-shot path, almost all runs take the graph path, and the choice between extending the guards and dropping the protection claim belongs to the operator. References to the event `review_loop_cycle_complete` and to the review-loop-failure budget are left alone, because `core.EventTypeReviewLoopCycleComplete`, `ChargeReviewLoopFailure` and `MaxReviewLoopFailures` all still exist. |
| 2026-07-27 | 0.2.1 | agent (codename: input-ack-contract) | **Input-ack consumption reconciled with the owner contracts (coordinated drift correction; co-landed with [agent-input.md] 0.1.1 and [handler-contract.md] 0.8.1).** RSM-027 carried a three-valued acceptance class (`Accepted` / `Rejected` / `Degraded`) that never existed in the owner specs: the `Ack` outcome landed BINARY (`Delivered` / `Rejected`) in AIS-003 / HC-070 the day after this spec, with positive acceptance decoupled onto the async `agent_input_acked` event. RSM-027 is amended in place (NOT renumbered) to consume that contract: `Ack{Delivered}` is a driver handoff that leaves positive acceptance pending; `Ack{Rejected}` fail-closes to RSM-025; the correlated `agent_input_acked` is the positive-acceptance event; the correlated `agent_input_stale` fail-closes to RSM-025. The four routes are stated as total. The `input_seq` consumption rule is made explicit — consume the first synchronous outcome once, then the first correlated asynchronous terminal wins; drop only repeated or late observations, never the first `agent_input_acked`; add no second timer. RSM-024's resume-seed bullet is reconciled so a `Delivered` return alone no longer satisfies the sub-bound, citing AIS-003 + AIS-004 + AIS-INV-001 as the composite authority. RSM-027 also names the run-level backstop for a `Delivered` whose async terminal never arrives: the already-composed RSM-024 timer stack (ready sub-bound, `post_ready_hang`, absolute commit-watchdog ceiling) routing to RSM-025 — NOT a new input timer. The correlation bullet also attributes the sequence id to AIS-003b and its serialized `input_seq` payload field name to [event-model.md §6.3], keeping this spec clear of the event payload. `Accepted`, `Degraded`, and the "three-valued acceptance class" are removed. No requirement renumbered; no port, `Ack` record, event schema, timer semantics, or production behaviour changed. |
