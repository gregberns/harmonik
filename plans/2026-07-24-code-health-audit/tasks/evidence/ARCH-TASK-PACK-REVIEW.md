# Architecture-first task-pack review

- Date: 2026-07-26
- Reviewer: `task_pack_arch_audit`
- Verdict: **APPROVE**
- Independent graph reviewer: `core_graph_analysis`
- Graph verdict: **APPROVE**

## Scope reviewed

- `TASK-INDEX.yaml`
- `CORE-QUEUE-PI-PLAN.md`
- `tasks/README.md`
- all new `ARCH`, `WL`, `BR`, `PS`, `RL`, `DOT`, and `CG` cards
- amended queue, Run, Pi-mode, recovery, and end-to-end cards
- all bounded direct queue-persistence caller migrations

## Findings resolved before approval

1. Promoted run architecture from deferred cleanup to the P0 bootstrap program.
2. Split maintenance, dispatch gating, inline queue mutation, selection,
   reservation, recovery wiring, and final spawn ownership.
3. Split per-run resolution, placement, tunnel/worktree, prepared resources,
   mode execution, and terminal coordination.
4. Reconciled existing reviewloop work before production integration and split
   continuity, lifecycle/subscription, reviewcycle, and phase-shell tasks.
5. Added cognition-gate and DOT progression/agentic/coordinator owners.
6. Split phase lifecycle protocol from the local process/session adapter.
7. Replaced a flag-day composition migration with constructor foundation plus
   four one-commit cutovers and a production-graph proof.
8. Added bounded migration cards for eager refill, operator pause/resume, budget
   pause/unpause, review-failure charging, CLI bootstrap, and crew placeholder
   creation.
9. Made structural targets coordinator-approved before a task can become ready;
   baseline updates are not shared worker writes.
10. Corrected shared leases, dependencies, conflicts, and the four-agent
    staffing cadence.

## Mechanical evidence

- YAML parsed successfully.
- 84 task records and 84 task cards were present.
- Every hard dependency resolved.
- The 84-node dependency graph was acyclic.
- Card dependency metadata matched the index.
- Conflicts were symmetric.
- Every open same-family lease pair was mutually excluded.
- `git diff --check` passed.

## Review conclusion

The pack now targets maintainability of the queue/run execution core rather
than accumulating fixes inside the existing giant state machines. Tasks are
bounded for one-agent handoff, preserve the existing P2/LIFT and
reviewloop-decoupling progress, and serialize shared-spine work while allowing
file-disjoint contracts, adapters, evidence, and proof to proceed in parallel.
