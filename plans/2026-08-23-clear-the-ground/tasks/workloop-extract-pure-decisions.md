---
id: workloop-extract-pure-decisions
title: Take runWorkLoop's complexity from 158 to under 30, one pure decision at a time
type: task
priority: 0
labels: [daemon, run-machine, clear-the-ground]
depends_on: [workloop-name-the-state]
blocks: []
workstream: W2
batch: 2
---

## Problem

`runWorkLoop` carries cyclomatic complexity 158 — a state machine encoded as control flow inside one
function. `workloop-name-the-state` gives its collaborators and its state names; this task removes the
branching.

There is a worked example in the repo of the shape wanted:
`internal/orchestrator.SelectNextQueue` is a pure, snapshot-based selector that the daemon delegates
to. Use its shape. **Do not re-implement it** — the 2026-08-22 review confirmed from source that it
already exists and already has a delegating caller, contrary to what the rejected decomposition plan
claimed.

## Scope

`internal/daemon/scheduler.go`, plus a home for each extracted decision.

Work one decision at a time. Each extraction is: identify a branch cluster that is a decision over
values, lift it to a pure function over a snapshot, delegate to it, and cover it with a table test.

## Done when

1. `runWorkLoop`'s cyclomatic complexity is **under 30**.
2. Every extracted decision has a table test, and **a deliberate mutation of the production call path
   makes that test fail**. Record one such mutation per extracted decision in the commit body. A test
   that passes whether or not the production code calls the decision has not proved anything.
3. The `gocognit` and `cyclop` entries for `runWorkLoop` are **deleted** from
   `tools/lintreport/allow.txt` because the findings are gone — not silenced, not re-keyed.
4. Daemon behaviour is unchanged.

## Limits

- **One decision per commit.** This function has resisted every previous attempt made in one pass.
- **Do not create a port, an interface or a seam that has one caller and no test that needed it.**
  Say in the commit body what each new abstraction buys — a second caller, a test seam with no other
  route, or a boundary a linter requires.
- Do not move `runWorkLoop` or `scheduler.go` into a new package. The review rejected scheduler and
  work-loop package movement, and the reason still holds: moving the file does not fix the function.
- Do not touch `driveDotWorkflow`, `beadRunOne` or `runAgentLaunch` here. They are
  `run-machine-second-wave` and they must be serialized behind this task — they share files.
