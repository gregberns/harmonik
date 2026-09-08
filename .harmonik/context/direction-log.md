<!-- TIER: 2 (operational state, sequencing intent across direction changes)
     LOADED BY: admiral + captain @ boot, AFTER tier-3 (project.yaml) + tier-2 (captain-lanes.md), BEFORE acting.
     OWNER: admiral. APPEND-ONLY. ONE entry per direction CHANGE (never a status update, never per-tick, never by crews).
     Newest-first. ~3-5 lines/entry. Capped ~10 entries / ~60 lines; delete oldest on overflow (no archive).
     Four load-bearing fields per entry: WHAT / WHY / RETURN-PATH(sequence) / expires:.
     ON EXPIRY the DEFAULT is LAPSE -> revert to the standing autonomous posture, NEVER a hold.
     The admiral audit OWNS flagging an expired-but-present entry: re-confirm with the operator or strike it.
     See .harmonik/context/AGENTS.md for the full forced-write/forced-read discipline. -->

# Direction log — temporal sequencing intent across direction changes

## 2026-09-07 — operator: platform re-grounding is the line · expires: 2026-09-21T00:00:00Z
WHAT: Revived the kernel/fabric substrate + a segmented-module restructure, re-grounded on the prior
      design (agent-substrate-v2 + p1-kernel-fabric). Design of record: plans/2026-09-07-harmonik-{bus,restructure}/.
ORDER: kernel/bus (P1/P3) can start (first slice = in-mem kernel + one subprocess plugin + reload-zero-loss gate).
      Restructure is GATED — it collides with the active delete-and-rewrite program.
RETURN-PATH: AWAITING operator: restructure-vs-delete-and-rewrite, module count, first-slice go/no-go. Nothing committed.

<!-- Ancient pre-2026-09 entries (freeze-and-carve, generative-system, codebase-overhaul) deleted as stale; in git history if needed. -->
