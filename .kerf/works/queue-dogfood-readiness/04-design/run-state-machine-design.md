# Change Design — Run state machine

## Current state

`RSM-020` centralizes close-or-reopen behavior. `RSM-022` requires a live
context for terminal reopen. `RSM-021` says shutdown drain has no pre-merge
synchronization. `RunBridge.Drain` now synchronizes before merge with a
cancellation-free context and reopens on sync or merge failure.

## Target state

Replace the conflicting `RSM-021` clause:

1. Shutdown drain remains a distinct terminal edge. It skips the normal gate
   and normal outcome emission.
2. Tip resolution, remote synchronization, merge, close, and reopen use a
   cancellation-free context.
3. The run branch synchronizes before merge, including a path that could report
   no change.
4. Synchronized merge or no-change enters the existing close ladder.
5. Missing tip, sync failure, merge failure, and close failure enter the
   existing reopen ladder.
6. Drain completes only after one terminal close-or-reopen result.

Add a remote-worker fixture. It proves fetch, merge, and close ordering. Add a
sync-failure case that proves reopen without close.

## Rationale

An absent local branch can look like no work. Synchronization makes no-change
meaningful. The terminal spine keeps one close-or-reopen result and prevents a
shutdown-only bypass.

## Requirements traceability

- `02-components.md`: live context, remote sync, sync-failure reopen, one
  terminal spine.
- `03-research/run-state-machine/findings.md`: `RSM-020`, `RSM-021`, and
  `RSM-022`; `RunBridge.Drain` and `drainMergeHook`.
- `execution-model-design.md`: release rules from `EM-052` and `EM-053`.
- `operator-nfr-design.md`: drain ordering from `ON-008`, `ON-027`, and
  `ON-027a`.
