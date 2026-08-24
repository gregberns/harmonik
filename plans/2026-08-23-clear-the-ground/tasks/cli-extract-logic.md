---
id: cli-extract-logic
title: Move business logic out of the command-line tool and leave it thin
type: task
priority: 1
labels: [cli, architecture, clear-the-ground]
depends_on: [cli-structure-assessment, lint-rekey-exclusion-list]
blocks: []
workstream: W4
batch: 4
---

## Problem

`cmd/harmonik` is the command-line tool and holds 24,491 production lines. Operator direction,
2026-08-23: it should be lean, with the substance living in reusable packages.

`cli-structure-assessment` decides what moves and where. This task performs the moves.

## Scope

Per the assessment's boundaries. Largest candidates, for orientation only — the assessment decides:
`comms.go` (1,897), `keeper_enable_doctor_cmd.go` (1,207), `keeper_cmd.go` (904), `harness.go` (898).

**One receiving package per landing**, same discipline as `core-split-by-cluster`: move the code and
its tests together, delete shims in the landing that created them, run the full gate, land.

## Done when

For each landing:

1. The moved logic is reachable and tested from its new package, not through the command-line tool.
2. What remains in `cmd/harmonik` for that command is argument parsing, wiring, and output.
3. `tools/lintreport/allow.txt` gains nothing.
4. No behaviour change to the command's observable output or exit codes. Prove it: the command's
   existing tests pass unmodified, or if none exist, say so in the commit body rather than asserting
   the behaviour is unchanged.

And for the workstream: the test ratio for whatever remains in `cmd/harmonik` improves, because the
logic that was hard to test from a `main` package now lives where it can be tested.

## Limits

- **Do not move code into a package that exists only to receive it**, unless the assessment named a
  second caller or a test seam that has no other route. A one-caller package is the same problem
  wearing a different directory name.
- Do not combine a move with a change.
- Do not touch `run() int`. It belongs to the run-machine chain.
- Do not start before the assessment lands. Moving 24,491 lines on instinct is how `internal/core`
  got 449 flat files.
