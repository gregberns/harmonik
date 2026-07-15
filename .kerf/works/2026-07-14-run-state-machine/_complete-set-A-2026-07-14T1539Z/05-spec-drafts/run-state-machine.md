# Run State Machine

```
spec-id: run-state-machine
requirement-prefix: RX
status: draft
version: 0.1.0
depends-on:
  - replay-substrate        # the seam (EventSource/Effector/Run) + ClockPort, required by reference
informative-references:
  - session-keeper          # SK-INV-005/SK-015 — the bounded-liveness template this spec's RX-INV-003 mirrors
  - event-model             # existing run-scoped event vocabulary (NO amendment; RX-025)
  - execution-model         # EM-053 reopen pattern, EM-054 working-tree refresh, EM-INV-005
  - process-lifecycle       # PL-021b substrate seam; HC-065 lifecycle machine (unmoved)
```

## 1. Purpose

The daemon's per-bead run lifecycle is a single ~2,366-line function
(`beadRunOne`) with no explicit state representation: the lifecycle is spread
across a formal 3-terminal `hclifecycle.Machine`, the observable pre-exec event
chain, a four-way terminal switch, and the review-loop iteration state; the
launch→gate→merge→close terminal logic is open-coded four times; and a single
global `mergeMu` serialises `git rebase` → `go build`/`go vet` → format-check →
`git push` → `git reset --hard` → `br sync` across ALL queues. Separately, the
resume/relaunch branch's liveness rests on three uncoordinated wall-clock
mitigations — a fixed 2 s synthetic-ready grace, the 150 s/210 s agent-ready
timeout, and a ~30 min reaper — while the 300 s heartbeat masks the coarse
`run_stale` detector, so a resumed run can sit run-correlated-event-silent for
the whole gap between mitigations, and the DOT driver has no resume grace at
all. (This states the measured current behavior; the folklore "hangs forever"
is not accurate on the current tree — the defect is unstructured, uncoordinated
bounds and replay-invisible silence, not literal unboundedness.)

This spec makes the run lifecycle an explicit functional-core state machine —
the second production instantiation of the replay-substrate seam, mirroring the
landed session-keeper reactor — and replaces the global merge lock with an
explicit merge queue whose exclusive section contains no build-class work.
Bounded liveness on the resume path is structural (the daemon peer of
[session-keeper.md] SK-INV-005), not a caulk.

## 2. Scope

### 2.1 In scope
The `internal/runexec` pure machines (`Dispatch`, `Run`) and their Event/Action
vocabulary; the `internal/mergeq` serial executor and the merge
prepare/commit split; the daemon-side shell (effector + per-run drive loop);
the reactor↔input-driver contract (the surface `agent-input-substrate` M2-1
implements against); the run-path ClockPort migration; the run-lifecycle port
bundles; the bounded-liveness invariants and their replay checkers.

### 2.2 Out of scope
The full daemon god-package breakup (M5); the agent-input channel rebuild
itself (M2 — this spec only fixes its consumer-side contract); the remote
execution rebuild (M4 — the merge queue is its prerequisite; the remote
base-sync exclusion member is carried unchanged, RX-013); dissolving the commit
watchdog (`pasteInjectQuitOnCommit`) into reactor timers (frozen, RX-024);
full DOT-cascade and review-loop-control reactorization (their dispatch
segments are in scope; their control flow is not); `cacheReapMu`; stall-sentinel
semantics (its signatures are reused, not redefined).

## 3. Glossary

- **Dispatch** — one agent session's lifecycle (launch → ready → brief → work →
  terminal), a pure machine instantiated per agent dispatch: once for
  single-mode, per review-loop iteration (including resumes), per agentic DOT
  node.
- **Run** — the run-level spine machine (resolve → provision → dispatch → guard
  → gate → merge → finalize → done).
- **shell** — the imperative daemon-side driver: effector + drive loop + ports.
- **merge critical section** — the strictly-serialised commit phase of a merge:
  re-validate, FF-check, update-ref, push, rollback, working-tree refresh,
  br sync.
- **exclusion domain** — the set of critical sections serialised by the ONE
  mergeq executor (merge commits, the escape-worktree check, the remote
  base-sync + worktree-add section).
- **resume** — an agent dispatch that reattaches a prior session
  (`--resume`-class), today's `implementer_resumed` boundary.

## 4. Normative requirements

### 4.1 Packages and boundaries

#### RX-001 — runexec is a pure functional core, direction-locked
The machines and vocabulary MUST live in a leaf package (`internal/runexec`)
that imports at most the standard library, `internal/core`, and
`internal/substrate`, and MUST NOT import `internal/daemon` (depguard-enforced
inverse edge). `Step` implementations MUST be total and pure: no IO, no clock
reads, no id minting; every timestamp is the shell-stamped event `At`; run and
input ids are shell-minted. (Mirrors the keeper reactor contract, SK-009.)

#### RX-002 — mergeq is a leaf serialization mechanism
The merge queue MUST live in a leaf package (`internal/mergeq`) importing at
most the standard library, and MUST NOT import `internal/daemon`
(depguard-enforced). The git/build work it serialises is supplied by the caller
as closures; the package owns only ordering and exclusivity.

#### RX-003 — the shell lives in the daemon and owns all effects
The effector, drive loop, port adapters, and every side effect (git, tmux/
substrate, br, SSH, event emission) MUST live daemon-side. Per-action failure
policy MUST be stated in one effector table and MUST be behavior-compatible
with the pre-change call sites (best-effort discard where today discards;
fatal only where today aborts).

#### RX-004 — the seam is instantiated, not forked
The machines MUST be drivable by `substrate.Run[runexec.Event, runexec.Action]`
([replay-substrate.md] RS-002) and all shell timing MUST go through
`substrate.ClockPort` (RS-015, required by reference — never redefined). Any
genericization gap discovered MUST be fixed in the substrate/replay packages,
not copied locally.

### 4.2 The machines

#### RX-005 — Dispatch: explicit per-session states
One agent dispatch MUST be expressed as the pure machine
`Launching → AwaitingReady → Briefing → Working → {Completed | Exited |
Stalled | Failed | Aborted}` (ready-handshake-skipping harnesses pass
`Launching → Working` directly). Every dispatch — fresh or resume, builtin
review-loop or DOT — MUST ride this machine; per-mode open-coded
launch/ready/wait segments are non-conformant once their migration task lands.

#### RX-006 — Run: one terminal spine
The run lifecycle MUST be expressed as the pure machine `Resolving →
Provisioning → Dispatching → [Guarding] → Gating → Merging → Finalizing →
Done{closed | reopened}` in which the gate→merge→close-or-reopen tail exists
EXACTLY ONCE. The four pre-change open-coded terminal blocks (review-loop, DOT,
agent-completed, exit-0 auto-close) MUST become entry events into that single
tail; their distinct trigger conditions and their exact outcome/summary strings
are preserved as event data. The shutdown-drain terminal MUST be a distinct
edge whose action batch the effector executes under a background-derived
context without session-data collection (pre-change semantics).

#### RX-007 — timers are events
The machines MUST model every wait as `ArmTimer{kind}`/`CancelTimer{kind}`
actions and `TimerFired{kind}` events; the shell owns ClockPort deadlines and a
nearest-deadline wake. Timer kinds and their default values MUST equal the
pre-change constants (agent-ready 150 s local / 210 s remote; ready-kill-reap
10 s; worker-socket-ready 10 s; stop-hook-grace 3 s; input-ack 30 s default,
new). Context cancellation MUST map onto the phase-appropriate timeout edge.

#### RX-008 — success is terminal state, not an out-parameter
Run success MUST be readable from the Run machine's terminal state. The
`runSucceeded *bool` out-parameter and the `emitDone` closure MUST be removed;
the post-run consumers (queue group-advance, flywheel evaluation) MUST read the
terminal. The terminal-emission action MUST preserve the cancelled-context swap
(emit under a background context when the run context is cancelled).

### 4.3 The dispatch input contract (the M2-1 surface)

#### RX-009 — delivery resolves to ack-or-fail, never silence
Every `ActDeliverInput{InputID}` MUST be accompanied by an armed input-ack
timer, and MUST resolve to exactly one of `EvInputAck{InputID}`,
`EvInputRejected{InputID}`, or `EvTimerFired(input_ack)`; the timeout edge MUST
have an outgoing action (bounded retry or dispatch failure). Ack semantics:
the agent harness ACCEPTED the input for processing — not "bytes queued in a
pane buffer". A transitional pane-based driver MAY approximate acceptance
(paste-verification) but MUST still resolve every delivery.

#### RX-010 — ready is required on start AND resume
The input driver MUST deliver `EvAgentReady` (or `EvAgentExited`) for every
session start AND every reattach/resume before the reactor's agent-ready timer
fires. A driver that cannot observe readiness owns a bounded fallback; the
reactor's `TimerFired(agent_ready)` edge is the backstop and MUST terminate the
dispatch as a failure (kill + failure event + reopen), never proceed silently.
Synthetic readiness emissions MUST carry the run id (replay-joinable).

#### RX-011 — transport shape is free; the event contract is fixed
The driver MAY expose a synchronous submit-with-ack or an event stream; the
shell adapts either into the RX-009/RX-010 events. Per-session events MUST be
delivered in observation order. `InputID` is shell-minted and opaque to the
driver.

### 4.4 The merge queue

#### RX-012 — one exclusion domain, three members, strict FIFO
ONE serial executor MUST serialise, in submission order across all named
queues: (a) merge critical sections; (b) the escape-worktree check; (c) the
remote base-sync + worktree-add section. Members (b) and (c) preserve the
pre-change invariants (no false escape positives during a sibling's
update-ref→reset window; no concurrent worker-side fetch/worktree-add or box-A
index.lock races). Member (c) is carried unchanged pending the remote rebuild.

#### RX-013 — prepare/commit split with stale re-validation
The merge MUST split into a speculative prepare phase executed OUTSIDE the
exclusion domain (worktree-local: churn handling, rebase, run-context strip,
build, vet, format incl. auto-fix commit) and a commit phase INSIDE it. The
commit phase MUST re-resolve the target tip and MUST refuse (returning a
stale indication that triggers re-prepare) when the tip moved since prepare.
The retry budget and retryable-reason vocabulary MUST equal the pre-change
values.

#### RX-014 — no build-class work in the critical section
`go build`, `go vet`, formatters, and `git rebase` MUST NOT execute inside any
exclusion-domain critical section. The commit phase's command inventory is
CLOSED: ref reads, ancestor check, update-ref, push, fetch (non-FF recovery),
rollback update-ref, staged-restore, reset-hard, ledger import-sync. A test
MUST fail when a build-class command executes inside the domain or the
inventory grows.

#### RX-015 — the ref-mutation unit stays atomic under the domain
update-ref, push, their rollback pairing, the working-tree refresh, and the
conditional ledger sync remain INSIDE the critical section: update-ref↔push are
rollback-paired (a push failure rolls the local ref back), and the refresh/sync
mutate the main checkout inside the escape-check window. (This deliberately
supersedes the earlier goal wording "push outside any lock"; the achieved
property is no BUILD under any daemon-wide exclusion.)

### 4.5 Ports and clock

#### RX-016 — run-path dependencies are explicit bundles
The run shell MUST consume its dependencies as narrow port bundles (behavioral
ports / immutable env / shared-by-reference handles) instead of the by-value
85-field struct; the two dead fields are deleted; loop-owned maintenance values
are lifted out of the run path; nil-means-disabled defaults are preserved
exactly. (Bundle composition is design-owned; this requirement pins the
boundary, not the field list.)

#### RX-017 — run-path wall-clock reads go through ClockPort
Every wall-clock read and timed select on the run path (the enumerated
workloop/reviewloop/dot_cascade sites) MUST go through `substrate.ClockPort`;
outer-loop maintenance sites are out of scope. Deterministic fake-clock tests
MUST be able to drive every timeout edge.

### 4.6 Events and parity

#### RX-018 — zero new durable event types
This change registers NO new event types. The liveness fix reuses the existing
`agent_ready_timeout` / `run_stale` / terminal families; queue observability is
process-log only. The event-model spec is not amended.

#### RX-019 — behavior parity with a closed allowlist
Per-run observable event streams (names, order, reason/summary strings, bead
transitions) MUST be byte-compatible with the pre-change system EXCEPT the
closed allowlist: (a) the bounded-liveness fix class (resume bound replacing
the fixed grace; the DOT path gaining the bound; a formerly-silent run now
reaching agent_ready_timeout + reopen); (b) the run id stamped on synthetic
readiness; (c) the shrunk escape-check exposure window; (d) no transient
target-ref advance during failed builds. Any other divergence is a defect.

#### RX-020 — the lifecycle machine is a projection, unmoved
The HC-065 `hclifecycle.Machine` remains session-scoped and daemon-driven via
the terminal action; this spec does not move its ownership.

## 5. Invariants

#### RX-INV-001 — input-ack liveness
For every delivered input `i` on dispatch `d`, exactly one of ack(i),
reject(i), or ack-timeout(i) MUST be processed, and the timeout edge lands in
a state with an outgoing action. Structural per RX-009.

#### RX-INV-002 — ready-or-fail bound on every dispatch
Every dispatch (fresh or resume) MUST reach `Working` or a failure terminal
within the agent-ready bound. No brief/resume-prompt delivery may precede
readiness (structural: the only brief-delivery constructor requires the ready
edge). Fail direction is CLOSED: the timeout kills and reopens.

#### RX-INV-003 — resume bounded liveness, never silence (the SK-INV-005 peer)
For every run `r` emitting `implementer_resumed(r)`, exactly one run-correlated
terminal (`review_loop_cycle_complete(r)` / `run_completed(r)` /
`run_failed(r)`) or failure-class event (`agent_ready_timeout(r)` /
`run_stale(r)`) MUST follow within the bounded window
`W = agent-ready bound + input-ack bound + the working-segment ceiling
(commit-watchdog hard ceiling, 90 min, frozen) + grace overheads`. A run with
neither is a conformance failure. Structurally, every `TimerFired` edge in
`Dispatch` lands in a state with an outgoing action, so the machine cannot
wedge silently. (Template: [session-keeper.md] SK-INV-005 / SK-015.)

#### RX-INV-004 — terminal exclusivity through one spine
Every run reaches EXACTLY ONE `Done` terminal, and every close/reopen +
outcome-emission pairing executes through the single Run tail. Emitting both
run_completed and run_failed for one run, or closing and reopening the same
bead from one run, is a conformance failure.

#### RX-INV-005 — merge critical-section purity
No build-class command inside the exclusion domain (RX-014), verified
mechanically per replay/recording of the executed command inventory.

## 6. Verification

The invariants MUST be verified out-of-band (no daemon pipeline):
- **L0** — pure transition-table tests (every row incl. explicit no-ops) +
  property tests over random interleavings: RX-INV-001/002/004 structural
  checks; every TimerFired edge yields an action or terminal.
- **L1** — a run replay corpus extracted per run id from the frozen events
  baseline (EventID-sorted streams + terminal-summary goldens, strata pinned in
  a manifest) replayed through the machines; an extended `internal/replay`
  run-keyed checker set (an `RX9` finalizer mirroring `SR9Checker`: flags
  resumed-but-no-terminal and terminal-exclusivity breaches) over re-emitted
  streams.
- **L2** — fake-clock shell tests (deterministic timeout races); the mergeq
  recording-runner inventory test (RX-INV-005); the replay-substrate fault
  matrix over synthesized dispatch schedules — the headline cell: FaultStall
  after `implementer_resumed` MUST produce agent_ready_timeout + reopen within
  the virtual-time window, never silence.
- **Oracle** — one deterministic resume-fault test + N=10 consecutive clean
  relaunch cycles green; the ~466 incident-pinned regression tests green per
  increment; the measured coverage floor from the pre-extraction audit met on
  the new packages; out-of-band jq/grep over a replayed log confirms RX9 flags
  a seeded hung-run fixture and passes post-fix streams.

## 7. Cross-references

- **[replay-substrate.md]** RS-001/002 (seam + Run), RS-015 (ClockPort),
  RS-012 (fault modes), RS-017 (tiers) — instantiated by this spec (RX-004).
- **[session-keeper.md]** SK-INV-005/SK-015 — the bounded-liveness template
  RX-INV-003 mirrors; SK-009/010 — the pure-Step + timers-as-events contract
  RX-001/RX-007 transposes to the daemon. Read-only lineage; no reverse
  dependency.
- **[event-model.md]** — the existing run-scoped vocabulary this spec reuses
  unchanged (RX-018).
- **[execution-model.md]** EM-053/EM-054/EM-INV-005 — reopen pattern,
  working-tree refresh, rollback reconciliation backstop, all preserved inside
  the commit phase (RX-015).
- **[process-lifecycle.md]** PL-021b — the substrate spawn seam the LaunchPort
  wraps; HC-065 lifecycle machine (RX-020).
- **agent-input-substrate (M2, kerf work)** — implements the RX-009..011 driver
  contract; this spec owns the reactor-facing surface, M2 owns the transport.
