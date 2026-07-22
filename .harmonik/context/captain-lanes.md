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

## CURRENT TRUTH 2026-07-22 — FLEET DISPATCHING (inline mode), RELEASE/REBUILD IN PROGRESS.

> The 2026-07-12 freeze was LIFTED long ago; the fleet has dispatched for ~10 days since.
> This block replaces the superseded freeze text (pre-freeze history archived at
> `.harmonik/archive/2026-07-12-freeze-and-carve/`, not boot-read). 7 sessions up and
> working: captain + admiral (oversight) + crews india, juliet, kilo, lima, mike, assessor.
> INLINE MODE — crews implement in in-crew subagents; daemon queue-dispatch is OFF until
> the tmux-input fix hk-9hvr0 deploys. A major release is mid-flight: 31 landed commits are
> NOT yet running — the prod daemon is stuck on eb2b4f1a (since 09:03Z 2026-07-22); a gated
> redeploy (daemon-20260722-02) is being prepared: admiral gates via the assessor, captain
> drives, no swap until admiral clears.

### Lanes (live)
- codex-first (hk-tckw3, india) — Step-2 GO/NO-GO gated on the rebuild.
- flake initiative (hk-f8o5u, juliet) — both arms discharged; root cause = leaked
  test-daemon cache-wipe artifact; hk-gjbpp is the fix; clean re-measure post-rebuild.
- sandbox (hk-scaj0, lima) — hk-guapd/dqo9u/bzydx/quoka/s13ee landed; hk-rhhig/mp37h/155gs remain.
- process-group-provenance (kilo) — kerf pass-5 spec-draft in review.
- daemon-reliability + the release batch — the rebuild is the culmination.

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
