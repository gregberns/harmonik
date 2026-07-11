<!-- DRAFT — purge note + illustrative post-purge body for the live
     .harmonik/crew/missions/kynes.md (startup-doc revamp Stage 2 companion, per
     02-cutover §0.3 / step 2.3 + 00-SYNTHESIS §6 "State tier" table, "missions/kynes.md"
     row). Missions are crew-owned, gitignored, live state — this file is NOT a drop-in
     replacement to deploy verbatim; it is the purge SPEC plus a worked example so the
     principle ("missions become overwrite-only: goal rewritten on re-task, exactly ONE
     Current State block") is unambiguous the next time captain re-tasks kynes. Apply the
     PRINCIPLE, not this exact text, at re-task time — the live file has moved on since
     this draft was written (epic hk-hcrvb closed 2026-07-11 ~03:10Z; kynes now on
     hk-8ziid.2 per the current fleet handoff) and the goal block below is illustrative,
     not current. -->

# missions/kynes.md — purge spec (what to remove, and why)

## What's wrong in the live file (verified against .harmonik/crew/missions/kynes.md)

1. **`goal` is append-only, not overwrite-only.** The live block stacks a
   `═══ RE-TASK 2026-07-11 ~01:25Z … GATE-0 ═══` directive (~30 lines) UNDERNEATH the
   original TRACK-A-ONLY goal text instead of replacing it. GATE-0 itself is now DEAD:
   direction-log's 07-11 ~01:54Z entry recorded the GATE-0 verdict (`--no-extensions`
   REFUTED as the pi fix) and the flagship epic hk-hcrvb CLOSED 07-11 ~03:10Z — the
   entire goal block (both the original TRACK-A text and the GATE-0 re-task) describes
   finished, superseded work. **Fix:** on next re-task, `goal` is REPLACED wholesale
   with the new assignment. Never append a new directive under a `═══ RE-TASK … ═══`
   marker inside `goal` — that marker pattern is itself the anti-pattern.
2. **Three stacked `## Prior: … — SUPERSEDED` Current-State blocks** (live L70–97):
   "PRODUCTION CANARY GREEN", "captain go/no-go delivered", "Prior State
   (keeper-restart resume)". Each is explicitly self-labeled SUPERSEDED and each is
   consumed by the CLOSED epic. Git already holds this history (every state transition
   was also posted to comms + `br comments`, per the crew contract) — the mission file
   is not the archive. **Fix:** delete all three; keep exactly ONE `## Current State`
   block, replaced in place at every status change (not appended below the old one).

## The rule (goes in `.harmonik/agents/crew/operating.md` Bounds, per 00-SYNTHESIS §6
"State tier" — this draft only documents the mission-file consequence)

- `goal`: captain OVERWRITES on every re-task. No `═══ RE-TASK … ═══` markers, no
  layering old goals under new ones. The current goal is the only goal.
- `## Current State`: crew REPLACES this single section in place on every update.
  Exactly one heading, exactly one block, always. Prior states are not retained in the
  file — `br comments list <epic_id>` and `comms log --from kynes --topic status` are
  the durable history a captain re-derives from if needed.

## Worked example — what a compliant file looks like

```markdown
schema_version: 1
crew_name: kynes
queue: kynes-q
epic_id: <current epic, overwritten on re-task — do NOT carry hk-hcrvb after close>
captain_name: captain
goal: |
  <the CURRENT assignment only. No stacked RE-TASK markers, no history of prior goals.>

## Current State (<timestamp> — <one-line status>)
<current state only. Replaced in place on every update, not appended below.>
```

Applying this to kynes concretely: the moment hk-hcrvb closed (07-11 ~03:10Z) and
captain re-tasked kynes onto the next lane, `goal` should have been replaced entirely
(no TRACK-A / GATE-0 residue) and `## Current State` reduced to one block describing
the new assignment — not three "Prior: … SUPERSEDED" blocks left standing under it.

Banner removed on deploy (there is nothing to "deploy" here — this file documents the
purge principle; the live mission file is corrected the next time captain re-tasks or
refreshes kynes per SHUTDOWN.md §5b, at which point it is overwritten per the rule
above, not hand-edited from this draft).
