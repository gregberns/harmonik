<!-- PP-TRIAL:v2 2026-04-30 main -->
# Session Handoff

## Status
**Clean.** Phase-0 meta-graph loaded into Beads (1 epic + 42 workflow tasks under `tag:meta` / `phase:0`). HC r1 review still pending — that is the next hands-on task.

## What we did and why
User asked: "Why not decompose everything that needs to be done and put it in Beads, so what's left and what's done is unambiguous?" Right call. We added a workflow-task layer alongside the impl-task layer in the same Beads workspace, segmented by label so they don't mix. The loader was extended with `kind: workflow` (no `req` field needed), `meta-pilot-data.yaml` was authored, and the load ran clean (48 edges, 0 rejections, no cycles).

## Where things stand
- Meta epic: `hk-ahvq` ("Phase 0 completion — load remaining pilots and exit to code phase").
- 42 children in `draft` (same convention as impl beads — they need promotion to `open` before `br ready` surfaces them).
- Coverage: HC remaining (review + patch), 5 spec pilots × 7 tasks each (data file → narrative → dry-run → load → r1 review → patch → backfill of earlier pilots' parked citations), 5 phase-exit tasks (cycle check, citation zero-out, mnem-map consolidation, bootstrap-subset identification, milestone).
- Working tree very dirty: meta yaml, loader edit, loader-tooling.md edit, 2 new memory files, plus everything that was already uncommitted at session start. **User asked but did not answer**: should anything be committed before HC r1 starts? Worth asking up front.

## Next step
**HC r1 review** = bead `hk-ahvq.1`. Mechanics unchanged from the prior handoff: 3-reviewer pass per `docs/decompose-to-tasks/pilot-review-protocol.md` §6 covering the 6 cycle-rejected edges in `hc-load-findings.md` plus the EV-edge count tension (`hc-pilot.md` §5.3 says 22, the data file has 26). Apply patches to `hc-pilot-data.yaml`, re-run the loader (resume mode auto-skips loaded beads).

## If something changes
- If HC r1 surfaces a brand-new rule class not covered by `discipline.md` v0.8: patch discipline.md FIRST, propagate to prior pilots, then continue. Do not fold rule-class discovery into the patch bead.
- After HC r1+patch lands, the natural next task is `hk-ahvq.3` (CP — Control Points — author yaml). The user has agreed CP authoring is autonomous.

## Files to open first
1. `HANDOFF.md` (this file).
2. `docs/decompose-to-tasks/meta-pilot-data.yaml` — the new workflow task graph.
3. `docs/decompose-to-tasks/hc-load-findings.md` — what HC r1 must chase.
4. `docs/decompose-to-tasks/pilot-review-protocol.md` §6 — review mechanics.
5. `scripts/load-pilot.py` — loader (extended with `kind: workflow` this session).

## Blocking question
Does the user want any of the working-tree changes committed before HC r1? They asked at end of session and the session ended before they answered.
