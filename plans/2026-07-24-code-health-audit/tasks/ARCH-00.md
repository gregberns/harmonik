# ARCH-00 — Reconcile the live run-architecture graph

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: none
- Work type: read-only architecture baseline

## Objective

Produce `tasks/evidence/ARCH-00.yaml`: the current production control, data,
ownership, and dependency graph from queue admission through terminal recovery.
Reconcile what the P2/RT/LIFT and reviewloop-decoupling work actually landed,
what is only staged, and which old instructions are now stale. No production
change.

## Evidence to verify first

- `_plan.md` run-orchestration hotspot and production-graph findings.
- `E5-CHUNK-CATALOGUE.md`, `DAEMON-PARALLEL-ROADMAP.md`, current Git ancestry,
  `reviewloop-decoupling`, and the preserved L8 worktree.
- `workLoopDeps`, `runWorkLoop`, `beadRunOne`, `runReviewLoop`,
  `driveDotWorkflow`, `dispatchDotAgenticNode`, daemon boot composition, queue
  persistence callers, and tmux/process ownership.

## Exclusive lease

Only `tasks/evidence/ARCH-00.yaml`. Source, Git, kerf, and plan inspection are
read-only.

## Required work

1. Record exact LOC, cognitive complexity, function span, imports, callers, and
   90-day churn for the five giant state-machine symbols.
2. Map queue, Bead claim, Run, process/session, worktree, and terminal ownership.
3. Map production construction separately from test-only construction.
4. Classify every landed P2/LIFT/reviewloop artifact as active production,
   unused kernel, boundary-only move, or superseded plan.
5. Identify direct package and symbol cycles that constrain extraction.

## Acceptance

- Every graph edge cites a current production symbol.
- Landed, staged, and planned work are never conflated.
- The evidence names safe leaf contracts and all shared-spine collision zones.
- A different Sol agent approves the baseline before `ARCH-01`.

## Verification

YAML parse/schema check, repeatable local graph commands, Git ancestry checks,
and zero source diff.

## Escalate when

Stop and report if the current integration ref does not contain the P2 lineage
the plans claim or if two active branches contain irreconcilable implementations.

## Return

Use the directory evidence return contract. **COMMIT EXPLICITLY.**
