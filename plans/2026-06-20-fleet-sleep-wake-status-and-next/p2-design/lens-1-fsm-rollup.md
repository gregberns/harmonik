# Lens 1 — Per-session lifecycle FSM → top-level state roll-up

**Codename:** `fleet-state` · **Bead:** hk-9fvk (P2-DESIGN) · **Date:** 2026-06-20
**Locked labels:** PROCESSING / WAITING / DRAINING / INACTIVE · **Models ACTUAL-state now** (desired-state is a forward stub).

This lens answers: *what real underlying facts exist today, and what is the exact predicate that rolls them up to the 4 labels, and what does each label gate?* Every claim is cited to real code.

---

## 1. The real per-session lifecycle states (today)

The per-session FSM is the leaf and it already ships. It is `internal/handlercontract/lifecycle`.

8 states, `LifecycleState uint8` (`internal/handlercontract/lifecycle/types.go:18-38`):

| State | iota | Meaning (verbatim from types.go) | Terminal? |
|---|---|---|---|
| `StateSpawning` | 0 | process started, handshake not yet complete | no |
| `StateInitializing` | 1 | handshake done, skills provisioning in progress | no |
| `StateReady` | 2 | agent_ready fired; idle, accepting input | no |
| `StateExecuting` | 3 | command in flight (between input-send and outcome) | no |
| `StateSuspended` | 4 | per-session operator pause (distinct from handler-type pause) | no |
| `StateTerminating` | 5 | SIGTERM sent; Wait not yet returned | no |
| `StateTerminated` | 6 | Wait returned exit==0 / expected code | **yes** |
| `StateFailed` | 7 | Wait returned classified error / silent-hang (HC-026) | **yes** |

- `IsTerminal()` → `Terminated || Failed` (`types.go:42-44`).
- Edge table is authoritative at `internal/handlercontract/lifecycle/table.go:14-54`. Key edges: `Ready↔Executing` bidirectional; `Ready↔Suspended` bidirectional; every non-terminal can reach `Terminating` and `Failed`; terminal states have no outgoing edges.
- The machine itself: `internal/handlercontract/lifecycle/machine.go:14-36` (`Machine` struct, `New()` starts in `StateSpawning`). `Current()` at `machine.go:73-77`, `EnteredAt()` at `machine.go:79-84`.

**Crucial scope caveat — this FSM exists ONLY per in-flight RUN, not per long-lived LLM session.** A `lifecycle.Machine` is attached to a `RunHandle` (the implementer/reviewer subprocess for one bead) at `internal/daemon/runregistry.go:82` (`machine atomic.Pointer[hclifecycle.Machine]`), set at `internal/daemon/workloop.go:3282-3287` from `sess.Machine()` after `handler.Launch`. There is **no `lifecycle.Machine` attached to the captain or a crew orchestrator session** — those are interactive `claude --remote-control` panes, observed only indirectly (keeper gauge `.ctx` file, `tmux has-session`, crew registry presence). This is the single biggest "the real code can't cleanly support a label" gap; see §6.

### The run registry (in-flight runs)
- `RunRegistry` = `map[core.RunID]*RunHandle` (`internal/daemon/runregistry.go:107-110`).
- `RunHandle` (`runregistry.go:29-89`) carries `BeadID`, `QueueName`, `StartedAt`, `Cancel`, `Watcher`, `OwningEpicID/Assignee`, and the lifecycle `machine`.
- Counts: `Len()` = global in-flight tally (`runregistry.go:148-153`); `LenForQueue(name)` = per-queue tally (`runregistry.go:155-173`); `Snapshot()` returns all handles (`runregistry.go` Snapshot method). Register on dispatch at `workloop.go:2179-2188`; `defer Unregister` on goroutine exit at `workloop.go:2195-2198`.

### The queue state model (QueueStore)
- `QueueStore` = `map[string]*queue.Queue` + a `wakeC` (`internal/daemon/queuestore_hkj808w.go:68-76`). State lives on each `queue.Queue`, NOT on the store.
- `QueueStatus` (`internal/queue/types.go:42-80`): `active`, `paused-by-failure`, `paused-by-drain`, `paused-by-budget`, `completed`, `cancelled`. **There is no `draining` queue status** — drain is represented as `paused-by-drain`.
- Per-queue concurrency = `Queue.Workers int` (`internal/queue/types.go:287`); defaults to global `--max-concurrent` when 0.
- `GroupStatus` (`internal/queue/types.go:95-113`): `pending`, `active`, `complete-success`, `complete-with-failures`.
- "Has a dispatchable item": `selectNextQueue` (`internal/daemon/workloop.go:1100-1193`) — a queue contributes a candidate iff `Status==active` AND `LenForQueue < effectiveQueueWorkers` AND some active group has `len(queue.EligibleItems(group)) > 0` (`internal/queue/state.go:97-124`).

### The drain oracle (already fuses all the above into one verdict)
- `GenuineDrain` / `DrainDetector` (`internal/daemon/draindetect.go`) returns `DrainResult{State, Reasons}` with tri-state `DRAINED / HAS_WORK / UNSURE` (`draindetect.go:60-85`). It already reads: `br ready --limit 0`, in-flight runs (`RunRegistry.Len()`), non-terminal queue items, paused/failed queues, open-epic-blocked ready children, and live `.harmonik/worktrees/*`. Under P1 this is reshaped to `GatherDrainFacts` (a typed fact bundle) but keeps all 5 false-negative defenses. **This is the fact source the roll-up consumes — the labels are a presentation of these same facts plus run-registry FSM states.**

---

## 2. The 4-label predicate table (ACTUAL-state roll-up)

The roll-up is a **fold** over three fact sources, evaluated top-down (first match wins). Define the primitive facts:

```
RUNS_INFLIGHT   = RunRegistry.Len() > 0
RUN_EXECUTING   = ∃ handle ∈ RunRegistry.Snapshot():
                    handle.GetMachine() != nil &&
                    handle.GetMachine().Current() == StateExecuting
RUN_TERMINATING = ∃ handle: machine.Current() == StateTerminating
QUEUE_DISPATCHABLE = ∃ q ∈ QueueStore: q.Status==active &&
                       LenForQueue(q.Name) < effectiveQueueWorkers(q) &&
                       ∃ active group g: len(EligibleItems(g)) > 0
HAS_LATENT_WORK = GatherDrainFacts() != DRAINED
                  // (in-flight OR ready OR paused/failed queue OR blocked-epic child)
QUEUE_DRAINING  = ∃ q ∈ QueueStore: q.Status==paused-by-drain   // sleep/shutdown drain
ANY_SLEEPING    = ∃ live .harmonik/.sleeping.<sid> marker
SESSIONS_ALIVE  = ∃ live captain/crew tmux session (crew registry presence + tmux has-session)
```

| Label | Boolean predicate (first match wins, top→bottom) |
|---|---|
| **PROCESSING** | `RUNS_INFLIGHT  OR  QUEUE_DISPATCHABLE` — at least one run is in-flight (any non-terminal FSM state: Spawning/Initializing/Ready/Executing) **or** an active queue has an eligible item the dispatcher could claim right now. This is "the fleet is actively doing or about to do bead work." |
| **DRAINING** | `(NOT PROCESSING) AND (QUEUE_DRAINING OR RUN_TERMINATING OR ANY_SLEEPING)` — no new work is being dispatched, but the fleet is mid-wind-down: a queue is `paused-by-drain`, a run is in `StateTerminating`, or a sleep marker has been written and we're letting the last things settle. The transient "no dispatchable work yet some runs finishing/terminating" sits here. |
| **WAITING** | `(NOT PROCESSING) AND (NOT DRAINING) AND SESSIONS_ALIVE AND HAS_LATENT_WORK` — sessions are alive and armed, nothing dispatchable *right now*, but work is latent: ready beads exist but are epic-blocked/assignee-wedged, a queue is `paused-by-failure`/`paused-by-budget`, or a `.failed-*` archive is un-reconciled. The captain is the one who must act (decompose, unblock); Go just waits. `GatherDrainFacts==UNSURE` also lands here (fail-awake). |
| **INACTIVE** | `(NOT PROCESSING) AND (NOT DRAINING) AND (NOT WAITING)` — i.e. `GatherDrainFacts==DRAINED` with no runs and no dispatchable queues, **OR** no live sessions at all (everything asleep/torn down). True rest. This is the only label that licenses stopping watchers. |

### Resolving the "some sessions process, others wait" ambiguity — **union/max rule**
The top-level label is the **max over the priority ordering PROCESSING > DRAINING > WAITING > INACTIVE** (a union, not an average). If *any* run is in-flight or *any* queue is dispatchable, the whole fleet is PROCESSING — even if three crews are idle. Rationale: the labels gate *which polls run* (§4); a single live run must keep the run-watchers armed, so the most-active leaf wins. The per-session detail (which crew is idle) is still available in `harmonik state --json` as the unfolded leaf array; the single label is the safety-conservative roll-up.

### DRAINING vs WAITING — the disambiguator
Both mean "not dispatching right now," but they differ on **intent**:
- **DRAINING** = a wind-down is *in motion* (a marker was written / a queue was drain-paused / a run is terminating). Direction: heading toward INACTIVE.
- **WAITING** = fully armed and idle with latent (non-dispatchable) work; the captain needs to *generate* dispatchable work. Direction: heading back toward PROCESSING once the captain acts.
This is the ZFC line made visible: DRAINING is a Go-executed transition state; WAITING is the state where cognition (captain) is required.

---

## 3. The fact→label evaluation, in order

```
func RollUp(rr *RunRegistry, qs *QueueStore, facts DrainFacts, markers, sessions) Label {
    if rr.Len() > 0 || anyQueueDispatchable(qs) {           // §2 PROCESSING
        return PROCESSING
    }
    if anyQueueDrainPaused(qs) || anyRunTerminating(rr) || anySleepMarker(markers) {
        return DRAINING                                     // §2 DRAINING
    }
    if anySessionAlive(sessions) && facts.State != DRAINED { // HAS_WORK or UNSURE
        return WAITING                                      // §2 WAITING
    }
    return INACTIVE                                          // §2 INACTIVE
}
```

`anyQueueDispatchable` reuses `selectNextQueue`'s candidate test verbatim (`workloop.go:1114-1140`). `facts.State` is the `GatherDrainFacts` verdict (P1, hk-pfr4). `anyRunTerminating` walks `rr.Snapshot()` reading `handle.GetMachine().Current()`.

---

## 4. Gating table — what each label enables/disables

The operator's original insight: **INACTIVE ⇒ stop run-watchers/gauges; PROCESSING ⇒ all armed.** Below is the concrete poll inventory (intervals + cite) and what each label does to each.

| Poll / watcher | file:line | interval | PROCESSING | WAITING | DRAINING | INACTIVE |
|---|---|---|---|---|---|---|
| StaleWatcher (run staleness, silent-hang, never-spawned reaper) | `internal/daemon/stalewatch.go:301`, interval `:51` | 30 s | ON | OFF¹ | ON² | **OFF** |
| BandwidthTuner (auto-tune max-concurrent) | `internal/daemon/bandwidthtuner.go:160`, `:41` | 60 s | ON | ON | reduce | **OFF** |
| DOT gate-file poll (per-run verdict file) | `internal/daemon/dot_gate.go:665`, `:80` | 2 s | ON (per active run only) | n/a | ON until run terminal | **OFF** |
| Keeper gauge watcher (per-session ctx-fill) | `internal/keeper/watcher.go:930`; interval `thresholds.go:127` | 5 s | ON | ON | ON until parked | **OFF** (skip `.sleeping.*` sessions) |
| Keeper heartbeat (refresh `.ctx`) | `internal/keeper/heartbeat.go:190` (called from watcher tick) | per-tick | ON | ON | ON | **OFF** |
| Reverse-tunnel worker-socket poll (remote agent_ready) | `internal/daemon/reversetunnel.go:263`, `:69` | 300 ms | ON (per remote run) | n/a | ON until run done | **OFF** |
| QuiesceArbiter / WindDownCoordinator tick | `internal/daemon/quiesce.go:413`, `:63` | 30 s | ON (wake-trigger arm) | ON | ON (executes park + failsafe) | reduced (failsafe-only) |
| Reconciliation cadence | `reconciliationcadence_rc020a.go:100`, `:44` | 1 h | ON | ON | ON | optional (cheap; may keep) |
| ctx-watchdog (Sonnet 300K governor) | `scripts/ctx-watchdog-launch.sh:54-65` (sleep-marker skip) | ~30 min | ON | ON | skip parked | **OFF for parked sessions** |
| DaemonWatchdog (supervisor revive) | `internal/supervise/daemon_watchdog.go:125`, `:62` | 30 s | ON | ON | ON | **ON** (always — keeps daemon alive even at rest) |

¹ StaleWatcher in WAITING: there are no in-flight runs to watch (RunRegistry empty), so it is effectively idle — keep it ON but it does nothing; cheap. (Marked OFF only conceptually.)
² DRAINING: keep StaleWatcher ON so a terminating run that hangs is still caught.

**The load-bearing gate is INACTIVE.** At INACTIVE the daemon: (a) stops the StaleWatcher / BandwidthTuner / DOT / reverse-tunnel scans (nothing in-flight to watch); (b) the keeper watchers skip every `.sleeping.*` session (already partially honored by the ctx-watchdog at `ctx-watchdog-launch.sh:54-65`, P0-a extends it daemon-side); (c) the QuiesceArbiter drops to failsafe-only (the 4 h auto-wake at `quiesce.go:69` still runs, plus the event-reflex wake triggers `QueueStore.WakeCh()` / `epic_completed` / `agent_message{to=captain}` stay armed so new work flips back to PROCESSING). The **DaemonWatchdog is the one watcher that NEVER stops** — it keeps the daemon process itself revivable at rest.

---

## 5. The fold — location + what it consumes (P2-b, hk-w6q7)

**Where it lives:** a new small file in `internal/daemon` (candidate `internal/daemon/fleetstate.go`), package `daemon`, alongside `quiesce.go` / `draindetect.go` / `runregistry.go` / `queuestore_hkj808w.go` — every input is already a `daemon`-package value, so no new dependency edges. It is the `RollUp` function in §3 plus the gating dispatcher in §4.

**What it consumes (all existing, no new readers):**
1. `*RunRegistry` — `.Len()` and `.Snapshot()` → per-handle `GetMachine().Current()` (`runregistry.go:148-173`, `:91-101`).
2. `*QueueStore` → each `*queue.Queue.Status` + `selectNextQueue`'s dispatchable test (`queuestore_hkj808w.go:68`, `workloop.go:1114-1140`).
3. `GatherDrainFacts()` (P1 reshape of `GenuineDrain`, `draindetect.go`) → the `DRAINED/HAS_WORK/UNSURE` verdict + per-axis counts. This is the single richest input and already fuses br-ready / blocked-epic / paused-queue facts.
4. Sleep markers — `.harmonik/.sleeping.<sid>` scan (`quiesce.go` marker dir `:71-74`; with P0-d `source`+`level` fields).
5. Session liveness — crew registry presence (`internal/crew/registry.go`) + `keeper.ResolveTmuxTarget` / `tmux has-session` for the captain (the same resolver `quiesce.go:resolveCaptainTarget` already uses).

**Size estimate:** the fold itself is the ~15-line `RollUp` + a ~10-line per-poll gate switch + ~25 lines of the 5 small `anyX(...)` helpers that wrap existing methods = **~50 lines**, matching the scaffold's P2-b estimate. It adds **zero new state** — it is a pure projection of `RunRegistry` + `QueueStore` + `GatherDrainFacts` + markers, computed on demand (called by `harmonik state` in hk-gv04 and by the poll-arming logic at daemon tick).

**Consumed by:** `harmonik state [--json]` (hk-gv04) prints the label + the unfolded leaf detail; the daemon tick reads the label to arm/disarm the §4 polls; the keeper/ctx-watchdog read the label (or the markers it's derived from) to skip INACTIVE sessions.

---

## 6. Where the real code can't cleanly support a label (flagged gaps)

1. **No FSM for the long-lived captain/crew sessions.** `lifecycle.Machine` is per-RUN only (`runregistry.go:82`). The 4 labels are about the *fleet*, but PROCESSING/WAITING leans on the captain/crew being "armed and idle" — and that aliveness is observed only via the keeper `.ctx` gauge + `tmux has-session` + crew-registry presence, **not** a state machine. WAITING and INACTIVE both depend on `SESSIONS_ALIVE`, which is a presence probe, not an FSM read. *Recommendation: keep the roll-up keyed off run-registry FSM + queue status + drain facts (all solid), and treat captain/crew liveness as a coarse boolean `SESSIONS_ALIVE` derived from the crew registry + tmux probe — do NOT invent a per-captain FSM in this pass (out of scope per the scaffold).*
2. **No `draining` queue status.** The queue model has `paused-by-drain`, not `draining` (`internal/queue/types.go:53-55`). DRAINING the *fleet label* is therefore synthesized from `paused-by-drain` + `StateTerminating` runs + sleep-marker presence, not read from a single field. This is fine but must be documented in `specs/system-state.md` so a reader doesn't expect a `queue.draining` enum.
3. **DRAINING vs WAITING can briefly race** — the moment a sleep marker is written but `GatherDrainFacts` still reports HAS_WORK (the captain chose to sleep over latent-but-non-dispatchable work). The first-match-wins ordering (DRAINING checked before WAITING because it tests markers) resolves this deterministically: marker present ⇒ DRAINING. State this rule explicitly in the spec.
4. **UNSURE has no dedicated label** — it folds into WAITING (fail-awake). Correct for safety (UNSURE keeps the fleet awake), but the spec should note WAITING ⊇ {HAS_WORK, UNSURE} so an operator reading WAITING knows it can mean "read error, assuming work."
