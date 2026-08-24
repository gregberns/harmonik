---
id: workloop-extract-pure-decisions
title: Take runWorkLoop under the three complexity ceilings, one pure decision at a time
type: task
priority: 0
labels: [daemon, run-machine, clear-the-ground]
depends_on: [workloop-name-the-state]
blocks: []
workstream: W2
batch: 2
---

## Problem

`runWorkLoop` is a state machine encoded as control flow inside one function. It is over three
separate ceilings at once, all measured 2026-08-23:

| Linter | Ceiling in `.golangci.yml` | `runWorkLoop` today |
|---|---|---|
| `cyclop` (cyclomatic) | 15 | **161** |
| `gocognit` (cognitive) | 20 | **505** |
| `funlen` (length) | 100 lines / 60 statements | **384 statements** |

All three measured 2026-08-23 evening, with the suppression stripped so the linters could see the
function. Cognitive complexity is the hard one — more than three times the cyclomatic figure,
because it charges for nesting. Driving the cyclomatic number down does not drag it along. `funlen`
counts statements here, not lines, so deleting comments does nothing for it. `workloop-name-the-state` gives its collaborators and its state names; this task removes the
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

1. `runWorkLoop` is under all three ceilings named above: `cyclop` under 15, `gocognit` under 20,
   `funlen` under both its limits. **Strip the `//nolint` directive before you measure** —
   `golangci-lint` obeys it and will report nothing for this function while it is in place, which
   reads as success. `gocognit` run as a standalone tool ignores it. So:
   `~/go/bin/gocognit internal/daemon/` works as-is, and
   `golangci-lint run --enable-only=cyclop ./internal/daemon/...` needs the directive gone first.
   Quote all three numbers in the commit body.
2. Every extracted decision has a table test, and **a deliberate mutation of the production call path
   makes that test fail**. Record one such mutation per extracted decision in the commit body. A test
   that passes whether or not the production code calls the decision has not proved anything.
3. The suppression is **deleted** because the findings are gone — not silenced, not re-keyed. It is
   the `//nolint:gocognit,cyclop,funlen` directive on the line directly above `runWorkLoop` in
   `internal/daemon/scheduler.go`. **It is not in `tools/lintreport/allow.txt`** — an earlier draft
   of this task said it was, and that was wrong. That list is keyed per file and linter today, so no
   `runWorkLoop` entry can exist there in the current format. (`lint-rekey-exclusion-list` is
   changing that key to name a symbol. It lands first. Even after it does, the suppression you want
   is the directive, not a list entry.) The one `scheduler.go` line in
   the allow list is a `gocritic` entry, and it is not yours to remove.
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
