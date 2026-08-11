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

The scenario now runs the real `queue dry-run` client before submission. The returned plan marks only A as pending. It marks B, C, D, and E as dependency-deferred and reports all six graph edges. The dry run does not create the canonical queue file. The strengthened scenario passed in 28.47 seconds on 2026-08-11.

A second dry run omits A. It reports B, C, and D as pending and reports only their three edges into E. This proves that dry-run checks the requested subgraph. It does not discover an omitted blocker or certify that the submitted set is complete.

### Parent epic terminal behavior

The strengthened live fan graph passed in 31.13 seconds after parent assertions were added. All five children closed. The parent epic stayed open. The daemon emitted exactly one run-scoped `epic_completed` event for it.

The event is a fact, not a ledger transition. `QuiesceArbiter.handleEpicCompleted` routes that fact to the captain wake path. The current design therefore needs no supervisor during successful child processing. It asks for one supervisor decision after the whole child set finishes.

### Explicit validation gate

The fan graph now uses the canonical `standard-bead.dot` topology instead of the reduced review-only fixture. Every child runs implement, `make full`, independent review, and close before merge. The throwaway project supplies a small real `make full` target. It checks that the implementer commit contains the expected artifact and appends durable gate evidence outside the disposable worktree.

The strengthened graph passed in 32.65 seconds. The durable evidence contains five gate passes. This proves “validated and tested before merge” through the running DOT tool-node path. It is no longer inferred from a successful review-only fixture.

The durable event log proved:

- A was the first run.
- B, C, and D started only after A completed.
- At least two branch runs overlapped before the first branch completed.
- E started only after B, C, and D all completed.
- All five beads closed.
- The integration branch advanced by five commits while `main` stayed unchanged.

No supervisor changed queue state or ledger state during the run.

### Parent-derived integration branch extension

Focused command:

```text
go test ./internal/workspace ./internal/daemon -run 'TestEnsureIntegrationBranchCreatesOnceAndConverges|TestRunPlan_BranchingPrecedence' -count=1
```

Result: PASS on 2026-08-11.

The run plan now reads an outgoing parent-child edge. It derives `harmonik/integration/<parent-id>` when a higher branch setting does not set that field. The precedence remains field by field. An explicit `start_from` can coexist with a derived `lands_on`. An explicit `lands_on` can coexist with a derived `start_from`.

The workspace test starts eight branch creation calls at the same time. All calls converge on one branch at the base commit.

The live fan graph fixture now creates an epic and parent-child edges for A through E. It expects all five commits on the derived branch while the configured base stays unchanged.

The first live run found that queue-path hydration copied labels, title, and description from `ShowBead`, but dropped dependency edges. The focused run-plan test had supplied edges directly and did not expose that adapter-to-plan gap. The queue path now carries the complete Bead record.

The second live run passed in 27.79 seconds on 2026-08-11. All five children landed on the parent-derived branch. The configured integration base and `main` stayed unchanged.

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

The scenario created `A -> B` and submitted both once through the Unix-socket CLI. It now lets A implement and commit, then fails A's canonical `make full` node four times until the DOT traversal cap routes the run to failure. The daemon emitted `run_started(A)`, `run_failed(A)`, `queue_group_completed`, then `queue_paused`. B never emitted `run_started`. Both beads were open after the failure.

The integration branch stayed unchanged and no validation-pass record was written. The first strengthened run exposed that the queue briefly offered B after A reopened. The ledger's blocked-claim guard refused it before launch, but the queue had already crossed a safety boundary it could enforce itself.

Delta now propagates a failed item through its in-group dependency descendants during the same durable group-completion decision. Each descendant moves directly from dependency-deferred to failed with `dependency_failed:<blocker>`. Independent chains in the same group are unchanged. The scheduler does not reserve or claim the dependent.

The fixed live scenario passed in 15.79 seconds. B never launched, the queue record names A as its failed dependency, and the queue paused. The core did not choose a repair. It recorded typed failure state and stopped. This is the correct supervisor boundary.

The pre-fix characterization passed in 16.01 seconds on 2026-08-11 because the ledger caught the unsafe offer. The post-fix result above is the current evidence.

## Restart contract trace

Command:

```text
go test ./internal/daemon -run '^TestQueueCancel_(TransitionsToCancelled|NamedQueue_ArchivedOnShutdown)$' -count=1 -v
```

Result: both tests passed on 2026-08-10.

The queue and operator specs describe a graceful stop as a durable `paused-by-drain` transition. The live clean-exit path does something else. `daemon.drainCancelledQueue` waits for current runs, calls `queue.CancelQueueOnShutdown` for every still-active queue, archives the canonical queue, and removes it from the live store. The passing tests defend that cancellation and archive behavior.

This is not crash-recovery evidence. It proves that a normal stop and start cannot continue the submitted graph from its queue record. The next restart has no active canonical queue to load or resume.

### Real daemon stop and start

Command:

```text
go test -tags=scenario ./internal/daemon -run '^TestScenario_QueueSubmit_CleanStopArchivesPendingGraph$' -count=1 -v
```

Result: PASS in 12.53 seconds on 2026-08-11.

The scenario used the full daemon composition root, real Beads, real Git, and the canonical queue store. A handler pause held one open bead at a stable between-run point in an active stream queue. The first clean daemon stop archived `main.json` as `main.json.cancelled-*`. The bead stayed open. A second daemon started against the same project. It loaded no canonical queue, emitted no `run_started`, and left the bead open.

This characterized the old behavior through the real start and stop boundary. The graph did not continue. An agent or operator had to reconstruct and resubmit it.

### Clean restart continuation

Command:

```text
go test -tags=scenario ./internal/daemon -run '^TestScenario_QueueSubmit_CleanStopResumesPendingGraph$' -count=1 -v
```

Result: PASS in 39.24 seconds on 2026-08-11.

The first daemon loaded one active pending queue. A handler pause held execution while clean shutdown persisted `paused-by-drain` with a one-shot restart intent. The second daemon loaded the same canonical queue, restored it to active after dispatched-item recovery and before three-way reconciliation, dispatched the bead once, closed it, and landed its commit. No submit or queue resume occurred between daemon runs.

### Resolved restart pause contract

`QM-055` says a persisted drain pause survives restart and remains paused. It names fresh submit after operator action as the v0.1 recovery path. `QM-002b Class D` goes further. Startup marks every pending or deferred item in that paused queue as failed because it treats the queue as abandoned.

The current daemon also has an operator-resume consumer that changes `paused-by-drain` back to `active`. That is useful before restart. After restart, Class D has already made the pending graph items terminal. A resume can change the queue status, but it cannot continue those items.

Delta now distinguishes the two intents with the durable optional `resume_on_start` field. Clean shutdown sets it and startup consumes it before reconciliation. Explicit operator pause leaves it false and stays paused. Startup no longer destroys pending items in an operator-paused queue.

## Abrupt-crash recovery components

Command:

```text
go test ./internal/daemon -run '^TestRunSessionAdoption_(ARunLaunchedBeforeARestartIsAdoptedAfterIt|ASweptSessionIsAdoptedAsDeadAndItsBeadGoesBackOnTheQueue|TheLiveMonitorGivesTheBeadBackWhenTheAgentFinallyExits)$' -count=1 -v
```

Result: all three tests passed on 2026-08-10.

The startup path has the parts needed for abrupt-crash recovery. A dead recorded run resets its `in_progress` bead to `open`. The startup queue cross-check then changes its queue item from `dispatched` to `pending`. A live independent tmux session is adopted until it exits, then the same durable release returns the item for dispatch. The release matches queue name, queue ID, group index, item index, bead ID, and run ID. This prevents an old monitor from releasing a newer reservation.

### Real process-group death during a fan graph

Command:

```text
go test -tags=scenario ./internal/daemon -run '^TestScenario_QueueSubmit_AbruptCrashResumesFanGraph$' -count=1 -v
```

Result: PASS in 30.03 seconds on 2026-08-11.

The scenario started the first full daemon in a separate process group. It submitted the real epic graph through the socket. It waited for A to complete and for branch runs to start, then sent `SIGKILL` to the daemon and its handler children.

The second full daemon used the same Git repository, Beads database, queue file, and event log. Startup returned the three dispatched branch items to pending. The daemon completed the graph without resubmit. A did not run or merge twice. E started once and only after B, C, and D completed. All five beads closed. The parent-derived integration branch advanced by exactly five commits.

This proves abrupt process death for the current queue and run-session contract. Alpha's durable dispatch replay work can add stronger intent-level assertions when its producer and startup replay paths land.

The restart scenario now also proves the parent decision point survives the process boundary. The parent stays open and the combined event log contains exactly one `epic_completed`. The strengthened scenario passed in 29.08 seconds.

The crash graph now uses the same canonical validation topology as the normal fan graph. After restart, its durable gate evidence contains exactly five passes. No child bypassed validation and no completed child ran the gate twice. The strengthened crash scenario passed in 31.14 seconds.

## Combined queue scenario gate

Command:

```text
go test -tags=scenario ./internal/daemon -run '^TestScenario_QueueSubmit_(DeferredUndefer_hknbjht|FanOutFanIn|FailedBlockerPauses|CleanStopResumesPendingGraph|AbruptCrashResumesFanGraph)$' -count=1 -v
```

Result: all five scenarios passed in 118.51 seconds on 2026-08-11. The clean-restart fixture then disabled the production restart backoff and passed alone in 8.81 seconds. This keeps the same two-daemon behavior while removing 30 seconds of test-only delay.

The strengthened gate passed again in 88.54 seconds after the dry-run and omitted-blocker assertions were added.

After alpha sync, canonical validation was added to the fan, crash, and failed-blocker paths. The full five-scenario gate passed again in 101.43 seconds. This is the current combined result.

The real merge conflict and merge serialization scenarios passed together in 5.57 seconds after all delta changes.

The focused queue, queue-wiring, and workspace packages pass. The broad daemon package has three unrelated failures in `dot_node_baseline_test.go`. Each fixture reached no terminal transition within 50 seconds. Delta does not change those tests or their product path. The operator directed delta to disregard unrelated red tests and continue.

After the final alpha sync at `f0704feee`, the five live scenarios passed in 140.64 seconds. The real merge conflict and serialization scenarios passed in 13.95 seconds. Queue, queue wiring, and workspace package tests passed.

The final static gate found three delta contract gaps. The clean-restart fixture did not cancel its context on every path. The scheduler freeze gate still named the retired shutdown-cancel helper. Dependency failure wrote item status outside the transition owner. Delta fixed all three. Tagged vet, the scheduler freeze gate, the queue status ratchet, focused tests, and the reachability gate then passed.

The restart design made the old shutdown-cancel APIs and their tests unreachable. Delta removed them. The branch resolver returned to the production branch-plan path. The reachability gate reports no new unreachable names.

The last pre-commit `make fast` run reached changed-line lint. It stopped on five findings in `internal/keeper/heartbeat.go` and `internal/workspace/workertrust_isolation_test.go`. Those files are unchanged from alpha. The operator directed delta to disregard red tests outside its scope.

After commit `2881c0c4`, `PATH=/opt/homebrew/bin:$PATH make fast` passed. It ran 8,482 tests across nine packages. All packages passed and 45 tests were skipped by the fast target.

Use one section per scenario. Include the exact command, binary commit, fixture commit, event IDs or stable log paths, branch graph, ledger state, and result.
