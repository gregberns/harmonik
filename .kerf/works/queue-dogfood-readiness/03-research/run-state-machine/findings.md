# Research — Run state machine

## Questions

1. Does the terminal spine give shutdown one close-or-reopen result?
2. Does shutdown drain retain a live context for recovery work?
3. Must a remote run branch synchronize before shutdown merge?
4. Does shutdown still skip the normal gate and outcome emission?

## Findings

- `RSM-020` centralizes the close and reopen ladder. `RSM-022` requires a
  cancellation-free context for a terminal reopen.
- `internal/runloop/runbridge.go` `RunBridge.Drain` sends one shutdown-drain
  event into the run machine. An empty tip reopens. Merge success or no-change
  closes. Every other merge result reopens.
- `drainMergeHook` derives a live context with `context.WithoutCancel`. It
  passes that context to `PreMergeSync` and `runmerge.RunBranchToTarget`.
- `TestRunBridge_DrainSynchronizesWithALiveContextBeforeMerge` proves that
  synchronization receives a live context and that sync failure reopens
  without close.
- `RSM-021` says shutdown drain uses no pre-merge synchronization. This is a
  direct conflict with the corrected remote-safe implementation.

## Patterns to keep

- Shutdown skips the normal gate and approved-outcome emission.
- An uncommitted shutdown run reopens for later dispatch.
- Missing tip, sync failure, and merge failure share the reopen edge.

## Risks and decisions

- Replace the RSM-021 prohibition with a requirement to synchronize when the
  run branch is remote or not locally present. Use a cancellation-free context.
- Require synchronization before a no-change close. Otherwise an absent local
  branch can look like no work.
- Add a remote-worker fixture. The local DOT regression and bridge test do not
  prove an end-to-end remote fetch and merge.
