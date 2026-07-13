<!-- TIER: 2 (operational state, sequencing intent across direction changes)
     LOADED BY: admiral + captain @ boot, AFTER tier-3 (project.yaml) + tier-2 (captain-lanes.md), BEFORE acting.
     OWNER: admiral. APPEND-ONLY. ONE entry per direction CHANGE (never a status update, never per-tick, never by crews).
     Newest-first. ~3-5 lines/entry. Capped ~10 entries / ~60 lines; delete oldest on overflow (no archive).
     Four load-bearing fields per entry: WHAT / WHY / RETURN-PATH(sequence) / expires:.
     ON EXPIRY the DEFAULT is LAPSE -> revert to the standing autonomous posture, NEVER a hold.
     The admiral audit OWNS flagging an expired-but-present entry: re-confirm with the operator or strike it.
     See .harmonik/context/AGENTS.md for the full forced-write/forced-read discipline. -->

# Direction log — temporal sequencing intent across direction changes

> The one thing no other doc holds: WHY we paused X for Y and IN WHAT ORDER we resume.
> Pre-freeze sequencing history (14 superseded entries: 5-lane priority, remote worktree,
> pi redeploy, codex option-B, QA-gate, v0.5.0 cut) is preserved in git history + the
> snapshot at .harmonik/archive/2026-07-12-freeze-and-carve/. Struck 2026-07-12 by the
> admiral audit under the retention/anti-rot rule — all superseded by the pivot below.

## 2026-07-12 ~23:35Z — operator+admiral: REFRAME — the goal is a generative SYSTEM, not the tool; carve is downstream · expires: 2026-07-14T00:00:00Z
WHAT: Operator confirmed ("Yes") a reframe of the whole project. We are not building a tool; we are
      building a complex ADAPTIVE SYSTEM (principles + a self-pruning metabolism + agents that reason
      from principles) that BUILDS the tool as the encoding of the system's own attributes. Three hard
      problems named: (1) principles not structure — kerf gives a track, no compass (a 2300-line fn
      lived the whole life, no agent ever flagged it); (2) alignment across ~1000 varied sessions is
      EMERGENT like ants/flocks — shared principles + signals, a gradient not a rail; (3) stay stable
      AND improve WITHOUT accreting — promote only what generalizes, prune/fold by default, cap the
      count, selection not authorship. Recursion: the builder must run on the same principles it encodes.
WHY:  the treadmill is not a code problem, it's a BUILDER problem — the process that built harmonik
      carries no principles, so it produced (and could not reject) an unmaintainable system. Fixing the
      artifact without fixing the generative system just regenerates slop.
ORDER: freeze still HOLDS; nothing dispatches. The real STEP-0 is now the CHARTER (the few load-bearing
      principles + the metabolism that keeps them few/live/self-pruning). The freeze-and-carve carve
      (STEP-0/M1–M4, plans/2026-07-12-codebase-census/) is DOWNSTREAM — the first thing the system
      produces and proves itself on, not the north star. PLAN.md v2's "Acceptance Oracle" (Q1) is
      SUPERSEDED (wrong question: "is a fix real"); its residue folds into principle-space.
RETURN-PATH: capture DONE (plans/2026-07-12-generative-system/CAPTURE.md). NEXT: operator+admiral talk
      next steps, then draft the charter. Do NOT re-stand any lane; do NOT dispatch the carve until the
      charter exists and the operator lifts the freeze.

## 2026-07-12 ~20:5xZ — operator+admiral: PLAN-FIRST resolved → clean slate executed; PLAN.md v2 awaiting ratification · expires: 2026-07-14T00:00:00Z
WHAT: the 18:00Z pivot's a/b/c offer is RESOLVED — operator chose PLAN-FIRST + a clean slate.
      Executed: fleet torn down (all crews down, run worktrees reaped, 267 beads closed, ~2GB history
      cleared; captain+admiral up). Admiral authored + independently review-hardened the full carve
      program: plans/2026-07-12-codebase-census/PLAN.md v2 (STEP-0 → M1 → M2 → M3 → M4, with the
      Acceptance Oracle standard-of-proof). Initiative/lane trackers archived + cleared to the
      frozen state.
WHY:  no dispatch until the plan is ratified — the daemon run pipeline itself is untrustworthy
      (resume-hang + false-close), so STEP-0 must repair it OUT-OF-PIPELINE before any carve work flows.
ORDER: everything stays FROZEN. On operator ratification: settle PLAN Q1 (the Acceptance Oracle) →
      captain re-stands FRESH agents, STEP-0 first (resume-hang + noChange false-close + honest-probe
      re-land, all out-of-pipeline) → M1 (delete test-theater) concurrent → M2/M3/M4 kerf-first.
RETURN-PATH: AWAITING operator ratification of PLAN.md v2 (7 open questions; Q1 = standard of proof is
      the crux). Do NOT re-stand any pre-freeze lane; the carve program is the single front line.

## 2026-07-12 ~18:00Z — operator (via admiral): STRATEGIC PIVOT → FREEZE-AND-CARVE; fleet QUIESCED · expires: 2026-07-14T00:00:00Z
WHAT: Operator voiced a deep architectural concern ("everything keeps breaking, remote poorly
      architected, don't know what's real, can't build the system with itself, so much slop,
      mutexes=bugs") → authorized a codebase CENSUS (10 Fable assessors + 10 adversarial challengers
      + synthesis; every verdict UPHELD). Report: plans/2026-07-12-codebase-census/REPORT.md.
WHY:  the treadmill (43% of 20-day commits are hardening; 80% land in the 55k-LOC daemon god-package)
      is architectural, not review-debt. Domain logic is SOUND; the two ack-free IO boundaries (tmux
      paste-inject + remote SSH) + the god-package + no-single-writer are the root. Verdict: KEEP the
      proven core (queue model, lifecycle sweeps, harness axes, ~466 regression tests), REBUILD
      daemon-workloop core + remote + tmux-input, SIMPLIFY the rest, DELETE ~50k LOC of test-theater.
RETURN-PATH: superseded by the ~20:5xZ entry above (plan-first + clean slate executed). Retained here
      as the root direction change of record.
