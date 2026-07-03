# 01 — Problem Space: Watch-Officer Tier + Captain Wake-Economy

> Kerf work: `wake-economy` (jig=spec). Epic: hk-var9b. Bead label: `codename:wake-economy`.
> Source brief: `plans/2026-06-23-captain-wake-economy/README.md` (admiral, operator-paired, 2026-06-23).
> This problem-space is grounded in a 5-dimension read-only codebase audit (paul, 2026-06-24);
> evidence with file:line in `scratchpad/wake-economy-research.md` and inline below.

## What is changing, and why

The Opus captain burns context because it **wakes too often**. The brief's audit found ~240–370 Opus
wakes/day, 80–90% pure churn. The audit predates a key fact this work must build on:

**CE4 already offloaded the *deterministic checks*.** The captain's `/loop 12m` health tick no longer
re-derives fleet health — a daemon-scheduled `every@5m` **bash** ops-monitor (`scripts/ops-monitor-check.sh`)
runs all the deterministic checks (daemon-up, supervisor-up, paused-queues, single-mode, crew-staleness,
review-gate, backlog-ready, idle-fleet) and lands them in `.harmonik/ops-monitor/latest.json`; the captain
tick is one cheap read + judgment on flags (`STARTUP.md:513-534`, CE4 note `:519-527`).

So the **remaining** burn is concentrated in two places, and *that* is what this work removes:
1. **Watcher 1 churn** — `comms recv --agent captain --follow` (`STARTUP.md:505-509`) wakes the Opus captain
   on **every** bus message, including routine crew status posts, ops digests, keeper ACKs (120–250/day).
2. **The 12-min read-loop itself** — even as a cheap read, `/loop 12m` is a fixed ~120 wakes/day that should
   be event-driven instead of timer-driven (operator: **no poll loops, no 30s loop**).

The structural fix the brief calls for: move the *triggers* off the captain. Introduce a cheap **Sonnet
"watch officer" session** between ops-monitor (bash, deterministic) and the Opus captain. It consumes the
bus + the ops-monitor digest + crew reports, records everything to a queryable ledger, and escalates
**only actionable summaries** to the captain — event-driven, so the captain wakes on a real escalation, not
on churn or a timer.

Important framing correction (audit finding R4): the brief calls ops-monitor "already a Sonnet offload."
It is **not** — ops-monitor is a bash script with no LLM and no session; it cannot receive comms, decide,
or be reasoned with. The watch officer is the **missing Sonnet session tier**, layered on top of ops-monitor.

## Goals (what is true about the system after this change)

- **G1.** A cheap always-on **watch-officer session** (Sonnet) intercepts the majority of bus events,
  records them to a **queryable event ledger** (nothing lost), and does **not** wake the captain for
  routine items. (O1)
- **G2.** The watch officer forwards to the captain **only actionable summaries**, in the captain's own
  words ("X lane finished — needs a re-task decision"; "subsystem Z erroring — look"). (O1)
- **G3.** Escalation is **event-driven** — the captain wakes on a real escalation (a directed, woken comms
  message) plus a **long heartbeat** as a liveness fallback only. **No short poll loop.** (O2)
- **G4.** Scheduled tasks ("a task that must take place," e.g. "verify all services up") run on a
  **config-driven** time+interval — both target and interval are config, never hardcoded. (O3)
- **G5.** A **configurable (default hourly)** mutual liveness check: watch officer checks the captain AND
  the captain checks the watch officer — neither can silently die. (O4)
- **G6.** Operator→captain messages reach the captain **directly** — never intercepted, buffered, or
  downgraded by the watch officer. (O5)
- **G7.** The captain's STARTUP/skill is rewritten so it **stops re-arming the churn watchers**; the win
  **survives every keeper restart**. After: captain watchers = {filtered comms (operator + watch-officer
  escalations), `epic_completed`, a long heartbeat}; the `/loop 12m` health loop is **removed**. (O6)
- **G8.** Opus-only judgment is preserved — crew-failure/kill, new-initiative ranking, locked-decision
  reversal, destructive ops, and the staffing decision (which crew + epic) **never** downgrade to the
  cheap tier; the watch officer may *flag* "ready + free slot," the captain decides. (O7)

## Success criteria (what the specs should describe when complete)

- **SC1.** The spec defines a **watch-officer role**: its inputs (bus via `subscribe`/`comms`, the
  ops-monitor digest, crew reports), its ledger, its escalation outputs, and the explicit boundary of what
  it may decide vs. must escalate (the O7 judgment list).
- **SC2.** The spec defines an **escalation taxonomy**: which event/signal kinds are IMMEDIATE (wake captain
  now), which are BATCHED (periodic digest), and which are LEDGER-ONLY (never wake) — grounded in the real
  ~79 bus event types and ops-monitor's existing IMMEDIATE/DIGEST topics.
- **SC3.** The spec defines the **event ledger**: its location (recommended: reuse `events.jsonl` +
  a watch-officer cursor file), its query surface, and the `event_id` dedupe contract (N3 / EV-018).
- **SC4.** The spec defines the **config-driven scheduled-task** mechanism for O3/O4, reusing the existing
  `harmonik schedule` primitive (`.harmonik/schedules.json`, `every@<dur>`/`daily@HH:MM`), and resolves the
  one gap: there is no `comms-send` action kind today (decision: command-wrapper vs native action).
- **SC5.** The spec defines the **captain's new watcher set** and the exact, normative STARTUP.md / SKILL.md
  edits (Step 6, resume re-arm, wake-path, "EXACTLY two watchers" language) that remove the 12-min loop and
  filter comms — written so the reduction **survives keeper restart**.
- **SC6.** The spec defines the **mutual liveness** check by **reusing the just-shipped component-liveness
  alerting** (captain-liveness via `comms who` last_seen + tmux probe; escalation tiers; per-crew keeper
  coverage), adding the watch officer as a tracked critical component.
- **SC7.** The spec defines **launch & keep-alive**: the watch officer reuses **crew+keeper machinery**
  (`harmonik start crew`, `model: sonnet` mission frontmatter, dedicated epic+queue), surviving keeper
  restart via durable queue + beads assignee. It picks a **name** (Q1) and uses it consistently.

## Non-goals (explicitly out of scope)

- **NG1.** The **R1 "de-hardcode messages & intervals" sweep** (epic hk-2ffoj) — a separate, lower-priority
  project. O3/O4 are concrete instances of that rule but the general codebase audit is not folded in here.
- **NG2.** Re-implementing the deterministic checks — CE4's bash ops-monitor already owns them; the watch
  officer consumes its output, it does not re-derive it.
- **NG3.** Changing the captain's model (always Opus) or the daemon's dispatch/review-loop.
- **NG4.** Building a new scheduler — the `harmonik schedule` primitive already exists; at most we add a
  `comms-send` action kind.
- **NG5.** **Implementation/build itself** — this work produces a reviewed *spec* only. Build is gated on
  reviewers-with-critics approval (operator hard requirement), then dispatched as `codename:wake-economy`
  beads.

## Constraints

- **C1.** Spec-first — this is a **normative change to the captain skill**; the spec is authoritative.
- **C2.** **Reviewers WITH critics** must review the produced design **before any build** (operator hard req).
- **C3.** Planned/built **by a crew** (paul orchestrates), not by the captain inline.
- **C4.** **No hardcoded** messages/targets/schedules/intervals — config-driven, consistent with the
  keeper "no hardcoded thresholds" mandate.
- **C5.** **No short poll loops / no 30s loop** — operator-emphatic. Event-driven + long heartbeat only.
- **C6.** Do **not** reintroduce the forbidden `run_stale,heartbeat` "observe everything" subscribe
  (operator-flagged 2026-06-11; `STARTUP.md:495-503`).
- **C7.** Reuse existing primitives wherever they exist (schedule, comms/subscribe filtering, component-
  liveness, crew+keeper launch) — net-new code only where an audited gap requires it.

## Affected spec / artifact areas (preliminary)

- `.claude/skills/captain/STARTUP.md` — Step 6 watcher arming; resume re-arm prose; wake-path. **(normative)**
- `.claude/skills/captain/SKILL.md` — "EXACTLY two watchers" language; epic_completed fold. **(normative)**
- A **new** watch-officer skill (`.claude/skills/watch-officer/` or chosen name) + mission template.
- `specs/` — a new or extended spec section defining the watch-officer role + escalation taxonomy + ledger.
- `internal/schedule` + `cmd/harmonik/schedule.go` — only if a `comms-send` action kind is added (gap).
- `cmd/harmonik/comms.go` / `internal/daemon` — only if a repeatable `--from` filter is added for the
  captain's filtered receive (gap).
- `scripts/ops-monitor-check.sh` + component-liveness design — add watch-officer as a critical component.
- Embedded-asset mirrors (`cmd/harmonik/assets/skills/*`) for any skill edit (TestSkillAssetsEmbedInSync).

## Open questions carried into design (from the brief)

1. **Name** for the role (operator wants better than "duty officer"): watch officer / signal officer /
   purser / steward / quartermaster. Pick one, use consistently.
2. **Event ledger location** — recommend reuse `events.jsonl` + cursor file; confirm in design.
3. **Scheduled-message primitive** — `harmonik schedule` exists and covers it; resolve the `comms-send`
   action gap (command-wrapper vs native).
4. **Escalation taxonomy** — exact IMMEDIATE/BATCHED/LEDGER-ONLY partition.
5. **Launch & keep-alive** — recommend crew+keeper; confirm restart survival.
6. **Model** — Sonnet (triage judgment) vs Haiku (deterministic only). Recommend Sonnet; ops-monitor bash
   already owns the deterministic slice.
