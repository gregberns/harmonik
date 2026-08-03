# Session

## Current pass

The work is shelved in problem-space. The source inventory and problem-space
record are complete. No contract design, spec draft, task list, or code change
is authorized yet.

## Decisions and evidence

- Step 14 replaces private daemon substrate discovery with a declared
  capability contract.
- The source inventory is 16 private interfaces and 30 production assertions
  in 11 `internal/daemon` files.
- Alpha owns the daemon contract, composition, and shared boot wiring.
- Bravo may take only a later scoped non-daemon handoff.
- `bootState.wireWatchersAndObservers` is shared with Step 12. Serialize any
  edit to that function.
- The retired imperative workflow tail is outside this work.

## Open questions

- Which capabilities remain optional, and what must each missing path do?
- What is the smallest declared contract that preserves tmux and non-tmux
  behavior?
- Which focused double proves each present and missing capability path?

## Next steps

1. Resume this work with the current plan, source inventory, and Step 12
   collision record.
2. Complete decomposition and research before selecting the contract surface.
3. Produce the contract design and tests, then hand it to Alpha for the daemon
   implementation decision.
4. Do not resume `daemon-config-construction` until this work reaches its
   complete handoff.

## Reading order

1. `plans/2026-07-27-delete-and-rewrite/LANES.md`
2. `plans/2026-07-27-delete-and-rewrite/STEP-14-SINGLE-WORKFLOW-TAIL-INVENTORY.md`
3. `01-problem-space.md`
4. `plans/2026-07-27-delete-and-rewrite/STEP-27A-DAEMON-CONFIG.md`
