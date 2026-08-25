---
id: workloop-name-the-state
title: runWorkLoop takes 22 parameters on one line and keeps its state in a stack frame
type: task
priority: 0
labels: [daemon, run-machine, clear-the-ground]
depends_on: []
blocks: [workloop-extract-pure-decisions, run-goroutine-supervisor]
workstream: W2
batch: 2
---

## Problem

`internal/daemon/scheduler.go` `runWorkLoop` is 816 lines, cyclomatic complexity 161, and takes **22
parameters** declared on a single source line. It is the largest function and the widest signature in
the repo, and it drives the whole dispatch loop.

It did not get better this week. The comment cut took it from 1,432 lines to 816 and left the
parameter count and the complexity exactly where they were. Line count is not the measure here.

Eleven of the 22 parameters are `*Port` structs of function fields — `loopLifecycle`,
`ledgerRepair`, `scheduleInput`, `coordinatorReap`, `diskReclaim`, `eagerRefill`, `governor`,
`capacity`, `queueSurface`, `dispatchGates`, plus the `governorEnabled` flag that belongs with
`governor`. They are declared across seven files (`scheduler.go`, `dispatchports.go`,
`loopmaintenance.go`, `scheduletick.go`, `diskcheck_hksxlb.go`, `eagerfill_em063.go`,
`movementgovernor.go`). Each is genuinely used inside the body — between 2 and 28 references — so
this is a grouping job, not a forwarding cleanup.

The loop's mutable state is also unnamed: `claimSem`, `lastSeenPauseEpoch`, `rrCursor`, `wg` and the
rest live as locals in an 816-line frame, which is why nothing can be extracted without dragging the
frame with it.

## Scope

- `internal/daemon/scheduler.go` — `runWorkLoop` and the port types declared there.
- The six other files declaring loop port types, listed above.
- `internal/daemon/loopmaintenance.go` — `newLoopMaintenance` already takes 12 of the same
  collaborators and is the nearest existing example of the grouping.

Two moves, in this order:

1. Give the eleven collaborator ports one named type. They are the loop's collaborators; name them
   as such.
2. Give the loop's mutable state a named type with the locals as fields.

## Done when

1. `runWorkLoop`'s signature is **6 parameters or fewer**.
2. The loop's mutable state is a named type, not a set of locals.
3. `internal/daemon` behaviour is unchanged — the existing daemon tests pass without modification to
   their assertions.
4. The complexity suppression on `runWorkLoop` is **not** touched. It is the
   `//nolint:gocognit,cyclop,funlen` directive on the line directly above the function in
   `internal/daemon/scheduler.go`. (An earlier draft of this task placed it in
   `tools/lintreport/allow.txt`. It is not there — that list is keyed per file and linter today.
   `lint-rekey-exclusion-list` is changing the key to name a symbol, but the suppression on this
   function is the directive either way.) The directive comes out when the complexity actually falls, in
   `workloop-extract-pure-decisions`, and not before.

## Limits

- **Do not extract any decision logic in this task.** This one names things. Splitting the work in
  two is what makes each half reviewable.
- Do not move anything to a new package. That needs `lint-rekey-exclusion-list` and is separate work.
- Do not report progress as a line count. The acceptance test is the parameter count.
