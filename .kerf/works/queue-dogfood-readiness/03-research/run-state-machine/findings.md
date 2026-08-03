# Run state machine research findings

## Questions

1. Is the run state machine normative?
2. Does it define shutdown recovery and have a production path?
3. What durable boundary must align with execution recovery?

## Findings

`specs/run-state-machine.md` is normative despite its `draft` status. Its
purpose defines the per-bead lifecycle and its requirements use MUST language.
`RSM-015` requires a merge queue whose context outlives the run context.
`RSM-021` requires a shutdown-drain terminal edge.

`internal/runexec/run.go` implements the pure reactor. `EvShutdownDrain` with
`WorktreeAheadSHA` moves to `RunMerging`; `drainReopen` handles no-commit and
failed-merge paths. `RunBridge.Drain` feeds that event. `drainMergeHook` and
`closeHook` remove cancellation for a draining run. `mergeq.Queue.Submit`
serializes critical sections. No production call to `RunBridge.Drain` exists.

## Patterns and risks

RSM-021 is the intended terminal spine, but the active shutdown bypasses it.
The in-memory draining flag cannot resolve a post-crash state. RSM recovery
must be reconciled with reconstruction and queue state.

## Design constraints

- Keep RSM-021 as the single in-process drain-and-terminal spine.
- Add a durable handoff record or derive an equally unambiguous durable state.
- Define rows for no commit, unmerged commit, merge in progress, merged but
  unclosed, and merge failure.
- Each row performs exactly one of redispatch, merge, close, or queue release.
