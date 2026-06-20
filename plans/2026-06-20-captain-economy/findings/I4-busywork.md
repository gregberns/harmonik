# I4 — Captain Busy-Work Inventory

> **Investigation:** offload/eliminate captain busy-work. Operator complaint:
> "The captain does too much busy work. We need to get that offloaded."
> Leanfleet epic **hk-itoc** priority #3: "offload admin/checks to a cheaper model."
>
> **Sources read:** `.claude/skills/captain/STARTUP.md` + `SKILL.md`,
> `docs/orchestrator-rules.md` (Monitor pattern), `docs/plans/leanfleet/design.md`
> (D3 noise-cut, D4/D6 Sonnet ops-monitor, D5 3-tier handoff), and memories
> `reference_captain_crew_status_polling`, `reference_captain_quality_check_broken`,
> `feedback_captain_delegation_discipline`.
>
> **NOTE on a missing source:** `docs/retro/2026-06-17/token-burn-analysis.md`
> (cited as the WS5 monitor-effectiveness source) **does not exist on disk and was
> never committed** — `docs/retro/` holds only `2026-06-10/` and `2026-06-12/`. The
> design.md preserves the load-bearing findings from it (95.9% cache-read spend; the
> 600s heartbeat flagged for removal; the recurring Sonnet ops-monitor), so this
> inventory relies on design.md + the live skill files for the WS5 conclusions.

---

## Context: where the captain's tokens go

95.9% of fleet spend = **cache-read = context-size × turns × sessions** (design.md
§Finding). The captain is a long-lived Opus session. Every recurring action that
**re-invokes the captain** (a Monitor wake, a `/loop` tick, a poll) re-sends its
whole context at Opus rates. So the cost of a recurring task is dominated NOT by the
shell command but by **how often it wakes the Opus captain and whether that wake
produces an ACTION or is a no-op**. No-op wakes are pure waste — that is the
busy-work the operator is complaining about.

---

## Inventory + classification table

| # | Recurring task | Where defined | Fire cadence | Cost/firing | Usually ACTION or no-op? | Disposition |
|---|---|---|---|---|---|---|
| 1 | **Watcher 1 — `comms recv --agent captain --follow`** (operator direction + crew milestone/error/`epic_completed` feed) | STARTUP §6; SKILL §7 | Event-driven (wakes only on a bus message) | One Opus wake per delivered msg | **ACTION** — this is the sparse, actionable channel | **KEEP** (already lean — event-driven, not polling) |
| 2 | **Watcher 2 — `/loop 12m` health tick** (8 sub-checks: daemon up, queues unpaused, crews comms-fresh, drain comms, backlog-pull/staff, lull-deploy, quality-check, self-audit) | STARTUP §6; SKILL §0.5 | Every 12 min, unconditionally | **HIGH** — full Opus wake + 4–6 shell calls + reasoning, every 12 min, whether or not anything changed | **Mostly no-op** — most ticks find a healthy fleet and do nothing; the staffing/deploy sub-checks occasionally act | **CONSOLIDATE-INTO-PULSE** — split: deterministic checks (daemon up, paused queues, crew comms-freshness, quality-check, backlog readiness) → Sonnet ops-monitor (D4/D6) emitting signal-vs-digest; only the **judgment** sub-checks (staff a lane, lull-deploy, self-audit stalls) wake the Opus captain, and only when the ops-monitor flags them |
| 3 | **600s subscribe heartbeat** (run-level `subscribe --heartbeat 60s/600s` keepalive carrying `active_runs` ages) | Historical captain pattern; STARTUP §6 explicitly **forbids re-creating it**; design.md D3 says "drop it" | Every 60–600s | HIGH per wake — re-invokes captain every interval with run-level telemetry it should ignore | **No-op / anti-signal** — trains the captain to react to individual runs (the "observe everything" failure, operator-flagged 2026-06-11) | **ELIMINATE** — design.md D3: the `/loop 12m` tick is a strict superset; the 600s watcher "adds noise with no signal value." STARTUP already bans it; this confirms removal and that no successor is needed |
| 4 | **Quality-check sub-check** (grep `run_started` for `workflow_mode == single`) | STARTUP §5d & §6 tick (7); SKILL §0.5 | Every 12m tick (part of #2) | LOW shell cost, but **0% value** | **Always no-op — BROKEN** | **ELIMINATE the broken form / FIX + OFFLOAD-TO-SCRIPT.** `workflow_mode` does **not exist** on `run_started` (`reference_captain_quality_check_broken`; payload keys are only `bead_id, queue_group_index, queue_id, run_id, started_at, workspace_path`). The grep ALWAYS returns null = permanent false all-clear; a real review-gate bypass goes undetected. Correct check = per-`run_completed.run_id`, assert a matching `reviewer_verdict` event exists. This is **deterministic** → move it into the Sonnet ops-monitor's `latest.json` (#2), not an Opus tick |
| 5 | **Crew-status polling** (catch up on what each crew is doing) | `reference_captain_crew_status_polling`; NOT in STARTUP/SKILL as an inline-read | On-demand + optional ~10–15m | Designed as a **Haiku** sub-agent firewall (captain sees ≤5 lines) | ACTION (informs re-task/release decisions) | **KEEP — already offloaded to Haiku.** The skills do NOT instruct inline crew-transcript reading; they use `capture-pane` (instant, bounded) + `comms log`/`br comments` (read-only). The memory's Haiku-sub-agent protocol is the intended mechanism. **No change needed**, but verify STARTUP §5d's `capture-pane` taps stay bounded (`tail -25`) — they are |
| 6 | **Boot ground-truth (Step 2)** — daemon up, comms who, crew list, tmux windows, subscribe heartbeat, comms log, paused queues | STARTUP §2 | Once per boot/resume/wake | ~7–10 shell calls | ACTION (necessary state grounding) | **OFFLOAD-TO-SCRIPT (done) — KEEP.** Already consolidated into `~/.claude/captain-tools/captain-boot-digest.sh` (one call → one Markdown digest). This is the right pattern; the only residual cost is the Opus captain reading the digest, which is unavoidable on boot |
| 7 | **Backlog-pull / staffing sweep** (`kerf next` + `br ready --limit 0`, staff every free slot) | STARTUP §6 tick (5); SKILL §0 | Every 12m tick (part of #2) | Shell + Opus judgment to decide staffing | **Sometimes ACTION** (when ready work + free slot), often no-op | **CONSOLIDATE-INTO-PULSE.** The *detection* ("is there ready work AND a free slot?") is deterministic → Sonnet ops-monitor flags it. The *staffing decision* (which crew, which lane) is judgment → Opus captain, but only woken when the monitor flags a gap |
| 8 | **Lull-deploy + verify own merged work** (ff-after-push during a true lull) | STARTUP §6 tick (6); SKILL §0 | Every 12m tick (part of #2) | Opus judgment + git ops | Rarely fires (needs a true 0-merging lull) | **CONSOLIDATE-INTO-PULSE.** "Am I in a true lull (0 merging runs)?" is deterministic → ops-monitor. Deploy itself stays Opus (judgment + non-ff race awareness, `reference_captain_deploy_nonff_race`) |
| 9 | **Self-audit (stalled-initiative check)** | STARTUP §6 tick (8) | Every 12m tick | Opus reasoning | Mostly no-op | **CONSOLIDATE-INTO-PULSE** — fold into the judgment slice woken by the ops-monitor; do not run as a standalone Opus reasoning pass every 12m |
| 10 | **Crew-restart ACK verification** (`keeper await-ack` after a triggered restart) | SKILL §10 | Only when captain TRIGGERS a crew restart (rare) | One bounded shell wait | ACTION (load-bearing — verifies the restart) | **KEEP** — rare, durable, correct. Already a script (`keeper-restart-verified.sh`) for the captain's OWN restart |
| 11 | **Keeper WARN handling** (terse-ack, then restart-now at next clean checkpoint) | STARTUP §6; SKILL §10 | On each keeper WARN | One terse line (the HARD rule forbids re-narration) | ACTION | **KEEP** — already minimized to a terse-ack + restart-now; the 40+-idle-warn-cycle failure was fixed (hk-4zy9). Do not re-add re-narration |
| 12 | **`epic_completed` attribution + re-task** | SKILL §5 | Event-driven (on each `epic_completed`) | `br show --assignee` + re-task | ACTION (genuine orchestration) | **KEEP** — design.md D6 explicitly lists this as STAYING on the captain (needs judgment) |

---

## Specific assessments asked for

### 1. The two captain watchers / heartbeats — is the 600s heartbeat noise?

There are effectively **three** monitor constructs, not two:

- **Watcher 1** (`comms recv --follow`) — event-driven, sparse, actionable. **KEEP.**
- **Watcher 2** (`/loop 12m` health tick) — unconditional 12-min Opus wake doing 8
  sub-checks. The bulk of it is deterministic and mostly no-op. **CONSOLIDATE into
  the Sonnet pulse** (see #2 and D4/D6).
- **The 600s subscribe heartbeat** — **YES, it is noise. ELIMINATE.** design.md D3
  is explicit: "Drop the captain 600s subscribe heartbeat … the /loop 12m tick is a
  strict superset; the 600s watcher adds noise with no signal value." STARTUP §6
  already forbids re-creating the run-level `--heartbeat 60s` subscribe (the
  "observe everything" failure, operator-flagged 2026-06-11). It re-invokes the Opus
  captain on a short timer with per-run telemetry the captain is told to ignore —
  pure cache-read burn. No successor needed.

### 2. Crew-status polling — Haiku sub-agent or inline?

**Already offloaded to Haiku, correctly.** STARTUP/SKILL do NOT instruct the captain
to read crew transcripts inline; they use bounded `capture-pane` (`tail -25`) +
read-only `comms log` / `br comments`. The richer catch-up path
(`reference_captain_crew_status_polling`) dispatches a **Haiku** sub-agent running
`crewlog.sh <session_id> 60` that returns ≤5 lines — a context firewall so the Opus
captain never sees the multi-MB JSONL. **No change needed.** (One gap: this Haiku
protocol lives in memory, not in STARTUP/SKILL — worth promoting into the skill so it
is not forgotten across resets, but that is documentation hygiene, not new offload.)

### 3. The /loop health tick + quality-check — wasting turns on a broken check?

**Yes.** The quality-check sub-check (tick step 7, also STARTUP §5d) greps
`run_started` events for `workflow_mode == single`. **That field does not exist** on
`run_started` (`reference_captain_quality_check_broken`, verified 2026-06-17 against
daemon binary 7ee10031). The grep **always returns null = a permanent false
"no single-mode dispatches" all-clear** — so a genuine review-gate bypass (the
historical hk-u6zp class) goes undetected. The captain runs this check every 12m,
gets a meaningless null, and reports a false all-clear. **ELIMINATE the broken
grep; replace with the deterministic correct check** (for each `run_completed.run_id`,
assert a matching `reviewer_verdict` event with that `run_id` exists; a completed run
with no verdict ran review-bypassed) and **run it in the Sonnet ops-monitor**, not an
Opus tick. The captain only needs the flagged exceptions.

### 4. What the recurring Sonnet ops-monitor (D4/D6) absorbs

design.md §6 (ADMIN-OFFLOAD): a **Sonnet ops-monitor on `ops-q`, 5-min cadence**, runs
the deterministic checks, writes `latest.json`, and sends the captain a
**signal-vs-digest** (alert only on a state change / actionable gap). It **shrinks the
captain `/loop 12m` tick**. It absorbs the deterministic detection slices of Watcher 2:

- daemon-up / RPC-reachable check
- paused-queue / `complete-with-failures` sweep
- crew comms-freshness (each crew <150s in `comms who`)
- review-gate honesty check (the **fixed** quality-check, #4 above)
- backlog readiness detection (`kerf next` + `br ready` → "ready work + free slot exists")
- lull detection ("0 merging runs right now")

**STAYS on the Opus captain** (design.md §6, needs judgment): `epic_completed`
attribution + re-task, operator comms, failure escalation/triage, the actual staffing
**decision**, and the lull **deploy**. Net effect: the captain stops waking every 12m
to run no-op checks; it wakes on (a) a real bus message (Watcher 1) or (b) a Sonnet
ops-monitor signal that something needs a judgment call.

---

## Summary classification

- **KEEP:** Watcher 1 (comms recv), Haiku crew-status polling, boot-digest script,
  crew-restart ACK verify, keeper-WARN terse-ack, epic_completed re-task. (#1, 5, 6,
  10, 11, 12)
- **ELIMINATE:** the 600s subscribe heartbeat (#3); the broken `workflow_mode==single`
  quality grep in its current form (#4).
- **OFFLOAD-TO-CHEAP-MODEL (Sonnet ops-monitor) / CONSOLIDATE-INTO-PULSE:** the
  deterministic slices of the `/loop 12m` tick — paused-queue sweep, crew-freshness,
  the *fixed* review-gate check, backlog-readiness detection, lull detection,
  self-audit detection (#2, 4, 7, 8, 9).
- **OFFLOAD-TO-SCRIPT (already done, keep):** boot ground-truth via
  `captain-boot-digest.sh` (#6).
