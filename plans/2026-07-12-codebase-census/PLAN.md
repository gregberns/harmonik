# Freeze-and-Carve Execution Plan

> **STATUS: PARTIAL SCAFFOLD — preserve-before-teardown commit.**
> Written by `commodore` from the admiral's planning brief (2026-07-12 19:46Z) to
> survive a queued fleet clean-slate teardown. This captures the *sequenced
> structure, constraints, and open questions* from the brief; the per-move
> analysis (grounded in `REPORT.md` verdicts + the two live exemplars) is NOT yet
> filled in. Resume by expanding each move's scope/DoD/risk against
> `REPORT.md` and the `~18:00Z STRATEGIC PIVOT` direction-log entry.

## Inputs (read first)
- `plans/2026-07-12-codebase-census/REPORT.md` — census: verdicts, 5 root problems,
  Moves 1–4, the hard call, Addendum (two live exemplars).
- `.harmonik/context/direction-log.md` — the `2026-07-12 ~18:00Z STRATEGIC PIVOT` entry.

## Global constraints (apply to every move)
- Keep the proven core. **NO big-bang rewrite.**
- Every phase stays behind the existing **~466-test regression net**.
- Freeze: **plan only** — no execution, no new beads, no dispatch until the operator lifts the freeze.
- Structural moves that grow real contracts (M2 / M3 / M4) are **kerf-first (spec-first)**.
  Note *where* a kerf work should be created; do **not** create it yet (freeze).

---

## STEP-0 — Prerequisite-zero (unblock the pipeline the carve runs through)
The carve flows through the daemon pipeline; these wedge it, so they land first.

1. **Resume-hang / QA-execution-gate** — implementer-resume relaunch hang (~`0adb6551`).
   Wedges the pipeline. Must be fixed before any carve work can flow in-pipeline.
2. **noChange-subsumption false-close** (data-integrity) — subsumption matches a bead-ID
   *mention* rather than *fix content*; false-closed against docs commit `32dc13f7`.
3. **Re-land the honest-probe fix under a clean ID** — the gb-mbp probe bug is still live
   behind false-closed `hk-2hfyt`; re-land under a fresh bead ID.

- **Goal / scope / files / DoD / risk:** _TODO — expand from REPORT.md._
- **Pipeline:** STEP-0 itself must run **OUT-OF-PIPELINE** (direct agent + manual land),
  since the pipeline is what it repairs.

---

## M1 — Delete test-theater + dead surface
- **Goal:** remove self-asserting "test-theater" (operatornfr / specaudit tests that assert
  their own inputs) + the dead event-registry surface.
- **Scope / files / deps / DoD / risk:** _TODO — expand from REPORT.md._
- **Pipeline:** likely in-pipeline **once STEP-0 lands** (deletion, guarded by regression net).

## M2 — Rebuild agent input channel behind `handler.Substrate`
- **Goal:** structured protocol for agent input; tmux becomes **observation only**.
- **Kerf:** spec-first — note a kerf work to create (do NOT create; freeze).
- **Scope / files / deps / DoD / risk:** _TODO._
- **Pipeline:** _TODO — likely partly out-of-pipeline until STEP-0._

## M3 — Extract run-lifecycle state machine from `beadRunOne`
- **Goal:** lift the run lifecycle out of `beadRunOne`; split the merge-coordinator so
  **no mutex is held over network / build IO**.
- **Kerf:** spec-first — note a kerf work to create (do NOT create; freeze).
- **Scope / files / deps / DoD / risk:** _TODO._
- **Pipeline:** _TODO._

## M4 — Remote rebuild (worker-resident agent, real protocol)
- **Goal:** worker-resident agent speaking a real protocol; slots into **M3's `Substrate` interface**.
- **Depends on:** M3 (Substrate interface must exist first).
- **Kerf:** spec-first — note a kerf work to create (do NOT create; freeze).
- **Scope / files / deps / DoD / risk:** _TODO._
- **Pipeline:** _TODO._

---

## Ordering summary
STEP-0 (out-of-pipeline) → M1 → M2 → M3 → M4 (M4 after M3's Substrate lands).

## OPEN QUESTIONS (need an operator decision)
- _TODO — enumerate as the per-move analysis is filled in._
- Placeholder: does M1 test-theater deletion require sign-off on which specaudit/operatornfr
  tests are genuinely load-bearing vs theater?
- Placeholder: STEP-0 re-land of the honest-probe fix — clean new bead ID, or reopen `hk-2hfyt`?

---

*Next step on resume: expand each `_TODO_` against `REPORT.md` + direction-log, then post
'PLAN.md draft ready' to admiral + operator for independent architecture review.*
