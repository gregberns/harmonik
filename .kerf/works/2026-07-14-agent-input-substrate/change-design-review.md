# Change-Design Review — `agent-input-substrate` (M2)

> Round 1, INDEPENDENT reviewer (fresh-context sub-agent, 2026-07-14). Signoffs waived per planner
> direction; independent-reviewer sub-agent stands in for the operator gate. Inputs read: all five
> `04-design/*-design.md`, `04-design/00-decisions.md`, `02-components.md`, plus repo spot-checks on
> `phase1-session-restart-substrate`.

## Verdict: APPROVE (no required fixes)

## DoD criteria
| Criterion | Verdict | Evidence |
|---|---|---|
| 4 sections per file (current/target/rationale/traceability) | PASS | all 5 area files + 00-decisions; harness file correctly framed as DoD (no new normative reqs) but 4-section complete. |
| Every C1–C7 addressed by a target state | PASS | C1→AIS-001..005+HC-058/059/060/INV-008; C2→AIS-006..010/015/016; C3→AIS-011/012+PL-021b; C4→AIS-013/014/INV-002; C5→harness file; C6→PL deletion-boundary+SK carve-out; C7→AIS-017. |
| No gold-plating | PASS | every target traces to a C/SC/decision; the one net-new req (SK-021) defended as a normative precondition gating C6, not decoration. |
| Current state accurate | PASS | verified HC-054 obs-only (:1143), PL-021d write clause (:770), SK-002 PanePort/PL-021d (:70), substrate.go no-ops (:140/:173), RS-012/016/018/020 exist. |
| Specific enough to draft from | PASS | exact IDs, front-matter, verbatim demotion/carve-out clause text, HC numbering (058/059/060+INV-008, 0.5.4→0.6.0), 12-section layout pinned; opens tagged PLANNER-RECONCILE not left silent. |
| Rationale cites research | PASS | seam-contract Q1/Q4/Q5, driver Q1–Q5, capture-tee Q1, census PLAN.md:205-208, keeper ports.go + keepertest/keepertwin templates. |
| No cross-file contradictions | PASS | see below. |

## Cross-file consistency (critical) — PASS
- **PL-021d demotion** consistent three ways: process-lifecycle (demote-not-delete, preserved for
  keeper+CLI) ↔ session-keeper (SK-002 note, keeper EXCLUDED from C6 boundary) ↔ agent-input AIS-012
  (retirement scoped to daemon-RUN path). Same-commit motion.
- **D0 ownership** (M2 OWNS, M3/M4 consume; dual sync-return+event = direction-agnostic) identical
  across 00-decisions D0 / AIS-004 / HC-059.
- **tmux-inspectability** reinterpret-not-reopen identical across D5 / AIS-011 / PL-021b.
- **Bounded liveness** AIS-INV-001 ↔ HC-INV-008 ↔ harness FakeClock oracle: same output-or-stale /
  ClockPort / never-cite-STEP-0a phrasing.
- **capture-pane** scoping coherent: survives as observation read; SC6 grep-ban scoped to driver
  packages — distinct surfaces. StdinDevNull respecification points one way across HC-058/AIS-010/PL.

## D0 pivot confirmed against the repo
TASKS.md is unambiguous: M3-4 depends on "M2-1 (seam input/ack contract)" (:85); M2-1 = "Seam input
method + ACK contract" (:97); only M3-4 needs M2-1 (:77); M4 hard-prereq lists M2-1 (:138). Arrow
points M3→M2 and M4→M2. The design's reading (M2 OWNS) matches the repo and correctly flags the
planner brief asserted the opposite — surfaced as PLANNER-RECONCILE with a direction-agnostic
composition (right handling, not a DoD failure). TASKS.md M2-3 does say "keeper migrated to the M2-2
structured driver," confirming the D6 reconcile tension the design honestly carries.

## Open PLANNER-RECONCILE items (correctly externalized, not silently resolved)
D0 ownership direction; D4 claude input-side unproven + billing; D5 inspectability operator-confirm;
D6 migrate-vs-carve-out; small D1 port-placement / D10 spec-naming calls.

## Advance
Criteria met, no contradictions → status advances `change-design → spec-draft`.
