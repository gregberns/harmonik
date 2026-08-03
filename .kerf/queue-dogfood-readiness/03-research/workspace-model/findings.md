# Workspace model research findings

## Questions

1. What lease disposition fits committed but unmerged work?
2. How can restart adopt the worktree without treating it as an orphan?
3. When may the lease and worktree be released?

## Findings

`WM-010` holds a workspace lease from `workspace_leased` to
`workspace_merge_status{merged}` or `workspace_discarded`. `WM-013b` makes
release idempotent. `WM-021` has pending and merged merge status. `WM-031`
retains failed or canceled worktrees but releases their lock. `WM-034` requires
a fresh worktree and branch for reopen. `WM-035` keeps a worktree for intra-run
recovery.

`WriteLeaseLockAtomic`, `ReadLeaseLock`, and `ReleaseLeaseLock` in
`internal/workspace/leaselock.go` are the authority boundary. `SweepStaleLeaseLocks`
clears dead daemon locks but keeps branches and worktrees.

## Patterns and risks

Git worktree and branch are durable evidence. The lock grants authority, not
evidence retention. Treating this state as a failure releases the lock too
early. Reopening creates a new branch and violates the no-second-merge rule.

## Design constraints

- Add a nonterminal recovery disposition that retains the branch, worktree,
  and merge evidence.
- Define restart adoption or recovery lease handoff before orphan sweep.
- Keep existing failed retention. Do not use `discarded` when the ladder may
  still continue.
- A stop/restart test proves one merge or recovery decision with no new
  worktree or terminal action.
