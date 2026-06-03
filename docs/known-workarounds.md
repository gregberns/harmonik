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

## Daemon keep-alive

**DAEMON CRASH-LOOP / EXIT-17 (first seen v54, ongoing).**
Symptom: harmonik daemon exits unexpectedly; queue stalls; `harmonik queue status` returns exit 17.
Resolution: Use `scripts/hk-keeper.sh` (checked in, canonical) to keep the daemon alive:

```bash
# From the repo root (parameterized — defaults to CWD, concurrency=6):
./scripts/hk-keeper.sh [/path/to/project] [max-concurrent]
# Or via env vars:
HK_PROJECT=/path/to/project HK_CONCURRENCY=4 ./scripts/hk-keeper.sh
```

The script runs *outside* tmux, launches the daemon in a `hkdkeeper` tmux session (not `harmonik-*` prefixed so orphan sweeps can't kill it), strips `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN` (subscription-billed), and revives on process absence. Do NOT `rm` the socket while the daemon is live — only the keeper does that after confirming the process is gone.

---

## Harness issues

**HARNESS BLOCKS `.md` WRITES FOR SUB-AGENTS (first seen v47).**
Symptom: sub-agent Write tool calls on `.md` files are blocked by the harness.
Resolution: Orchestrator must persist markdown files via its own Write tool; do not delegate `.md` writes to sub-agents.
