# Research — Execution model

## Questions

1. What durable fact proves that a DOT run committed before shutdown?
2. Must merge finish before the bead can close?
3. How does restart recover a commit when its terminal ledger write did not run?
4. Does the model distinguish a worktree commit from an execution checkpoint?

## Findings

- `EM-016`, `EM-023`, and `EM-024` make a checkpoint commit on the run branch
  the durable run-state authority.
- `EM-052` orders normal success as merge, push, then bead close.
  `EM-053` orders merge, rebase, build, and push failure to reopen rather than
  close.
- `EM-031` and `EM-031a` require restart reconstruction from git and Beads.
  JSONL is not the recovery authority.
- A run branch alone cannot recover the dispatch head, merge target, or remote
  worker location. Those facts are not durable in Beads, and a daemon-local
  registry disappears on restart.
- `internal/daemon/workloop.go` `beadRunOne` detects cancellation after the
  DOT driver returns. It resolves the worktree tip and sends it to
  `RunBridge.Drain`.
- The focused DOT shutdown test proves that a local committed worktree is
  merged before the bead closes. It does not exercise a real remote worker.

## Patterns to keep

- Git holds the durable release evidence. Beads holds the coarse terminal
  state.
- Merge-before-close is already the normal release rule.
- `EM-025a` uses the useful order: make the durable ref change, then emit the
  observable projection.

## Risks and decisions

- The spec does not name the state where an agent commit exists but release is
  unfinished. Define a Git-backed, immutable release claim on the checkpoint
  transition. It must carry the dispatch head, resolved merge target, and the
  optional remote endpoint used for synchronization.
- `EM-052` needs a cross-reference to the shutdown-drain edge. That edge must
  synchronize a remote run branch before merge and reopen on failure.
- A crash after merge and before bead close needs an explicit reconstruction
  rule. It must not cause duplicate release or silent redispatch.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Execution model research findings

#### Questions

1. What durable facts identify a committed but unmerged DOT run?
2. What does graceful shutdown do now?
3. Can restart distinguish terminal recovery from a new dispatch?
4. Where does queue advancement occur after a run terminal?

#### Findings

`specs/execution-model.md` `EM-052` defines merge, push, close, and terminal
emission as an ordered success ladder. `EM-031` and `EM-031a` reconstruct from
Git and Beads. `EM-015f` advances a queue after a run terminal. The spec has
no durable state for a committed but unmerged run.

`runWorkLoop` in `internal/daemon/scheduler.go` waits to
`shutdownDrainTimeout`, kills windows, then `drainCancelledQueue` cancels and
archives active queues. `evaluateGroupAdvanceWithOutcome` is the current
terminal-to-queue handoff.

`internal/runexec/run.go` has an `EvShutdownDrain` branch. A
`WorktreeAheadSHA` enters `RunMerging`; no SHA reopens for requeue. The bridge
method `RunBridge.Drain` in `internal/runloop/runbridge.go` has no production
caller. The existing intended drain path is therefore not wired into shutdown.

#### Patterns and risks

The code has cancellation/archive shutdown and an unwired drain-and-merge
model. Reactor state is in memory, so it cannot be the restart discriminator.

#### Design constraints

- One durable discriminator must bind run, bead, queue item, branch tip, and
  terminal-ladder stage.
- One owner must either drain to merge and close or preserve reviewable
  recovery. Both a second dispatch and a second merge or close are forbidden.
- Reconstruction and queue advance must follow that selected action once.
- An isolated stop-window proof must exercise a committed DOT branch.
