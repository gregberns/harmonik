# Session

## Current pass

The work is in integration. The problem-space, decomposition, research, change
design, specification draft, and integration check are complete. Independent
review approved the integrated package. Advance only to the task pass.

## Decisions and evidence

- Step 14 replaces private daemon substrate discovery with a declared
  capability contract.
- The live source inventory is 16 private interfaces and 30 production
  assertions in 11 `internal/daemon` files.
- The approved table in `04-design/daemon-contract-design.md` gives every
  capability a consumer port, selected-mode requirement, absent result, and
  focused proof.
- The handler boundary stays narrow. This work does not add tmux, pane,
  session-sweep, or launch-cap methods to handler contracts.
- Alpha removes the unused run-session interface, `SpawnRunSession`, its
  session-name helper, `runSessionID`, its `SpawnWindow` branch, and the
  run-only tests and documentation. Input buffers use each run's captured pane
  target. Two shared-session runs must produce distinct valid buffer names.
- `bootState.wireWatchersAndObservers` is shared with Step 12. Alpha makes one
  integrated edit after the Step 12 base change. The edit keeps every disabled
  subsystem switch effective.
- Alpha owns the daemon contract, composition, and shared boot wiring. Bravo
  may take only a later scoped non-daemon handoff.

## Next steps

1. Write ordered implementation tasks. Keep the two validation beads attached.
2. Preserve construction and operation boundaries in every task.
3. Keep `daemon-config-construction` shelved until this work completes.

## Reading order

1. `06-integration.md`
2. `integration-review.md`
3. `05-spec-drafts/process-lifecycle.md`
4. `04-design/step12-serialization.md`
