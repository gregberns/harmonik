# How the restructure relates to the ACTIVE program (delete-and-rewrite), not the dead one

**Date:** 2026-09-07 · **Corrects** an earlier draft of this file that called the restructure
"P2 (extraction)". That was wrong: **`plans/2026-07-21-p2-extraction/` was SUPERSEDED on
2026-07-27** (STATUS.md, top block) — "do NOT resume their task cards." Its *measurements*
survive where cited; its lane models and task indexes are dead.

## What the ACTIVE program is

Per `STATUS.md` (2026-07-28), the active program is **delete-and-rewrite**
(`plans/2026-07-27-delete-and-rewrite/CHARTER.md`):
- **Phase 1 — deletion** (COMPLETE): ~225k lines of test-theater + dead code removed.
- **Phase 2 — config-driven subsystem partition** (first switch landed).
- **Phase 3 — rewrite `internal/daemon/workloop.go` + `dot_cascade_core.go` as one unit.**

This program decomposes the run machine **IN PLACE** inside `internal/daemon` (config-driven
partition, then rewrite the god-functions). It shares `PRINCIPLES.md` as its standard.

## The real relationship — and the open question only the operator can settle

The 2026-09-07 restructure seed proposes something **strategically different** from delete-and-
rewrite's in-place decomposition: **move good code OUT into separate segment modules** behind a
one-way boundary (`06-segmented-layout.md`, `07-segmented-structure-and-day1-standards.md`).

Both target the same disease (the daemon god-package + the `internal/core` giant leaf). They are
**two strategies for one goal**, and they are not automatically compatible:

- delete-and-rewrite Phase 2/3 = **partition + rewrite in place**, `internal/daemon` stays home.
- 2026-09-07 restructure = **extract into segment modules** (`kernel/`, `tools/*`, `shared/`).

**OPEN OPERATOR DECISION (the crux for this whole track):** does the platform re-grounding
(segment modules + kernel) **replace** delete-and-rewrite's in-place approach, **run after** its
Phase 3 completes, or **run alongside** it? Until this is answered, an agent could easily work
two contradictory decompositions of the same files. This is flagged for the operator; it is not
a plan edit's to decide.

## What still holds regardless of that answer

- **`internal/core` is split LAST** — delete-and-rewrite, the superseded P2, and this track's
  fresh measurement all agree (60-fan-in giant leaf; a wrong move is a repo-wide merge storm).
- **Extract-then-delete, steady stream** — component by component, test and release each.
- **The segmentation + day-1 standards** (`07-...`) are correct as an engineering standard no
  matter which program owns the timeline.

## Correction trail

Files that carried the "restructure = P2" framing and are corrected/footnoted to point here:
`07-segmented-structure-and-day1-standards.md` §5; `../2026-09-07-harmonik-bus/05-CORRECTION-the-real-design.md`
and `07-operator-amendments-2026-09-07.md` (program-framing sections). The bus track's mapping
to **P1 (kernel)** and **P3 (dispatch)** is unaffected — P1 and P3 were not superseded; only
P2-extraction was.
