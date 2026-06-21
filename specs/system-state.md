# System State Model

```yaml
---
title: System State Model
spec-id: system-state
requirement-prefix: SS
spec-category: foundation-cross-cutting
status: draft
spec-shape: requirements-first
version: 1.0.0
spec-template-version: 1.1
owner: fleet-state-author
last-updated: 2026-06-20
depends-on:
  - operator-nfr            # ON-008/ON-010 sleep-is-LLM-initiated (P1-SPEC reword)
  - park-resume-protocol    # PARK/wake session-side contract + sleep markers
  - process-lifecycle       # daemon status; live-session enumeration
  - queue-model             # QueueStore statuses
  - execution-model         # RunState / in_flight(run); the per-run lifecycle FSM
  - beads-integration       # the work-axes (ready / in-progress / blocked / needs-attention)
---
```

> **Status: DRAFT (P2-DESIGN, pending operator sign-off).**
> This is the normative output of the `fleet-state` design pass (bead hk-9fvk).
> It GATES all Phase 2 code (`harmonik state` hk-gv04, the fold hk-w6q7,
> context-into-state hk-jay1) — no Phase 2 code lands before this spec is
> reviewed and signed off. It is a DRAFT until the operator ratifies it.
>
> Load alongside `captain/STARTUP.md` (the captain reads `harmonik state` to
> decide wind-down) and the `keeper` skill (the ctx-watchdog reads the cognition
> fields instead of eyeballing gauges).

---

## 1. Purpose

This spec defines harmonik's normative model of its **ACTUAL fleet state**: a
typed, facts-only snapshot that the captain LLM reads in order to decide
wind-down (SLEEP / PARK), and that the daemon's poll-arming logic and the
ctx-watchdog read to decide which watchers to run.

The snapshot **Gathers** real underlying facts (live sessions, queue statuses,
the run registry, per-session context sizes, the work backlog) and **folds**
them into four activity labels — but it **NEVER decides** sleep, park, stop, or
teardown. The decision to wind down requires understanding the backlog
(including latent and generative work that is not yet a dispatchable bead) and
is therefore the captain's judgment, not the framework's. This boundary is the
core invariant of the spec (§5, SS-INV-001) and is grounded in Zero Framework
Cognition (`docs/concepts/zero-framework-cognition.md` §51–56: Go does Gather /
Validate / Execute; the model does Decide; §32 bans completion-detection-as-
heuristic from the framework).

This is a **decision-locus correction plus a state model**. The deterministic
"drain oracle" (`GenuineDrain`) is *demoted* — its fact-gathering is kept as a
read-only tool (`GatherDrainFacts`, §4.3), but its `DRAINED → park everything`
auto-decision is removed (the captain decides; Go's only authority is a
one-directional veto-on-execute, §4.5 / SS-INV-005).

## 2. Scope

### 2.1 In scope

- The ACTUAL-state envelope: the top-level `StateSnapshot` shape (§4.1).
- The four activity labels and their normative predicates: PROCESSING /
  WAITING / DRAINING / INACTIVE (§4.2), and what each label gates (the poll
  inventory).
- The fact-bundle work-axes (`FleetFacts`): the demoted oracle, recast as a
  read-only counts-plus-lists tool that preserves the five false-negative
  defenses (§4.3).
- The cognition-observability fields: per-session context size, the
  too-big / not-changing / loop-detected signals, and their provenance (§4.4).
- The wind-down actuator boundary: the snapshot is read-only; non-force
  `harmonik sleep` veto-on-execute (§4.5).
- The seven ZFC-purity invariants (§5) and their sensors.
- The observation-only invariant (the snapshot never mutates, never affects an
  in-flight run).
- The desired-state placeholder (§6) — named, with the boundary rule stated
  normatively, but NOT modeled in v1.0.

### 2.2 Out of scope

- **The DESIRED-state model and reconcile loop** — Phase 4, on HOLD
  (bead hk-cyec). §6 is a forward-looking STUB only.
- **The wind-down workflows / `--level` enum** (L0 abandon / L1 drain /
  L2 handoff / L3 finish-lane) and the W1/W3/W4 sequences — owned by
  [park-resume-protocol.md] (P3-SPEC).
- **Auto-remediation** of a lost captain (token-burn after many `/clear`s) or a
  crew stuck at high context — operator-declared out-of-scope. The cognition
  fields of §4.4 make these *visible*; they do not remediate them (SS-INV-001,
  SS-012/SS-013).
- **A per-captain / per-crew session FSM.** Long-lived interactive sessions are
  observed via a coarse presence/liveness probe (`SESSIONS_ALIVE`, §4.2), NOT a
  state machine. The 8-state lifecycle FSM
  ([execution-model.md] / `internal/handlercontract/lifecycle`) stays attached
  to in-flight RUNS only. Inventing a session FSM is explicitly future work
  (see §6 note); v1.0 does not model it.
- The internal `QuiesceArbiter` / `quiesce.go` symbol rename — a separate
  low-priority cleanup (candidate `WindDownCoordinator`), not gated by this
  spec. SS-INV-006 scopes the "quiesce" ban to the operator-facing surface only.

## 3. Glossary

- **ACTUAL state** — what is true of the fleet right now: live sessions, queue
  statuses, the run registry, per-session context sizes, the work backlog. The
  entire normative body of v1.0 is ACTUAL-state. Contrast DESIRED state (§6,
  HOLD).
- **activity label** — one of the four roll-up labels (PROCESSING / WAITING /
  DRAINING / INACTIVE) that the snapshot computes by folding the underlying
  facts. An **observation** of current activity, never a directive (SS-INV-001).
- **work-axis** — one bucket of the work backlog (ready, in-progress, lined-up,
  paused/failed queues, needs-attention/draft/deferred, needs-decomposition),
  reported as a count plus a list, never collapsed to a single boolean
  (SS-INV-002).
- **fact bundle** (`FleetFacts`) — the typed, read-only bundle of work-axes the
  captain reads instead of running `br ready` / `br list` by hand; the demoted
  drain oracle as a tool (§4.3).
- **cognition-observability** — per-session context-size readings and the
  derived too-big / not-changing / loop-detected signals folded into the
  snapshot so a reader (captain, ctx-watchdog) reads them instead of eyeballing
  gauges (§4.4).
- **veto-on-execute** — the one-directional guard: non-force `harmonik sleep`
  re-gathers the facts and **refuses** (vetoes) if work would be stranded; the
  facts may veto a sleep, never command one (§4.5, SS-INV-005).
- **`in_flight(run)`** — the predicate naming runs not yet terminal, owned by
  [operator-nfr.md §3] / [execution-model.md §7.1]:
  `run.state ∉ {completed, failed, canceled}`. **Reused here; not redefined.**
  Evaluated against the daemon's authoritative in-memory run table
  (`RunRegistry`).
- **`SESSIONS_ALIVE`** — a coarse boolean: ≥1 long-lived captain/crew session is
  alive, derived from the crew registry presence record + a `tmux has-session`
  probe. NOT a state machine (§2.2).
- **at rest / asleep** — the resting state of a session that has been put to
  SLEEP or PARK and carries a live `.harmonik/.sleeping.<sid>` marker. The
  resting state is named "asleep" / "at rest"; the word **"quiesce" is retired**
  from the operator surface (SS-INV-006).
- **source: live | disk** — the duplication-resolution tag (§4.1): a fact read
  from in-daemon memory (`RunRegistry` / `QueueStore`) is `live` and wins; the
  same fact re-derived from disk (`queue.json`, the worktrees scan) is `disk`
  and is the daemon-down fallback only. Conflicting counts are NEVER summed.

### 3.1 Vocabulary table (operator-facing wind-down terms — "quiesce" retired)

| Term | Meaning | Reversible? |
|---|---|---|
| **SLEEP** | Operator "done for the day"; whole fleet, work-finishing. | yes (wake) |
| **PARK** | Interrupt-and-hold at the nearest safe point. | yes (wake) |
| **STOP** | One crew, pane killed. | re-startable |
| **TEARDOWN** | Irreversible fleet shutdown. | no |
| **asleep / at rest** | The resting *state* of a session under SLEEP or PARK. | — |

> The word **"quiesce"** MUST NOT appear in this spec, the `StateSnapshot` JSON
> field names, the CLI output of `harmonik state`, or any operator-facing copy
> (SS-INV-006). The internal `QuiesceArbiter` symbol rename is a separate
> low-priority cleanup and is out of scope. [park-resume-protocol.md] v1.0 still
> uses the word; its operator-facing scrub is P3-SPEC, not this spec.

## 4. Normative requirements

### 4.1 The state envelope (top-level snapshot shape)

#### SS-001 — `harmonik state` emits a single typed StateSnapshot

`harmonik state [--json]` MUST emit a single typed `StateSnapshot`:

```jsonc
{
  "schema_version": 1,
  "captured_at":    "2026-06-20T19:31:05Z",   // RFC-3339 read time
  "daemon":         { "up": true, "pid": 4123, "socket": "…" },
  "activity_label": "PROCESSING",             // SS-002..SS-006
  "runs":           [ /* in-flight runs; §4.1 SS-001a */ ],
  "queues":         [ /* per-queue status; source-tagged */ ],
  "sessions":       [ /* per-session liveness + sleep + cognition; §4.4 */ ],
  "work_axes":      { /* FleetFacts; §4.3 */ },
  "read_quality":   { "ok": true, "unsure": false, "reasons": [] }
}
```

The snapshot MUST report counts plus lists per axis; it MUST NOT collapse the
work picture to a single boolean (SS-INV-002). The snapshot is **observation-
only**: it MUST NOT mutate daemon state and MUST NOT affect any in-flight run
(SS-INV-007).

Tags: mechanism

#### SS-001a — Live-vs-disk source tagging; never sum conflicting counts

Every fact in the snapshot that has both an in-daemon reader and a disk reader
(per-queue status, the in-flight run count) MUST carry a `source: "live" |
"disk"` tag. When the daemon is UP, the in-memory reader (`RunRegistry`,
`QueueStore`) is authoritative and tagged `live`; the disk re-derivation
(`queue.json`, the `.harmonik/worktrees/*` scan) is the daemon-DOWN fallback,
tagged `disk`. The snapshot MUST NOT silently union or **sum** a live count with
a disk count for the same fact (e.g. `RunRegistry.Len()` and the worktrees-dir
count are never added — they are two readers of ONE truth). The fold's
run-presence test is therefore `RUNS_INFLIGHT = RegistryCount > 0 OR (daemon-down
AND LiveWorktrees > 0)` — an OR, NOT a sum: when the registry is empty or
unavailable AND the daemon is down, a leaked/live worktree still counts as
run-presence (so the fold does not under-count to INACTIVE on a blind disk read),
but the two readers are never added together. A daemon-down
snapshot MUST mark `runs[]` and `activity_label` as best-effort / `disk`-sourced
rather than omit them.

**Daemon-down ⇒ unsure ⇒ never INACTIVE (safety, NORMATIVE).** When the daemon is
DOWN (the snapshot is a disk-only read), the snapshot MUST set
`read_quality.unsure = true` and MUST NOT emit `activity_label: INACTIVE`. A blind
disk read cannot prove the fleet is at rest — stale or leaked worktrees can
over- OR under-count the run picture — so a daemon-down read must never license
sleep / stand-down. The best-effort label holds at WAITING or DRAINING (or is
reported as not-determinable); INACTIVE is reserved for a live, authoritative
read. (Sensor: a daemon-down snapshot test asserts `read_quality.unsure == true`
AND `activity_label != INACTIVE`.)

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### SS-002fold — The snapshot is a fold over EXISTING readers; no new persistent store

The `StateSnapshot` MUST be assembled from existing readers — the per-run
lifecycle FSM (`RunHandle.GetMachine()`), `QueueStore`, `RunRegistry`, the crew
registry, the keeper gauge (`.ctx`), the sleep markers (`.harmonik/.sleeping.*`),
and `GatherDrainFacts` (§4.3) — plus the daemon-down disk re-derivations of those
same facts. It MUST add **no new persistent store**: it is a pure projection
computed on demand. The Go shape is the union:
`digest.Build` (the disk/durable half — runs daemon-down) **∪** a new daemon-side
`state` RPC (snapshots `RunRegistry` + `QueueStore` and computes the §4.2 fold
in-daemon, where the in-memory readers live) **∪** a sleep-marker scan. The CLI
degrades to disk-only on daemon-down.

Tags: mechanism

The in-flight `runs[]` array carries, per run (read from `RunRegistry.Snapshot()`
+ each `RunHandle.GetMachine()`):

```go
// StateRun is one in-flight run, projected from a *RunHandle.
type StateRun struct {
	RunID          string `json:"run_id"`
	BeadID         string `json:"bead_id"`
	QueueName      string `json:"queue_name"`
	WorktreePath   string `json:"worktree_path,omitempty"`
	StartedAt      string `json:"started_at"`           // RFC-3339
	OwningEpicID   string `json:"owning_epic_id,omitempty"`
	OwningAssignee string `json:"owning_epic_assignee,omitempty"`
	// LifecycleState is the per-RUN FSM state, one of:
	// spawning | initializing | ready | executing | suspended |
	// terminating | terminated | failed. This FSM is attached to RUNS only,
	// never to a long-lived captain/crew session (§2.2).
	LifecycleState string `json:"lifecycle_state"`
	Source         string `json:"source"`               // "live" | "disk"
}
```

### 4.2 The four activity labels (normative predicates)

The activity label is a **fold** — a pure projection over three fact sources,
evaluated top-down, **first match wins**. Define the primitive facts (each reads
an existing daemon-package value):

```
RUNS_INFLIGHT      = RegistryCount > 0
                       //   OR (daemon-down AND LiveWorktrees > 0)   // §4.2 / SS-001a
                       // KEYS ON REGISTRY MEMBERSHIP, not the lifecycle FSM
                       // state. A run is registered for its ENTIRE life — claim
                       // → agent → review loop → merge (serialized on a global
                       // mutex) → build/vet gate → push-with-retry → worktree
                       // removal — typically 20–60 min, worst-case hours. So the
                       // PROCESSING window covers the whole post-agent
                       // merge/build/push/cleanup TAIL (the real teardown), which
                       // is exactly when run-watchers MUST stay armed (a hung
                       // merge / reviewer stall / remote worktree-removal is the
                       // silent-hang class to catch). The lifecycle FSM
                       // `Terminating` STATE — a sub-second SIGTERM-sent micro-
                       // state — is NOT this teardown window and is irrelevant to
                       // the label. When the registry is empty AND the daemon is
                       // down, a leaked/live worktree still counts (don't
                       // under-count to INACTIVE on a blind disk read).
                       // (See §4.2 note + SS-003 + SS-005.)
QUEUE_DISPATCHABLE = uses the exact selectNextQueue candidate filter
                       (workloop.go:1100, incl. EligibleItems) as the single
                       source of truth — an active queue has a slot under its
                       effective worker cap AND an eligible item to claim now.
QUEUE_DRAINING     = ∃ q ∈ QueueStore: q.Status == paused-by-drain
ANY_SLEEPING       = ∃ live .harmonik/.sleeping.<sid> marker
SESSIONS_ALIVE     = ∃ live captain/crew session (crew-registry presence +
                       tmux has-session)
HAS_LATENT_WORK    = GatherDrainFacts shows ≥1 non-empty axis OR Unsure==true
                       // ready / in-progress / paused-or-failed queue /
                       // blocked-by-open-epic / needs_decomposition / Unsure
                       // NB: needs_decomposition IS counted here (it keeps the
                       // fleet in WAITING, out of INACTIVE — that is mechanism).
                       // This is DISTINCT from the sleep veto (SS-015), which
                       // does NOT list needs_decomposition. "Keeps out of
                       // INACTIVE" ≠ "vetoes sleep".
```

**Priority order: PROCESSING > DRAINING > WAITING > INACTIVE.** The fold:

```go
func RollUp(rr *RunRegistry, qs *QueueStore, f *FleetFacts, markers, sessions) Label {
    if RUNS_INFLIGHT || QUEUE_DISPATCHABLE {            // SS-003
        return PROCESSING                               // RUNS_INFLIGHT incl. terminating
    }
    if QUEUE_DRAINING || ANY_SLEEPING {                 // SS-005
        return DRAINING
    }
    if SESSIONS_ALIVE && HAS_LATENT_WORK {              // SS-004 (Unsure folds here)
        return WAITING
    }
    return INACTIVE                                     // SS-006
}
```

**Note — `RUNS_INFLIGHT` keys on REGISTRY MEMBERSHIP, not the FSM state.** The
justification for "a terminating run = PROCESSING" is **registry membership**, not
the lifecycle FSM. A run is registered in `RunRegistry` for its ENTIRE life —
claim → agent → review loop → **merge** (serialized on a global mutex) →
**build/vet gate** → **push-with-retry** → **worktree removal** — which is
typically 20–60 min and worst-case hours, NOT brief. PROCESSING therefore covers
the whole post-agent merge/build/push/cleanup **tail** (the real "teardown"
window), which is exactly when the run-watchers MUST stay armed: a hung merge, a
reviewer stall, or a stuck remote worktree-removal is the silent-hang class this
label exists to keep caught.

This is a SEPARATE thing from the lifecycle FSM `Terminating` STATE. That FSM
state is a sub-second "SIGTERM-sent, process-exiting" micro-state — it transitions
back-to-back to `Terminated` with no work in between — and is NOT the teardown
window the label cares about. The removed `RUN_TERMINATING` DRAINING term keyed on
*that* sub-second FSM state, which is precisely why removing it was correct: while
the handle is registered it is already PROCESSING (by registry membership, through
the whole tail above), and once deregistered its FSM state is unreadable, so
`RUN_TERMINATING` could never fire without `RUNS_INFLIGHT` already having fired. Do
NOT conflate FSM-`Terminating` (sub-second, irrelevant) with registry-membership
teardown (the 20-min-to-hours tail, load-bearing). DRAINING now keys solely on
`QUEUE_DRAINING OR ANY_SLEEPING`. (See §9 changelog — #4 RATIFIED = PROCESSING by
the operator panel.)

#### SS-003 — PROCESSING

`PROCESSING ≡ RUNS_INFLIGHT OR QUEUE_DISPATCHABLE` — at least one run is
**registered** in `RunRegistry` (membership, NOT a particular FSM state) **or** an
active queue has an eligible item the dispatcher could claim right now. This is
"the fleet is actively doing, or about to do, bead work." Registry membership is
the key: a run stays registered through its whole life — claim → agent → review
loop → merge → build/vet → push → worktree removal — so PROCESSING deliberately
covers the long (20–60 min, worst-case hours) post-agent merge/build/push/cleanup
**tail**, the very window where a hung merge or reviewer stall must keep the
run-watchers armed. A run's lifecycle FSM `Terminating` state is a sub-second
SIGTERM micro-state and is NOT what licenses PROCESSING here — registry membership
is (§4.2 note). Because the label is a **union (max over the priority order)**, if
*any* run is registered or *any* queue is dispatchable, the whole fleet is
PROCESSING even when other crews are idle — the most-active leaf wins (the label
gates which polls run; a single live run must keep the run-watchers armed). The
per-session detail remains available in the unfolded `runs[]` / `sessions[]`
arrays.

Tags: mechanism

#### SS-004 — WAITING

`WAITING ≡ (NOT PROCESSING) AND (NOT DRAINING) AND SESSIONS_ALIVE AND
HAS_LATENT_WORK` — sessions are alive and armed, nothing is dispatchable right
now, but work is latent: ready beads exist but are epic-blocked or
assignee-wedged, a queue is paused-by-failure / paused-by-budget, an
un-reconciled `.json.failed-*` archive sits on disk, or a childless open epic
needs decomposition. The captain is the one who must act (decompose, unblock); Go
just waits. Direction of travel: back toward PROCESSING once the captain acts.

`WAITING ⊇ {HAS_WORK, UNSURE}`: a `GatherDrainFacts` read whose `Unsure == true`
ALSO lands here (fail-awake). An operator reading WAITING must understand it can
mean "the read was doubtful, assuming work" — `Unsure == true` MUST prevent
INACTIVE (SS-010, SS-INV-002).

Tags: mechanism

#### SS-005 — DRAINING

`DRAINING ≡ (NOT PROCESSING) AND (QUEUE_DRAINING OR ANY_SLEEPING)` — no new work
is being dispatched, but the fleet is mid-wind-down: a queue is `paused-by-drain`,
or a sleep marker has been written and the last things are being let to settle.
Direction of travel: heading toward INACTIVE.

A still-registered run does NOT key DRAINING — it keys PROCESSING by **registry
membership** (it counts toward `RUNS_INFLIGHT` for its whole merge/build/push/
cleanup tail; see the §4.2 note). The old `RUN_TERMINATING` DRAINING term keyed on
the sub-second lifecycle-FSM `Terminating` state, NOT on this teardown tail; it was
removed as redundant — a registered run is already PROCESSING, and once
deregistered its FSM state is unreadable.

**Disambiguator (DRAINING vs WAITING).** Both mean "not dispatching right now"
but differ on *intent*: DRAINING is a wind-down *in motion* (a marker written / a
queue drain-paused); WAITING is fully-armed idle with latent
non-dispatchable work. The first-match-wins ordering resolves the race
deterministically: if a sleep marker is present, the label is DRAINING **even
when** `GatherDrainFacts` still shows latent work (the captain chose to sleep over
latent-but-non-dispatchable work). Marker present ⇒ DRAINING. The latent-work
counts nevertheless remain VISIBLE in the unfolded `work_axes` even when the label
is DRAINING — folding to DRAINING never erases the per-axis counts, so a reader is
not left blind to the remaining work the marker-wins rule deprioritized.

**Note on the queue model.** There is no `draining` queue status; the queue model
has `paused-by-drain` ([queue-model.md] / `internal/queue/types.go`). The
DRAINING *fleet label* is SYNTHESIZED from `paused-by-drain` + sleep-marker
presence; a reader MUST NOT expect a `queue.draining` enum. (Terminating runs are
counted under PROCESSING via `RUNS_INFLIGHT`, not folded into DRAINING — see the
§4.2 note.)

Tags: mechanism

#### SS-006 — INACTIVE

`INACTIVE ≡ (NOT PROCESSING) AND (NOT DRAINING) AND (NOT WAITING)` — i.e.
**anything that is neither PROCESSING, DRAINING, nor WAITING**: every work axis
empty (whether or not a session is alive) OR no live sessions at all. The
"live session alive but zero work" case lands here cleanly — a session being
alive does NOT by itself license WAITING (that needs latent work too), so an
alive-but-workless fleet is INACTIVE. Concretely: no runs in-flight, no
dispatchable queue, `GatherDrainFacts` shows every axis empty with
`Unsure == false` — regardless of session liveness — OR no live sessions at all
(everything asleep / torn down). This is true rest, and the only label that
licenses standing down watchers.

INACTIVE is an **OBSERVATION, not a sleep command** (SS-INV-001). INACTIVE gates
**POLLS only** (SS-007); it MUST NEVER trigger a park. Park is the captain's
decision (SS-INV-004). A `StateSnapshot` whose `read_quality.unsure == true` MUST
NOT carry `activity_label: INACTIVE` (a doubtful read cannot justify standing
down watchers); it holds at WAITING or DRAINING. **A daemon-DOWN snapshot is one
such doubtful read by construction** (SS-001a): a disk-only read cannot prove the
fleet is at rest — leaked/stale worktrees can over- or under-count — so it MUST
set `unsure = true` and MUST NOT emit INACTIVE. INACTIVE is reserved for a live,
authoritative (daemon-up) read.

Tags: mechanism

#### SS-007 — The label gates which polls Go runs (poll-arming is mechanism)

The activity label gates which deterministic polls/watchers the daemon runs:
**INACTIVE ⇒ stop the run-watchers and per-run gauges; PROCESSING ⇒ all armed.**
This is mechanism (a deterministic poll-on/off rule keyed on activity), NOT a
wind-down decision (SS-INV-004). The poll inventory:

| Poll / watcher | PROCESSING | WAITING | DRAINING | INACTIVE |
|---|---|---|---|---|
| StaleWatcher (run staleness / silent-hang / never-spawned reaper) | ON | idle¹ | ON² | OFF |
| BandwidthTuner (auto-tune max-concurrent) | ON | ON | reduce | OFF |
| DOT gate-file poll (per-run verdict file) | ON (per active run) | n/a | ON until terminal | OFF |
| Keeper gauge watcher (per-session ctx-fill) | ON | ON | ON until parked | **ON for any live session (skip only `.sleeping.*`)**³ |
| Reverse-tunnel worker-socket poll (remote agent_ready) | ON (per remote run) | n/a | ON until done | OFF |
| WindDownCoordinator tick (the marker/wake plumbing) | ON (wake-arm) | ON | ON (park + failsafe) | reduced (failsafe-only) |
| ctx-watchdog (context governor) | ON | ON | skip parked | **ON for any live session (skip only `.sleeping.*`)**³ |
| DaemonWatchdog (supervisor revive) | ON | ON | ON | **ON (always)** |

¹ WAITING: StaleWatcher is **ON-but-idle** — there are no in-flight runs to watch,
so it costs nothing to leave armed. (This reconciles the change from lens-1's
"OFF" at WAITING: functionally identical — nothing to watch either way — just
flagged ON-but-idle rather than OFF so the arm/disarm transition is one fewer.)
² DRAINING: keep StaleWatcher ON so a terminating run that
hangs is still caught. ³ INACTIVE: the per-session **keeper gauge watcher** and
the **ctx-watchdog** are gated on SESSION LIVENESS, NOT on the activity label — a
live session keeps its context gauge armed even when the fleet label is INACTIVE
(the alive-but-workless case). The whole purpose of the cognition fields is to
catch an alive-but-confused captain/crew burning tokens at rest, so these two
watchers stand a session's gauge down ONLY when that session is actually
asleep/gone (the existing `.sleeping.*` skip), never merely because the fleet
label folded to INACTIVE. (This is the only liveness-gated, not label-gated, pair
in the table; the heavy RUN-watchers above still stand down at INACTIVE.) The
**DaemonWatchdog is the one watcher that NEVER
stops** — it keeps the daemon process itself revivable at rest. At INACTIVE the
event-reflex *wake* triggers (`QueueStore.WakeCh`, `epic_completed`,
`agent_message{to=captain}`) stay armed, so new work flips the fleet back to
PROCESSING (a deterministic reflex, ZFC-legal — not a completion decision).

The INACTIVE poll-off path MUST NOT share a call frame with any park/sleep
actuator — enforced by SS-INV-004's sensor (a poll-off keyed on activity is
mechanism; it must never be the trigger that parks).

Tags: mechanism

> **NB — two distinct axes.** The four activity labels are *observations* of
> current activity. The operator verbs SLEEP / PARK / STOP / TEARDOWN are a
> SEPARATE axis (a wind-down action a human or the captain takes). A label MUST
> NOT be read as a verb: `INACTIVE` does not mean "go to sleep"; it means "the
> fleet is at rest, so these polls may stand down."

### 4.3 The fact-bundle work-axes (the demoted oracle, as a tool)

#### SS-008 — `work_axes` is `FleetFacts`: per-axis counts + lists, never a verdict

`GatherDrainFacts(ctx) (*FleetFacts, error)` replaces the `GenuineDrain` oracle's
DRAINED control signal. It returns a typed fact bundle reporting facts per axis as
**counts + lists**; it MUST NOT render a DRAINED / HAS_WORK decision, MUST NOT
short-circuit at the first sign of work, and MUST carry no `State` / `DrainState`
/ `Drained bool` field (SS-INV-002). The bundle preserves all five false-negative
defenses of the oracle *as the sourcing of each axis* (§4.3a). The Go type:

```go
// FleetFacts is the read-only fact bundle the captain reads to decide whether to
// wind the fleet down. It reports per-axis counts + lists; it never renders a
// decision and never short-circuits at the first sign of work. Produced by
// GatherDrainFacts. (Demoted from GenuineDrain — the DrainState verdict is gone.)
type FleetFacts struct {
	// Dispatchable-now (defense #1, #5: br ready --limit 0).
	Ready BeadAxis `json:"ready"`

	// In-flight work (defense #4).
	InProgress BeadAxis `json:"in_progress"` // ledger in_progress
	Runs       RunAxis  `json:"runs"`        // RunRegistry + live worktrees

	// Lined-up / queued-but-not-yet-dispatchable (defense #2 queue, #3).
	Queued QueueAxis `json:"queued"`

	// Standalone blocked-by-an-open-epic (defense #2 epic).
	BlockedByOpenEpic []EpicBlockEdge `json:"blocked_by_open_epic"`

	// The other dropped buckets (not dispatchable, not lost).
	NeedsAttention BeadAxis `json:"needs_attention"`
	Draft          BeadAxis `json:"draft"`
	Deferred       BeadAxis `json:"deferred"`

	// The ONE generative category: flagged, never scored (SS-009).
	NeedsDecomposition []string `json:"needs_decomposition"` // childless OPEN epics

	// Read quality — a caveat, NOT a control signal (SS-010, SS-INV-002).
	Unsure        bool     `json:"unsure"`
	UnsureReasons []string `json:"unsure_reasons,omitempty"`

	GatheredAt string `json:"gathered_at"` // RFC-3339 read time, for staleness
}

type BeadAxis struct {
	Count int        `json:"count"` // == len(Beads) by construction
	Beads []BeadFact `json:"beads"`
}
type BeadFact struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Type   string   `json:"type"`
	Labels []string `json:"labels,omitempty"`
}
type RunAxis struct {
	RegistryCount int      `json:"registry_count"` // RunRegistry.Len()  (source: live)
	LiveWorktrees int      `json:"live_worktrees"` // .harmonik/worktrees (source: disk)
	WorktreePaths []string `json:"worktree_paths,omitempty"`
}
type QueueAxis struct {
	NonTerminalItems []QueueItemFact `json:"non_terminal_items"`
	PausedQueues     []PausedQueue   `json:"paused_queues"`   // failure/drain/budget
	FailedArchives   []string        `json:"failed_archives"` // *.json.failed-*
	Count            int             `json:"count"`
}
type QueueItemFact struct {
	Queue  string `json:"queue"`
	BeadID string `json:"bead_id"`
	Status string `json:"status"`
}
type PausedQueue struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type EpicBlockEdge struct {
	EpicID  string `json:"epic_id"`
	ChildID string `json:"child_id"`
}
```

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### SS-008a — The five false-negative defenses are preserved verbatim

`GatherDrainFacts` MUST preserve, as the **sourcing** of each axis, all five
false-negative defenses of the drain oracle (`draindetect.go`):

| # | Defense | Axis sourcing |
|---|---|---|
| 1 | **br-ready pagination** — never the default-paginated `Ready()` | `Ready` ← `br ready --limit 0` |
| 2 | **ledger-dep / epic gating** — deferred-for-ledger-dep + open-epic-blocked child are work | `Queued.NonTerminalItems` + `BlockedByOpenEpic` |
| 3 | **paused-by-failure / failed-archive** — any paused-by-* queue + un-reconciled `*.json.failed-*` is work | `Queued.PausedQueues` + `Queued.FailedArchives` (direct glob, NOT the archive filter) |
| 4 | **in-flight runs** — `RunRegistry.Len() > 0` OR a live worktree is a run | `Runs` |
| 5 | **kerf-next-empty** — MUST NOT consult `kerf next` (false-empty for works lacking a `bead_filter`); `br ready --limit 0` + the ledger are authoritative | policy (by omission) |

The bundle additionally enumerates the buckets the oracle *dropped* —
`InProgress`, `NeedsAttention`, `Draft`, `Deferred` — as first-class axes, and
adds `NeedsDecomposition` (§4.3 SS-009). The `failed_archives` scan MUST use the
direct glob (bypassing the archive filter); a reimplementation via normal queue
enumeration silently drops failed archives and re-opens a false-drain hole.

Tags: mechanism

#### SS-009 — `needs_decomposition` is flagged only, never scored

`needs_decomposition` (childless OPEN epics — the one work category the oracle is
structurally blind to, because the captain *generates* the work by decomposing
the epic) MUST appear as a flagged, enumerated list of epic IDs plus a count. It
MUST NOT carry a `priority`, `score`, `rank`, or `urgency` field, and the
snapshot MUST NOT auto-decompose it or let its *absence* license sleep. It is
handed to the captain LLM, which alone owns the judgment "this latent work means
don't wind down" (SS-INV-003).

Tags: mechanism

#### SS-010 — `read_quality` / `Unsure` is a caveat, never a license to act

`read_quality.unsure` (mirrored as `FleetFacts.Unsure`) MUST be set true on any
axis read error or transient inconsistency (the QM "all items terminal, status
not yet rolled" race; an unrecognised queue status; a nil epic seam). It is a
**read-quality caveat** the captain weighs — NOT a verdict, NOT a license to act,
NOT a command. `Unsure == true` MUST keep the fleet awake: it MUST prevent the
fold from emitting INACTIVE (folds into WAITING — fail-awake, SS-004 / SS-006),
and non-force `harmonik sleep` MUST refuse when `Unsure == true` (SS-015). It is
demoted from the oracle's UNSURE *verdict* (which kept the fleet awake as control)
to a flag; the safety it provided is re-homed in the fold (no INACTIVE on Unsure)
and the veto (refuse on Unsure).

Tags: mechanism

### 4.4 Cognition-observability fields

#### SS-011 — Per-session cognition is keyed by agent name; thresholds are config-sourced

Each entry in `sessions[]` MUST carry a `cognition` object capturing the session's
context size and three derived signals. It MUST be **keyed by agent name** (the
stable `.ctx` / `.sid` filename key, which survives the `/clear` session-id flip),
with the session UUID carried as a *resolved-at-read* attribute, not the join key.
Each entry MUST carry BOTH a `session_id_declared` (the UUID from the crew
registry, recorded at spawn) AND the live `session_id` (resolved-at-read from the
agent's `.sid` file); a mismatch between them MUST be surfaced as a read-quality
flag (`sid_desync: true`). The `/clear` SID-flip desync (the live `.sid` diverging
from the registry-declared UUID) MUST be VISIBLE in `state`, never silently masked
by overwriting one with the other.
Every threshold MUST be a *reference to the resolved keeper config*
(`ResolveKeeperConfig`, fail-loud) — **never a literal baked into the snapshot
builder**, and **never a product-imposed default**. Per the operator's standing
no-hardcoded-keeper-thresholds MANDATE (`docs/concepts/zero-framework-cognition.md`
+ the keeper config contract): the product imposes ZERO default keeper thresholds;
the operator sets every value via config/flag or `ResolveKeeperConfig` fails loud
(aggregated, naming each missing key). The NEW cognition knobs introduced here —
`stuck_min_intervals` (SS-012) and any future cognition threshold — are therefore
**opt-in**: they are NOT defaulted from the resolved struct, and until the operator
sets one the dependent signal is DARK/absent (e.g. `context_static.flat` is simply
not emitted — `null`), with NO convenience fallback. The signal records both the
`threshold_ref` (which config knob) and the resolved numeric (or `null` when the
knob is unset). (`loop_confidence_min` stays deferred with the `loop_detected`
producer, SS-013.) Shape:

```jsonc
// sessions[<agent>].cognition
{
  "agent":              "paul",          // STABLE join key, survives /clear
  "session_id":         "a1b2…",         // LIVE: resolved-at-read from .sid; may be ""
  "session_id_declared":"a1b2…",         // DECLARED: from the crew registry (spawn-time)
  "sid_desync":         false,           // read-quality flag: declared ≠ live (a /clear SID-flip)
  "context": {
    "tokens":      182304,               // CtxFile.Tokens, or heartbeat derive fallback
    "window_size": 1000000,              // 0 ⇒ resolved FallbackWindowSize
    "fill_frac":   0.182,                // tokens / effective window (cross-model primary)
    "source":      "gauge",              // gauge | heartbeat_derive | capture_pane | absent
    "gauge_ts":    "2026-06-20T19:30:00Z",
    "read_ts":     "2026-06-20T19:31:05Z",
    "age_seconds": 65
  },
  "signals": {
    "too_big":        { "tripped": false, "band": "warn",  "threshold_ref": "keeper.warn_abs_tokens", "threshold": 200000, "value": 182304 },
    "context_static": { "gauge_age_seconds": 65, "staleness_ref": "keeper.staleness", "staleness_s": 120, "tokens_unchanged_intervals": 0, "stuck_min_intervals_ref": "keeper.stuck_min_intervals", "stuck_min_intervals": null, "flat": null },
    "loop_detected":  { "tripped": false, "source": "haiku", "checked_ts": "…", "confidence": 0.0, "note": "" }
  },
  "subagents": null   // STRETCH SLOT (SS-014slot) — null/absent in v1, never plumbed
}
```

The `source` discriminator answers the known crew-gauge drift: a consumer MUST
treat `source: "absent"` as **unknown, not zero** (an `absent` gauge ≠ a small
context). `fill_frac` is the cross-model-safe primary fill (the absolute band
wins on a 1M window, the pct ceiling wins on a 200K window).

**Effective-band selection (NORMATIVE).** The cross-model fill selection rule is:
the effective band is `min(absolute_band_tokens, pct_ceiling × window_size)` —
i.e. whichever knob yields the smaller token budget wins. This is the real helper
`EffectiveBandTokens` / `minAbsOrPctCeil` in
`internal/keeper/thresholds.go` (~line 181); citing it makes "which knob wins"
testable, not prose. The same `min(abs, pct×window)` rule produces every band
level (`warn` / `act` / `force_act` / `hard_ceiling`).

Tags: mechanism

#### SS-012 — The three derived signals; observation, never remediation

`cognition.signals` MUST carry three signals, each a surfaced FACT for a reader
(the ctx-watchdog reads them instead of eyeballing gauges); the snapshot MUST NOT
itself restart, `/clear`, or remediate a session on them (auto-remediation is
out of scope, §2.2):

- **`too_big`** — context over the band. Input: `context.tokens` / `fill_frac`.
  Threshold-source: the resolved keeper band (`warn` / `act` / `force_act` /
  `hard_ceiling`). Reports the band *level* reached (not a bare bool) so a reader
  can choose nudge vs restart vs alarm, matching the `HardCeilingMode`
  (off | alarm | restart) config. Computed every snapshot build (a cheap
  deterministic comparison).
- **`context_static`** — context not changing, **reported as RAW FACTS, never a
  "stuck" verdict.** Declaring a session "stuck" from token-flatness in
  deterministic Go is exactly the ZFC §32 heuristic-judgment that SS-INV-001 bans
  (a session can be legitimately token-flat during a long test run or tool call).
  So this signal is renamed from the old `stale_stuck` (a verdict name) to
  `context_static` (a neutral, fact-not-verdict name) and reports ONLY facts about
  the gauge readings:
  - `gauge_age_seconds` — how old the gauge reading is (vs the `keeper.staleness`
    config); a **measurement-quality fact**, not a verdict.
  - `tokens_unchanged_intervals` — the count of consecutive gauge samples with no
    token movement. The prior per-agent token reading is read from the
    **ALREADY-EXISTING keeper gauge history** — the `.ctx` gauge's `Ts` / `Tokens`
    fields, written by the **keeper**, NOT by the snapshot path. This adds **no
    new persistent store** (reconciling SS-002fold: the snapshot READS existing
    gauge history, it does not write one), keeping SS-INV-007 true.
  - `flat` — a deterministic boolean = (`tokens_unchanged_intervals ≥
    stuck_min_intervals`). This is a FACT about the readings ("tokens have not
    moved for N intervals"), NOT a verdict that the session is stuck. Per the
    zero-defaults mandate (see SS-011 + `docs/concepts/zero-framework-cognition.md`
    and the no-hardcoded-keeper-thresholds rule), `stuck_min_intervals` has NO
    product default: until the operator sets it in the keeper config, `flat` is
    **DARK/absent** (`null`, not emitted) and `ResolveKeeperConfig` fails loud on
    any path that requires it — no convenience fallback.

  `context_static` MUST NOT render or be named a "stuck" judgment, and NO Go path
  may act on it as a stuck-verdict. The interpretation "flat tokens means this
  session is actually stuck" belongs to the READER — the captain, or the same
  gated model pass as `loop_detected` (SS-013) — exactly mirroring how `too_big`
  reports a band *level* (a fact) not a restart verdict, and how `loop_detected`
  is a model call. It MUST distinguish *gauge stale* (a measurement gap —
  `source: absent/stale`, a read-quality flag) from *genuinely flat* (token-flat
  on a FRESH gauge): an `absent`/stale gauge is NOT `flat`, so the reader does not
  mistake a healthy-but-unmeasured crew for a static one. Computed every snapshot
  build (a pure read over the existing gauge history).
- **`loop_detected`** — repeating-pattern detection. See SS-013.

Tags: mechanism

#### SS-013 — `loop_detected` is a Haiku model call with provenance, never a Go heuristic

The `loop_detected` signal ("is this session doing the same thing over and over"
— re-running a command, re-reading a file, an ACT-loop, a reviewer/implementer
ping-pong) MUST be produced by a **Haiku model call** over the last K
assistant/tool turns of the session transcript, returning
`{tripped, confidence, note}` with provenance (`source: "haiku"`, `checked_ts`).
It MUST NOT be a Go regex / substring / pattern-match heuristic (that is the
ZFC §32 keyword-completion anti-pattern); the snapshot merely *carries* the
reported result. It MUST NOT run on every snapshot build (that re-introduces a
constant LLM poll — exactly what the "context-into-state, not a constant checker"
decision removes). It runs **on-demand, gated**: only for sessions whose
deterministic signals already look suspicious (`too_big.tripped` OR
`context_static.flat`); cost scales with suspicion, not fleet size.

**Producer DEFERRED (v1.0 does not specify it).** WHERE the loop-check runs and
HOW its verdict is written into state is an OPEN design question, deferred to a
later slice — v1.0 does NOT prescribe a mechanism (an earlier draft invented a
`harmonik keeper loop-check` verb; that was undiscussed and is withdrawn). The two
**deterministic** cognition signals (`too_big`, `context_static`, SS-012) are the
ACTIVE v1.0 signals — both are cheap read-only computations safe inside
`harmonik state`. `loop_detected` is a **RESERVED** signal whose producer is TBD:
until a producer is chosen and built, `harmonik state` reports `loop_detected` as
absent / `null`, and the snapshot path MUST NOT itself run the Haiku pass or write
any loop verdict (SS-INV-007 — `state` stays read-only). What v1.0 DOES fix is the
purity constraint that binds whatever producer is chosen later: the signal MUST
come from a model call with provenance, never a Go heuristic, and no Go path may
auto-act on it (SS-INV-001). The producer mechanism is its own future decision.

Tags: mechanism

#### SS-014slot — `subagents` is a reserved null stretch slot

`cognition.subagents` MUST be present as a reserved name (`null` / absent) in
v1.0, with **no reader, no writer, and no signal derivation**. It reserves the
shape (an array of per-subagent `{subagent_id, tokens, fill_frac, source,
gauge_ts}`, same as the session `context` block) so a later slice can populate it
without a snapshot-format break. The session-level signals of SS-012/SS-013
generalize per-subagent unchanged once the readings exist; nothing in v1 logic
assumes session-only.

Tags: mechanism

### 4.5 The wind-down actuator boundary (veto-on-execute)

#### SS-015 — Non-force `harmonik sleep` re-gathers facts and refuses if work would be stranded

The `StateSnapshot` is read-only; the actuators (`sleep` / `wake` / `park` /
`teardown`) are the ONLY mutators and live in the command surface, not in the
snapshot path. Non-force `harmonik sleep` MUST re-gather `FleetFacts` (§4.3) at
execute time and **REFUSE** (veto, exit non-zero) if any dispatchable or
in-flight axis is non-empty (`Ready` / `InProgress` / `Runs` / a paused-or-failed
queue / a blocked-by-open-epic child), OR if `Unsure == true`. `--force`
overrides the veto.

`needs_decomposition` is **NOT a veto axis** — a childless open epic does not
block a non-force `sleep`; the captain may legitimately choose to sleep over
un-decomposed generative work (that is the captain's judgment, per SS-INV-003).
The veto enumeration above is exhaustive and does NOT list `needs_decomposition`.

The facts may **veto** a sleep; they MUST NEVER **command**
one — `GatherDrainFacts` has exactly two consumers: (a) the `harmonik state`
output and (b) the `sleep` veto-guard, and zero call-sites that invoke an
actuator (SS-INV-005). This satisfies "don't sleep over work" without the daemon
*deciding* to sleep. (Cross-ref [operator-nfr.md] ON-008 / ON-010, reworded in
P1-SPEC from "daemon auto-sleeps when drained" to "sleep is LLM-initiated; the
daemon refuses to execute a sleep that would strand work.")

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

## 5. Invariants

These invariants keep the Go side **facts-only**. Each is a normative MUST with a
sensor a reviewer or test can mechanically catch.

#### SS-INV-001 — The snapshot reports facts; it never decides sleep / wake / park

The system-state snapshot (`harmonik state`, the `StateSnapshot`, the
`GatherDrainFacts` bundle, and the cognition signals) MUST be a pure
**Gather + structural-fold** operation. It MUST NOT contain, return, or persist
any field whose value is a wind-down *decision* — no `should_sleep`,
`recommend_park`, `wind_down: bool`, or equivalent. The four activity labels are
**observations** of current activity, never directives.

**Sensor.** Grep the `StateSnapshot` / `FleetFacts` struct + their JSON tags for
decision-verbs (`should_`, `recommend_`, `auto_`, `_decision`, `wind_down`) — a
match is a BLOCK. A unit test asserts the snapshot type has no boolean/enum field
that an actuator (`sleep` / `wake` / `park`) consumes as its *trigger*; actuators
take the snapshot as *evidence to validate against*, never as a command.

#### SS-INV-002 — Work is never collapsed to a single DRAINED boolean

The snapshot MUST emit **all work axes** as **counts + lists** (ready,
in-progress, lined-up / blocked-by-open-epic, paused/failed queues,
needs-attention / draft / deferred, childless-open-epics). It MUST NOT
short-circuit at the first sign of work and MUST NOT expose a single
`drained: bool` (or re-use `DrainState{DRAINED}` as a control signal). `Unsure`
survives only as a read-quality flag, never as a license to act.

**Sensor.** The banned `Drained` field can never be grepped (SS-008 forbids it
existing at all), so a "no `Drained` field" grep is vacuous. The load-bearing
sensor instead asserts: **no actuator (`sleep` / `park` / `wake`) call-site is
gated on a caller-side fold over `FleetFacts` fields** — e.g. no
`if len(facts.Ready)==0 && len(facts.Runs)==0 && … { sleep() }` reconstructing the
banned DRAINED boolean from the per-axis counts. This is caught by SS-INV-005's
zero-actuator-call-sites test (the only consumers of `GatherDrainFacts` are
`harmonik state` output and the `sleep` veto-guard). A complementary test asserts
the fact bundle returns per-axis counts even when every axis is non-empty (no
early short-circuit / `return DRAINED`).

#### SS-INV-003 — Generative categories are flagged for the captain, never scored or auto-acted

Childless open epics (`needs_decomposition`) and any other generative / latent
work category MUST appear as a flagged, enumerated fact (count + epic IDs). The
snapshot MUST NOT score, rank, prioritize, or auto-decompose them, and MUST NOT
let their *absence* license sleep. They are handed to the captain LLM, which alone
owns the judgment that latent work means "don't wind down."

**Sensor.** The `needs_decomposition` axis is a list of IDs + a count — no
`priority` / `score` / `urgency` field on it. A test: a project with only
childless-open-epics (zero ready beads) produces a snapshot whose work-axes are
non-empty / flagged (the epics appear in `needs_decomposition`), giving the
CAPTAIN grounds to refuse to wind down. **Note:** `needs_decomposition` does NOT
mechanically veto sleep (SS-015) — the test asserts the axis is FLAGGED, NOT that
the Go `sleep` veto refuses. A childless-open-epics-only project has every
*dispatchable / in-flight* axis empty, so the Go veto would NOT fire; the
flagged-but-non-vetoing axis is the captain's generative judgment to weigh.

#### SS-INV-004 — No Go auto-drain: the captain initiates sleep; Go validates + executes

No Go code path MAY *initiate* a wind-down (SLEEP / PARK) from the snapshot. There
MUST be no timer, tick, or reflex that reads the snapshot and calls an actuator.
The captain LLM initiates; Go's role is veto-on-execute (re-gather, refuse if work
would be stranded) + mechanical execution. The 4h max-sleep failsafe and the
event-reflex *wake* survive (they are deterministic policy / reflex, not
completion decisions). INACTIVE gates POLLS only (SS-007); it MUST NEVER trigger a
park.

**Sensor.** A test asserts `GenuineDrain` / `DrainStateDrained` (the demoted
drain oracle's verdict) has **ZERO call-sites inside any `time.Ticker` / goroutine
tick body** — i.e. no Go code path autonomously parks on drain. This is a
**positive structural assertion of absence**, independent of whether any deletion
has happened: even before P1-b's removal of the auto-park tick lands, the sensor
holds iff no tick body folds drain into a park. Equivalently, grep for any
goroutine / `time.Ticker` whose body calls `parkAllSessions` / `parkSession`
*without* a CLI-`sleep` / operator command on the stack — the only callers of the
park actuator are (a) the `harmonik sleep` command path and (b) the 4h failsafe. A
test asserts no snapshot-reading code path reaches an actuator. (Implementing
work: P1-b deletes the `quiesce.go` drain-tick + `SetDrain` daemon wiring; the
sensor asserts the *absence of the path*, not the deletion event.)

#### SS-INV-005 — The veto is one-directional (facts may refuse sleep, never command it)

The fact bundle MAY **veto** a sleep (non-force `harmonik sleep` re-gathers and
refuses if any axis shows dispatchable / in-flight work, or on `Unsure`). The fact
bundle MUST NEVER **command** a sleep. This asymmetry keeps Go facts-only while
still closing the dead-fleet-on-ready-beads wound.

**Sensor.** A test asserts `GatherDrainFacts` has zero call-sites that invoke an
actuator; its only consumers are (a) `harmonik state` output and (b) the `sleep`
veto-guard. The five false-negative defenses (SS-008a) are preserved verbatim —
they are read-correctness, not judgment.

#### SS-INV-006 — Vocabulary: "quiesce" is absent from the operator-facing surface

The spec, the CLI output, the snapshot field names, and all operator-facing copy
MUST use **SLEEP / PARK / STOP / TEARDOWN**; the resting state is "asleep" / "at
rest." The word **"quiesce"** MUST NOT appear in any operator-facing string, JSON
field, event, or doc surface of this spec. (The internal `QuiesceArbiter` /
`quiesce.go` symbol names are out of scope — a low-priority rename, candidate
`WindDownCoordinator`.)

**Sensor.** Grep `specs/system-state.md`, the `StateSnapshot` JSON tags, and
`cmd/harmonik/state*.go` operator-facing output strings for `quiesce` / `Quiesce`
— zero hits in operator-facing positions. A reviewer flag fires on any new
operator-facing use. (Known residue: [park-resume-protocol.md] v1.0 still uses the
word; its operator-facing scrub is P3-SPEC, tracked separately.)

#### SS-INV-007 — The snapshot is observation-only (no mutation, no in-flight effect)

`harmonik state` / the snapshot path MUST NOT mutate daemon state and MUST NOT
abort, pause, or resume any in-flight run. It is a read surface, exactly like
`harmonik subscribe` (cf. [operator-nfr.md] ON-INV-006's exemption-by-
construction for `subscribe`).

**Sensor.** The snapshot path takes only read locks / read-only adapters; a test
asserts no write to `.harmonik/` and no actuator call during a `harmonik state`
invocation. The `state` path NEVER writes — in particular it never runs the
`loop_detected` Haiku pass and never writes a loop verdict. Whatever producer is
later chosen for `loop_detected` (DEFERRED, SS-013) writes OUTSIDE the `state`
path; `harmonik state` only *reads* an already-written loop verdict if one exists,
and reports the signal absent / `null` when none does.

> **Tie-back to ZFC.** SS-INV-001/002/003 enforce **Gather**-step purity (no
> interpretation, ZFC §24); SS-INV-004/005 enforce that **Decide** lives only in
> the model (the ZFC §32 completion-detection ban); SS-INV-007 enforces smart
> endpoints / dumb pipes (ZFC §34–35). The snapshot is the highest-value "bone":
> Gather made consistent, never Decide.

## 6. Desired state (Phase 4 — HOLD; NOT modeled in v1.0)

This section is a **forward-looking STUB**. The full actual↔desired reconcile
controller is Phase 4, on HOLD (bead hk-cyec), and is its own kerf work. v1.0
models ACTUAL state only.

1. **Concept (named for a stable anchor).** A *desired-state document* would
   declare which sessions / crews / queues *should* exist, with a per-session
   `state ∈ {ready, suspended, torn-down}`. Downstream specs may cite this name;
   nothing in v1.0 implements it.
2. **Boundary rule (NORMATIVE).** The ACTUAL-state `StateSnapshot` of this spec
   MUST NOT embed, infer, or persist any desired-state field. No desired-state
   field may appear in the actual snapshot. Desired state, when introduced, is
   authored by the captain LLM (cognition) and validated *structurally* by Go
   (name regex + referential integrity only — **no merit judgment**); it is its
   own kerf work and does NOT alter the ACTUAL-state contract.
3. **HOLD condition.** Phase 4 is revisited only if Phase 2 proves the ACTUAL
   fact snapshot insufficient. Until then this section forbids desired-state from
   leaking into v1.0 (the ZFC-overapplication temptation — "framework
   intelligence in prompt-land", ZFC §65 — is kept out of scope by construction,
   not left as silence that invites it).

> **Note — no session FSM in v1.0.** Long-lived captain/crew sessions are
> observed via the coarse `SESSIONS_ALIVE` presence probe (§4.2), NOT a state
> machine. A per-session FSM (analogous to the per-run lifecycle FSM) is
> explicitly future work, not modeled here. The desired-state per-session
> `state ∈ {ready, suspended, torn-down}` enum above is the eventual home for
> that concept — in Phase 4, not v1.0.

## 7. Conformance & sensors

An implementation conforming to v1.0 MUST pass every requirement SS-001 through
SS-015 (including SS-001a, SS-002fold, SS-008a, SS-014slot) and every invariant
SS-INV-001 through SS-INV-007. The sensor for each invariant is stated inline in
§5; the per-label predicate sensors are:

- **Per-label predicate tests** — fixtures exercising each of PROCESSING /
  WAITING / DRAINING / INACTIVE against the §4.2 predicates, including the
  first-match-wins DRAINING-vs-WAITING race (marker present ⇒ DRAINING) and the
  `Unsure ⇒ not INACTIVE` fail-awake.
- **No-decision-field grep** (SS-INV-001), **no-DRAINED-boolean** (SS-INV-002),
  **generative-only-still-vetoes** (SS-INV-003), **no-auto-park-tick grep**
  (SS-INV-004), **GatherDrainFacts-has-no-actuator-callsite** (SS-INV-005),
  **no-"quiesce"-in-operator-surface grep** (SS-INV-006),
  **state-is-read-only** (SS-INV-007).
- **Live-vs-disk** — a test that a daemon-up snapshot tags queue/run facts
  `live` and a daemon-down snapshot tags them `disk`, and that no count sums a
  live and a disk reader of the same fact (SS-001a).
- **Cognition provenance** — a test that `loop_detected.source == "haiku"` (never
  a Go heuristic, SS-013) and that thresholds carry a `threshold_ref` rather than
  a literal (SS-011).
- **Poll-gating conformance** (SS-007) — a test drives the daemon to each of the
  four labels and asserts the armed/disarmed watcher set matches the SS-007 table:
  at minimum INACTIVE disarms StaleWatcher + BandwidthTuner + the DOT gate-file
  poll + the reverse-tunnel poll, while the **keeper gauge watcher and ctx-watchdog
  stay ON for any live session at INACTIVE** (skip only `.sleeping.*`,
  liveness-gated not label-gated) and DaemonWatchdog stays ON in all four labels.
- **Daemon-down ⇒ unsure ⇒ never INACTIVE** (SS-001a / SS-006) — a daemon-down
  snapshot test asserts `read_quality.unsure == true` AND
  `activity_label != INACTIVE`.
- **Cognition knobs opt-in / dark-when-unset** (SS-011 / SS-012) — a test that
  with `stuck_min_intervals` unset, `context_static.flat` is `null`/absent (no
  product default), and that `ResolveKeeperConfig` fails loud on a path requiring
  an unset cognition threshold.

## 8. Bead references

| Bead | Role |
|---|---|
| hk-up4b | Umbrella epic — `fleet-state` (fleet sleep/wake + state model) |
| **hk-9fvk** | **P2-DESIGN pass → this spec (gates Phase 2 build)** |
| hk-8lne | P2-SPEC — `specs/system-state.md` normative ratification |
| hk-pfr4 | P1 — reshape `GenuineDrain` → `GatherDrainFacts` (the §4.3 fact bundle) |
| hk-gv04 | P2-a — `harmonik state [--json]` aggregator (dep: hk-pfr4, hk-9fvk) |
| hk-w6q7 | P2-b — the system-state fold + poll-gating (dep: hk-pfr4, hk-9fvk) |
| hk-jay1 | P2-c — context-into-state (session + subagent; loop/stale signals) (dep: hk-9fvk) |
| hk-zqb3 | non-force `sleep` = veto-on-execute → SS-015 (+ SS-INV-005) |
| hk-kj7d | delete the auto-park tick → SS-INV-004 |
| hk-cyec | P4 — desired-state reconcile loop — **HOLD** (§6 stub) |

## 9. Changelog

| Date | Version | Author | Change |
|---|---|---|---|
| 2026-06-20 | 1.0.0 | fleet-state-author | Initial DRAFT — synthesis of the five-lens `fleet-state` P2-DESIGN pass (per-session-FSM roll-up, the `GatherDrainFacts` fact bundle, cognition-observability, the union-of-readers survey, the ZFC spec shape). Establishes the ACTUAL-state envelope (SS-001/SS-001a/SS-002fold), the four activity-label predicates PROCESSING/WAITING/DRAINING/INACTIVE with priority order PROCESSING > DRAINING > WAITING > INACTIVE and first-match-wins (SS-003–SS-007), the fact-bundle work-axes preserving the five false-negative defenses (SS-008/SS-008a/SS-009/SS-010), the cognition-observability fields (SS-011–SS-014slot), the veto-on-execute boundary (SS-015), the seven ZFC-purity invariants (SS-INV-001–007), the desired-state HOLD stub (§6). Status: DRAFT, pending operator sign-off. IDs frozen once ratified; retirements logged here and never reused. |
| 2026-06-20 | 1.0.0-draft → review pass 3 (design panel) | fleet-state-author | **Design panel (4-analyst) pass; status stays DRAFT.** (1) **Terminating-run = PROCESSING RATIFIED** (operator panel): the justification is tightened to **registry membership**, NOT the lifecycle FSM. A run is registered for its whole life (claim → agent → review loop → merge → build/vet → push → worktree removal; 20–60 min, worst-case hours), so PROCESSING covers the merge/build/push/cleanup TAIL — the real teardown, exactly when run-watchers must stay armed. The FSM `Terminating` STATE is a sub-second SIGTERM micro-state (irrelevant to the label); the removed `RUN_TERMINATING` term referenced that micro-state, which is why removing it was correct (§4.2 RUNS_INFLIGHT comment + note, SS-003, SS-005). (2) **`stale_stuck` → `context_static`** — demoted from a Go JUDGMENT to RAW FACTS (ZFC §32 / SS-INV-001 conformance): renamed to a fact-not-verdict name; reports `gauge_age_seconds`, `tokens_unchanged_intervals`, and a deterministic `flat` bool only; MUST NOT be a "stuck" verdict and no Go path may act on it as one (the "actually stuck" reading is the reader's / a gated model pass, mirroring `too_big`'s band-level and `loop_detected`'s model call); gauge-stale ≠ flat preserved (SS-011 JSON, SS-012, SS-013 gating cross-ref). (3) **Keeper gauge + ctx-watchdog armed on SESSION LIVENESS, not label** — at INACTIVE these two stay ON for any live session (skip only `.sleeping.*`), reversing the drafted INACTIVE-stand-down, so an alive-but-confused captain/crew burning tokens at rest is still caught (SS-007 table rows + footnote ³). **Operator may reverse.** (4) **Daemon-down ⇒ unsure ⇒ never INACTIVE** — a disk-only read cannot prove rest (leaked/stale worktrees mis-count), so a daemon-down snapshot MUST set `read_quality.unsure = true` and MUST NOT emit INACTIVE; holds at WAITING/DRAINING (SS-001a + SS-006, +§7 sensor). (5) **New cognition knobs opt-in / fail-loud** — honoring the zero-defaults mandate, `stuck_min_intervals` (and future cognition thresholds) are NOT product-defaulted; until operator-set the dependent signal (`context_static.flat`) is DARK/absent and `ResolveKeeperConfig` fails loud; `loop_confidence_min` stays deferred (SS-011 + SS-012). (6) **Fold run-presence accounts for live worktrees** when the registry is empty/unavailable AND daemon-down: `RUNS_INFLIGHT = RegistryCount > 0 OR (daemon-down AND LiveWorktrees > 0)` — an OR, never a sum (§4.2, SS-001a). (7) **Marker-wins keeps latent counts visible** in the unfolded `work_axes` even at DRAINING (decision marker-wins ⇒ DRAINING KEPT; reader not left blind) (SS-005). REVIEWED & KEPT AS-IS: the max/union rollup (SS-003), the five false-negative defenses verbatim (SS-008a). |
| 2026-06-20 | 1.0.0-draft → review pass 2 (operator) | fleet-state-author | **Operator review pass.** Decisions: (1) RATIFIED — `needs_decomposition` does not block wind-down. (2) AGREED — `harmonik state` is read-only. (3) RESHAPED — the `harmonik keeper loop-check` verb (an undiscussed, agent-invented mechanism) is WITHDRAWN; `loop_detected`'s producer is now DEFERRED (reserved signal, producer TBD), leaving `too_big` + `stale_stuck` as the active v1 cognition signals; the model-call-not-Go-heuristic purity constraint is retained for whatever producer is chosen later (SS-013, SS-INV-007 updated). (4) OPEN — terminating-run = PROCESSING is NOT yet ratified; awaiting operator decision after a plain-English explanation of the teardown edge case (§4.2 / SS-005 left as-drafted pending that call). Status stays DRAFT. |
| 2026-06-20 | 1.0.0-draft → review pass 1 | fleet-state-author | **Review pass 1** (two adversarial reviews resolved; 14 fixes, status stays DRAFT). (1) Resolved the SS-INV-003 ↔ SS-015 contradiction: `needs_decomposition` is NOT a sleep-veto axis — it keeps the fleet in WAITING/out of INACTIVE (mechanism, §4.2) but does NOT fire the Go veto; the captain owns the generative judgment (SS-009/SS-015/SS-INV-003). (2) Removed `RUN_TERMINATING` from the DRAINING predicate as redundant: a still-registered terminating run counts toward `RUNS_INFLIGHT` (PROCESSING through teardown); DRAINING now keys on `QUEUE_DRAINING OR ANY_SLEEPING` (§4.2/SS-003/SS-005). (3) `harmonik state` is purely read-only; the loop-detector Haiku pass + `<agent>.loop.json` write move to a SEPARATE `harmonik keeper loop-check` verb (no `--refresh-loop-check` flag) (SS-013/SS-INV-007). (4) SS-INV-004 sensor reworded to a positive absence-assertion (zero `GenuineDrain`/`DrainStateDrained` call-sites in any tick body). (5) §7 poll-gating conformance sensor added (SS-007). (6) SS-007 co-location guard. (7) SS-012 `stale_stuck` reads the existing keeper `.ctx` gauge history, no new store. (8) SS-011 effective-band formula made normative (`min(abs, pct×window)`, `EffectiveBandTokens`). (9) SS-011 declared-vs-live SID split + `sid_desync` read-quality flag. (10) §4.2 `QUEUE_DISPATCHABLE` re-stated as the exact `selectNextQueue` candidate filter (workloop.go:1100). (11) SS-INV-002 sensor retargeted to "no actuator gated on a caller-side fold over `FleetFacts`". (12) SS-007 WAITING StaleWatcher footnote = ON-but-idle. (13) SS-006 prose: alive-but-workless = INACTIVE. (14) §8 bead map +hk-zqb3 (SS-015) +hk-kj7d (SS-INV-004). |

> **Operator ratification status (through review pass 3 — design panel).**
> 1. **`needs_decomposition` does NOT block wind-down** — ✅ RATIFIED.
> 2. **`harmonik state` is read-only** — ✅ AGREED.
> 3. **`loop_detected` producer** — DEFERRED per operator (the invented
>    `keeper loop-check` verb is withdrawn); reserved signal, producer TBD; the two
>    deterministic signals stay active in v1 (SS-013/SS-INV-007).
> 4. **Terminating-run classification** — ✅ RATIFIED = **PROCESSING** (operator
>    panel, pass 3), justified by **registry membership** (the whole
>    merge/build/push/cleanup tail), NOT the sub-second FSM `Terminating` state
>    (§4.2/SS-003/SS-005).
> 5. **`stale_stuck` → `context_static`** — ✅ RESOLVED (panel): a FACT
>    (`gauge_age_seconds` + `tokens_unchanged_intervals` + deterministic `flat`),
>    never a Go "stuck" verdict; the stuck reading is the reader's / a gated model
>    pass (SS-012).
> 6. **Keeper gauge + ctx-watchdog armed on session liveness** at INACTIVE (not
>    label-gated) — ✅ RESOLVED (panel), **operator may reverse** (this widens
>    at-rest gauge polling; flagged for a one-word confirm) (SS-007).
> 7. **Daemon-down ⇒ unsure ⇒ never INACTIVE** — ✅ RESOLVED (panel): a disk-only
>    read can't prove rest (SS-001a/SS-006).
> 8. **Cognition knobs opt-in / fail-loud** — ✅ RESOLVED (panel): zero product
>    defaults; `context_static.flat` is DARK until `stuck_min_intervals` is
>    operator-set (SS-011/SS-012).
>
> Plus, once cognition is built (hk-jay1): the keeper-config knob
> `stuck_min_intervals` (SS-012) must be added as an **opt-in, fail-loud** knob
> (no product default). (`loop_confidence_min` is deferred with the loop-signal
> producer.)
