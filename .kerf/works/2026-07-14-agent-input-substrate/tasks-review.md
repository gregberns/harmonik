# Tasks Review — `agent-input-substrate` (M2)

> Round 1, INDEPENDENT reviewer (fresh-context sub-agent, 2026-07-14).

## Verdict: APPROVE (advance to ready) — no required fixes

- All 8 DoD criteria PASS: task list + DAG + parallelization; every changelog entry maps to ≥1 task
  (agent-input→T1; HC-069→T2; HC-070→T2/T3; HC-071→T2; INV-007-carveout→T3; INV-008→T4; §8.21→T3;
  EM-015d→T8; PL-021b/d→T8/T11; SK-002→T8, SK-021→T1; _registry AIS→T1); each task has a Spec: ID;
  acceptance is concrete/testable; T7/T8/T10 truly independent after T6; granularity appropriate;
  settled-naming block removes residual design calls.
- **DAG: ACYCLIC**, every predecessor exists, no missing prerequisite.
- **Test-task gate PASS:** hk-1cjy5 (scenario, deps T6+T9), hk-1r5jt (exploratory, deps T8); normative
  CLOSE RULE — neither work nor impl beads T1–T11 close until both test beads close.
- **Gate placement PASS:** G1 gates T5 codec freeze; G2 gates T6 commit; T11 gated on T9 + bake window;
  deferred F5→T12, F11→T13 both non-blocking.

## Advance
Criteria met → status advances `tasks → ready`.
