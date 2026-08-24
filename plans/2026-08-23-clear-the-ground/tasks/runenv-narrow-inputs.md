---
id: runenv-narrow-inputs
title: RunEnv has 29 fields and SharedHandles 18, so no phase can say what it actually needs
type: task
priority: 1
labels: [daemon, run-machine, clear-the-ground]
depends_on: [run-goroutine-supervisor]
blocks: [beadrunone-extract-phases]
workstream: W2
batch: 2
---

## Problem

`internal/runloop.RunEnv` has **29 fields** and `SharedHandles` has **18** (counted 2026-08-23; the
2026-08-22 review reported the same figures, so neither has moved). Every phase of a run receives
both wholesale.

The cost is that ownership is invisible to the compiler. Nothing can tell you which phase reads
which field, so no field can be removed with confidence, and every new requirement is met by adding
a thirtieth field. This is the same disease as `runWorkLoop`'s 22 parameters, one layer down, and it
is why that signature is so wide in the first place.

`internal/daemon.Config` has 40 fields and is the third instance of the pattern. It is out of scope
here — noted so the next reader does not think it was missed.

## Scope

`internal/runloop` — `RunEnv`, `SharedHandles`, and the phase entry points that take them.

Give each phase an input type carrying only what that phase reads. The wide structs may remain as the
composition at the top; what must stop is passing them down whole.

## Done when

1. No phase entry point takes `RunEnv` or `SharedHandles` whole.
2. Each phase's input type has fewer than 10 fields.
3. Removing any field from a phase input breaks the compile of that phase and nothing else — this is
   the property being bought, so prove it once in the commit body by deleting a field and showing the
   failure is local.
4. No behaviour change.

## Limits

- **Do not add a field to `RunEnv` or `SharedHandles` in this task**, for any reason.
- Do not introduce an interface where a struct of the needed fields will do. A consumer-owned input
  type is the goal; an abstraction over it is not.
- `daemon.Config` (40 fields) is a separate task. Do not start it here.
