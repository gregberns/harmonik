# 07 — Tasks

12 test-authoring tasks + 1 epic, labeled `codename:comms-test-harness`, matching `SPEC.md` tranches T1–T4.
Epic = `hk-7m7o2` (already exists, assignee=yueh). All new task beads block the epic. T3 beads depend on all
of T1+T2. T4's beads depend on their evidence-source T1/T2 beads specifically.

## T1 — L0 (parallel, no cross-deps)
1. N1 `MatchAgentMessage` predicate table (directed/broadcast/topic/from-wildcard).
2. Presence TTL + `EffectiveLastSeen` clock matrix (scenario 4).
3. Cursor monotonicity / no-regress-under-race (harden existing).
4. `commsWakePaneCandidates` / `resolveProjectPath` ordering + symlink-hash (scenario 6 L0 half).

## T2 — L1 (parallel, no cross-deps)
5. N1/N2 live==replay boundary (scenario 3).
6. N3 multi-consumer fan-out + recipient dedupe on `event_id` (scenario 2).
7. B1 follow-starves-recv pin + inline doc (scenario 1).
8. Back-pressure drop-oldest + `subscription_gap` emission (G4 liveness).

## T3 — L2 (serial, depends on ALL of T1+T2 closing)
9. Daemon-restart reconnect, no message loss (scenario 5).
10. `--wake` real-pane paste + dead-pane-still-delivers (scenario 6 L2 half).
11. Multi-consumer socket sanity run (G6 e2e).

## T4 — spec/doc reconciliation (operator-in-the-loop)
12. T4 spec: B1 cursor-sharing + B2 who-vs-pane — executable spec + surface 2 design questions to operator.
    Depends on task 2 (presence matrix, B2 evidence) and task 7 (B1 pin, B1 evidence).

## Coverage check
Every SPEC.md tranche has ≥1 task; every acceptance-corpus scenario (design doc §3, 1-6) maps to at least
one task (1→7, 2→6, 3→5, 4→2, 5→9, 6→4+10). Scenario-test beads: tasks 1-11 are all scenario/regression
tests over real seams (no exploratory-test bead needed separately — this whole epic IS the test corpus, per
the harden-and-close-gaps framing; each task's Go test file gates its own close). DAG: T1,T2 → T3; T1(task2)+T2(task7) → T4(task12). No cycles.
