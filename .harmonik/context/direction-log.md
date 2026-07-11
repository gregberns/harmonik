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

## 2026-07-11 ~01:25Z — operator (via admiral, event 019f4ec1): pi redeploy DECOUPLED from gate beads → GATE-0 e2e is the sole gate · expires: 2026-07-13T00:00:00Z
WHAT: The pi/flagship redeploy is NO LONGER gated on hk-x2spu + hk-ih5k6. Those become a
      PARALLEL quality track (stilgar). The SOLE operator-mandated redeploy gate is now a single
      ISOLATED GATE-0 E2E TEST: repro the pi in-daemon hang on OLD binary d7abf34a, prove GREEN
      with afa32372 — the test doubles as root-cause confirmation. Owner: kynes (re-tasked off
      the "stand by for redeploy" idle-wait). On GATE-0 green, captain redeploys on own authority.
WHY:  operator found the afa32372 fix is UNIT-tested only (argv-carries-flag) + a root-cause
      coherence gap — the flag targets a flywheel-extension fork-bomb whose tree was ALREADY
      deleted (353fc3c1). Captain CONFIRMED the gap: no .pi/extensions tracked on main, none in
      home, none in a live worktree, and d7abf34a POSTDATES the deletion — so the old theory is
      suspect. An empirical GATE-0 is required before trusting the redeploy.
ORDER: kynes builds GATE-0 (flagship critical path) → green → captain redeploys → kynes re-runs
      pi seed → hk-hcrvb complete. hk-x2spu/hk-ih5k6 run in parallel, no longer flagship blockers;
      their LIVE fail-closed activation still needs main-lint-debt cleanup (new bead, stilgar lane).
RETURN-PATH: check kynes owns GATE-0 + reports {e2e result, extensions-present y/n}; stilgar's
      two beads move as an independent quality track. Redeploy gate = GATE-0 green ONLY.

## 2026-07-11 ~00:40Z — admiral (via captain, events 019f4e97/019f4e9d): WATCH RESTORED + hk-vdqe2 staffed · expires: 2026-07-13T00:00:00Z
WHAT: (1) Restored the always-on watch triage session (crew watch, spawned 00:39Z) — KNOWN
      staffing gap, admiral's autonomous call: watch-down >43h dumped every stall/release/
      paused-queue escalation straight on the captain. (2) Staffed NEW P1 hk-vdqe2 (keeper
      clear->brief race, cycle.go completeCycleTail) onto stilgar's lane.
WHY:  restore escalation-filtering so the captain isn't triaging ops noise inline while
      driving the flagship; hk-vdqe2 is operator-reported, seen repeatedly.
ORDER: hk-vdqe2 sequences AFTER the two redeploy-gate beads (hk-x2spu + hk-ih5k6 = critical
      path) and AHEAD of rfhaw/rg526. HARD MERGE GATE (operator, non-negotiable): rigorous
      ACTUAL e2e twin/scenario-harness repro of the slow-handoff/busy-pane race + a PERMANENT
      regression test that fails on current code / passes after fix — NOT unit tests alone.
RETURN-PATH: confirm watch online in `comms who`; check stilgar picks up hk-vdqe2 after the
      gate beads. 2-lane split unchanged (kynes paused/preserved + stilgar gate-clear+vdqe2).
> This is what a fresh /clear destroys. Read the newest RETURN-PATH as ground truth for sequencing.

## 2026-07-09 ~02:33Z — admiral (relaying operator: v0.5.0 SHIPPED → fleet QUIESCED) · expires: 2026-07-11T00:00:00Z
WHAT: v0.5.0 CUT + PUBLISHED (annotated tag v0.5.0 → e59beef8, GitHub release live; dispatch
      consent-modal hang fixed via #26+#29, live daemon redeployed to e59beef8 / tag
      daemon-20260708-01). Operator then AUTHORIZED a wide post-release teardown → fleet
      DELIBERATELY QUIESCED: dispatch PAUSED, 11 crew/watch sessions stood down (captain +
      admiral + ops-monitor kept), main cleaned (worktrees 14→2, branches 1882→547 with WIP
      preserved), and the release-infra WIP preserved on origin/release-infra (cb332c06).
WHY:  clean pause point after the release; operator wanted the deck cleared before the next initiative.
ORDER: fleet STAYS quiesced until the operator gives next-initiative direction. Re-stand with
      `harmonik supervise resume` + re-staff crews per Step 5. NO lanes should be running now —
      idle is the INTENDED end-state, NOT a stall (do not self-staff off a stale lane doc).
      NOTE: ops-monitor watch-down / paused-queue alerts are EXPECTED teardown artifacts, not incidents.
RETURN-PATH: resume by asking the operator "what's next after v0.5.0?" — the next initiative is
      theirs to rank (nothing ranked is being worked). The 07-08 Pi re-scope entry below stays
      live (expires 07-11) as the leading KNOWN candidate for the re-stand.

## 2026-07-08 ~00:20Z — operator (via admiral) · expires: 2026-07-11T00:00:00Z
WHAT: Pi provider RE-SCOPED — not "flip to MiniMax to unblock." Goal = pi works COMPLETELY
      through BOTH OpenRouter AND the DGX/ornith provider, with model/provider selectable
      PER-BEAD (switch between the two on a per-bead basis). If per-bead switching isn't
      supported today, IMPLEMENT it (kerf only if it grows a real contract). Extends chani
      epic hk-fdbhf + the operator pi-provider-switch kerf (2026-07-05). PLUS: the four
      known bug classes become PROVABLE quality-harness coverage (enforcement-first, not
      one-off patches): (a) pi model reaches harness with working tool_calls per provider,
      (b) no Claude-tier-3 leak into pi runs (hk-pkugu), (c) codex empty-model no-hang,
      (d) ST5 merge-race greens (hk-psrnc). Owned jointly chani (pi) + stilgar (harness).
WHY:  operator wants pi validated on BOTH substrates + switchable, not a single-provider
      shortcut; and wants the recurring bug classes gated by the harness so a regression
      re-breaks the gate, not production. This is the enforcement-first thesis applied to pi.
ORDER: chani drives dual-provider + per-bead capability ‖ chani+stilgar add the 4 corpus
      cases; gurney (Workstream A fail-closed gates) continues in parallel; PR #20 → green.
      STATUS NOTE (admiral 07-09): hk-fdbhf core-goal PROVEN in-daemon + CLOSED; the
      per-bead-switch + 4-corpus-harness scope is the live remainder for the re-stand.
RETURN-PATH: captain directive comms 019f3f26. Resume by checking which crew owns the
      dual-provider+per-bead capability + the harness-coverage plan for the 4 bug classes.

## 2026-07-10 ~11:5xZ — operator (via captain, event 019f4dc3): HOLD LIFTED → TRACK A ONLY · expires: 2026-07-12T00:00:00Z
WHAT: Overnight HOLD lifted into a single-lane directive. Staff ONE crew (kynes, Opus) on
      flagship core-loop-proof epic hk-hcrvb: rebase integration/core-loop-proof onto main,
      drive T9 (hk-jjt6w) to full-matrix green (Claude+pi REQUIRED, codex KNOWN-SKIP).
WHY:  operator + admiral finished the overnight-alignment audit; focus is the flagship
      core-loop proof, not broad volume. Grab-bag/kerf-next churn explicitly OFF.
ORDER: kynes rebases → drives T9 green. Cleanup beads ONLY if slots remain after T9 moving.
      HARD GATE: NO daemon redeploy until BOTH hk-x2spu AND hk-ih5k6 close.
RETURN-PATH: resume by checking kynes owns hk-hcrvb + T9 leg status; the paused quality-*
      queues stay PAUSED (do not resume — that was the pre-audit volume posture, now retired).
