# Harmonik Task

bead_id: hk-abc12
title: Shrink the file
phase: reviewer
iteration: 2
run_id: 0198f000-0000-7000-8000-000000000001
workspace_path: /wt/alpha

## Worktree Discipline (CRITICAL — read first)

Your working directory is `/wt/alpha`.
ALL file paths you read, write, or edit MUST be inside this worktree.
NEVER use absolute paths that begin with the MAIN repo root outside your worktree — writing there silently loses your work because the daemon merges from THIS worktree, not main.

When running discovery commands (find, grep, ls, rg), use relative paths anchored to your worktree, NOT the main repo:

  CORRECT:   find . -name '*.go'
  CORRECT:   find /wt/alpha/internal -name '*.go'
  WRONG:     any absolute path outside the worktree above   (your edits will be lost)

If a discovery command returns paths under the main repo root, translate them into your worktree before reading or editing.
Note: your worktree has its OWN `.harmonik/` directory at `/wt/alpha/.harmonik/` (agent-task.md, reviewer-feedback files). The MAIN repo's `.harmonik/` (queue.json, events.jsonl, daemon.sock) is a DIFFERENT tree — do not read or write there.

## Bead Lifecycle (CRITICAL — read before acting)

DO NOT run `br close`, `br update --status closed`, or any terminal bead transition from inside this worktree.
The daemon owns the lifecycle transitions (open → in_progress → closed/failed) of every bead it dispatches, and it dispatched this one.
Running `br close` from the worktree causes premature closure that leaks to the parent repo even when no implementation has landed.
Your job is to implement and commit. The daemon will close the bead on your behalf after verifying the commit.

## Task Description

Do the thing.

## Reviewer Constraint (CRITICAL — read before acting)

You are a READ-ONLY reviewer. You MUST NOT run any git command that changes repository state.
Forbidden commands: `git reset`, `git checkout`, `git cherry-pick`, `git merge`, `git branch -d`, `git push`, `git rebase`, or any other state-mutating git operation.
You operate on a detached-HEAD reviewer worktree. Produce your verdict by running `harmonik write-review-verdict --verdict=<APPROVE|REQUEST_CHANGES|BLOCK> --notes="<your rationale>" --flags=<comma,separated,tags>`.
DO NOT hand-write `.harmonik/review.json` directly with the Write tool — always use the `harmonik write-review-verdict` command above, even when notes quotes code containing backticks. This command writes the file atomically (temp file + rename), so no separate atomic-write step is needed.
Violating this constraint can corrupt the implementer's task branch and break the merge pipeline.

## Prior-Iteration Context

review_base_sha: aaaa1111
review_head_sha: bbbb2222

## Session Completion

Committing your work is what completes the task. Your session ends by itself when your turn ends — there is nothing else to run.
The commit message MUST carry the `Refs: <bead-id>` line on its own line in the body. That trailer is how the daemon detects that your work is done.
Do NOT try to quit, exit, or end the session yourself, and do not run any slash command. This harness has none; an attempt to end the session keeps it alive past its budget and the daemon then scores your correct work as a crash.
