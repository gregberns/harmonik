# Research — Execution model

## Questions

1. What durable fact proves that a DOT run committed before shutdown?
2. Must merge finish before the bead can close?
3. How does restart recover a commit when its terminal ledger write did not run?
4. Does the model distinguish a worktree commit from an execution checkpoint?

## Findings

- `EM-016`, `EM-023`, and `EM-024` make a checkpoint commit on the run branch
  the durable run-state authority.
- `EM-052` orders normal success as merge, push, then bead close.
  `EM-053` orders merge, rebase, build, and push failure to reopen rather than
  close.
- `EM-031` and `EM-031a` require restart reconstruction from git and Beads.
  JSONL is not the recovery authority.
- `internal/daemon/workloop.go` `beadRunOne` detects cancellation after the
  DOT driver returns. It resolves the worktree tip and sends it to
  `RunBridge.Drain`.
- The focused DOT shutdown test proves that a local committed worktree is
  merged before the bead closes. It does not exercise a real remote worker.

## Patterns to keep

- Git holds the durable release evidence. Beads holds the coarse terminal
  state.
- Merge-before-close is already the normal release rule.
- `EM-025a` uses the useful order: make the durable ref change, then emit the
  observable projection.

## Risks and decisions

- The spec does not name the state where an agent commit exists but release is
  unfinished. Define that handoff and its restart recovery evidence.
- `EM-052` needs a cross-reference to the shutdown-drain edge. That edge must
  synchronize a remote run branch before merge and reopen on failure.
- A crash after merge and before bead close needs an explicit reconstruction
  rule. It must not cause duplicate release or silent redispatch.
