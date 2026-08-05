# Workspace model change design

> **RETIRED 2026-08-05.** This design has no card of record and produced one
> requirement, WM-041, which is now retired in `specs/workspace-model.md` with
> its number marked not reusable. The work's plan-of-record changelog names no
> workspace-model target at all, and the work's `07-tasks.md` says bravo task 4,
> `WM-041`, "has no card of record". Do not implement the target state below.
> Bead: hk-6lt60.

## Current state

The workspace model retains failed worktrees but releases their lease. Reopen
creates a fresh worktree and branch.

## Target state

Add a nonterminal terminal-recovery disposition. It retains the task branch,
worktree, and merge evidence. Startup may adopt its lease through an explicit
recovery handoff before stale-lock sweep. It cannot use discard or reopen until
the recovery matrix has selected a terminal outcome.

## Rationale

The post-commit run needs its original branch and workspace to finish exactly
once.

## Requirements traceability

Addresses worktree retention and restart-adoption requirements.
