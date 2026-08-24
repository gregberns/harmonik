---
id: structural-scoreboard
title: A scoreboard that measures structure, because the line count is gameable and was gamed
type: task
priority: 1
labels: [observability, gate, clear-the-ground]
depends_on: []
blocks: []
workstream: W1
batch: 1
---

## Problem

The 2026-08-22 review's headline finding was that `internal/daemon` grew 4,350 production lines
between two reviews with nothing noticing. Its proposed fix was a line-count scoreboard.

A line-count scoreboard would have been defeated the next day. The comment cut (`028be0740`, 23 Aug)
removed 14,945 production lines from `internal/daemon` in one commit and changed no architecture.
`runWorkLoop` went from 1,432 lines to 816 while keeping all 22 parameters and all 158 of its
cyclomatic complexity. A line-count board would have shown the single largest "improvement" in the
program's history on a commit that improved nothing structural.

## Scope

A command that reports, for the three centre packages (`internal/daemon`, `internal/core`,
`cmd/harmonik`) and for the repo:

- Cyclomatic complexity of the top 20 functions, with names.
- Maximum and median parameter count across exported and package-level functions.
- File count per package directory, and the count of files sitting flat in the package root.
- Entries in `tools/lintreport/allow.txt`, split by linter and by package.
- Count of functions over complexity 30.

Write it in Go under `tools/`. The repo has one worked example of the right shape:
`scripts/queue-status-writer-ratchet.sh` is a 7-line `exec go run` shim over a real Go program.

## Done when

1. The command runs from a clean checkout and prints all of the above.
2. Its numbers are reproducible — two runs on the same tree agree exactly.
3. Running it against `028be0740^` and `028be0740` shows **no material movement**, demonstrating that
   the board is not fooled by comment deletion. Put that comparison in the commit body; it is the
   proof the task is done correctly.

## Limits

- **No line counts as a headline metric.** Report them if useful for context, never as the score.
- Do not wire it into a gate that blocks in this task. It is a measurement first; deciding what
  should stop a build is a later decision with evidence behind it.
- Do not add a dependency on an external analysis tool that is not already vendored — `gocyclo`,
  `golangci-lint` and `cloc` are all absent from this machine's PATH.
