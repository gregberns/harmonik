# Problem

## Goal

Prove whether a crew can submit a planned bead graph once and let the deterministic core run all ready work through implementation, review, test, and merge without routine supervisor action.

## Actors

- The planning agent defines bead scope and dependencies.
- The crew submits and observes one epic queue.
- The core moves typed state and enforces structural rules.
- DOT agents make semantic decisions and implement work.
- The bead ledger stores task and dependency state.
- Git branches isolate work and collect validated changes.

## Main questions

1. What must an agent submit: all child beads, an epic, or only current ready beads?
2. Does the queue read ledger dependencies before dispatch and after each completion?
3. Can a serial graph run to completion from one submission?
4. Can a fan-out and fan-in graph run with bounded parallel work from one submission?
5. What branch owns the epic result?
6. Who resolves merge conflicts, failed validation, blocked dependencies, and graph changes?
7. Which current supervisor actions are required, and which are noise?

## Acceptance claims

- Serial: for `A -> B -> C`, one valid submission can finish all three in order without a supervisor transition.
- Parallel: for `A -> [B, C, D] -> E`, the core starts only A, then starts B, C, and D up to the concurrency cap, then starts E only after all three succeed.
- Safety: a blocked bead never starts. A failed dependency prevents its dependent from starting.
- Merge: each successful child lands in one explicit epic integration branch. The core never chooses a semantic conflict resolution.
- Recovery: retry, repair, or replan decisions are explicit agent decisions. The core reports typed facts and waits.
- Observability: the evidence can distinguish dispatch, agent work, validation, merge, ledger transition, and dependency release.
- Repeatability: the real runtime scenarios become durable tests in the existing harness.
