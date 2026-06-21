# Lens 4 — Union-of-existing-readers survey for `harmonik state`

**Codename:** `fleet-state` · **Bead:** hk-gv04 (the `harmonik state [--json]` aggregator) · feeds the P2-DESIGN keystone (hk-9fvk).
**Lens charge:** make `harmonik state` *aggregate real sources* rather than invent them. This doc is the SOURCE→FIELD map: which existing reader supplies each envelope field, the exact call/file, and the new-vs-reprojection split.

---

## 0. The one architectural fact that shapes everything

There are **two distinct read substrates**, and the `state` command must straddle them:

1. **Durable-file readers** — work from any process, no daemon required. `queue.json`, `events.jsonl`, `.harmonik/crew/*.json`, `.harmonik/keeper/<agent>.ctx`, `.harmonik/.sleeping.*` markers, `br`/`kerf`/`git` subprocess calls. This is exactly what `harmonik digest` uses today — its help text literally says *"compute the cognition-loop status sheet (no daemon required)"* (`cmd/harmonik/digest.go:39`), and `digest.buildQueueSummary` reads queue state via `queue.Load(...)` from **disk** (`internal/digest/builder.go:241`), NOT from the live `QueueStore`.

2. **In-daemon memory readers** — `RunRegistry` and `QueueStore` are *fields on the running daemon*, not package globals (`internal/daemon/runregistry.go:7-13`, `queuestore_hkj808w.go:54-76`). A fresh CLI process **cannot** read them directly; it must either (a) ask the daemon over its socket (the `queue status` / `queue list` RPC path, `cmd/harmonik/main.go:103-104`), or (b) re-derive the same facts from disk (queue.json + the `.harmonik/worktrees/*` scan that `draindetect.liveWorktrees` already does, `draindetect.go:272-282`).

**Implication for the command shape (answer to charge #4):** `harmonik state` is a **thin aggregator with a daemon-liveness fork**, mirroring `digest`:
- **Daemon UP** → query the socket for the live queue/run picture (authoritative in-flight runs), union with the durable-file readers for everything else.
- **Daemon DOWN (exit 17)** → fall back to durable-file derivation (queue.json on disk + worktree scan), and *flag the run picture as best-effort*, exactly as `captain-boot-digest.sh:41-50` already degrades.

The boot digest "already does ~half" because it already gathers the **durable-file half** plus the **subprocess half** (br/kerf/git). The half it does **not** do is the **live-session / live-run / cognition** half (RunRegistry, the per-session FSM, the keeper gauge, sleep markers, the system-state roll-up label) — that is where the genuinely-new code lives.

---

## 1. Boot-digest inventory — what's *already* gathered ("the ~half")

Two boot-digest implementations exist and must be treated as **one logical source** with two surfaces:

### 1a. `scripts/captain-boot-digest.sh` (the shell digest — captain boot, human-facing markdown)
| # | Section | Command | Envelope mapping |
|---|---|---|---|
| 1 | Daemon up? | `harmonik queue status` (exit 17 = down) | `daemon.up` |
| 2 | Agents online | `harmonik comms who --json` (`.agent`, `.age_seconds`) | `presence[]` / `sessions[].last_seen` |
| 3 | Registered crews | `harmonik crew list --json` (`name`,`queue`,`session_id`,`status`) | `crews[]` |
| 4 | tmux fleet | `tmux list-sessions` / `list-windows -a` | `sessions[].tmux_*` (liveness) |
| 5 | Paused/failed queues | `harmonik queue list --json` filtered on `paused\|complete-with-failures` | `queues[].status` |
| 6 | Recent comms | `harmonik comms log --since 30m --json` | (context, not a state field) |
| 7 | Ready beads | `br ready --limit 0 --json` | `work.ready[]` |
| 8 | Open epics | `br list --status=open --type=epic --json` (incl. `assignee`) | `work.open_epics[]` |
| 9 | Kerf next | `kerf next --format=json` | `work.kerf_next` |
| 10 | Kerf map | `kerf map` | (context) |

### 1b. `internal/digest/builder.go` (the Go digest — `harmonik digest [--json]`, schema-versioned `DigestJSON`)
Gathers, all from **durable files / subprocess**, never the daemon:
| Field (`DigestJSON`, `types.go`) | Reader / call | file:line |
|---|---|---|
| `Queue` (`QueueSummary`: status, active_run_count, pending_count, active_runs[]) | `queue.Load(projectDir, "main")` from **queue.json on disk** | `builder.go:240-281` |
| `RecentCommits` | `git log origin/main --oneline -10` | `builder.go:332-356` |
| `RecentEvents` | `eventbus.ScanAfter(events.jsonl)` | `builder.go:284-304` |
| `ReadyBeads` | `br ready --format json --limit 0` (the `--limit 0` fix, hk-5kn3) | `builder.go:362-368` |
| `InProgressBeads` | `br list --status in_progress --json` | `builder.go:394-401` |
| `OpenNotes` | `.harmonik/cognition/notes.jsonl` | `builder.go:114-117` |
| `KerfNext` | `kerf next --format=json` | `builder.go:431-442` |
| `PendingDecisions` | scan `events.jsonl` for unacked `decision_required` | `builder.go:164-226` |
| `SuppressionState` | sentinel config + events scan | `builder.go:132-141` |
| `HasUndeployedTail` | `br list --status closed --limit 0` Phase-2-label scan | `builder.go:148-152` |

**Net:** the digest already supplies **queue status (from disk), ready/in-progress beads, open epics (shell), kerf next, recent commits/events, pending decisions, suppression state**. That is the "actual-state, durable half." It does NOT supply: in-flight RunRegistry runs, per-session FSM state, the keeper context gauge, the sleep-marker source/level, or any rolled-up system label.

---

## 2. Reader survey — every live source and what it contributes

| Reader | Type | Public read surface | What it uniquely supplies | file:line |
|---|---|---|---|---|
| **RunRegistry** | in-daemon memory | `Snapshot() []*RunHandle`, `Len()`, `LenForQueue(name)` | The **authoritative in-flight run list**: per run → `BeadID`, `QueueName`, `WorktreePath`, `StartedAt`, `OwningEpicID/Assignee`, and `GetMachine()` → the FSM | `runregistry.go:179-187, 32-101` |
| **QueueStore** | in-daemon memory | `AllQueues() map[string]*queue.Queue`, `QueueByName`, `Queue()` | Live per-queue **status** (active / paused-by-failure / -drain / -budget / completed / cancelled) + per-item status (pending / dispatched / deferred-for-ledger-dep / completed / failed) + group structure | `queuestore_hkj808w.go:162-207` |
| **queue.Load (disk)** | durable file | `queue.Load(dir, name)` reads `queue.json` | Same shape as QueueStore but **last-persisted** snapshot — the daemon-DOWN fallback the digest already uses | `builder.go:241` |
| **per-session lifecycle FSM** | in-daemon, hung off each run | `RunHandle.GetMachine()` → `Machine`; `LifecycleState` ∈ {Spawning, Initializing, Ready, Executing, Suspended, Terminating, Terminated, Failed}; `IsTerminal()`; `Transition` history (From/To/At/Reason) | The **per-session phase** that rolls up into the PROCESSING/WAITING/DRAINING/INACTIVE label | `lifecycle/types.go:16-107`; reached via `runregistry.go:99` |
| **crew registry** | durable file | `crew.List(projectDir)` → `[]Record` | Declared crews: `Name`, `SessionID`, `Queue`, `Epic`, `Handle`, `StartedAt` | `crew/registry.go:165-195, 38-46` |
| **keeper gauge** | durable file | `keeper.ReadCtxFile(dir, agent)` → `CtxFile` | Per-session **context-fill**: `Pct`, `Tokens`, `WindowSize`, `SessionID`, `Ts`; identity overridden by `.sid` | `keeper/gauge.go:18-63` (**lens 3 owns the schema; I note it as the source**) |
| **sleep markers** | durable file | scan `.harmonik/.sleeping.*`; `sleepMarker{Session, SleptAt, Source, Level}` | Per-session **asleep/at-rest** + **who** (`operator`/`captain`) + **depth** (`L0`–`L3`). Source+Level just landed (hk-caaf) | `quiesce.go:81-139, 122-139` |
| **GatherDrainFacts** (← GenuineDrain) | in-daemon, composed | `DrainDetector.GenuineDrain(ctx)` → `DrainResult{State, Reasons}` | Typed drain fact bundle across all 5 axes (ready/in-flight/paused/failed-archive/epic-blocked) | `draindetect.go:152-200` (**lens 2 owns the reshaped schema; I note it as a source**) |
| **comms presence** | durable / daemon | `harmonik comms who --json` (`agent`, `age_seconds`) | `last_seen` per agent → liveness/staleness | `captain-boot-digest.sh:55-62` |
| **tmux** | OS | `tmux list-sessions` / `list-windows -a`; keeper `ResolveTmuxTarget` | Pane/session **liveness** behind a SessionID (is the agent's tmux actually alive?) | `captain-boot-digest.sh:79-85` |
| **git** | subprocess | `git log origin/main --oneline` | Recent merge tail (completion authority) | `builder.go:332-356` |
| **br / kerf** | subprocess | `br ready --limit 0`, `br list --status …`, `kerf next` | The work backlog axes | `builder.go:362-442` |

---

## 3. SOURCE→FIELD map (the deliverable)

Proposed `state` envelope grouped as: `daemon` · `queues[]` · `runs[]` · `sessions[]` (the cognition/FSM/sleep roll-up) · `work` · `system_label`. For each field: the **existing reader** and whether it's a **reprojection** (R — existing reader, just reshaped) or **genuinely new** (N — no existing reader; new code/wiring needed).

### `daemon`
| Field | Source reader | call / file:line | R/N |
|---|---|---|---|
| `up` | queue-status RPC exit code | `captain-boot-digest.sh:40-50` | R |
| `pid` / `socket` | supervisor / socket file | `cmd/harmonik/supervise_cmd.go` | R |

### `queues[]`
| Field | Source reader | call / file:line | R/N |
|---|---|---|---|
| `name`, `status` | QueueStore live / queue.Load disk fallback | `queuestore_hkj808w.go:199`; `builder.go:241` | R |
| `active_run_count`, `pending_count` | same (item scan) | `builder.go:248-281` | R |
| `items[].{bead_id,status,run_id}` | same | `builder.go:252-279` | R |
| `paused_by` (failure/drain/budget) | QueueStore status enum | `queuestore` via `draindetect.go:211-215` | R |
| `failed_archives[]` (on-disk `*.json.failed-*`) | drain `failedArchives` | `draindetect.go:258-265` | R (via lens 2) |

### `runs[]` (in-flight)
| Field | Source reader | call / file:line | R/N |
|---|---|---|---|
| `run_id`, `bead_id`, `queue_name` | `RunRegistry.Snapshot()` | `runregistry.go:179-187, 36-48` | **N** (no CLI reader today — digest's `active_runs` is from queue.json items, not RunRegistry; it has no run_id-keyed live handle, worktree path, or FSM) |
| `worktree_path`, `started_at` | RunHandle fields | `runregistry.go:53-60` | **N** |
| `owning_epic_id`, `owning_epic_assignee` | RunHandle fields | `runregistry.go:70-77` | **N** |
| `lifecycle_state` (Spawning…Failed) | `RunHandle.GetMachine()` → FSM | `runregistry.go:99`; `lifecycle/types.go:18-38` | **N** (FSM exists & is attached, but nothing reads it out for a status surface) |
| `live_worktree_count` (DOWN fallback for run count) | drain `liveWorktrees` (`.harmonik/worktrees/*`) | `draindetect.go:272-282` | R |

### `sessions[]` (the per-session cognition + sleep + liveness roll-up)
| Field | Source reader | call / file:line | R/N |
|---|---|---|---|
| `agent` / `session_id` | crew registry + keeper `.sid` | `crew/registry.go:44-45`; `gauge.go:59-60` | R |
| `role` (captain/crew/…) | crew registry / naming | `crew/registry.go:38-46` | R |
| `queue`, `epic` | crew registry `Record` | `crew/registry.go:42-43` | R |
| `last_seen` / staleness | comms who | `captain-boot-digest.sh:55-62` | R |
| `tmux_alive` | tmux probe / `ResolveTmuxTarget` | `captain-boot-digest.sh:79-85` | R |
| `context_pct`, `tokens`, `window_size` | keeper gauge `ReadCtxFile` | `gauge.go:18-63` | R (reader exists; **lens 3 owns schema**) |
| `context_too_big` (derived) | derive from gauge pct vs config band | gauge + keeper config | **N** (derivation; lens 3 / P2-c) |
| `context_not_changing` (stale-token) | compare gauge `Tokens` across `Ts` snapshots | gauge over time | **N** (requires a stored prior sample; lens 3 / P2-c) |
| `repeating_pattern` (loop) | Haiku pass over recent messages | none | **N** (genuinely new; P2-c / hk-jay1) |
| `asleep` + `sleep_source` + `sleep_level` | sleep-marker scan | `quiesce.go:122-139` | R (marker landed; **no current CLI reader scans it** → thin new scan, but the *data* exists) |
| `fsm_state` (per session, not per run) | FSM via the session's active run | `lifecycle/types.go`; `runregistry.go:99` | **N** |

### `work`
| Field | Source reader | call / file:line | R/N |
|---|---|---|---|
| `ready[]` | `br ready --limit 0` | `builder.go:362-368` | R |
| `in_progress[]` | `br list --status in_progress` | `builder.go:394-401` | R |
| `open_epics[]` (+assignee) | `br list --status=open --type=epic` | `captain-boot-digest.sh:124-127` | R |
| `epic_blocked_children[]` | drain epic axis (`scanOpenEpics`) | `draindetect_epic.go` | R (via lens 2) |
| `lined_up` / `deferred_for_ledger_dep[]` | QueueStore item scan | `draindetect.go:234-236` | R (via lens 2) |
| `needs_decomposition[]` (childless open epic) | **none** | — | **N** (the generative category; P1 fact bundle / hk-pfr4) |
| `kerf_next` | `kerf next --format=json` | `builder.go:431-442` | R |
| `drain_facts` (typed bundle + UNSURE flag) | GatherDrainFacts | `draindetect.go:152-200` | R-reshaped (**lens 2 owns schema**) |

### `system_label` (the fold)
| Field | Source reader | call / file:line | R/N |
|---|---|---|---|
| `label` ∈ {PROCESSING, WAITING, DRAINING, INACTIVE} | **none** — computed from runs[].fsm + queues[].status | fold over FSM + QueueStore | **N** (the ~50-line fold; P2-b / hk-w6q7) |

---

## 4. New-vs-reprojection split (summary)

**REPROJECTIONS (existing reader; just reshape/union)** — the bulk:
- Entire `queues[]`, `work.*` (ready/in-progress/open-epics/kerf-next/epic-blocked/lined-up/drain-facts), `daemon.up`, recent commits/events, and the `sessions[]` *identity/liveness/queue/epic/last_seen/tmux* fields. All already gathered by `digest` + `captain-boot-digest.sh` + the drain detector.
- `sessions[].context_pct/tokens/window_size` (gauge reader exists), `sessions[].asleep/source/level` (markers exist on disk; only a thin new *scan loop* is needed — the **data is not new**, only a reader that walks `.harmonik/.sleeping.*` and parses `sleepMarker`).

**GENUINELY NEW (no existing reader / no existing derivation)** — the focused build surface:
1. **`runs[]` from RunRegistry** — run-id-keyed live handles with `worktree_path`, `started_at`, `owning_epic_*`, and the **per-run FSM state**. The digest's `active_runs` is NOT this — it's parsed from queue.json items and lacks the live handle, worktree, and FSM. Requires a **daemon-socket RPC** (or a daemon-side aggregator) because RunRegistry is in-daemon memory.
2. **`*.fsm_state` readout** — the lifecycle FSM is fully built and attached to every run (`GetMachine()`), but **nothing reads it out** into any status surface today. New: expose it.
3. **`system_label` fold** — PROCESSING/WAITING/DRAINING/INACTIVE rolled from runs-FSM + queue status. No reader; the ~50-line fold (P2-b).
4. **Cognition derivations** — `context_too_big`, `context_not_changing`, `repeating_pattern` (P2-c / hk-jay1; lens 3 owns the schema). The gauge *snapshot* exists; the *derivations* (especially the Haiku loop-pattern pass and the prior-sample diff) are new.
5. **`work.needs_decomposition`** — childless open epics flagged as the one generative category (P1 fact bundle / hk-pfr4).

---

## 5. Recommended shape of `harmonik state [--json]` (charge #4)

**It is a thin aggregator over existing readers, structured exactly like `harmonik digest`:**

1. **Reuse the digest pattern wholesale.** `harmonik digest` already is the durable-file + subprocess aggregator. `harmonik state` should reuse `digest.Build`'s durable readers for `work`, `queues` (disk fallback), commits/events, pending decisions — do NOT re-implement them. Practically, `state` can **embed `DigestJSON`** (or call `digest.Build`) for the durable half and *add* the four new daemon/cognition sections on top.
2. **Add the live-daemon RPC path** for the genuinely-new in-memory readers (`runs[]` from RunRegistry, live QueueStore status, the FSM readout, and the fold label). The daemon already serves `queue status`/`queue list` over its socket; extend that with a `state` RPC that snapshots RunRegistry + QueueStore + computes the fold *inside the daemon* (where the in-memory readers live) and returns it. The CLI then unions: `digest.Build` (durable, runs anywhere) **∪** the daemon `state` RPC (live runs/FSM/label).
3. **Daemon-DOWN degradation** — mirror `captain-boot-digest.sh:41-50`: when the socket is absent (exit 17), drop to disk-only: queue.json for queues, `.harmonik/worktrees/*` count (`draindetect.liveWorktrees`) for a best-effort run count, and mark `runs[]`/`system_label` as `unavailable: daemon down`. The durable half stays fully populated.
4. **Sessions roll-up** — walk crew registry (`crew.List`) for the declared set, then for each enrich with: keeper gauge (`ReadCtxFile`), sleep-marker scan (new thin `.harmonik/.sleeping.*` walker), comms-who `last_seen`, tmux liveness. This is pure file/subprocess work — runs without the daemon.
5. **Home:** `cmd/harmonik/state.go`, wired in `main.go` next to the `digest` dispatch (`main.go:664-665`), with the new daemon-side `state` RPC handler living in `internal/daemon/` alongside the existing queue-status handler. Schema-versioned envelope in a new `internal/state/types.go` (or extend `internal/digest` — but a separate package keeps the daemon-RPC dependency out of the daemon-free digest path).

**Bottom line:** ~70% of the envelope is a reprojection of `digest` + `captain-boot-digest.sh` + the (reshaped) drain detector. The genuinely-new code is the **daemon-side RunRegistry/FSM snapshot RPC + the fold label**, plus the cognition derivations that lens 3 owns. `harmonik state` should be `digest` ∪ a new daemon `state` RPC ∪ a sleep-marker scan — not a from-scratch reader set.

---

## 6. Duplication / conflict risks (flagged per charge)

1. **Queue status: live QueueStore vs queue.json on disk — TWO readers for the same fact.** `digest` reads queue.json (`builder.go:241`); a live `state` RPC would read the in-memory QueueStore. These can **disagree** during the persist window (item dispatched in memory but queue.json not yet rewritten) or when the daemon is down (disk is stale-but-only-source). **Resolution:** when the daemon is UP, the **in-memory QueueStore is authoritative**; queue.json is the DOWN-only fallback. The envelope should carry a `source: "live"|"disk"` tag per queue so the reader knows which it got. Do NOT silently union both.
2. **In-flight run count: RunRegistry.Len() vs `.harmonik/worktrees/*` count vs queue.json `dispatched` items — THREE readers.** `draindetect` already treats a non-empty worktrees dir as a live run (`draindetect.go:272-282`), but worktrees can be **stale** (un-reconciled) — that's why drain treats it fail-closed. RunRegistry is the truth when the daemon is up. **Resolution:** RunRegistry is authoritative live; worktree-count is the DOWN best-effort only; never sum them.
3. **Session identity: keeper gauge `session_id` vs `.sid` file vs crew registry `SessionID` vs sleep-marker `Session`.** The gauge already resolves gauge-vs-`.sid` internally (`.sid` wins when primary, `gauge.go:59-60`), but the crew registry and sleep markers are *separate* writers of "the session id for agent X." On a `/clear` the SID flips and these can desync (a known drift — see MEMORY: keeper session_id flips on /clear). **Resolution:** treat `.sid`/gauge as the *live* identity and crew-registry `SessionID` as the *declared* identity; surface both rather than picking one, so a desync is *visible* in `state` instead of silently masked.
4. **`asleep` truth: sleep-marker file vs the daemon's in-memory `sleeping` map.** `quiesce.reconcileOrphanedMarkers` (`quiesce.go:296-398`) exists precisely because these desync across a daemon restart. **Resolution:** the on-disk marker is the durable truth for a `state` read; the in-memory map is only the failsafe-timer bookkeeping. Read the marker files.
