# Tasks Review Findings — `pilot`

Reviewer pass over `07-tasks.md` + `05-changelog.md` + all four `05-spec-drafts/*`. Single round.

## Verdict: APPROVE — advance to `ready`

## Criteria check (per kerf tasks gate)

| Criterion | Status | Evidence |
|---|---|---|
| Task list with spec traceability | MET | Six impl tasks (IT-1..IT-6) + six test tasks (TT-1..TT-6); each cites file + section + requirement IDs. |
| Dependency graph present + valid DAG (no cycles, no missing prereqs) | MET | DAG section; edges all point prereq→dependent; verified acyclic. Daemon-side queue CLI + EM-062..065 mirror + QM-054 consumer noted as pre-existing (no unbuilt prereq inside the bundle). |
| Parallelization plan realistic | MET | Wave A/B/C; IT-1-quiet vs IT-2 declared non-conflicting (different files: `workloop.go` vs `supervise_cmd.go`/`commandcodes.go`). Only IT-1 is split across waves, with the rationale stated. |
| Every changelog entry has ≥1 implementing task | MET | Coverage table maps all 8 changelog rows; the queue-model A4 annotation-only row is satisfied transitively (no code change), explicitly justified. |
| Every task traces to a specific spec section | MET | Each IT/TT cites concrete §x + ID. |
| Acceptance criteria concrete + testable | MET | Each task's acceptance is an observable assertion; impl-task acceptance cross-links the validating test bead. |
| Task granularity appropriate | MET | One task per code surface; IT-1 split-but-coherent; not too fine. |
| Scenario-test + exploratory-test tasks present per changed spec area | MET | execution-model (TT-1 scenario / TT-2 explore); operator-nfr (TT-3 / TT-4); cognition-loop (TT-5 / TT-6). All six map to the Spec-Draft-pass beads. |
| Tasks concrete enough to execute without further design | MET | Each names files, line anchors, and the exact transition/event/flag to wire. |

## Bead reconciliation (work-brief requirement)

`br list --label codename:pilot` → 11 beads. Mapping:
- IT-2 → hk-ry8q1 (P0) · IT-3 → hk-3ix6o (P1) · IT-4 → hk-dg42b (P2) · IT-5 → hk-ytj2r (P2) · IT-6 → hk-5bw7a (P3)
- TT-1 → hk-h5lv2 · TT-2 → hk-ynjnf · TT-3 → hk-95a2r · TT-4 → hk-rnlxh · TT-5 → hk-iht2w · TT-6 → hk-va7z2
- IT-1 → hk-ry8q1 (EM-067 gate half) + **GAP-1** (EM-066 `--no-auto-pull` flag + quiet-branch half has no `codename:pilot` bead).

## GAP found (one)

**GAP-1:** the `--no-auto-pull`/`--queue-only` daemon flag + EM-066 quiet-branch wiring (`workloop.go` fallback branch + CLI flag + topology default) has no dedicated `codename:pilot` bead. hk-ry8q1 covers the pause verb + producer + the EM-067 br-ready *gate*, but not the EM-066 *flag/quiet-branch*. The pre-existing daemon-idle bead **hk-exd7m** (which EM-066 escalates per the assessment doc) is NOT labeled `codename:pilot`, so it is outside this work's set. Recommendation recorded in 07-tasks.md (label hk-exd7m `codename:pilot`, OR widen hk-ry8q1). No bead created (per the no-create-beads constraint).

## Out-of-bundle coordination (tracked, non-blocking)

COORD-1: EV `event-model.md §8.7.6` `drain_summary?` extension (pre-existing ON-013 request; IT-2 sub-task 6 consumes it). No pilot bead; intentionally out-of-bundle.

## No other issues. DAG valid. Coverage complete. APPROVE.
