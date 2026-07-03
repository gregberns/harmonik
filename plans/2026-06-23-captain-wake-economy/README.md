# Captain Wake-Economy + Watch-Officer Tier — Objectives

> **Status:** objectives brief (admiral, 2026-06-23, operator-paired). NOT a design spec.
> **Next:** captain dispatches a crew to produce the thorough design/spec from these objectives.
> **Review gate:** the produced plan MUST be reviewed by reviewers **with critics** before build.
> **Author:** admiral (oversight). **Owner of build:** captain → crew.

## Why

The Opus-model captain burns context because it **wakes too often**. An audit (2026-06-23)
found it arms three watchers that generate **~240–370 Opus wakes/day, 80–90% of them pure
churn**:

| Wake source | Volume/day | Nature |
|---|---|---|
| `comms recv --agent captain --follow` (wakes on **every** bus message) | 120–250 | churn — crew status posts, ops digests, keeper ACKs |
| `/loop 12m` health tick (re-runs checks ops-monitor already ran 5 min earlier) | 120 (fixed) | mostly deterministic, not Opus-worthy |
| `epic_completed` subscribe | 5–10 | genuine decisions |

Loop restarts now land ~180k context (better), but the captain still wakes on every crew
status post and re-runs deterministic checks a bash script already did. That is the burn.
The operator has previously told the captain to "delegate the checks" and it did not stick —
because the captain still **received the triggers** and re-armed the churn watchers at boot.
The structural fix is to move the *triggers* off the captain, not just its intent.

## What we're building (objectives)

### O1 — A cheap intermediary tier ("watch officer" — name TBD; see Open Questions)
A Sonnet/Haiku always-on session that sits **below** the Opus captain and **above** the crews.
It must:
- **Intercept the majority of bus events**, record them to a queryable **event ledger** (so
  nothing is lost — "track that they happened"), and **not wake the captain** for routine items.
- **Forward to the captain only actionable summaries**, in the captain's own words, e.g.
  "*X lane finished — needs a re-task decision*" or "*subsystem Z is erroring — captain should look*".
- **Own the deterministic checks** the captain currently does on its 12-min loop: are all
  services up, crew staleness, backlog-ready + free-slot, review-gate audit (run completed
  without a reviewer verdict).

### O2 — Event-driven, NOT polling
**No short poll loops. No 30-second loop — explicitly rejected by the operator.** The captain
wakes on a real escalation (event-driven) plus a long heartbeat *only* as a liveness fallback.
The watch officer batches escalations (small bucket) and sends them; the captain's monitor
wakes on receipt.

### O3 — Configurable scheduled-message mechanism
A mechanism (the supervisor, or the watch officer) that can **send scheduled messages where the
time and interval are configurable**. Each scheduled message is a **task that must take place**
(e.g. "verify all services up"). This sits alongside the event-driven path. **Both the messaging
targets AND the intervals must be config-driven — never hardcoded.** (See related project R1 —
this objective is a concrete instance of the broader "no hardcoded messages/intervals" rule.)
Check first whether an existing primitive (`harmonik schedule` / cron) already covers this.

### O4 — Mutual liveness check
A **once-an-hour (configurable)** task where the watch officer checks on the captain AND the
captain checks on the watch officer — bidirectional liveness so neither can silently die.

### O5 — Preserve the direct operator→captain path
Operator messages reach the captain **directly** — never intercepted, buffered, or downgraded.

### O6 — Make the reduction survive restarts
Rewrite the captain's STARTUP/skill instructions so it **stops re-arming the churn watchers**.
The win must persist across every keeper restart, not decay back to today's behavior. After the
change the captain's watchers should be: filtered comms (operator + watch-officer escalations
only) + `epic_completed` + a long heartbeat. The 12-min health loop is **removed** (absorbed by
the watch officer).

### O7 — Preserve Opus-only judgment
These NEVER downgrade to the cheap tier: crew-failure/kill decisions, new-initiative ranking
(work not already in `kerf next`), locked-decision reversal, destructive ops (force-push,
reset --hard), and the staffing decision (the watch officer may *flag* "ready + free slot";
the captain decides **which** crew + epic).

## Dependency — fix the captain-wake bug (prerequisite, dispatch now)
The escalation path only works if the watch officer can actually **wake** the captain.
Known bug: `comms send --to captain --wake` derives the pane name `…-crew-captain`, but the
captain's session is just `captain`, so the wake silently fails (ref
`reference_comms_wake_captain_pane_mismatch`). This must be fixed (or an equivalent reliable
wake path established) before O2's event-driven escalation can be trusted. Small, standalone —
dispatch ahead of the main design.

## Expected outcome
~90% fewer Opus captain wakes; comms-churn and 12-min-tick wakes eliminated; freed Anthropic
budget reallocated to real crew/throughput work.

## Open questions for the design crew
1. **Name** for the new role (operator wants a better one than "duty officer"). Candidates:
   *watch officer · signal officer · purser · steward · quartermaster.* It's a role, not a
   Dune-named crew instance (matches captain/admiral). Pick one and use it consistently.
2. **Where the event ledger lives** (a `.harmonik/` file surface? reuse the comms log? a new
   typed store?) and how the captain queries it on demand.
3. **Scheduled-message primitive**: does `harmonik schedule`/cron already provide this, or is a
   new config-driven scheduler needed? Reuse if it exists.
4. **Escalation taxonomy**: the exact set of escalation kinds + which are immediate vs batched.
5. **Launch & keep-alive**: reuse the ops-monitor (already a Sonnet offload) / crew+keeper
   machinery, or a dedicated launcher? How does it survive a keeper restart?
6. **Model**: Sonnet vs Haiku for the watch officer (Haiku cheaper; can it do the triage
   judgment, or only the deterministic checks with Sonnet for the summaries?).

## Related project (separate, lower priority) — R1: de-hardcode messages & intervals
Operator directive: stand up a **separate, lower-priority project** that audits the codebase for
**anywhere a message, message target, schedule, or interval is hardcoded** and moves it to
configuration — both the *messaging* and the *intervals*. O3/O4 above are specific instances of
this rule; R1 is the general sweep. Track as its own epic, not folded into this initiative.

## Constraints
- Spec-first (this is a **normative change to the captain skill** — kerf work).
- Reviewers **with critics** must review the produced design before any build.
- Planned/built **by a crew**, not the captain inline.
