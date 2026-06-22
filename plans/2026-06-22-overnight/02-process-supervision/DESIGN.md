# C2 — Process supervision + health checklist (DESIGN)

Date: 2026-06-22
Status: design-only (no code). Grounds in current source (file:line). Designs TO
the operator's chosen architecture (daemon-native supervision + a health
checklist agent + actionable ack-able comms escalation). Subsumes/reshapes held
beads **hk-sbitr** (watchdog auto-relaunch) and **hk-u5tgh** (watchdog↔daemon
keeper-spawn integration).

Motivating crisis: `plans/2026-06-22-keeper-coverage-investigation/00-SYNTHESIS.md`
— every live crew ran with ZERO context-overflow protection because (a) the
standalone Sonnet ctx-watchdog died silently ~36h ago with nothing to relaunch
it, and (b) crews launched off a pre-wiring binary so never got keeper windows.
Nothing in the fleet *verified* any helper was actually up.

---

## 1. Current state (the parts that already exist — file:line)

The good news: ~80% of the primitives this design needs already exist. The gap is
**wiring + coverage**, not new subsystems.

### 1.1 What the daemon already spawns + tracks

- **Workers** (claude-on-bead): spawned by the workloop and tracked in the
  **`RunRegistry`** (`internal/daemon/runregistry.go:103-110`) — a mutex-protected
  `run_id → *RunHandle` map (`RunHandle`: BeadID, QueueName, WorktreePath, Watcher,
  StartedAt, Cancel, FSM, `:32-89`). NOTE: it tracks NO PIDs — worker death is
  detected via **Watcher EOF** when the handler process exits (the Cancel func is
  signalling-only). This is the existing in-flight tracker the design mirrors for
  helpers: a durable map + a liveness signal, not a PID table.
- **Crews + their keeper windows**: `HandleCrewStart → SpawnCrewSession →
  spawnCrewKeeperWindow` (`internal/daemon/tmuxsubstrate.go:1346`, builder
  `:1430-1460`) → `crewKeeperWindowArgv` → `internal/agentlaunch/keeperargv.go:100`
  `KeeperWindowArgv`. This adds a sibling `keeper` tmux window running
  `harmonik keeper --agent <crew> --tmux <session>:agent --warn-only …`. Crews are
  launched **warn-only** (`keeperargv.go:107-108`) — they emit warn events but do
  NOT self-restart.
- **Crew registry** (the authoritative live-crew list): `internal/crew/registry.go`,
  records at `.harmonik/crew/<name>.json` — `Record{SchemaVersion, Name, SessionID,
  Queue, Epic, Handle, StartedAt}` (`:36-46`), `Write` (atomic, `:76`),
  `List(projectDir)` (sorted, `:165`), `Load`, `UpdateSessionID`, `Remove`. The
  daemon writes a record per crew at start (`crewstart.go:170-178`, handle updated
  `:283-290`), removes on stop. The `StartedAt` field makes the boot-grace guard
  (§8) trivial; the `Handle` is the tmux window string for `has-session` checks.
  **This is the list to walk for the keeper health check.**
- **Schedule loop** (the daemon's periodic-task engine): `runScheduleTick` fires
  jobs from `.harmonik/schedules.json`. Action kinds: `ActionKindCommand` (detached
  process, `scheduletick.go:227 fireCommandAction`) and **`ActionKindSpawnCrew`**
  (`scheduletick.go:172,205,256 fireSpawnCrewAction` — drives the SAME
  `HandleCrewStart` path, `scheduletick.go:71`). Schedules survive daemon restarts.
- **ops-monitor** (the existing health checklist): registered on every daemon boot
  as an `every@5m` command job (`internal/daemon/opsmonitor_schedule.go:32-47`),
  running `scripts/ops-monitor-check.sh`. It writes
  `.harmonik/ops-monitor/latest.json` with a `checks` map and sends comms to the
  captain. **This is the home for new checks** (see §3).

**Key gap in process tracking**: the daemon writes crew *registry records* but does
NOT poll keeper liveness after spawn. There is no periodic "is each crew's keeper
actually holding its lock?" sweep. The probe to do it already exists but is
unwired into the daemon loop.

### 1.2 The supervisor + its reusable watchdog primitives

- **Supervisor shim** `cmd/harmonik/supervise/shim.go:33 RunShim`, `:141-198` —
  restarts the daemon per `config.json` policy AND runs a `DaemonWatchdog`
  (`internal/supervise/daemon_watchdog.go`) that probes the daemon socket and
  re-spawns it on death.
- **`SupervisorWatchdog`** (`internal/supervise/supervisor_watchdog.go`) — the
  GENERIC reusable primitive this whole design leans on. Spec:
  `{ PidfilePath, CheckInterval, ReviveCmd, WorkDir, MaxRevives, ReviveBackoff,
  ReviveWindow, OnAlarm }`. Loop (`:83 Run`): probe liveness → if dead, call
  `OnAlarm()` → spawn `ReviveCmd` (detached, `:193 reviveWith` setsid) → poll until
  alive → reset counter on success → give up at `MaxRevives`. **This is exactly the
  shape needed to supervise the ctx-watchdog**: a liveness-probe + revive-cmd +
  alarm callback + revive cap. It generalizes from "pidfile" to any liveness probe.
- **Supervisor config** `cmd/harmonik/supervise/config.go` — `TokenCap` (`:69`) is a
  *spend* budget (ccusage), NOT a per-pane context gauge. The supervisor does NOT
  know about keepers or the ctx-watchdog today.
- **The supervisor-up check already exists** in ops-monitor
  (`ops-monitor-check.sh:151-172`, `checks["supervisor-up"]`) and currently shows
  `flag: not running` (the live `latest.json` confirms `supervisor_up:false`). The
  cry-wolf history (hk-pen9 / yrnui re-key f6b76f59) is why supervisor-down is an
  IMMEDIATE signal only when paired with the no-auto-revive framing.

### 1.3 The ctx-watchdog (the orphan)

- Launcher `scripts/ctx-watchdog-launch.sh`: a plain
  `claude --remote-control --model sonnet` tmux session named `ctx-watchdog`,
  deliberately OUTSIDE the `harmonik-<hash>-*` orphan-sweep namespace and NOT
  `*-default`, so no sweep touches it (`:13-17`). No keeper armed on it.
- Prompt `.harmonik/cognition/ctx-watchdog-prompt.txt`: a `/loop 30m` that reads
  each crew's `<agent>.ctx` gauge; any crew ≥**300000** tokens → `harmonik crew
  stop <name>` then `harmonik crew start <name> --queue … --mission …` (**which
  DOES route through HandleCrewStart → spawnCrewKeeperWindow**); FORCE fallback is
  raw `tmux kill-session` + re-run (`step 3`). SKIPS keeper sessions, the captain
  (own keeper), `*-default`, and itself.
- **The self-heal claim is fiction**: launcher header `:20-21` says "the captain
  health-tick re-runs this script if the pane dies" — nothing implements it
  (confirmed in synthesis finding 05). This is the root of hk-sbitr.
- **hk-u5tgh nuance, corrected**: the watchdog's *normal* restart (`crew
  stop`+`start`) DOES re-arm the keeper window. The bypass is (1) the FORCE path
  (`tmux kill-session` + re-run, which can leave the crew keeper-less if the
  re-`start` is skipped) and (2) the fragility of a *Sonnet agent* doing crew
  lifecycle ad-hoc (paul reproduced "no tmux target" after a subsequent restart).
  So hk-u5tgh is really "move the restart decision out of an LLM and into the
  daemon, using the keeper-arming spawn path deterministically."

### 1.4 keeper liveness probe + doctor (already wired)

- `keeper.LiveKeeperPresent(projectDir, agent)` (`internal/keeper/keeper.go:115-137`)
  — read-only shared-flock probe on `<agent>.lock`: live exclusive holder →
  `true`; stale corpse or missing → `false`. Already used by
  `set-dispatching`/`clear-dispatching` (`keeper_cmd.go:650,706`).
- **`keeper doctor` already calls it** (commit 79a3b0ce):
  `keeper_enable_doctor_cmd.go:683-694` emits the `live-watcher` check
  (`✓ live keeper process is running` / `✗ no live keeper watcher detected`). The
  gauge-only false-green gap (synthesis finding 02) is closed at the CLI level. The
  remaining gap: nothing runs `doctor`'s equivalent **across all crews on a timer**.

### 1.5 keeper config (no-hardcoded-thresholds mandate)

- `ResolveKeeperConfig` (`cmd/harmonik/keeper_cmd.go:323`) aggregates EVERY missing
  threshold and fails loud — no runtime defaults (operator MANDATE; memory
  `feedback_no_hardcoded_keeper_thresholds`). Config struct
  `internal/daemon/projectconfig.go:481 KeeperConfig`; file
  `.harmonik/config.yaml` `keeper:` block. `KeeperWindowArgv` omits unset (0) band
  values so the spawned keeper reads operator config and refuses to start if a
  required key is missing (`keeperargv.go:91-95,110-117`).
- Daemon-level config lives in the `daemon:` block → `DaemonConfig`
  (`projectconfig.go:566`). **There is no `supervision:` / `watchdog:` block yet —
  this design adds one** (§5).

### 1.6 comms (the escalation transport)

- `harmonik comms send (--to NAME | --broadcast) [--from NAME] [--topic T]
  [--reply-to ID] [--wake] -- <body>` (`cmd/harmonik/comms.go:532-540`). Messages
  are bus events carrying `event_id` (`:336`); threadable via `--reply-to`
  (`in_reply_to`, `:274`). At-least-once delivery, dedupe on `event_id` (agent-comms
  N3). ops-monitor already sends `--from ops-monitor --to captain --topic
  ops-monitor` (`ops-monitor-check.sh:687-693`).
- **Gap**: comms has no native "issue resolved / acknowledged" state. The
  escalation protocol (§4) must add an ack/resolve ledger.

---

## 2. Architecture — what the daemon spawns + tracks

Principle: **the daemon is the single supervision authority.** Helpers are either
(a) spawned-and-tracked by the daemon directly, or (b) liveness-probed by the
daemon and escalated via comms. No helper depends on an LLM agent to stay alive.

Three tiers, mapped to existing primitives:

```
supervisor (shim)  ── DaemonWatchdog ─────────────▶ daemon            [exists]
                   └─ SupervisorWatchdog (reverse) ◀─ daemon           [exists, supervisor-up check]

daemon
  ├─ schedule loop (every@5m)
  │    ├─ ops-monitor-check.sh  ─── writes latest.json checks{} + comms▶ captain   [exists]
  │    └─ ctx-watchdog ensure   ─── ActionKindCommand: relaunch if dead [NEW — Phase 1]
  ├─ crew registry  (List) ──┐
  ├─ keeper-sweep tick  ─────┴─ LiveKeeperPresent per crew ─ comms▶ captain        [NEW — Phase 1]
  └─ ctx-watchdog restart-intake (optional)  ─── ActionKindSpawnCrew (keeper-armed) [NEW — Phase 2, hk-u5tgh]
```

### 2.1 Liveness tracking per helper

| Helper | Liveness probe | Owner | Revive |
|---|---|---|---|
| daemon | socket dial | supervisor `DaemonWatchdog` | auto (exists) |
| supervisor | `supervise.pid` + kill(0) | ops-monitor `supervisor-up` check + (NEW) daemon `SupervisorWatchdog` goroutine | escalate; optional auto-revive `ReviveCmd` (exists, currently nil) |
| crew keeper (per crew) | `LiveKeeperPresent(projectDir, crew.Name)` | (NEW) daemon keeper-sweep tick | escalate to captain (NOT auto-restart in Phase 1) |
| ctx-watchdog | tmux `has-session -t ctx-watchdog` + gauge freshness | (NEW) daemon ensure-job | auto-relaunch via `ctx-watchdog-launch.sh` (idempotent) |

**Watchdog liveness probe**: the watchdog has no lockfile, so reuse two cheap
signals — `tmux has-session -t ctx-watchdog` (process present) AND
`<projectDir>/.harmonik/keeper/ctx-watchdog.ctx` mtime fresh (the loop is actually
ticking, not wedged). Either failing → "watchdog down". This catches both a dead
pane (hk-sbitr) and a hung/zombie pane (a silent stall that `has-session` alone
would miss).

### 2.2 Why escalate-not-auto-restart for crew keepers (Phase 1)

A missing crew keeper is recoverable two ways: (a) re-arm the keeper window, or (b)
`crew stop`+`start`. Auto-re-arming a keeper window on a *live* crew session is the
hk-u5tgh integration work and needs care (don't double-arm, don't restart a busy
crew). Phase 1 therefore **detects and escalates** (the cheap, safe, reversible
win); Phase 2 wires deterministic auto-re-arm. This matches the operator's "report
problems via comms" framing for crew keepers while keeping the watchdog (which
already restarts crews) as the live force-cut governor until Phase 2 lands.

---

## 3. The health checklist — contents + home

**Decision: extend the existing ops-monitor `checks{}` map; do NOT build a new
agent.** The operator's "checklist agent" is already realized by ops-monitor — a
deterministic bash pass the daemon runs every 5m that writes a structured
`checks{}` digest and escalates flagged items to the captain. Building a *new*
Opus/Sonnet "checklist agent" would (a) duplicate ops-monitor, (b) cost tokens,
and (c) reintroduce the exact LLM-liveness SPOF that killed the ctx-watchdog. The
checklist must be deterministic and daemon-owned. We ADD checks; we do not add an
agent. (The captain remains the human-facing escalation target that READS the
flags — that is the "agent" in the loop, and it already exists.)

### 3.1 Existing checks (keep)

`daemon-up`, `supervisor-up`, `paused-queues`, `single-mode`, `crew-fresh`
(comms presence), `review-gate`, `backlog-ready`, `lull`
(`ops-monitor-check.sh:577-594`).

### 3.2 NEW checks (this design)

| Check key | Logic | State | Signal class |
|---|---|---|---|
| `crew-keepers` | For each crew in `crew.List`, `LiveKeeperPresent(proj, name)`. Flag any crew with a registry record (and a live tmux session) but NO live keeper lock. | flag if any missing | IMMEDIATE (this is the crisis signature) |
| `watchdog-up` | `tmux has-session -t ctx-watchdog` AND `ctx-watchdog.ctx` mtime < staleness. | flag if dead/stale | IMMEDIATE |
| `gauge-fresh` | For each managed crew, `<crew>.ctx` mtime < staleness (the gauge writer is alive). Distinguishes "no keeper" (crew-keepers) from "no gauge writer" (statusline hook gone). | flag if stale | DIGEST |
| `keeper-band-armed` (optional) | Assert each crew keeper argv carries the operator-configured band (catches a crew armed warn-only when config says restart, once D4 flips). | flag on mismatch | DIGEST |

**Implementation note**: `LiveKeeperPresent` is Go; `ops-monitor-check.sh` is bash.
Two clean options:
1. **(recommended, BUILDABLE-NOW)** add a tiny read-only CLI surface
   `harmonik keeper liveness --json` that returns `{crew: {keeper_live, gauge_fresh_s}}`
   across all crews (walks `crew.List` + `LiveKeeperPresent` + gauge stat). The
   bash script shells out to it (same pattern as `harmonik queue status --json`).
   This keeps the flock probe in Go where it lives, and makes the data reusable by
   `keeper doctor --all` too.
2. (alt) reimplement the flock probe in bash — rejected (duplicates load-bearing
   probe logic; flock-in-bash is fiddly across macOS).

The watchdog-up + gauge-fresh checks are pure bash (`tmux has-session`, `stat`) and
can land directly in the script with no new CLI.

### 3.3 Reconciliation with ops-monitor

- The new checks slot into the same `checks{}` map, same `immediate_signals` /
  `digest_signals` lists, same comms send, same `latest.json` `schema_version` bump
  (2→3). No new file, no new schedule, no new process.
- `crew-keepers` and `watchdog-up` go in `immediate_signals` (the crisis was a
  silent multi-hour gap — these warrant the 30m-cooldown immediate path,
  `ops-monitor-check.sh:603-613`). Cry-wolf is bounded by the same
  `IMMEDIATE_COOLDOWN=1800` dedupe ledger.

---

## 4. Comms escalation protocol — "check this + call cmd-good / fix + call cmd-fixed"

The operator wants an **actionable, ack-able** escalation, not a bare alert: a
message that names the issue, gives a verify-command and a fix-command, and a way
for the daemon to KNOW it was resolved (so it stops re-alerting / can re-escalate
if unaddressed).

### 4.1 Message shape

ops-monitor (and the daemon keeper-sweep) sends a structured escalation with a
stable **issue key** so acks can be correlated:

```
harmonik comms send --from ops-monitor --to captain --topic supervision \
  --issue crew-keeper-missing:paul \
  -- "[SUPERVISION] crew 'paul' has no live keeper. \
      VERIFY: harmonik keeper doctor paul \
      If healthy:  harmonik supervision ack crew-keeper-missing:paul --ok \
      If broken:   harmonik crew stop paul && harmonik crew start paul && \
                   harmonik supervision ack crew-keeper-missing:paul --fixed"
```

The `--issue KEY` is the load-bearing addition: it is the correlation handle the
ack writes back against. `KEY = <check>:<subject>` (e.g.
`crew-keeper-missing:paul`, `watchdog-down:ctx-watchdog`).

### 4.2 New CLI verbs

A small `harmonik supervision` command group (new file
`cmd/harmonik/supervision.go`), backed by a JSON ledger
`.harmonik/supervision/issues.json`:

| Verb | Purpose |
|---|---|
| `supervision issues [--json]` | List open issues `{key, first_seen, last_alert, alert_count, state}`. The captain reads this. |
| `supervision ack <key> --ok` | Operator/captain attests "checked, everything's fine" → state `acked-ok`. Suppresses re-alert until the underlying check FLIPS back to green-then-flag (a genuinely new occurrence). |
| `supervision ack <key> --fixed` | "I fixed it" → state `acked-fixed`. Same suppression; distinguished for audit. |
| `supervision resolve <key>` | Force-close (the condition is gone). |
| `supervision reopen <key>` | Manual re-escalate. |

### 4.3 Resolution / re-escalation logic (how the daemon knows it's resolved)

The ack is a HINT, not ground truth — the deterministic check is. Each ops-monitor
pass reconciles the ledger against live `checks{}`:

1. Check is **green** and issue exists → auto-`resolve` (the condition cleared; the
   daemon owns the terminal transition, mirroring the beads "daemon owns terminal"
   discipline).
2. Check is **flag** and no open issue → open issue, send escalation (edge).
3. Check is **flag** and issue is `open` → re-alert on the existing
   `IMMEDIATE_COOLDOWN` (30m) — unchanged behavior.
4. Check is **flag** and issue is `acked-ok`/`acked-fixed` → SUPPRESS (the captain
   said it's handled) UNLESS the check went green since the ack (a *new*
   occurrence) → reopen + alert. **Escalation backstop**: if an issue stays `open`
   (never acked) past `escalation_ttl` (config, e.g. 30m), re-send `--to operator`
   (the captain itself may be down — the crisis class). This is the one rung above
   "captain handles it."

This gives the exact operator-requested loop: *"check this issue and call
ack --ok if everything's good — or fix it and call ack --fixed."* The daemon
re-asserts from the deterministic check, so a false ack self-corrects on the next
pass.

### 4.4 Why a ledger, not just comms threads

comms `--reply-to` threads messages but has no queryable open/closed state and no
"is this still a problem" reconciliation. The ledger is the minimal state needed
for "the daemon knows it's resolved." It is daemon-local JSON (same shape family
as `ops-monitor/state.json`), not a new store.

---

## 5. Watchdog promotion to first-class

### 5.1 Auto-start (config-driven)

Add a `supervision:` block to `.harmonik/config.yaml`, parsed into a new
`SupervisionConfig` (sibling of `DaemonConfig`/`KeeperConfig` in
`projectconfig.go`). The watchdog auto-launches on daemon boot via an
`ensureCtxWatchdogSchedule` (mirroring `ensureOpsMonitorSchedule`,
`opsmonitor_schedule.go:53`) that registers an `every@<interval>` `ActionKindCommand`
running `ctx-watchdog-launch.sh` (idempotent — re-run while alive is a no-op,
launcher `:23`). The every-tick re-run IS the auto-relaunch: a dead pane is
recreated on the next tick. **This directly closes hk-sbitr** without inventing a
new mechanism — it's the same daemon-schedule pattern that already runs
ops-monitor.

Alternative considered: a `SupervisorWatchdog` goroutine in the daemon with
`ReviveCmd = ctx-watchdog-launch.sh`. Cleaner liveness semantics (probe + backoff +
cap + OnAlarm) but adds a daemon goroutine; the schedule-ensure path is simpler and
already proven. **Recommend the schedule-ensure path for Phase 1**; promote to the
goroutine if the every-tick latency (up to one interval) proves too slow.

### 5.2 Daemon liveness check

The `watchdog-up` check (§3.2) makes the daemon (via ops-monitor) report a dead or
stalled watchdog to the captain even if the auto-relaunch is disabled or failing.
Belt (auto-relaunch) AND suspenders (report).

### 5.3 Restart-via-daemon keeper path (hk-u5tgh)

The durable fix moves the crew-restart DECISION off the Sonnet agent and onto the
daemon's deterministic, keeper-arming spawn path:

- **Phase 2a (smaller)**: change the ctx-watchdog FORCE fallback from raw `tmux
  kill-session` + ad-hoc re-`start` to ALWAYS `harmonik crew stop <name> &&
  harmonik crew start <name>` (which routes through `HandleCrewStart →
  spawnCrewKeeperWindow`, guaranteeing a keeper window). Remove the raw-kill path
  that can strand a crew keeper-less. This is a prompt + launcher edit.
- **Phase 2b (durable, NEEDS-DECISION)**: move the 300k force-cut into the daemon
  entirely. The watchdog's gauge-read+threshold logic becomes a daemon
  keeper-sweep arm: when a crew's `<crew>.ctx` ≥ the operator-configured
  `hard_ceiling.abs_tokens` (already in config, `:65`), the daemon itself fires
  `ActionKindSpawnCrew`-style `HandleCrewStart` to recycle the crew (keeper-armed
  by construction). This RETIRES the standalone Sonnet watchdog — the daemon, which
  cannot silently die without the supervisor reviving it, becomes the governor.
  This is the cleanest end-state (no LLM in the liveness path at all) but is a
  behavior change the operator should sign off on (it changes what the 300k
  governor IS).

---

## 6. Config surface

New `.harmonik/config.yaml` block (additive; absent = current behavior):

```yaml
supervision:                 # NEW — daemon helper-process supervision
  crew_keeper_check: true    # daemon checks each crew has a live keeper (cheap; default ON)
  escalation_ttl: 30m        # open (un-acked) issue → re-escalate to operator after this
  watchdog:
    enabled: true            # auto-start + check the ctx-watchdog (cheap to leave on)
    relaunch_interval: 5m    # every-tick ensure cadence (the auto-relaunch tick)
    # restart_threshold_abs_tokens: 300000   # Phase 2b ONLY — the daemon-native governor cut.
    #   NOTE: when the daemon owns the cut (2b), this is an OPERATOR-REQUIRED value
    #   (no hardcoded default — ResolveSupervisionConfig fails loud, mirroring keeper).
    #   While the Sonnet watchdog owns the cut (Phase 1/2a), the 300k lives in the
    #   PROMPT (operator-editable text), not a runtime fallback — mandate-compliant.
```

**No-hardcoded-defaults mandate compliance**:
- `crew_keeper_check`, `watchdog.enabled`, `relaunch_interval`, `escalation_ttl`
  are **operational toggles/cadences**, not *keeper context thresholds* — the
  mandate (`feedback_no_hardcoded_keeper_thresholds`) governs warn/act/force/ceiling
  TOKEN bands, not on/off switches. These may carry compiled defaults (the mandate
  explicitly allows `off`/`0s` as valid explicit values; an absent toggle resolving
  to a safe default mirrors `self_service.crews_enabled` absent→true,
  `projectconfig.go:541-546`).
- The **token thresholds** the watchdog/governor enforces are the EXISTING
  operator-required keeper values (`keeper.hard_ceiling.abs_tokens`,
  `config.yaml:65`) — no NEW threshold key is introduced for Phase 1/2a. Phase 2b's
  `restart_threshold_abs_tokens`, IF added, is operator-required (fail-loud), never
  defaulted. **This is the one place the mandate bites — flagged NEEDS-DECISION.**

---

## 7. Phased breakdown — BUILDABLE-NOW vs NEEDS-OPERATOR-DECISION

### Phase 1 — detect + escalate + auto-relaunch (all BUILDABLE-NOW)

| Bead | Scope | Verdict |
|---|---|---|
| **P1-a** | `harmonik keeper liveness --json` — walk `crew.List` + `LiveKeeperPresent` + gauge mtime, emit `{crew:{keeper_live,gauge_fresh_s}}`. Pure read, reuses existing probe. | BUILDABLE-NOW |
| **P1-b** | ops-monitor: add `crew-keepers` (via P1-a), `watchdog-up` (`tmux has-session` + `.ctx` mtime), `gauge-fresh` checks to `checks{}`; bump `schema_version` 2→3; add to immediate/digest signal lists. | BUILDABLE-NOW |
| **P1-c** | `ensureCtxWatchdogSchedule` — daemon registers an `every@<interval>` `ActionKindCommand` for `ctx-watchdog-launch.sh` (mirrors `ensureOpsMonitorSchedule`). Gated by `supervision.watchdog.enabled`. **Closes hk-sbitr.** | BUILDABLE-NOW |
| **P1-d** | `harmonik supervision` CLI group (`issues`/`ack`/`resolve`/`reopen`) + `.harmonik/supervision/issues.json` ledger + ops-monitor `--issue KEY` flag + reconcile-against-checks logic (§4.3). | BUILDABLE-NOW |
| **P1-e** | `SupervisionConfig` parse (`supervision:` block) + wire the toggles. | BUILDABLE-NOW |

Phase 1 alone resolves the crisis class: the watchdog auto-relaunches (no more
silent 36h death), and a missing crew keeper is detected within 5m and escalated
actionably. Everything reuses an existing primitive; no new subsystem, no new
long-lived process, no LLM in the liveness path.

### Phase 2 — daemon-native restart integration

| Bead | Scope | Verdict |
|---|---|---|
| **P2-a** | ctx-watchdog FORCE path → always `crew stop`+`start` (keeper-armed); drop raw-kill strand. Prompt + launcher edit. **Closes the hk-u5tgh bypass.** | BUILDABLE-NOW (low-risk text edit) |
| **P2-b** | Move the 300k force-cut INTO the daemon keeper-sweep (retire the Sonnet watchdog as governor); add operator-required `restart_threshold_abs_tokens` (fail-loud). | **NEEDS-OPERATOR-DECISION** |
| **P2-c** | Daemon auto-RE-ARM of a missing keeper window on a *live* crew (vs. escalate-only). Risk: don't restart a busy crew / don't double-arm. | **NEEDS-OPERATOR-DECISION** |

### The three NEEDS-OPERATOR-DECISION items (the morning questions)

1. **Governor ownership (P2-b)**: keep the Sonnet ctx-watchdog as the 300k
   force-cut governor (daemon just keeps it alive — Phase 1), OR retire it and make
   the daemon the governor (Phase 2b)? The daemon-native answer is strictly more
   robust (no LLM in the liveness path) but changes what the governor IS and adds
   an operator-required threshold key. *Recommendation: Phase 1 now; decide 2b in
   the morning.* (This is also synthesis remediation item **f** — the durable
   "crews covered by neither" answer.)
2. **Crew-keeper auto-restart vs escalate (P2-c)**: when a crew loses its keeper,
   escalate-only (Phase 1, safe) or daemon auto-re-arms (Phase 2c, needs busy-crew
   guard)?
3. **Default toggles**: confirm `crew_keeper_check` and `watchdog.enabled` default
   ON (the operator said "cheap to leave on" → yes, but it's a default-value
   decision worth one line of confirmation).

---

## 8. Risks

- **Cry-wolf** (hk-pen9 history): new immediate signals (`crew-keepers`,
  `watchdog-up`) could spam the captain. Mitigated by the existing
  `IMMEDIATE_COOLDOWN=1800` dedupe ledger AND the new ack-suppression (§4.3).
  Guard: a freshly-spawned crew has a brief window before its keeper grabs the lock
  — add a grace (skip crews whose registry record is < `boot_grace` old) to avoid
  flagging a crew mid-launch. (`boot_grace` already exists, `config.yaml:75`.)
- **Auto-relaunch storm**: if `ctx-watchdog-launch.sh` is broken, the every-5m
  ensure could spawn-fail repeatedly. The launcher is idempotent (no-op when alive)
  so a *working* watchdog never re-spawns; a *broken* one fails cheaply 12×/hr and
  the `watchdog-up` check escalates. Optional: a revive-cap (the
  `SupervisorWatchdog.MaxRevives` semantics) if the schedule-ensure proves too
  eager.
- **Stale registry records**: `crew.List` can include a crew whose tmux session
  died (orphan-sweep removes records, but a race exists). Cross-check
  `tmux has-session` before flagging "keeper missing" — a dead crew isn't a
  keeper bug. (orphansweep `:120` already special-cases registry-backed crews.)
- **The mandate edge (P2-b)**: any new daemon-owned token threshold MUST be
  operator-required/fail-loud — do NOT ship a `300000` default. Flagged.
- **Schema bump**: `latest.json` 2→3 — any consumer parsing it (the captain prompt,
  crewlog tooling) should tolerate new `checks{}` keys (they already iterate the
  map, so additive keys are safe).

---

## 9. Test plan

- **P1-a (`keeper liveness`)**: table test — 3 crews in a temp registry; arm a real
  flock on one `<crew>.lock`, leave a stale-corpse lockfile on another, none on the
  third → assert `{live, stale→false, missing→false}`. Reuses the
  `LiveKeeperPresent` test fixtures.
- **P1-b (ops-monitor checks)**: golden `latest.json` — seed a temp project with a
  live keeper lock + a fresh `ctx-watchdog.ctx` → assert `crew-keepers:ok`,
  `watchdog-up:ok`; remove the lock + age the watchdog gauge → assert both `flag`
  and present in `immediate_signals`. (ops-monitor is bash; test via a
  fixture-dir + diff on `latest.json`, the existing pattern.)
- **P1-c (auto-relaunch)**: integration — register the ensure-schedule, kill the
  `ctx-watchdog` tmux session, advance the schedule clock one interval, assert the
  session reappears (`tmux has-session`). Idempotency: run twice while alive →
  exactly one session. (Caution: per memory `reference_keeper_smoke_forkbomb`, gate
  any looping-launch test tightly — `-run` a single case, never a bare loop.)
- **P1-d (supervision ledger)**: unit — open issue on flag-edge; `ack --ok` →
  suppress; check goes green → auto-resolve; check re-flags after green → reopen +
  alert; un-acked past `escalation_ttl` → re-send `--to operator`. Pure ledger
  reconciliation, no tmux.
- **P2-a**: assert the watchdog FORCE path emits `crew stop`+`crew start` (not raw
  kill) — prompt/launcher snapshot test or a dry-run harness.
- **Regression**: full `go test ./cmd/harmonik/ ./internal/daemon/ ./internal/keeper/`
  (per memory `reference_embedded_asset_resync` — if any embedded script under
  `scripts/` is edited, `cp` to `cmd/harmonik/assets/...` and re-run the embed-sync
  tests, else `TestSkillAssetsEmbedInSync`/`TestCaptainLaunchShEmbedInSync` go RED).

---

## 10. Summary of bead-sized pieces

- **P1-a** `keeper liveness --json` — BUILDABLE-NOW
- **P1-b** ops-monitor crew-keepers/watchdog-up/gauge-fresh checks — BUILDABLE-NOW
- **P1-c** daemon ensureCtxWatchdogSchedule auto-relaunch (closes hk-sbitr) — BUILDABLE-NOW
- **P1-d** `harmonik supervision` ack/resolve ledger + `--issue` escalation — BUILDABLE-NOW
- **P1-e** `supervision:` config block + toggles — BUILDABLE-NOW
- **P2-a** watchdog FORCE→`crew stop/start` keeper-armed (closes hk-u5tgh bypass) — BUILDABLE-NOW (text edit)
- **P2-b** daemon-native 300k governor (retire Sonnet watchdog) — NEEDS-OPERATOR-DECISION
- **P2-c** daemon auto-re-arm missing keeper on live crew — NEEDS-OPERATOR-DECISION
