# Change Design — Beads integration

## Current state

`br ready` is planning input. Queue submit validates each item with `br show`.
Beads owns coarse state. Git owns completion. Terminal writes use durable intents.
No structured readiness snapshot exists.

## Target state

Add a retained readiness snapshot before canary selection.
It records capture time, commands, output paths, candidate set, selected item,
exclusion reasons, selected-item data, event-log path, and intent inspection.
It records each stale graph finding, checked source path, and disposition.

At implementation triage, a confirmed remaining condition becomes a new scoped
open record with current source evidence. Planning itself does not close,
create, or change fleet ledger state.
The selected item is open, repeat-safe, and suitable for one local stream run.
Scratch results remain evidence. They do not change fleet state.

## Rationale

The snapshot separates planning, admission, and completion authority.
It makes stale-finding decisions reviewable without changing fleet state.

## Requirements traceability

- `02-components.md`: beads integration and ledger-event artifacts.
- Research: `beads-integration` and `beads-ledger-events` findings.
- Existing requirements: `BI-013`, `BI-013b`, `BI-013c`, `BI-021` through `BI-023`.
