---
id: run-goroutine-supervisor
title: No single owner starts, watches and terminates a run's goroutines
type: task
priority: 1
labels: [daemon, run-machine, clear-the-ground]
depends_on: [workloop-name-the-state]
blocks: [runenv-narrow-inputs]
workstream: W2
batch: 2
---

## Problem

Run goroutines have no one lifecycle owner. They are started from inside the work loop, watched in
places, and terminated by whichever path notices first. The 2026-08-22 review recorded this as
unstarted work and named it the prerequisite for narrowing run inputs — you cannot say which phase
owns which handle until something owns the phases.

There is live evidence this is not theoretical: the ledger carries an open report that daemon tests
leak production-path daemons which outlive the test run, keep firing the proactive Go cache reap, and
wipe the shared build cache during *later* runs. A run whose goroutines nobody owns is a run that can
outlive its own process.

## Scope

`internal/daemon` — the goroutine starts inside the work loop and the paths that end them.

One supervisor owns: starting each run's goroutines, observing their terminal result, and ensuring
they are stopped exactly once. The terminal result becomes a value the supervisor returns, not a side
effect several paths race to record.

## Done when

1. Exactly one place starts a run's goroutines and exactly one place ends them.
2. Every run's terminal outcome is observable from the supervisor as a value.
3. A test proves no goroutine outlives its run: start a run, complete it, and assert the goroutine
   count returns to its pre-run value.
4. Cancelling a run mid-flight leaves no goroutine behind — same assertion, cancelled path.

## Limits

- **This task changes ownership, not behaviour.** Terminal outcomes must be identical.
- Do not fix the daemon test leak here. That is its own report in the ledger; this task makes the fix
  possible, and conflating them makes both harder to review.
- Do not add a new package. Ownership first; placement later.
- Do not build a general supervision framework. One owner for run goroutines, nothing wider.
