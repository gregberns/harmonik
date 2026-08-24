---
id: cli-structure-assessment
title: Find out what 24,491 production lines are doing inside the command-line tool
type: task
priority: 1
labels: [cli, architecture, clear-the-ground]
depends_on: []
blocks: [cli-extract-logic]
workstream: W4
batch: 4
---

## Problem

`cmd/harmonik` holds **24,491 production lines in `package main`** — the second-largest production
body in the repo after `internal/daemon`, and larger than `internal/core`'s. It also has the repo's
lowest test ratio (0.85 test lines per production line, against 2.32 for the daemon) and its lowest
comment density (5.0%).

It is the command-line tool. It should parse arguments, call a package, and print the result. Almost
none of that volume should be there, and it has never been assessed — no plan in this repo has ever
named it as a target.

It also carries **106 entries** in `tools/lintreport/allow.txt`, the second-largest concentration
after the daemon's 249.

## Scope

Read-only. The deliverable is an assessment.

Report:

1. **Is it well structured?** Say so plainly either way, with evidence. Do not assume it is bad
   because it is large.
2. The split of the 24,491 lines into: argument parsing and output formatting (legitimately CLI),
   business logic that belongs in a reusable package, and anything that is neither.
3. For each candidate to move, the package that should own it and whether that package exists.
4. The largest files and what each is: `comms.go` (1,897 lines), `keeper_enable_doctor_cmd.go`
   (1,207), `main.go` (991), `keeper_cmd.go` (904), `harness.go` (898).
5. Why the test ratio is 0.85. Is the logic untested, or is it tested through another package?
6. What the 106 lint exclusions are hiding, by linter.

## Done when

The assessment exists as a document under `plans/2026-08-23-clear-the-ground/`, answers all six
points, and names a first move with a reason.

## Limits

- **Move nothing, change nothing.** This is a read.
- `run() int` in `main.go` is 770 lines and complexity is its problem, not placement. Note it and
  leave it — it is handled with the run machine so it is not done twice.
- Do not recommend a move that has no home. If the receiving package does not exist, say what it
  would be and what else would use it. A package with one caller needs a reason.
