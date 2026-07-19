<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers
     OWNER: captain, updated at session end (before HANDOFF.md) or on any crew/epic change
     DO NOT PUT HERE: standing behavioral rules (→ orchestrator-rules skill);
                      this-session salvage / run-id play-by-play (→ HANDOFF.md tier-1);
                      durable phase/locked decisions (→ project.yaml tier-3) -->

# Tier-2 context: captain lane registry + medium-term tracker (days cadence)
# Captain reads on every boot (STARTUP.md Step 0b) BEFORE re-deriving lanes.
# Keep this SHORT — one current-truth block. Superseded history is DELETED, not archived here.
# Pre-freeze lane history: .harmonik/archive/2026-07-12-freeze-and-carve/ (not boot-read).

## ⭐ CURRENT TRUTH 2026-07-12 — FLEET TORN DOWN · EXECUTION FROZEN · PLAN-FIRST.

> **No lanes are staffed.** The operator ordered a freeze-and-carve clean slate
> (direction-log ~18:00Z). All worker + oversight crews are stopped, run worktrees
> reaped, queues idle. Only **captain + admiral** sessions remain up. **Nothing
> dispatches** until the operator ratifies the plan and lifts the freeze.

**Operative direction:** `.harmonik/context/direction-log.md` (the ~18:00Z STRATEGIC
PIVOT entry) is authoritative. The execution program is
`plans/2026-07-12-codebase-census/PLAN.md` (freeze-and-carve; STEP-0 → M1 → M2 → M3 → M4),
review-hardened v2, **awaiting operator ratification.**

### Lanes
_None active._ The pre-freeze 5/6-lane fleet (kynes/hawat/piter/stilgar/yueh/leto) is
deliberately torn down. Do NOT re-stand any lane until the plan is ratified.

**On ratification**, the first work is STEP-0 (resume-hang + noChange false-close +
honest-probe re-land), which runs **OUT-OF-PIPELINE** (direct agent + human-reviewed
merge), followed by M1 (delete test-theater) concurrently. See PLAN.md for per-move
scope, DoD (the Acceptance Oracle), and the in-pipeline-vs-out-of-pipeline call.

### Carried-forward defects (must survive into the carve; parked, not lost)
- **Resume-hang / QA-execution-gate** — implementer relaunch-on-gate-fail hangs silently
  (~5/5 recent runs). PLAN STEP-0a. Correlates with the QA-execution-gate (~0adb6551).
- **noChange-subsumption false-close** — the daemon closed `hk-2hfyt` on a bead-ID
  MENTION in an unrelated docs commit (32dc13f7), fix ABSENT. PLAN STEP-0b. Do NOT trust
  that closed status.
- **honest-probe still live** — the gb-mbp fleet-down probe bug behind false-closed
  `hk-2hfyt`; `createworktree.go` has only a partial HEAD probe. PLAN STEP-0c
  (re-land under a clean bead ID; gb-mbp stays DISABLED until it lands + re-validates).

### Open operator decisions
- **PLAN.md ratification** — 7 open questions in the plan; **Q1 is the crux: the
  Acceptance Oracle / standard of proof** for "a fix is real." Everything waits on this.
