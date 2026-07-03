# Component Liveness Alerting — Design
_2026-06-22_

## Problem

The captain manually noticed (via pgrep) that no `harmonik supervise` process appeared running, yet ops-monitor reported `supervisor-up: ok "running"`. This discrepancy is itself a bug: the captain's mental model (a binary called `harmonik supervise`) does not match reality (the supervisor role is played by `hk-keeper.sh`, a bash script). ops-monitor's detection WAS correct in this case — but the alert path is passive (digest to captain), not LOUD.

Two separate issues:
1. **pgrep mismatch** — the captain's ad-hoc `pgrep harmonik supervise` finds nothing because the supervisor is `hk-keeper.sh`, not a long-lived `harmonik supervise` binary. ops-monitor's `keeperLoopAlive()` uses `pgrep -f "hk-keeper.sh"` which DOES work. The check itself is not broken; the UX/docs are: the captain has no clear mental model of what "supervisor running" looks like in the process table.
2. **Alert path is passive** — `supervisor-down` already emits IMMEDIATE on the comms bus, but when everything is OK there is no active signal. The bug is that a DOWN state could go unnoticed for up to 5 minutes (the ops-monitor schedule), and even an IMMEDIATE only reaches the captain if the captain is actively reading comms. There is no escalation path if the captain itself is down.

## Root Cause of the pgrep-vs-digest Discrepancy

`hk supervise status --json` reports `running: true` via one of three paths:
1. `supervisor.pid` pidfile + `kill(pid, 0)` — the shim wrote a pid
2. `pgrep -f "hk-keeper.sh"` found a live process
3. `tmux has-session -t hk-<hash>-keeper` found the session

All three correctly identify the real supervisor (`hk-keeper.sh`). The captain's ad-hoc `pgrep harmonik supervise` finds nothing because no such binary runs continuously — only during the brief `status` call itself. **The detection is correct; the captain's mental model is wrong.** The fix is documentation + a `harmonik supervise ps` command that emits the canonical process and session signatures to remove ambiguity.

## Key Components and Liveness Checks

| Component | How to detect (current) | Reliable? | Notes |
|---|---|---|---|
| **Daemon** | `harmonik queue status --json` exit-17 = down | Yes | Socket probe, definitive |
| **Supervisor** (`hk-keeper.sh`) | `pgrep -f "hk-keeper.sh"` OR `tmux has-session hk-<hash>-keeper` | Yes | Both checked by `keeperLoopAlive()` |
| **Captain** | `harmonik comms who` presence + `tmux has-session hk-<hash>-captain` | Partial | comms presence goes stale; keeper status better |
| **Crew keepers** | `harmonik keeper doctor <crew>` / process table `harmonik keeper --agent <name>` | Yes | Already tracked per-crew via keeper subsystem |
| **Crew agents** | `harmonik comms who` last_seen_s + crew-staleness check | Yes | 150s threshold, 2-miss before signal |

## Alert Design: Loud + Escalating

### Tier 1 — IMMEDIATE on first detection (existing, keep)
Already implemented for `daemon-down` and `supervisor-down`. These fire on the first miss with a 30-minute cooldown re-alert. **No change needed for Tier 1 logic.**

### Tier 2 — Escalation when DOWN persists > 10 minutes
Currently missing. If a component is still down on the NEXT ops-monitor cycle (5 min) after an IMMEDIATE was sent, re-alert with higher urgency: `[ESCALATION] supervisor-down for >5m — no self-healing path`. Track in `state.json` as `alerted_immediate[<component>]: {ts, count}`.

### Tier 3 — Operator escalation when DOWN > 30 minutes
If a critical component stays down for 30+ minutes, the captain should attempt to wake the operator via a distinct topic: `--topic ops-CRITICAL` (the captain watches this separately from the normal digest topic). The captain's /loop handler should differentiate `ops-CRITICAL` from `ops-monitor` and respond immediately rather than on the next 12-minute tick.

### New: Captain-self-monitor
ops-monitor is scheduled by the daemon. If the CAPTAIN is down, nothing alerts the operator today. Need: a separate watcher that checks captain presence independently — either:
- Option A: the daemon itself emits `captain-down` when captain comms presence > threshold (no new code path needed, daemon already watches comms)
- Option B: a cron that checks `tmux has-session hk-<hash>-captain` and sends `--to operator` (but "operator" is not a comms participant)

Recommended: Option A — add captain liveness to ops-monitor checks using `comms who` + tmux session probe. IMMEDIATE if captain has been absent >10 min.

### New: Keeper coverage check
Per the keeper-coverage-crisis investigation (`plans/2026-06-22-keeper-coverage-crisis.md`), crews may run without keepers. ops-monitor should check: for each online crew (comms who), does a `harmonik keeper --agent <crew>` process exist in the process table? If not, flag as `keeper-missing:<crew>` IMMEDIATE.

## Changes Required

### 1. `harmonik supervise ps` subcommand (documentation/UX fix)
Print the canonical process signatures and tmux sessions that indicate supervisor is alive. Eliminates the captain's pgrep confusion. Low effort, high clarity.

### 2. ops-monitor: escalation tier in `state.json`
Track `{component: {first_alert_ts, alert_count}}` in `state.json`. On each cycle where a component is still down after a previous IMMEDIATE, increment count and send escalation with count in the message. At count ≥ 6 (30+ minutes), escalate to `--topic ops-CRITICAL`.

### 3. ops-monitor: captain liveness check
Add check 1b: `captain-up` — `harmonik comms who --json` to find captain's `last_seen_s`; if absent OR `last_seen_s > 600` (10 min), also probe `tmux has-session hk-<hash>-captain`. Flag as IMMEDIATE if both fail.

### 4. ops-monitor: keeper coverage check  
Add check 1c: `keepers-covered` — for each crew in `comms who`, verify a `harmonik keeper --agent <crew>` process exists (pgrep on the `harmonik keeper --agent` string). Flag missing keepers as IMMEDIATE.

### 5. ops-monitor: alert on EVERY critical failure, not just first + cooldown
The 30-minute IMMEDIATE_COOLDOWN suppresses re-alerts. For critical infrastructure (daemon, supervisor, captain), the re-alert cooldown should be much shorter: 5 minutes, not 30. A component that has been down for 30 minutes should have sent ~6 alerts by then.

## Implementation Priority

1. **Bead A** — ops-monitor escalation tier (state.json + escalation signal) — P1
2. **Bead B** — ops-monitor captain liveness check — P1  
3. **Bead C** — ops-monitor keeper coverage check — P1
4. **Bead D** — `harmonik supervise ps` UX subcommand — P2
5. **Bead E** — Shorten IMMEDIATE_COOLDOWN for critical components (daemon/supervisor/captain) from 30m → 5m — P1

## What NOT to change

- `keeperLoopAlive()` detection logic — it is correct.
- The existing `supervisor-down` IMMEDIATE path — it works.
- The 5-minute ops-monitor schedule — appropriate cadence.
