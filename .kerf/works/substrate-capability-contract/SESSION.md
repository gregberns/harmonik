# Session

## Current pass

The work is in change design. The problem-space, decomposition, research, and
design artifacts are complete. Independent review approved the design package.
It awaits Alpha review. Do not advance the work to spec drafting until Alpha
accepts the package.

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

1. Alpha reviews the complete change-design package and its review record.
2. After Alpha accepts it, advance to `spec-draft` and write the specification
   changes from the approved design.
3. Keep `daemon-config-construction` shelved until this work completes.

## Reading order

1. `04-design/daemon-contract-design.md`
2. `04-design/step12-serialization.md`
3. `change-design-review.md`
4. `03-research/capability-behavior/findings.md`
