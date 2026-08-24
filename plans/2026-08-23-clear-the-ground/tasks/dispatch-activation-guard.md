---
id: dispatch-activation-guard
title: Make it impossible to wire the crash-safe dispatch producer by accident
type: task
priority: 0
labels: [daemon, dispatch, gate, clear-the-ground]
depends_on: []
blocks: []
workstream: W0
batch: 1
---

## Problem

The crash-safe dispatch subsystem has typed decisions, real tests, and a durable intent store — and
no production caller. `internal/dispatch/result.go` records this in source: `dispatchstore.Store.Create`
is called from tests only. Meanwhile boot **refuses** any unresolved stored intent and several replay
actions are deliberately unimplemented.

The consequence, from the 2026-08-22 review: writing the first production intent today can
permanently wedge restart. The subsystem must stay inert until activation is designed as its own
piece of work.

Right now nothing enforces that. An agent reading the code sees a finished-looking subsystem with an
obvious missing call and helpfully adds it.

## Scope

- `internal/dispatch/`, `internal/dispatch/dispatchstore/`.
- A new gate, placed with the other structural gates so `make fast` runs it.

The guard asserts that no non-test file in the production tree calls `dispatchstore.Store.Create`,
and fails loudly with an explanation if one appears.

## Done when

1. The gate passes on the current tree.
2. The gate FAILS when a temporary production call to `Store.Create` is added — verified by actually
   adding one, running the gate, and removing it. Record the observed failure output in the commit
   body.
3. The failure message states *why* the prohibition exists — that boot rejects unresolved intents and
   replay is partial — not merely that a rule was broken. An agent that hits this gate must
   understand it is a design gap, not a lint nit.
4. `internal/dispatch/result.go`'s existing note points at the gate, so the two do not drift.

## Limits

- **Do not delete or weaken the dispatch code.** It is valuable and it is being preserved
  deliberately.
- **Do not implement the missing replay actions.** Activation is a separate program.
- Do not make the guard a comment or a doc note. It must fail a build.
