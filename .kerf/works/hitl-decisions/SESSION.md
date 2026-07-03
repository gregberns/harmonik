# hitl-decisions — Session state (shelved 2026-06-11, liet)

**Status:** `change-spec` — DESIGN DONE. The change-spec is written and reviewed (APPROVED). Do not advance to `integration`/`tasks` until captain/operator signs off the draft.

## What's done
- `01-problem-space.md` — finalized + reviewed (REVISE→5 gaps incorporated). 4 adopted decisions D1–D4; success criteria S1–S8.
- `02-analysis.md`, `03-components.md` — 7 components (K1 events · K2 raise/block · K3 projection · K4 operator surface · K5 orphan reaper · K6 keeper seam · K7 kerf view).
- `04-research/findings.md` — agent-comms FINALIZED spec as structural analog + code anchors; two hard constraints (F-class fsync durability; the open-subscribe-stream blocked-wait contract).
- `05-specs/hitl-decisions-spec.md` — **the deliverable.** 3 F-class events, `harmonik decisions` CLI, on-demand events.jsonl projection (no new service), 9 normative N-conditions, per-component impl handoff w/ file:line anchors, S1–S8 acceptance. Two review rounds: 3 P1 footguns (reaper TOCTOU, reaper 1h-cadence, answer-vs-wait-arm race) + 2 P2 fixed; confirmation APPROVE modulo 2 consistency fixes, applied.

## Next steps (gated)
1. **Operator sign-off** of the change-spec draft + the OPEN FLAG: v1 assumes a single-human answerer (first-writer-wins on `decision_id`); multi-human deferred — confirm OK for v1.
2. On sign-off → `kerf resume hitl-decisions` → advance `change-spec` → `integration` (consolidate SPEC.md) → `tasks`.
3. **Implementation is paul's daemon-infra lane**, NOT hitl-decisions' — this work produces the spec only.

## Context
- Bead: hk-0zxv6 (P3, `codename:hitl-decisions`). Design-done recorded as a bead comment.
- Crew liet was re-tasked here by captain as fill-work while the logmine recurring-pipeline mission is operator-gated on the trigger pick. Revert to logmine the moment that's answered.
