# Workspace model change design

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
