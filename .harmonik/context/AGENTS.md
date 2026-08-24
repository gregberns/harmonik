# .harmonik/context — operating directives for the artifacts in THIS folder

> CLAUDE.md is a symlink to this file. Same content. If you are an admiral or captain
> session and you are reading, editing, or reasoning about any file in this folder,
> these directives apply. They do NOT restate contracts — they point you at the
> canonical rule and tell you how to keep these specific artifacts honest.

## The artifacts here, and what each is for
- `project.yaml` (tier-3) — phase, locked decisions, guardrails.
- `captain-lanes.md` (tier-2) — current lanes + epics-in-progress + parked + the DATED
  OPERATOR-DIRECTIVES block (priority ordering).
- `lanes.json` — MACHINE-READABLE lane→epic index the ops-monitor reads (lane, epic_id,
  status, gate). Keep it in sync with the prose docs in the SAME action you change a lane.
  **Gate `expires` MUST be full RFC3339 (`2026-07-09T00:00:00Z`), not date-only** — the
  ops-monitor's `fromdateiso8601` parse needs the time component (a date-only value is
  treated as expired and would let a still-gated lane fire the stall wake).
- `admiral-initiatives.md` — the big-rocks registry (status snapshot).
- `direction-log.md` (tier-2) — APPEND-ONLY sequencing intent: one entry per direction
  CHANGE (WHAT / WHY / RETURN-PATH / expires). The file a fresh /clear reads to recover
  "why we paused X for Y and in what order we resume."

## Boot-read order (admiral + captain)
After tier-3 (project.yaml) and tier-2 (captain-lanes.md), READ direction-log.md before
acting. It is short by design. Its RETURN-PATH is ground truth for sequencing intent.

## KNOWN vs brand-new — DO NOT re-decide it here
Canonical in the orchestrator-rules skill, `REFERENCE.md` §Autonomy. The consequence for THIS folder: a lane
named in any durable doc here is KNOWN, so resuming, un-parking, or re-staffing it is
autonomous even when it shows zero ready beads right now. "Parked" only ever means "zero
ready beads now" — there is no PARKED-known tag to set, and a lane is GATED only when
`lanes.json` carries a non-null, unexpired `gate` object.

## Forced WRITE
- Whenever you ISSUE or RELAY a direction change, you MUST append a direction-log.md
  entry in the same action, with an `expires:`. A directive block with no matching log
  entry is a FINDING the next audit must raise.
- A dated operator-directive in captain-lanes.md MUST carry `expires:` and an owner.
- Whenever you ADD / RETASK / PARK a lane, update lanes.json in the same action.

## Forced READ + freshness (anti-rot)
- Every dated directive AND every direction-log entry has `expires:`. ON EXPIRY the
  DEFAULT is LAPSE → revert to the standing autonomous posture — NEVER to a hold.
- The admiral audit OWNS flagging expired-but-present directives/log-entries/gates and
  either re-confirming with the operator or striking them.

## Retention
direction-log.md is capped ~10 entries / ~60 lines, newest-first. Delete the oldest on
overflow. No archive.

## Keep the priority story in one place
The rule — stated intent first, then the ledger — is canonical in the `orchestrator-rules`
skill §Priority. This folder holds a SNAPSHOT of the resulting order, in the dated directive
block in `captain-lanes.md` and the direction-log RETURN-PATH. **Do not write a standing
priority list here.** Another one means two documents disagree about what comes next and the
next reader cannot tell which is current. Record the intent in the dated block and the
direction log, and let the ledger supply the rest.

`direction-log.md` takes direction CHANGES only. Status updates and per-tick notes drown
the signal a fresh session comes here to find. Crews do not write in this folder.
