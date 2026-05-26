# Known Workarounds

Dated entries for recurring operational issues. Each entry names the version it was first observed, the symptom, and the resolution. Extracted from HANDOFF.md to keep session state lean.

---

## Worktree issues

**WORKTREE BEADS-JSONL STALE-AT-FORK (first seen v48).**
Symptom: merge conflict on `.beads/issues.jsonl` when merging a worktree branch.
Resolution: `git checkout --theirs .beads/issues.jsonl`

**WORKTREE TASK-INJECTION LEAK (first seen v36, ongoing).**
Symptom: stale task-injection state leaks across worktree merges.
Resolution: Stash before merge.

**WORKTREE AUTO-REMOVED BY HARNESS (first seen v41).**
Symptom: worktree directory is gone by the time orchestrator tries to read it.
Resolution: The branch survives; merge directly from the branch ref.

**WORKTREE-REMOVE STEALS CWD (first seen v45).**
Symptom: shell CWD becomes invalid after daemon removes a worktree.
Resolution: Prepend `cd /Users/gb/github/harmonik` to all subsequent commands; prefer `git -C /Users/gb/github/harmonik` for git ops.

**WORKTREE BEADS-JSONL LEAK (first seen v41).**
Symptom: `.beads/issues.jsonl` changes from a worktree appear in a rebase.
Resolution: Stash before rebase; never pop the stash.

**ISOLATED-WORKTREE STALE-BASE BUG (first seen v35, ongoing).**
Symptom: code read from a worktree reflects an old base — changes from main are not visible.
Resolution: Rebase the worktree branch onto current main before reading code.

---

## Harness issues

**HARNESS BLOCKS `.md` WRITES FOR SUB-AGENTS (first seen v47).**
Symptom: sub-agent Write tool calls on `.md` files are blocked by the harness.
Resolution: Orchestrator must persist markdown files via its own Write tool; do not delegate `.md` writes to sub-agents.
