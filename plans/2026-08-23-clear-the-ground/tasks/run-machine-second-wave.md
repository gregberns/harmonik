---
id: run-machine-second-wave
title: The next four functions after runWorkLoop, same treatment, same owner
type: task
priority: 1
labels: [daemon, run-machine, clear-the-ground]
depends_on: [workloop-extract-pure-decisions]
blocks: []
workstream: W2
batch: 2
---

## Problem

`runWorkLoop` is the worst offender but not a lone one. Measured 2026-08-23:

| Function | File | Lines | Complexity |
|---|---|---|---|
| `driveDotWorkflow` | `internal/daemon/dot_cascade_core.go` | 495 | 94 |
| `beadRunOne` | `internal/daemon/workloop.go` | 501 | 77 |
| `runAgentLaunch` | `internal/daemon/agentlaunch.go` | 446 | 69 |
| `dispatchDotAgenticNode` | `internal/daemon/dot_cascade_core.go` | 408 | 69 |

`beadRunOne` was 2,289 lines at the start of the delete-and-rewrite program and is 501 now, so this
is not untouched ground — it is ground where the work stopped partway.

The charter names a specific reason `beadRunOne` is large: the reviewer is welded into the work loop
instead of being a switchable stage. Unfusing it is explicitly in scope for the program.

## Scope

The four functions above and the files holding them.

Same method as `workloop-extract-pure-decisions`: name the state, lift one pure decision at a time,
prove each with a mutation.

**Two of the four live in the same file.** `driveDotWorkflow` and `dispatchDotAgenticNode` are both
in `dot_cascade_core.go`; do them in sequence, not in parallel.

## Done when

1. All four are **under complexity 30**.
2. Each extracted decision has a table test that a deliberate mutation of the production path breaks.
3. The `gocognit`/`cyclop` entries for all four are deleted from `tools/lintreport/allow.txt` because
   the findings are gone.
4. For `beadRunOne` specifically: the reviewer is a stage the loop calls, not logic inlined into it.

## Limits

- **Single owner for this whole task.** These four functions and `runWorkLoop` touch overlapping
  files; two lanes working here will collide.
- Do not start before `workloop-extract-pure-decisions` has landed. It establishes the method and the
  review standard, and doing both at once is how the previous attempts got too large to review.
- Do not move any of these into a new package.
- `run() int` in `cmd/harmonik/main.go` is 770 lines and belongs to the command-line tool work, not
  here. Do not do it twice.
