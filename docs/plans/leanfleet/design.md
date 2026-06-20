# Leanfleet: Fleet Token-Efficiency Initiative — Converged Design

> **Source:** epic hk-itoc comment (2026-06-17), produced by 5 design agents + 2
> adversarial critics. This is the authoritative design record for the leanfleet
> initiative.
>
> **Refs:** epic hk-itoc. (The 2026-06-17 token-burn audit report was never
> committed as a standalone file — its conclusions are preserved in this
> design's "Finding" section below and in the hk-bsdr bead comments.)
>
> **Lane split (captain boundary):** PAUL = all keeper code; LOGMINE = non-keeper
> design/coordination docs.

---

## Finding

95.9% of fleet token spend is cache-read = context re-sent per turn. The bill is
the long-lived Opus captain+crew orchestrator sessions; daemon workers already run
Sonnet.

**Operator priority order:**
1. Cut restart/startup cost
2. Cut the noise captain+crew see
3. Offload admin/checks to a cheaper model
4. Make handoffs/pickups clear (short-term tasks / mid-term epics / long-term goals;
   mid/long are stable and should not be re-derived on each restart)

---

## Principle

95.9% of spend = cache-read = context-size × turns × sessions.
Levers = smaller context/turn (restart-earlier) + cheaper resume + fewer
no-action turns + cheaper model where safe.

---

## Workstream Design

### 1. RESTART-EARLIER (TA1 — hk-8hr1, P0, paul, DISPATCHED)

keeper warn 185–200K / act 215K default + persist (thresholds.go:24-25 or
`.harmonik/config.yaml` `keeper.context_thresholds`).

**C1 MUST-FIX:** set `force_act_abs_tokens >= 240K` explicitly + assert
`warn LT act LT force_act` after `min(abs, pct_ceil × window)` resolution
(it inverts on small/fallback windows). Band-width is a NON-issue: handoff is
written by the keeper-injected `/session-handoff` during the cycle and truncated
on resume; anti-loop + BootGrace prevent thrash.

### 2. SELF-RESTART HINT (paul / keeper)

ONE-TIME 190K hint then the agent self-fires
`harmonik keeper restart-now --agent SELF` (already exists, `cycle.go:1082
RunOnDemand`).

**C1 guard:** hint text MUST enumerate unsafe-to-fire conditions (armed Monitor /
pending sub-agent result / unverified file edit) or agents self-fire mid-monitor-loop
and drop daemon completion events.

### 3. IDLE (C2 — overruled reap-timer)

C2 OVERRULED a per-crew reap-TIMER (cold-start cost + comms-race exceed
idle-burn saved). Use TA1 restart-to-SMALL to make idle crews cheap-but-warm;
REAP only at epic-complete (exists) + fleet-sleep (hk-rl4b, paul).
Below-act idle restart-to-small = low-pri keeper bead.

### 4. NOISE (D3)

The repeated "[NOTICE] prepare for restart" = the inject-RETRY loop
(`watcher.go:780-805`) retrying the paste every 5s, NOT the warn (which latches).

**Fixes:**
- 30s back-off on the retry (paul)
- Drop the captain 600s subscribe heartbeat (STARTUP.md, logmine) — the /loop 12m tick
  is a strict superset; the 600s watcher adds noise with no signal value
- `no_gauge` re-emit 120s → 300s (paul)

**KEEP** the "ample buffer remaining" text (anti-premature-quit guard, purposeful).

### 5. MODEL-TIERING (D4 + C2)

Per-crew model field in the mission front-matter; daemon injects `--model` on the
crew launch argv (`crewlaunchspec.go`, stilgar).

**Decision rule:**
- **Sonnet** = lane-drain crews with file-disjoint clean beads + mission clause
  "escalate to captain on ANY run_failed, do NOT self-classify"
- **Opus** = design / test / investigation crews

**C2 bright line = failure-triage.** Sonnet crews escalate ALL failures to the
captain; they do not self-classify or decide whether to retry.

### 6. ADMIN-OFFLOAD (D4)

A Sonnet ops-monitor on `ops-q`, 5m cadence, deterministic checks to `latest.json`
+ signal-vs-digest to captain; shrinks the captain `/loop 12m` tick.

**STAYS on the captain** (need judgment): epic_completed attribution / operator
comms / failure escalation.

### 7. 3-TIER HANDOFF (D5)

| Tier | File | Cadence | Content |
|---|---|---|---|
| Tier-3 | `.harmonik/context/project.yaml` | Weeks | Phase, forbidden actions, locked decisions |
| Tier-2 | `.harmonik/context/captain-lanes.md` | Days | Active lanes, operator initiatives, parked, next pipeline |
| Tier-1 | `HANDOFF.md` + mission `## Current State` | Every /clear | queue_id, in_flight, monitor, next_action, blockers, translations |

**Boot sequence:** read tier3 → tier2 → tier1 → VERIFY live state.
`kerf next` / `STATUS.md` become VERIFICATION not DISCOVERY.
Boot shrinks from ~10–15 calls to 3 reads + 2 verifies (logmine).

The GLOBAL `session-handoff` / `session-resume` skills are edited separately
(the daemon cannot reach `~/.claude/skills`).

---

## Beads

| Bead | Workstream | Owner | Status |
|---|---|---|---|
| hk-8hr1 | TA1 restart-earlier | paul | DISPATCHED |
| hk-n3w1 | TA2 boot-digest | paul | dispatched |
| hk-rl4b | fleet-sleep / idle-reap | paul | open |
| hk-6xiy | LF-A in-repo skills+context (this doc) | logmine | dispatched |
