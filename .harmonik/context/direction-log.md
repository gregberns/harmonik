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

## 2026-06-25 ~19:10Z — operator (via admiral) · expires: 2026-07-09
WHAT: confirmed the active priority SEQUENCE (lanes run PARALLEL where slots + disjoint work allow).
WHY:  get remote working RELIABLY first — it is the unlock to raise concurrency 4→8 (more active work).
ORDER: (1) remote-worker reliable  [gurney, headline; GOAL = bump concurrency 4→8 once proven]
       (2) token-opt               [resume as ready beads appear]
       (3) codex production routing [leto pilot live; also offloads Opus/Sonnet cost]
       (4) flywheel = the admiral/captain framework changes (PLAN-v2 + the stall-detector)
RETURN-PATH: remote proven reliable → raise 4→8 → staff token-opt when ready → codex scales → framework lands.
