# Harmonik Task

bead_id: hk-abc12
title: Shrink the file
phase: implementer-resume
iteration: 2
run_id: 0198f000-0000-7000-8000-000000000001
workspace_path: /wt/alpha
base_branch: work/integration

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
Your job is to implement, commit, and `/quit`. The daemon will close the bead on your behalf after verifying the commit.

## Task Description

Do the thing.

## Extra Context

Predecessor commit abc1234 has landed.

## Tests

A test earns its place by executing product code and failing when the behavior breaks. Asserting that a file exists, grepping prose, or pinning an internal signature is not a test.
Reach the change through the entry point a user or the daemon actually calls; prefer the existing `_test.go` for the code you changed, and name any new file after the behavior it protects — never after a bead or ticket ID.
Do not write a tier you will not run. If the natural gate is a suite you cannot run here, that is a signal the change is too big for one dispatch — say so rather than committing an unrun test.
A bad test is worse than bad production code: it makes the production code harder to fix while claiming it is protected. When a test already sitting in your path does not clearly earn its keep by the standard above — it never reaches product code, it pins an internal signature, or it is large and intricate out of proportion to what it protects — the default is to delete it, as part of this change rather than as a follow-up.
Deleting such a test is ordinary work, not a permission you need. Say what you deleted and why in the commit message.

## Structure

If the shape of the file or function you have to touch is what makes this change hard, say so in the bead rather than working around it. Make the smallest change that does not deepen the problem.

## Commit Message (a gate refuses any other shape)

Write the commit message into a file under `${TMPDIR:-/tmp}` — a scratch directory git ignores — so the file itself never becomes part of your change. Commit with `git commit -F "${TMPDIR:-/tmp}/commit-msg.txt"`. Do NOT use `git commit -m`.

`${TMPDIR:-/tmp}` and this worktree are the only places you can count on being able to write. A sandboxed run refuses every other path, and `sudo` does not lift that refusal.

The subject is the first line. Write your own, short and in the imperative — the gate reads the trailers below it and does not read the subject at all.
The body MUST hold a `Reviewed-By:` line and a `Review-Verdict:` line. Each one starts at the beginning of its own line, with no spaces before it.
The `Review-Verdict:` JSON MUST be on ONE line.
The body MUST also hold a `Refs:` line that names the bead. The commit-message gate never reads that line — the daemon does.
Your work counts as done only when BOTH of these are true: this worktree's HEAD has advanced past where it started, and the new commit carries that `Refs:` line. If HEAD does not advance, the workflow sends this task back to you.
That line is also what ties the commit to the bead, so a later reconciliation can match the two.
NEVER write a verdict of APPROVE. You did not review your own work. The pipeline replaces the provisional review lines below with the real reviewer's verdict after review. If no reviewer is reached, these lines remain as the settled, honest absent-reviewer record.

Copy the lines between the two fence lines below. Do NOT copy the fence lines. Write your own subject line and your own sentence, and keep the provisional `Refs:`, `Reviewed-By:`, and `Review-Verdict:` lines exactly as they are:

```
docs(workspace): remove the stale mode note from the Terminal comment

The comment named a mode this type no longer has. Remove that sentence.

Refs: hk-abc12
Reviewed-By: none
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "No reviewer was reached for this commit."}
```

If your change is only a typo or a whitespace fix, you MAY write the single line `Trivial: true` in place of the `Reviewed-By:` and `Review-Verdict:` lines. Start it at the beginning of its own line, with no spaces before it.

## Prior-Iteration Context

reviewer-feedback: /wt/alpha/.harmonik/reviewer-feedback.iter-1.md
prior-verdict-summary: REQUEST_CHANGES — address flagged issues

## Session Completion

IMPORTANT: You MUST run `/quit` as your final action after committing all work.
Do not ask the user to run it — you must type `/quit` yourself and submit it.
The daemon cannot detect that your task is complete until you exit this session.
Failure to run `/quit` will leave the workflow permanently stalled.
