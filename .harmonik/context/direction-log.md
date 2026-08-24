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
> admiral audit under the retention/anti-rot rule. What superseded them was itself struck
> on 2026-08-24 as lapsed; see the note under the heading below.

## No live direction change.

Every entry was struck on 2026-08-24 by the captain-rethink session, NOT by an admiral
audit, because every one had lapsed. The four entries here
described a 2026-07-12 freeze-and-carve pivot and a generative-system reframe, and each
carried an `expires:` of 2026-07-14 or 2026-07-15 — six weeks past. This file's own rule is
that an expired entry lapses to the standing autonomous posture and gets struck, and none had
been. Reading them as live sequencing intent was the failure they caused: they describe a
frozen fleet that has been dispatching for weeks.

Striking is the non-authoritative half of this file's two-branch rule. Re-confirming a
lapsed entry revives a hold and is the admiral's alone; striking one only records what the
unconditional `ON EXPIRY the DEFAULT is LAPSE` rule had already done to it on 2026-07-15.
The admiral has not booted since 2026-07-22, and these entries had already made a captain
read a frozen fleet. Audit this exception on the next admiral boot.

Git history holds the struck text.

**An empty log means what it says: no direction change is holding anything.** Act on the
standing posture.
