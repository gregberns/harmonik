# reap — Tasks Review

Fresh-context re-read of `07-tasks.md` against `SPEC.md`, `06-integration.md`, and `05-specs/{C1..C6}` (project review-gate; spawning agent has no Agent tool). **Verdict: PASS.** All 13 tasks map to an existing `codename:reap` bead; the one task→bead boundary item is correctly flagged as a sequencing GAP, not a missing-bead defect; the DAG is acyclic; both test beads appear with correct gate direction.

## Review checklist (jig Review Criteria)

### 1. Every SPEC section covered by ≥1 task — PASS
The §"Coverage check" table in `07-tasks.md` maps every SPEC section (§3.1 pre-sweep set, §3.2 payload, §3.3 step labels, §3.4 exit codes, §4 C1-C6 clauses, §6 testing) to a task. Spot-verified: §3.1→T3, §3.4 ordered codes→T7+T9, C1 PL-006e→T4, C2 PL-006f→T1+T5, C3 QM-002c→T6, C4 PL-014b/PL-019(i)→T11/T10, C5 PL-002c→T7+T8, C6 PL-005c→T2+T9. No section unassigned.

### 2. Every component AC appears in ≥1 task — PASS
C1-AC1..7→T4; C2-AC1..6→T1/T5; C3-AC1..6→T6; C4-AC1..8→T10 (AC1-4) + T11 (AC5-8); C5-AC1..6→T7 (AC6) + T8 (AC1-5); C6-AC1..7→T9 (+T2 AC4). Each task's Acceptance line names the specific ACs it discharges. The two reproduce-first ACs (C2-AC1, C6-AC1) are explicitly named in T5 and T9 with the both-branches-one-table-driven-test framing.

### 3. Dependencies correct + form a DAG — PASS
Traced the graph: T1,T2,T3,T7,T11 are roots (no deps). T4←T3; T5←T1,T3; T6←T3,T4,T5; T8←T7; T9←T2,T7,T8; T10←T8; T12←T4,T5,T6,T9; T13←T8,T9,T10,T11. No back-edge creates a cycle (verified by topological order: T1,T2,T3,T7,T11 → T4,T5,T8 → T6,T9,T10 → T12,T13). The T1↔T2 generation edge is correctly marked SOFT (advisory, with the documented daemon-start-ns surrogate) so it does not force a hard cycle or block. The hard ordering constraints (T7-before-T8/T9 for exit codes; T8-before-T9 for step 1.5-before-1.6; T8-before-T10 for the C5 safety basis; T4-before-T6 for sidecar evidence; T5-before-T6 for exclusion-a) all match `06-integration.md` §5.1-§5.4 and §3.

### 4. Tasks appropriately sized — PASS
Each task is a single coherent unit (one new file or one focused edit-site cluster + its test), maps to one component clause, and names concrete files + ACs. T7 (exit-code registry) is the one cross-component task; it is correctly scoped to the operator-nfr §8 + commandcodes surface and its two-bead ownership is flagged. No task bundles unrelated components; no task is so large it spans an entire bead's worth of disjoint work without sub-structure.

### 5. Integration tasks exist + correctly ordered — PASS
T3 (shared pre-sweep set + payload plumbing) is the explicit integration task for the C1⟷C3 boundary and is correctly placed before T4/T5/T6. T7 (ordered exit-code registry) is the explicit integration task for the C5+C6 boundary and is correctly placed before T8/T9. The build-step grouping (A-G) mirrors `06-integration.md` §3. No integration concern is left implicit.

### 6. ≥1 scenario-test bead + ≥1 exploratory-test bead with correct deps — PASS
T12 → hk-a31od (`scenario-test`, EXISTS, open); T13 → hk-izs8s (`exploratory-test`, EXISTS, open). Both appear as dependents of the core implementation tasks (T12←T4/T5/T6/T9; T13←T8/T9/T10/T11). The existing bead wiring (both test beads BLOCK all four implementation beads) is the correct test-gate direction — an implementation bead cannot close until its gating test bead is satisfied — matching the jig's Validation/Acceptance Tests requirement and the hk-37zy8/hk-aievp/hk-ry3be motivation. No new test bead needed; neither pre-existing test bead was duplicated.

## Task→bead reconciliation — PASS (one flagged GAP, correctly non-blocking)
All 13 tasks own an existing bead (hk-9eury: T1/T3/T4/T5/T6; hk-xb5yi: T10/T11; hk-li14r: T7-portion/T8; hk-7t9g1: T2/T7-portion/T9; hk-a31od: T12; hk-izs8s: T13). The §"GAPS" section flags exactly one real boundary item — T7's exit-code registry work spans hk-li14r (24/25) and hk-7t9g1 (26) — and resolves it within the existing beads (implement under hk-li14r, append 26 under hk-7t9g1, sequence hk-li14r first) without requiring a new bead. GAP-2 (sibling-work ordering) and GAP-3 (generation source) are correctly identified as orchestrator-level sequencing notes, not missing beads. No beads were created (per the Tasks-pass instruction). This is the correct handling.

## Verdict
PASS. Complete SPEC + AC coverage, acyclic dependency DAG, integration + test tasks present and correctly ordered, both test beads gating, every task bead-reconciled with the single boundary item flagged as a non-blocking sequencing GAP. Advance to Ready.
