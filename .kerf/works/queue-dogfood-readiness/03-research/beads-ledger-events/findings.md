# Research — Beads ledger and event artifacts

## Questions

1. What is the minimum pre-selection snapshot?
2. Which sources can close stale graph work?
3. How do scratch results relate to fleet records?
4. What must remain after an unresolved finding?

## Findings

- Preserve the command result and `br show` details used to choose the canary.
  `BI-013` and `BI-015` define the planning and admission distinction.
- Event logs support investigation but cannot override Git or Beads. See
  `BI-023`.
- A scratch result attaches to the assessor report after the controlled run. It
  is not fleet-ledger authority.
- An unresolved condition remains scoped work. It is not hidden by closing an
  old graph record without source evidence.

## Patterns to keep

- Name the exact event file and capture time. Do not write only “events”.
- Check stale intent files before selection. They can represent a terminal write
  that needs reconciliation.

## Risks and decisions

- There is no structured readiness snapshot today. The task design needs a
  small tracked record or report section with the command, source path, time,
  candidate set, selected item, and exclusion reasons.
