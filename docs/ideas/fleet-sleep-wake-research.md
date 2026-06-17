# Captain-Driven SLEEP / WAKE — Research Report

> **Provenance:** Research seed for the operator-directed initiative **`hk-rl4b`** ("Fleet
> sleep/wake: quiesce token-burning LLM loops when work drains, wake on new work").
> Dispatched 2026-06-16 EOD by `captain` as a bounded read-only research agent during
> the wind-down session; report persisted by the captain (sub-agents cannot write `.md`).
> **Next step (next session): `kerf new fleet-sleep`** to turn this into a bead plan.
> All file:line citations below were produced by the research agent against
> `/Users/gb/github/harmonik` at that time — re-verify line numbers before implementing.

---

## Problem

When the operator is away with no dispatchable work, **deterministic loops keep WAKING
the ~6 long-lived `claude --remote-control` sessions** (captain + crews). Each wake
re-processes context = burns the operator's Claude **subscription weekly** token
allocation, for zero work. (Codex soak *runs* are ChatGPT-billed, not Claude — the
Claude burn is the LLM sessions' idle wakeups.) Goal: a captain-driven **SLEEP**
(quiesce all LLM-waking loops so sessions cost ~zero) when work is **genuinely
drained**, and **WAKE** (resume) on new work or operator return — keeping ONE cheap
**deterministic** listener (the daemon) alive as the wake-trigger.

---

## 1. TOKEN-BURN INVENTORY

Every mechanism that can wake a long-lived Claude session (captain/crew), classified
DETERMINISTIC/cheap (pure Go, no LLM turn) vs LLM-WAKING (delivers a line into a Claude
tmux pane / re-invokes a Monitor turn, costing context re-processing).

| # | Mechanism | file:line | Class |
|---|---|---|---|
| 1 | **Daemon `subscribe` heartbeat emitter** — timer writes a `{"type":"heartbeat",active_runs,...}` NDJSON line every `heartbeat_seconds` (clamp [10,600], default 60) to every attached subscriber, even when fully idle | `internal/daemon/subscribe.go:46-49,114-118,332,456-499,516-609` | **LLM-WAKING** — each line is a Monitor notification → re-invokes the captain/crew agent turn (exactly the "60s keepalive re-invoked the captain every minute" failure flagged in `captain/STARTUP.md:322-328`). The bus emission is cheap Go; the COST is the agent turn it triggers. |
| 2 | **`harmonik subscribe` CLI** armed in a Monitor by captain (`--types epic_completed`) and every crew (`--types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s`) | `crew-launch/SKILL.md:196-204`, `captain/SKILL.md:426-428`, `captain/STARTUP.md:343-345` | **LLM-WAKING** — the heartbeat (#1) and every real run event become an agent turn. Crews explicitly subscribe to `heartbeat` at 60s. |
| 3 | **`comms recv --follow --json`** armed for life of session by captain and every crew (drains backlog, then streams live `agent_message`) | `crew-launch/SKILL.md:117-119,130-147`, `captain/SKILL.md:500`, `captain/STARTUP.md:334`; socket `subscribe.go:125-130` | **LLM-WAKING when a message arrives** (each delivered comms message = an agent turn). Idle-with-no-traffic = cheap (no heartbeat on this path unless `--heartbeat` passed). |
| 4 | **Captain `/loop 12m` health tick** — self-pacing recurring prompt that re-invokes the captain every 12 min | `captain/STARTUP.md:337-341`, `captain/SKILL.md` (§A) | **LLM-WAKING** — a full captain turn every 12 min unconditionally, regardless of whether work exists. The single largest scheduled captain burn while idle. |
| 5 | **Crew ≤10-min progress-feed timer** — crew MUST post a status line on a ≤10-min timer while its loop is active | `crew-launch/SKILL.md:243,278-283,349` | **LLM-WAKING** — a crew turn at least every 10 min. |
| 6 | **Crew idle `br ready` re-poll** — when no ready beads, crew waits on inbox but may re-poll `br ready` (capped ≥10 min) | `crew-launch/SKILL.md:228-239` | **LLM-WAKING** (the agent turn to run the poll). |
| 7 | **Keeper watcher loop** — polls the `.ctx` gauge every ~5s; on WARN injects warn text into the pane; on ACT runs handoff→/clear→/session-resume | `internal/keeper/watcher.go`, `cmd/harmonik/keeper_cmd.go:281-291`; thresholds `internal/keeper/cycle.go:39-43` | **MOSTLY DETERMINISTIC** — the 5s poll is pure Go. LLM-WAKING only when it crosses WARN/runs the reset. On a genuinely-idle session whose context isn't growing, it never crosses a threshold → stays cheap. |
| 8 | **Comms presence refresh beats** — daemon emits `agent_presence{reason:refresh}` on any `comms recv`/`send`/subscribe (TTL 120s, ~60s cadence) | `internal/daemon/commspresencehandler_7t27s.go:27-46`, `commsrecvhandler_nnwaa.go:210-213`, `subscribe.go:354-358`; `internal/presence/presence.go:44-48` | **DETERMINISTIC/cheap** — Go-side emissions; do NOT inject into a Claude pane. Only keep `comms who` fresh. No token cost. |
| 9 | **`comms send --wake`** — after delivering a directed message, nudges the recipient's tmux pane (load-buffer → paste-buffer → send-keys Enter) | `cmd/harmonik/comms.go:345-411` | **LLM-WAKING by design** — the explicit "rouse an idle agent" path. Cheap to NOT call; this is the lever WAKE should use. |
| 10 | **Daemon dispatch workloop** (poll tick + `WakeCh`/`SetQueue` + `runScheduleTick`) | `internal/daemon/workloop.go:388-401,560-585,1155-1208`; `internal/queue/rpc.go:842` | **DETERMINISTIC** for the daemon itself, but it SPAWNS implementer/reviewer claude per bead = the real subscription consumer when work IS flowing. Idle (empty queue, `--no-auto-pull`) = spawns nothing. |

**Key finding:** the idle burn comes from **scheduled LLM re-invocations (#1/#2 heartbeat,
#4 captain `/loop`, #5 crew timer)** — NOT from the daemon's deterministic Go loops
(#7 poll, #8 presence, #10 workloop), which are nearly free when the queue is empty.

---

## 2. SLEEP DESIGN

Goal: quiesce mechanisms #1–#6 (scheduled LLM re-invocations) so the ~6 sessions cost
~zero, while keeping ONE cheap deterministic listener that can re-wake the captain.

- **Crews → `harmonik crew stop <name>` (not pause).** `crew stop` removes the registry
  record + tmux pane + keeper marker (`captain/STARTUP.md:131-133`), killing #2, #3, #5,
  #6, #7 for that session entirely (a stopped pane processes nothing). State is fully
  recoverable: `{queue, epic_id}` are durable in beads via the `--assignee` mirror and
  the mission file persists, so `crew start` re-hydrates with zero work loss
  (`crew-launch/SKILL.md:305-321`). A "pause" that leaves the pane up still has the keeper
  polling + recv-follow armed, so `stop` is the correct token-zero choice.
- **Captain → quiesce its own loops but DO NOT exit.** The captain MUST NOT self-terminate
  (`captain/SKILL.md:631,655`). On sleep it: (a) cancels its `/loop 12m` health tick (#4 —
  the biggest idle burn), (b) tears down its `subscribe` heartbeat Monitor (#1/#2), and
  (c) drops `comms recv --follow` to no-heartbeat passive (or stops it). The captain pane
  stays alive but receives no scheduled nudges, so it sits at ~zero cost until something
  injects a line.
- **Daemon → stop spawning, keep listening.** Daemon already runs `--no-auto-pull`
  (queue-only — `daemon.go:336-352`), so an empty queue spawns nothing. SLEEP additionally:
  pause any active/paused-by-failure queues (`harmonik queue pause`, `main.go:357`) and
  optionally `queue set-concurrency 0` so a stray submit doesn't spawn until WAKE. The
  daemon Go workloop, presence beats (#8), and `WakeCh` listener stay running — free.
- **What stays up as the wake-trigger (the one cheap deterministic listener):** the
  **daemon process itself** plus its **`comms` bus and `WakeCh`**. Pure Go, no tokens,
  already the thing that learns about new submits. The captain's keeper stays running too
  (5s Go poll, free while context isn't growing) so it can rebind on resume.

---

## 3. WAKE DESIGN

Three triggers, all routed through the deterministic daemon that stayed up in §2:

- **(a) New ready work (auto-wake the captain).** The daemon already detects ready work two
  ways: `WakeCh()` fires on every `queue submit`/`append` (`workloop.go:388-401`,
  `queue/rpc.go:842`), and the eager-fill / `kerf next --only=bead` readiness path knows
  what is ready (`eagerfill_em063.go:19-20`). On a transition from drained→ready-work-exists
  while sleeping, the daemon **wakes the captain pane via the exact mechanism that already
  exists** for `comms send --wake`: `commsWakePaneForAgent` → `commsInjectTmuxPane`
  (load-buffer → paste-buffer → send-keys Enter, `comms.go:356-411`). Pane target resolves
  the same way (`crew.Load` handle, else `lifecycle.TmuxSessionName(hash,"crew-<name>")`;
  for the captain, the `captain` session from `captain-launch.sh`). Injected line e.g.
  *"WAKE: ready work detected (N beads); run STARTUP.md boot sequence."*
- **(b) Operator command — `harmonik wake`.** Deterministic CLI: clears the daemon sleep
  flag, `queue resume` any paused queues (`main.go:359`), restores `set-concurrency`, and
  injects the WAKE nudge into the captain pane. Symmetric `harmonik sleep` sets the flag.
  No LLM needed to flip the mode.
- **(c) Optional schedule.** The `schedule` subsystem already exists and is wired into the
  workloop (`runScheduleTick`, `workloop.go:1184-1189`; daily fire, `schedule/types.go:29-42`).
  It supports `ActionKindCommand` (run any argv) and `ActionKindSpawnCrew`. A
  `daily@HH:MM` job with `action=command argv=["harmonik","wake"]` gives scheduled morning
  wake with **no new primitive** — just a `schedule add` row. The captain's `/loop 12m`
  (#4) is NOT re-armed until wake, so the schedule replaces the perpetual idle tick.

---

## 4. GENUINE-DRAIN GUARD (load-bearing)

The captain historically false-declares "no work." Before SLEEP, the captain MUST pass
ALL of these checks (each closing a specific false-empty trap):

1. **Unpaginated ready sweep:** `br ready --limit 0 --json` — NOT bare `br ready`
   (default `--limit 20` hides ready beads → lane looks drained; `captain/STARTUP.md:156`,
   memory `br_ready_limit20_pagination`). Must return empty.
2. **No paused-by-failure / paused-by-budget queues:**
   `harmonik queue list --json | jq '.queues[]|select(.status|test("paused|complete-with-failures"))'`
   (`captain/STARTUP.md:91-94`). A paused queue is NOT drained — it has stuck work.
   Budget-paused queues (`perqueuespendmeter_tigaf11.go:38-48`) and failure-paused queues
   must be resolved/resumed, NOT slept through.
3. **All open epics' ready children:** for every `br list --status=open --type=epic`,
   intersect children with `br ready` (`crew-launch/SKILL.md:167-172`). Ledger-dep gating
   silently defers children of an OPEN epic so they don't appear in a naive `br ready`, and
   `dep add <task> <epic>` makes a task blocked-by the open epic (memories
   `beads_epic_dep_blocks_dispatch`, `daemon_crash_and_gating_2026-05-31`). Use
   **`harmonik queue dry-run --beads <candidates>`** to surface ledger-dep deferrals
   (AGENTS.md "Submitting work": dry-run "reports ledger-dep deferrals") — anything
   deferrable-but-ready ⇒ NOT drained.
4. **Nothing in flight:** `harmonik queue status` shows no active runs;
   `harmonik subscribe --types heartbeat --heartbeat 1s --json | head -1`
   (`captain/STARTUP.md:85-86`) — empty `active_runs` snapshot. A merging/reviewing run is
   not drain.
5. **kerf-next is NOT a sole oracle:** `kerf next` reports empty for works lacking a
   `bead_filter` clause (AGENTS.md beta caveat). So drain = (#1 empty AND #2 none paused AND
   #3 no open-epic ready children AND #4 nothing in flight); `kerf next` empty alone is
   insufficient.

**Drain is declared ONLY when checks 1–4 all pass.** This gate prevents the "captain says
no work, sleeps, but there were 15 ready children under an open epic" failure.

---

## 5. MINIMAL SURFACE + CAPTAIN PROTOCOL

**Smallest surface — reuse existing primitives; add one daemon flag + two thin CLI verbs:**

| Item | Change type | Detail |
|---|---|---|
| `harmonik sleep` / `harmonik wake` top-level verbs | **CODE** (small) | No such verbs today (`main.go` grep empty). `sleep` = set a daemon `sleeping` flag (gitignored `.harmonik/` marker) + `queue pause` all + `set-concurrency 0`. `wake` = clear flag + `queue resume` + restore concurrency + inject WAKE nudge into captain pane. Mirrors the existing `supervise pause/resume` shape (`supervise_cmd.go:55-57`). |
| Daemon "sleeping" mode | **CODE** (small) | One boolean read in the workloop: while sleeping, suppress subscribe heartbeat emission (#1) and fire the auto-wake nudge only on the drained→ready edge. Reuses `WakeCh` + `commsWakePaneForAgent`; no new dispatch path. |
| Daily wake schedule | **NO CODE** | `harmonik schedule add --id morning-wake --schedule "daily@09:00 local" --action command --argv harmonik,wake` — schedule subsystem already executes `command` actions via `runScheduleTick`. |
| Crew sleep/restore | **NO CODE** | `harmonik crew stop <name>` / `crew start <name> --mission <existing>` already exist. |
| Pane wake nudge | **NO CODE** | `commsWakePaneForAgent`/`commsInjectTmuxPane` already exist (`comms.go:356-411`). |

**Captain sleep/wake decision protocol** (all **CAPTAIN-BEHAVIOR**, no code):

- **SLEEP decision:** on a health tick that finds drain, run the §4 GENUINE-DRAIN GUARD
  (checks 1–4). If — and only if — all pass: surface "drained; sleeping" to the operator
  (dual-channel, `captain/SKILL.md:455-461`), `crew stop` each crew (state durable in
  beads), cancel own `/loop 12m` and subscribe Monitor, run `harmonik sleep`. Do NOT exit
  the captain session (`captain/SKILL.md:655`). This is a NEW captain behavior — today the
  captain is forbidden from idling while work exists (§0.2) but has no "genuinely-drained →
  sleep" branch; this adds the drained terminal state.
- **WAKE decision:** when the captain pane receives the injected WAKE line (daemon auto-wake
  on new work, `harmonik wake`, or the daily schedule), it runs the full STARTUP.md boot
  sequence (§0.5: ground-truth, reconcile, organize lanes, establish+verify crews, re-arm
  watchers + `/loop 12m`). A WAKE is treated exactly like a fresh boot — re-derive live
  state, don't trust the pre-sleep snapshot.
- **Anti-false-sleep guard (HARD):** SLEEP is gated behind §4; a sleep that skipped any of
  checks 1–4 is the same class of failure as a §0.2 "idling while ready work exists"
  violation. The captain must log the drain-check results in its sleep status so a missed
  open-epic child is auditable.

**Net:** ~3 small code changes (two CLI verbs + one daemon flag); everything else is
composition of existing primitives (`crew stop/start`, `queue pause/resume`,
`set-concurrency`, `schedule add`, `commsWakePaneForAgent`, `WakeCh`) plus one new captain
behavioral branch (drain-guarded sleep + wake-as-boot).
