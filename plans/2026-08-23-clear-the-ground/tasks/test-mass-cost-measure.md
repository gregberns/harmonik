---
id: test-mass-cost-measure
title: Measure whether the test mass still stops us changing code
type: task
priority: 1
labels: [test-quality, clear-the-ground]
depends_on: []
blocks: []
workstream: W5
batch: 5
---

## Problem

There are **332,828 test lines against 193,655 production lines** — a ratio of 1.72:1. The
delete-and-rewrite program already removed about 225,000 lines, and the ratio is still 1.72:1.

The original diagnosis was that the suite pinned internal function signatures rather than behaviour,
so any restructuring broke hundreds of tests that were never protecting anything. The operator's
stated concern, 2026-08-23: if that is still true, this program fails, because every workstream in it
is a restructuring.

Nobody has re-measured since the deletion. The line count does not answer the question — a large
suite that tests behaviour is fine, and a small one that pins signatures is not.

## Scope

Read-only measurement.

Take **three representative production changes** — one in `internal/daemon`, one in `internal/core`,
one in `cmd/harmonik` — each small and each of a kind this program will actually perform (rename a
symbol, change a signature, move a file between packages). For each, record:

1. How many test files fail to compile or fail outright.
2. For each failing test: does it fail because **behaviour changed** (it was protecting something) or
   because **a name or shape changed** (it was pinning structure)? This split is the deliverable.
3. How long the affected tests take to run.

Then report the ratio of behaviour-pinning to structure-pinning failures, and where the
structure-pinning tests are concentrated.

## Done when

The measurement exists as a document under `plans/2026-08-23-clear-the-ground/` and answers one
question with a number: **of the tests that break when we restructure, what fraction were protecting
behaviour?**

## Limits

- **Report before proposing.** No deletion campaign, no target, no plan for the suite comes out of
  this task. The measurement first.
- **Do not delete a test in this task.** The last sweep took ten behavioural tests with it and they
  had to be restored (`11c058b95`). That is the exact failure this task exists to avoid repeating.
- Revert each of the three trial changes. This task leaves the tree as it found it.
- Do not use whole-suite line count as an answer. It is the number that has already failed to answer
  this question once.
