<!-- DRAFT — proposed replacement for .claude/skills/captain/STARTUP.md (startup-doc revamp, Stage 3).
     Do NOT apply until the PRECONDITION holds (01-revamp-process.md Stage 3 / 00-SYNTHESIS.md §6 keep-list):
     every rule this file used to carry must already be live at its new home —
     (1) LEAN keeper-restart resume, identity/two-captains collision guard, direction-log LAPSE rule,
         single-pass digest rule, and the two-watcher rule in .harmonik/agents/captain/operating.md.
         VERIFY BLOCKER: the LIVE operating.md today still arms `subscribe --types
         epic_completed,run_failed,run_stale,heartbeat` (line 13) — the exact run-telemetry
         subscribe this file forbids — and lacks the identity guard / direction-log / LEAN-resume
         sections. The drafts/agents/captain/operating.md replacement must land FIRST.
     (2) crew classification table, staffing discipline (lazy-boot PARKED gate, 5a-5d, 2-min stagger,
         re-task-not-restart), the crew liveness sweep + clear-and-retype recovery, healthy-fleet
         checklist, idle-crew pane-nudge, and the 3 surviving anti-patterns in .claude/skills/captain/SKILL.md.
     (3) DIGEST PARITY: `harmonik digest` must carry the fleet ground-truth sections
         captain-boot-digest.sh emits today — comms who, crew list, tmux fleet, PAUSED-QUEUE sweep
         (healthy-fleet criterion 6 false-green guard), ready beads, kerf next/map — and its br_ready
         collector must not error, BEFORE the script is called retired. As of 2026-07-10 the native
         verb emits only queue/commits/in-progress-beads/notes/events: zombie detection and the
         paused-by-failure sweep would have NO home at boot.
     (4) A named home for four rules currently dispositioned NOWHERE (not in the 00-SYNTHESIS
         keep/cut table, absent from all drafts):
         - the ≤5-min REFRESH-AND-STAFF backlog pull between events + the FAILED-monitoring
           condition (c) "ready beads + free slot + not staffed = MISSED STAFFING FAILURE"
           (suggested: orchestrator-rules §ANTI-IDLE gains the cadence sentence);
         - the CE4 ops-monitor checks map (read `.checks` from .harmonik/ops-monitor/latest.json;
           per-flag judgment actions incl. review-gate → surface `.review_bypass_run_ids`; the
           "latest.json missing or ts >15m stale ⇒ monitor down, surface" tripwire — this tripwire
           is what makes the "ops-monitor owns the review-bypass check" CUT safe);
         - keystone-gated bead marking (a bead depending on an open epic silently insta-fails at
           dispatch: group_failure, no run_started — mark BLOCKED, do not dispatch; suggested:
           captain SKILL.md §Staffing discipline);
         - the once-per-restart goal-state.json re-ground + §4.4 idle-triggered realign
           (3 idle conditions + 3 guards; no per-turn injection — operator-locked), or an explicit
           operator-approved CUT.
     (5) Tier-2 end-of-session update discipline (update captain-lanes.md + append any
         direction-log entry BEFORE writing the handoff) confirmed present in SHUTDOWN.md.
     Re-point SHUTDOWN.md and cmd/harmonik/assets/skills/captain/ cross-references in the same change. -->

**SUPERSEDED as a boot document — boot = `harmonik agent brief --wake fresh|keeper-restart|trigger:<id>`; that output is your complete boot context.**

Do not boot from this file. What it used to carry now lives at:

- **Boot flow itself** — identity guard, tier reads + direction-log LAPSE rule, single-pass `harmonik digest`, LEAN keeper-restart variant, the two-watcher rule: `.harmonik/agents/captain/operating.md` (injected into your `agent brief` output — you never read it separately)
- **Captain mechanics** — spawn, mission handoffs, re-task, attribution, error-edge table, crew liveness sweep, staffing discipline, healthy-fleet checklist: `.claude/skills/captain/SKILL.md`
- **Standing rules** — dispatch, priority, autonomy, anti-idle/backlog-pull, review gate: the `orchestrator-rules` skill (`.claude/skills/orchestrator-rules/SKILL.md`)
- **Keeper** — context-fill watcher, restart cycle: the `keeper` skill (`.claude/skills/keeper/SKILL.md`)
- **Daemon down / redeploy** — exit-17 recovery, supervisor, in-place binary swap: `docs/daemon-redeploy.md`
- **Park / wake** — fleet idle-down and wake procedure: `specs/park-resume-protocol.md` §3.3 and §4.1
