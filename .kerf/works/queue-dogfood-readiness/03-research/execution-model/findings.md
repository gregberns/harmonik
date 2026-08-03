# Execution model research findings

## Questions

1. What durable facts identify a committed but unmerged DOT run?
2. What does graceful shutdown do now?
3. Can restart distinguish terminal recovery from a new dispatch?
4. Where does queue advancement occur after a run terminal?

## Findings

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

## Patterns and risks

The code has cancellation/archive shutdown and an unwired drain-and-merge
model. Reactor state is in memory, so it cannot be the restart discriminator.

## Design constraints

- One durable discriminator must bind run, bead, queue item, branch tip, and
  terminal-ladder stage.
- One owner must either drain to merge and close or preserve reviewable
  recovery. Both a second dispatch and a second merge or close are forbidden.
- Reconstruction and queue advance must follow that selected action once.
- An isolated stop-window proof must exercise a committed DOT branch.
