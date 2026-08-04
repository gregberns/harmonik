# Execution model change design

## Current state

`specs/execution-model.md` defines run outcomes that queue item state consumes.
It also owns the daemon dispatch caller for deferred recovery.

## Target state

No normative execution-model text changes in this work.

Queue transitions retain the current mapping from run outcome to item outcome.
The new queue surface accepts named causes. It does not claim that every item
transition came from a completed run.

The daemon call to deferred recovery is outside Bravo ownership. Bravo supplies
a store-based queue API for Alpha to wire later. Until then, this one source is
not counted as durable coverage.

## Rationale

Startup repair, retry, and pre-claim failure are valid item-transition causes.
A run-outcome-only interface would force callers to provide false inputs. The
separate handoff preserves the no-daemon-edit lane boundary.

## Requirements traceability

| Requirement | Design response |
| --- | --- |
| Existing run outcome meaning | Keep the current completed and failed mappings. |
| Deferred recovery each tick | Preserve the existing helper and provide a transactional replacement for Alpha. |
| No daemon change | Do not change the existing scheduler caller in this work. |
