# Evidence

## S1 proof: two-bead serial chain

Command:

```text
go test -tags=scenario ./internal/daemon -run '^TestScenario_QueueSubmit_DeferredUndefer_hknbjht$' -count=1 -v
```

The first run passed in 20.62 seconds. The strengthened socket-driven run passed in 16.96 seconds on 2026-08-10.

The strengthened scenario used a real daemon, the real Unix socket, the real `harmonik queue submit --beads` client path, real `br` 0.2.10, a real Git repository, real worktrees and merge path, and the Claude twin handler. It created A and B with `B depends on A`, submitted both once, observed B as `deferred-for-ledger-dep`, then observed the event order `run_started(A)`, `run_completed(A)`, `run_started(B)`, `run_completed(B)`. Both beads closed. The integration branch advanced by two commits while `main` stayed unchanged.

This proves the normal CLI and work loop can continue a serial dependency without supervisor action.

The earlier test-only submit adapter was removed from this path. Daemon composition now supplies the production Beads adapter for both submit-time deferral and later re-evaluation.

The scenario now creates its throwaway project under `/tmp`, so the macOS Unix-socket path fits and the CLI transport is exercised.

## S2 proof: fan-out and fan-in

Command:

```text
go test -tags=scenario ./internal/daemon -run '^TestScenario_QueueSubmit_FanOutFanIn$' -count=1 -v
```

Result: PASS in 27.15 seconds on 2026-08-10.

The scenario created `A -> [B, C, D] -> E` in real Beads. It submitted all five IDs once through the real Unix-socket CLI. The client created the normal stream group. The daemon ran with a global and per-queue concurrency ceiling of three.

The durable event log proved:

- A was the first run.
- B, C, and D started only after A completed.
- At least two branch runs overlapped before the first branch completed.
- E started only after B, C, and D all completed.
- All five beads closed.
- The integration branch advanced by five commits while `main` stayed unchanged.

No supervisor changed queue state or ledger state during the run.

## Existing conflict and merge-serialization evidence

Command:

```text
go test -tags=scenario ./internal/daemon -run '^TestScenario_MultiBead_(ConflictSkipsButOthersProceed|SerializedNCompletion)$' -count=1 -v
```

Result: both tests passed on 2026-08-10.

These tests use the real work loop and real Git merge path with a deterministic handler. One proves that a conflicting bead reopens while three clean siblings still land. The other proves five near-simultaneous completions serialize their merges without losing a commit.

## S3 proof: failed blocker

Command:

```text
go test -tags=scenario ./internal/daemon -run '^TestScenario_QueueSubmit_FailedBlockerPauses$' -count=1 -v
```

Result: PASS in 5.80 seconds on 2026-08-10.

The scenario created `A -> B`, submitted both once through the Unix-socket CLI, and made A fail through the Claude twin. The daemon emitted `run_started(A)`, `run_failed(A)`, `queue_group_completed`, then `queue_paused`. B never emitted `run_started`. Both beads were open after the failure.

The core did not choose a repair. It recorded typed failure state and stopped. This is the correct supervisor boundary.

## Restart contract trace

Command:

```text
go test ./internal/daemon -run '^TestQueueCancel_(TransitionsToCancelled|NamedQueue_ArchivedOnShutdown)$' -count=1 -v
```

Result: both tests passed on 2026-08-10.

The queue and operator specs describe a graceful stop as a durable `paused-by-drain` transition that can be resumed. The live clean-exit path does something else. `daemon.drainCancelledQueue` waits for current runs, calls `queue.CancelQueueOnShutdown` for every still-active queue, archives the canonical queue, and removes it from the live store. The passing tests defend that cancellation and archive behavior.

This is not crash-recovery evidence. It proves that a normal stop and start cannot continue the submitted graph from its queue record. The next restart has no active canonical queue to load or resume.

### Real daemon stop and start

Command:

```text
go test -tags=scenario ./internal/daemon -run '^TestScenario_QueueSubmit_CleanStopArchivesPendingGraph$' -count=1 -v
```

Result: PASS in 12.53 seconds on 2026-08-11.

The scenario used the full daemon composition root, real Beads, real Git, and the canonical queue store. A handler pause held one open bead at a stable between-run point in an active stream queue. The first clean daemon stop archived `main.json` as `main.json.cancelled-*`. The bead stayed open. A second daemon started against the same project. It loaded no canonical queue, emitted no `run_started`, and left the bead open.

This proves the consequence through the real start and stop boundary. The graph does not continue. An agent or operator must reconstruct and resubmit it.

## Abrupt-crash recovery components

Command:

```text
go test ./internal/daemon -run '^TestRunSessionAdoption_(ARunLaunchedBeforeARestartIsAdoptedAfterIt|ASweptSessionIsAdoptedAsDeadAndItsBeadGoesBackOnTheQueue|TheLiveMonitorGivesTheBeadBackWhenTheAgentFinallyExits)$' -count=1 -v
```

Result: all three tests passed on 2026-08-10.

The startup path has the parts needed for abrupt-crash recovery. A dead recorded run resets its `in_progress` bead to `open`. The startup queue cross-check then changes its queue item from `dispatched` to `pending`. A live independent tmux session is adopted until it exits, then the same durable release returns the item for dispatch. The release matches queue name, queue ID, group index, item index, bead ID, and run ID. This prevents an old monitor from releasing a newer reservation.

These are focused component tests. They do not yet prove that a full dependency graph survives a process kill. The required child-process scenario remains open.

Use one section per scenario. Include the exact command, binary commit, fixture commit, event IDs or stable log paths, branch graph, ledger state, and result.
