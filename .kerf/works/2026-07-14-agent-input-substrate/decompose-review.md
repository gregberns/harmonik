# Decompose Review — `agent-input-substrate` (M2)

> Review round 1 (autonomous, signoffs waived per planner direction 2026-07-14). Reviewer =
> design agent self-review against the pass criteria in the spec jig. Inputs read:
> `02-components.md`, `01-problem-space.md`, `internal/handler/substrate.go`,
> `plans/2026-07-13-code-revamp/{ROADMAP,REVIEW-FINDINGS,PLANNING-LOG,reconciliation,DECISIONS}.md`,
> P1 bench `.kerf/works/session-restart-substrate/` (exemplar).

## Verdict: APPROVE (after one required fix, applied)

## Criteria check

| Criterion | Verdict | Notes |
|---|---|---|
| Each affected area listed with change description + requirements + deps | PASS | C1–C7 each carry what/why-separable/order/spec-surface; deps in the component map. |
| Every goal from 01 maps to ≥1 component | PASS | Traceability table G1–G6 + spec-first + WAL orphan → C1–C7. |
| No unjustified spec area | PASS | C7 traces to ROADMAP orphan homing (`ROADMAP.md:98` WAL-guard→M2); all others trace to goals. |
| Requirements describe what-should-be-true, not text edits | PASS | Requirements are behavioral; open questions correctly deferred to design. |
| All relevant existing spec files accounted for | PASS | PL-021b family, HC-054, new input-protocol spec; RS/SK consumed by reference (01 §Affected areas). |
| Dependencies correctly identified | **FIXED** | See F1. |

## Findings

**F1 (REQUIRED — fixed).** C6's deletion boundary listed spawn (`crewstart.go`) and remote (M4)
but omitted the **keeper**: P1's shipped keeper vertical is a live paste-inject consumer
(PL-021d `load-buffer`+`paste-buffer` via `PanePort.Inject`). This is exactly REVIEW-FINDINGS
**A11** ("UNGUARDED DELETION HAZARD — M2-3/M2-6 must depend on keeper migrated to the M2-2
structured driver"). Fix applied: keeper added to C6's deletion-boundary open questions;
the design pass MUST resolve migrate-vs-carve-out and the task graph MUST carry the edge.

**F2 (note, no change).** The decompose banner says "work stops after decompose — design
deferred until P1 proves the seam." That hold is now LIFTED by planner direction (2026-07-14):
P1 landed `internal/substrate` (green), `internal/replay`, the RS/SK specs (commit 35d623eb),
and the keeper Step reactor + shell (commit 6a47e1bd T7). The A16 un-hold condition is
satisfied per the planner; this work proceeds through design.

**F3 (note, no change).** The seam-choice premise (build on `handler.Substrate`, reversing
remote-substrate-phase2 DEC-A) is operator-ratified — DECISIONS.md C2 APPROVED 2026-07-13.
Not a locked-decision hazard.

**F4 (note, carried to research).** The M2-1 ↔ M3-4 cross-work edge is live: M3
(`2026-07-14-run-state-machine`) is itself only at decompose, so no reactor-Step input/ack
contract exists yet to consume. Design will pin M2-1 self-contained and flag divergence
risk as a PLANNER-RECONCILE item rather than guess M3's contract.

## Advance

Criteria met → status advances `decompose → research`. Research areas (mirroring P1's
4-findings layout): `seam-contract` (C1+C3+C6 boundary), `driver` (C2+C7),
`capture-tee` (C4), `harness` (C5).

---

## Review round 2 — INDEPENDENT reviewer (fresh-context sub-agent, 2026-07-14)

> Post-restart validation: the round-1 review above was a self-review; planner direction
> requires an independent-reviewer sub-agent at each pass boundary. A fresh-context reviewer
> re-checked `02-components.md` against the decompose DoD and spot-checked factual claims
> against the tree on `phase1-session-restart-substrate`.

### Verdict: APPROVE (no required fixes)

- All 6 DoD criteria PASS (components complete w/ deps; G1–G6 + WAL orphan traceable;
  no unjustified areas; requirements behavioral; PL-021b/HC-054/new-spec accounted for;
  C1→C2→{C4→C5}→C6 ordering coherent; C6 keeper gate matches REVIEW-FINDINGS A11).
- Factual spot-checks EXACT for: substrate.go :30/:101/:140/:173; pasteinject.go six
  side-interfaces :187/:206/:236/:254/:280/:493, injectAndVerifySeed :1708; osadapter.go
  :379/:405/:464/:486/:512/:541; tmuxsubstrate.go :2218; workloop.go :489/:4346/:8064;
  substrate/seam.go :27, replay.go Twin :82 / FaultConfig :46.
- MINOR cosmetic line drift only: apptap tap.go fields ~:55–:63 (cited 48/58/63);
  code-revamp ROADMAP orphan items now :94/:98 (cited :69/:73). Within the docs' own
  "verified 2026-07-14, may drift" framing; no artifact change required.

Decompose stands approved by independent review; the earlier `decompose → research`
advance is ratified.
