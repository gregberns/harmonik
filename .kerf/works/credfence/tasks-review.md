# Tasks Review (self-review) — `credfence`

> Autonomous self-review (no human reviewer present; user delegated all decisions per the 2026-05-30 assessment doc). Checks the pass-7 done-criteria from `kerf show credfence`. One round.

## Criteria check

| Criterion | Status | Note |
|---|---|---|
| Task list with spec traceability | PASS | 16 tasks (T-CORE-1/2, T-CRED-1..5, T-SPEND-1..3, T-OP-1..3, T-TEST-1..4); each names its spec sections (CI-/CL-/CP-/HP-/ON- IDs with file). |
| Dependency graph present, valid DAG | PASS | §Dependency Graph: two chains (core→spend, cred→supervise) + four singletons; all edges prerequisite→dependent; no back-edges; every dependent's prerequisites present. No cycles. |
| Parallelization plan, realistic | PASS | 5 waves; the cred chain and spend chain share only the deny-list constant (owned by T-CRED-1, read by T-CRED-3 — sequenced, not parallel); singletons touch disjoint files. No two same-wave tasks write the same file. |
| Every changelog entry has ≥1 implementing task | PASS | §Changelog coverage check maps all 7 draft entries + the implementation-task-anchors section to tasks. Spec-text-only amendments land at finalize; their forced code = G-1/G-2. |
| Every task traces to a specific spec section | PASS | Each task's **Spec sections** line cites file + requirement ID(s). |
| Acceptance criteria concrete and testable | PASS | Each task has by-key / observable-terminal-condition acceptance (e.g. "constructed child env contains zero deny-list keys; a non-deny-list var survives"; "budget_exhausted{handler_account} + handler_paused + budget-paused; no further run_started"). |
| Task granularity appropriate | PASS | Split by review surface: core types (internal/core), credential (daemon env + supervise), spend (daemon meter + Pi budget.ts), operator knobs (router.ts/config/flag), tests. Not one mega-task; not per-line. |
| Tasks executable without further design | PASS | Code anchors (osadapter.go:497, shim.go:103, index.ts:64, router.ts:84, budgetscope.go, budgetevents_hqwn59.go, handlerpause_policy_37zy8.go) verified against the live tree; precedence/fail-closed/sentinel behaviors specified. |
| ≥2 test tasks; scenario + exploratory beads exist and depend on impl tasks | PASS | 4 test tasks: scenario hk-24d72 (dep T-CRED-1/2) + hk-c7lxc (dep T-CORE-1/2, T-SPEND-1); exploratory hk-96s75 (dep T-CRED-3/4) + hk-0p9so (dep T-SPEND-1/2). All four beads pre-exist (created pass-5); listed as explicit tasks with declared dependencies. |
| Bead reconciliation (brief requirement) | PASS | All 15 credfence beads mapped 1:1 (§Task→Bead reconciliation). 2 GAPs flagged (G-1, G-2) — recorded, NOT created. 3 reconciliation notes (R-1 hk-c1ah6-vs-CL-090c, R-2 ON-004x side-effect entries, R-3 spec-text-only amendments). No beads duplicated. |

## GAP summary (carried to the work summary)

- **G-1:** `internal/core/budgetscope.go` `handler_account` enum value — no bead. P0, blocks hk-k3f8g.
- **G-2:** `internal/core/budgetevents_hqwn59.go` `BudgetExhaustedEventPayload` account-scoped fields — no bead. P0, blocks hk-k3f8g.
- Both are the core-type half of hk-k3f8g; orchestrator should file two P0 `internal/core/` beads blocking hk-k3f8g (recommended) or broaden hk-k3f8g.

## Verdict

APPROVE. All pass-7 criteria pass. DAG is valid and acyclic; every changelog entry and every credfence bead is covered; the four required test beads are explicit tasks with correct dependencies; the two foundation GAPs are recorded (not created) per the brief. Advance to Ready.
