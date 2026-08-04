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

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Run state machine research findings

#### Questions

1. Is the run state machine normative?
2. Does it define shutdown recovery and have a production path?
3. What durable boundary must align with execution recovery?

#### Findings

`specs/run-state-machine.md` is normative despite its `draft` status. Its
purpose defines the per-bead lifecycle and its requirements use MUST language.
`RSM-015` requires a merge queue whose context outlives the run context.
`RSM-021` requires a shutdown-drain terminal edge.

`internal/runexec/run.go` implements the pure reactor. `EvShutdownDrain` with
`WorktreeAheadSHA` moves to `RunMerging`; `drainReopen` handles no-commit and
failed-merge paths. `RunBridge.Drain` feeds that event. `drainMergeHook` and
`closeHook` remove cancellation for a draining run. `mergeq.Queue.Submit`
serializes critical sections. No production call to `RunBridge.Drain` exists.

#### Patterns and risks

RSM-021 is the intended terminal spine, but the active shutdown bypasses it.
The in-memory draining flag cannot resolve a post-crash state. RSM recovery
must be reconciled with reconstruction and queue state.

#### Design constraints

- Keep RSM-021 as the single in-process drain-and-terminal spine.
- Add a durable handoff record or derive an equally unambiguous durable state.
- Define rows for no commit, unmerged commit, merge in progress, merged but
  unclosed, and merge failure.
- Each row performs exactly one of redispatch, merge, close, or queue release.
