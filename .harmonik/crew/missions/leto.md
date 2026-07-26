---
schema_version: 1
crew_name: leto
queue: leto-q
epic_id: hk-j5yer
goal: Execute the captain-startup-revamp — drive cutover Steps 0->5 to the manifest-boot model, coherent at every commit, with the digest-parity hard gate held before the Step-4 tombstone.
captain_name: captain
---

# leto — captain-startup-revamp cutover lane

You own epic **hk-j5yer**. This is OPERATOR-DIRECTED PURE EXECUTION (2026-07-11): all 9
open questions are already resolved in `03-operator-decisions.md`, which OVERRIDES the drafts.
Do NOT re-open them. This replaces last night's stalled build.

## Read first (authoritative, in order)
1. `plans/2026-07-11-captain-startup-revamp/02-cutover-and-open-questions.md` — the execution spec, Steps 0->5.
2. `plans/2026-07-11-captain-startup-revamp/03-operator-decisions.md` — resolves/OVERRIDES all §4 open questions.
3. `plans/2026-07-11-captain-startup-revamp/00-SYNTHESIS.md` + `drafts/` — context + the draft set you edit.

## The three buckets
- **Bucket 1 — ~27 draft text edits [A]** across 6 drafts: `agents/captain/operating.md`, `captain/SKILL.md`,
  `context/captain-lanes.md`, `crew-launch/SKILL.md`, `orchestrator-rules/SKILL.md`, `agents/_skills/SYNC.md`.
  FOUR LOAD-BEARING reversals (the drafts contradict operator rulings):
  1. Crew-collision auto-retry -> REVERSE to NO-retry (collision = lane already staffed; fail fast + loud, per doc-03 Q3).
  2. Daemon-down escalation -> OPERATOR, not admiral (comms is a daemon RPC; admiral is unreachable when the daemon is down).
  3. Restore beads write-discipline pointer + ALL FOUR retrieved refs (add beads-cli + harmonik-dispatch) in operating.md.
  4. Restore the placeholder-purge half of the fail-closed hooks gate in captain-lanes.md.
- **Bucket 2 — ~14 companion docs** (Step 0.3): AGENT_INDEX, STATUS (keep the `#decisions-locked-in-2026-04-19`
  anchor), HANDOFF, AGENTS.template, project.yaml, harmonik-dispatch additions, crew-handoff-schema,
  crew operating.md Bounds, direction-log compaction, admiral-initiatives trim, missions/kynes purge,
  context/CLAUDE retention, SHUTDOWN re-point, HANDOFF-captain tombstone. Several have draft stubs — verify/finalize, don't recreate.
- **Bucket 3 — 5 code beads** (file these to your OWN queue `leto-q`, normal queue + pre-deploy e2e + docs/daemon-redeploy.md):
  (a) brief-renderer retrieved-refs [follow-up, do NOT block cutover]; (b) handoff CLAIM header;
  (c) shared renderReviewerConstraint(); (d) **harmonik digest parity — THE HARD GATE** (add comms-who + crew-list
  + tmux-fleet + paused-queue sweep + ready-beads + kerf-next/map, fix br_ready collector exit-2); (e) agents-skills-sync.sh
  (needs SYNC.md [A] fixes done first).

## Governing OVERRIDE — PRINCIPLES, NOT RULES (doc 03)
Operating docs must LEAD with principles; restored rules get RECAST as guardrails-under-a-principle, not
re-injected as checklists. Any closed-category list/checklist is a smell. This makes Bucket-1 judgment-heavy.
HARD-RULE tag set = the 9 from doc-03 Q9 (queue-not-daemon, 3 sub-agent exceptions, daemon-owns-beads,
review-every-batch, never-cd-worktree, no-deploy-without-e2e, fan-out-on-recurring-failure, STREAM-NOT-WAVES,
scratch-lane discipline). Q1 escalation is a PRINCIPLE (adopt-then-verify; raise only genuine operator-stakes).

## Sequencing (LOAD-BEARING — coherence gate)
Step 0 (draft edits + companion docs + file code beads) is all NON-LIVE prep; parallelize it freely via `leto-q`.
Then the LIVE landings are tight, coherent commits: **never land a half-old/half-new doc set** (git add specific
paths, never -A; strip DRAFT banners at landing). Step 1 = additive re-homing (destinations) before any cut.
Step 2 = captain-side swap under **keeper hold**. Step 3 = crew-side flip (canary ONE crew, no force-restart).
Step 4 = router + tombstone = POINT OF NO RETURN, HARD-GATED on Step 2 landed AND bead (d) digest-parity DEPLOYED.
Step 5 = enforcement + soak.

**BEFORE any Step-2 captain-side landing OR the Step-4 tombstone: comms `--to captain --topic status` and WAIT for
my go.** Those two are the irreversible/keeper-hold points and I serialize them at the fleet level. Everything at
or below Step 1 you land yourself as reversible small commits.

## Deferred (file as backlog beads under the epic, NON-blocking)
- Q6: broad 49-stash triage (the ONE orphaned settings.json stash is already delegated to stilgar separately).
- Q7: research whether watch/watchdog/flywheel earn their keep (research bead, not a lanes.json durability row).

## Standing rules
Follow crew-launch/SKILL.md boot + operating loop. Dispatch ONLY to `leto-q`, never `main`. Post status on
bead-close + a <=10-min timer while dispatching. Never pre-set in_progress (daemon owns terminal transitions).
Surface — do not decide — a crew-failure/kill, a genuinely-new operator decision, a locked-decision reversal, or
any destructive/redeploy op. Model: Opus (judgment-heavy recast work).
