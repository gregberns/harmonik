# Cycle Dependency Research

## Questions

- Which dependencies are required?
- How can construction validate dependencies without breaking callers?
- Which effects can use explicit no-op implementations?

## Findings

The cycle needs pane writes, context state, activity observations, gate observations, a handoff document, a journal store, and a clock. Respawn and event emission can remain optional through explicit no-op values.

`NewCycler` returns only a pointer and has many callers. A new validated constructor can return an error. The legacy constructor can adapt old configuration until migration ends.

Validation must not call a dependency. It must detect typed nil values and return stable missing-contract names.

## Options

- Change `NewCycler` in place. This creates a large flag-day migration.
- Add `NewCyclerWithDeps`, migrate production first, then migrate tests.

Use the staged constructor.

## Risks

The clock must resolve before adapters capture it. The pane capture operation is not an automatic-cycle dependency.
