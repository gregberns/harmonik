<!-- DRAFT — proposed replacement for .harmonik/context/AGENTS.md (CLAUDE.md is a symlink
     to it; one file, two names). Startup-doc revamp, cutover Step 0.3 companion; principles
     pass per 03-operator-decisions.md. Deploys together with the captain-lanes.md compaction,
     direction-log.md compaction, and admiral-initiatives.md trim — the retention rules below
     describe the compacted files, not today's append-only ones. Banner removed on deploy. -->

# .harmonik/context — operating directives for the artifacts in THIS folder

> CLAUDE.md is a symlink to this file. Same content. If you are an admiral or captain
> session and you are reading, editing, or reasoning about any file in this folder,
> these directives apply. They do NOT restate contracts — they point you at the
> canonical rule and tell you how to keep these specific artifacts honest.

## The principle: snapshots, not journals

Every file here is a bounded SNAPSHOT of current truth, read as DATA at the manifest
wake step — never a boot ritual, never an archive. Git is the archive: supersession is
DELETION, not a "SUPERSEDED" annotation stacked on top. And documents only CLAIM —
`harmonik digest` is live ground truth and overrides every claim in every file here.
All the rules below are guardrails keeping that principle true.

## The artifacts here, and what each is for
- `project.yaml` (tier-3) — phase, direction/tooling status, durable guardrails,
  locked decisions. Weeks cadence.
- `lanes.json` — the AUTHORITATIVE lane registry (lane, epic_id, status, gate),
  machine-readable; the ops-monitor reads it. Update lanes.json FIRST, prose second,
  in the SAME action you change a lane. **Gate `expires` MUST be full RFC3339
  (`2026-07-09T00:00:00Z`), not date-only** — the ops-monitor's `fromdateiso8601`
  parse needs the time component (a date-only value is treated as expired and would
  let a still-gated lane fire the stall wake).
- `captain-lanes.md` (tier-2) — thin prose snapshot over lanes.json: ONE current-truth
  block (lanes + epics-in-progress + parked + dated items). It does NOT hold the
  operator priority order — lanes.json + admiral-initiatives.md own that.
- `admiral-initiatives.md` — the big-rocks registry (status snapshot, one line per
  initiative); with lanes.json, the home of the operator priority order.
- `direction-log.md` (tier-2) — sequencing intent, NEWEST-FIRST: one entry per direction
  CHANGE (WHAT / WHY / ORDER / RETURN-PATH / expires, 3–5 lines); superseded/consumed
  entries are DELETED, never annotated. The file a fresh wake reads to recover "why we
  paused X for Y and in what order we resume."

## How these files are read (manifest boot — no reading-order ritual)

`harmonik agent brief` IS the boot; there is no doc chain to walk. The captain's wake
step reads this folder as data — project.yaml → lanes.json + captain-lanes.md →
direction-log.md — then ONE `harmonik digest` for ground truth. direction-log's
RETURN-PATH is ground truth for sequencing intent. Freshness tiebreak: every state doc
carries `updated:` (RFC3339); on cross-doc conflict the NEWEST stamp wins; the digest
beats all documents.

## KNOWN vs brand-new — DO NOT re-decide it here
The canonical definition lives in the orchestrator-rules skill (§Autonomy). In one line:
a lane that appears in ANY durable doc in this folder (or any past kerf-next ranking) is
KNOWN — resuming/un-parking/re-staffing it is AUTONOMOUS, NOT an operator escalation,
EVEN IF it is parked or shows zero ready beads in the live feed right now. Only a
NEVER-ranked body of work is the operator's to rank. A lane is GATED only if a NAMED,
DATED, OWNED, EXPIRING gate is present (in lanes.json: a non-null, unexpired `gate`
object); absence of a live named gate means KNOWN/resumable. There is no PARKED-known
tag to set — "parked" just means "zero ready beads now."

## Forced WRITE
- Whenever you ISSUE or RELAY a direction change, you MUST append a direction-log.md
  entry in the same action, with an `expires:`. A directive block with no matching log
  entry is a FINDING the next audit must raise.
- A dated item in captain-lanes.md MUST carry `expires:` and an owner.
- Whenever you ADD / RETASK / PARK a lane, update lanes.json in the same action
  (lanes.json first, prose second).
- Commit tier-2 state immediately: stage the SPECIFIC path (never `git add -A`) and
  commit in the same action as the edit — uncommitted, a replace-in-place rewrite is
  the only copy of current truth. Canon: orchestrator-rules §CWD/commit discipline.

## Forced READ + freshness (anti-rot)
- Every dated item AND every direction-log entry has `expires:`. ON EXPIRY the
  DEFAULT is LAPSE → revert to the standing autonomous posture — NEVER to a hold.
- The admiral audit OWNS flagging expired-but-present directives/log-entries/gates and
  either re-confirming with the operator or striking them.

## Retention — the caps that keep "snapshot" true
- `captain-lanes.md`: ONE CURRENT TRUTH block, hard cap 60 lines, REPLACE IN PLACE —
  an update DELETES what it supersedes; "SUPERSEDED"/"LIFTED"/history prose is BANNED.
- `direction-log.md`: capped ~10 entries / ~60 lines, NEWEST-FIRST. Strike expired
  entries per the LAPSE rule; delete the oldest on overflow. No archive file.
- `admiral-initiatives.md`: ≤50 lines, one line per initiative. No stale tables, no
  audit-marker journals — audits report over comms/handoff, not here.
- `project.yaml`: weeks-cadence facts only; anything faster-moving goes to tier-2.

## Don't
- Don't add a 4th priority list here. lanes.json + admiral-initiatives.md own the
  operator priority order; `kerf next` ranks the unclaimed backlog below it; dated
  directives are time-boxed biases that expire; captain-lanes prose only snapshots.
- Don't write status updates or per-tick notes into direction-log.md. Direction CHANGES
  only. Crews never write here.
