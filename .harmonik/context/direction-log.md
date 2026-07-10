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
