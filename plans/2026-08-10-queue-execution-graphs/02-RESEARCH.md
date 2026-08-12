# Research map

## Alpha integration reconciliation — 2026-08-11

Delta moved from `a8557207e` to alpha integration commit `7cf5c73df`. This added 34 commits.

The queue-relevant additions are transactional group completion, `specs/live-bead-state.md`, typed dispatch intents and results, the filesystem dispatch-intent store, and the reviewed C21 startup-replay design.

Alpha has not wired the scheduler producer or startup replay. The C21 design requires them to land together. A producer must not create an intent that startup cannot replay. The branch therefore has the storage primitive but does not yet use it as the active dispatch recovery authority.

This changes the planned crash test. The final test must observe the C21 intent phases and replay results. A test based only on the older session run record would defend a transitional path.

The reconciliation also confirmed two earlier findings remain current. `workspace.IntegrationBranchName` still has no production caller. Both generated dispatch skills still describe streams as head-of-line serial and waves as the only concurrent group form. The real stream graph scenario disproves that text.

## Sources to inspect

- Prior plans that mention queue groups, dependencies, epics, branch targets, work loops, merge, and supervisor activity.
- Normative specs for queue, bead integration, workspace, execution, process life cycle, DOT, and events.
- Queue admission and persistence code.
- Ledger adapter readiness and dependency code.
- Scheduler and work-loop dispatch code.
- Workspace branch creation and merge code.
- Scenario and digital-twin harnesses.

## Trace format

For each claim, record:

1. The submitting command or API value.
2. The stored queue value.
3. The readiness decision and its owner.
4. The dispatch event.
5. The worktree and branch base.
6. The validation result.
7. The merge and ledger transitions.
8. The next readiness decision.

## Guardrail

Separate three facts in every finding:

- The spec says it should work.
- The code appears to implement it.
- A real runtime run proves it works.

## Findings in progress

### Dependency execution exists

`specs/queue-model.md` `QM-025` defines `deferred-for-ledger-dep`. Submission checks `blocks` edges between items in the same group. The dispatcher checks deferred items on each loop tick. It moves an item back to `pending` only when all in-group blockers are terminal or no longer open in the ledger.

The production path is split across:

- `internal/queue/rpc.go` builds the deferred set at submit time.
- `internal/queuewiring/beadledger.go` maps Beads edge direction into `BlocksEdge(blocker, blocked)`.
- `internal/queue/state.go` `ReevaluateDeferred` releases items after blockers resolve.
- `internal/daemon/scheduler.go` runs re-evaluation and dispatch.

This is deterministic mechanism. It does not make a semantic scheduling choice.

### One group can express a dependency graph

A wave makes all dependency-free items eligible up to capacity. A stream returns one eligible item per queue scan. Both group kinds skip deferred items. Both can therefore represent `A -> [B, C, D] -> E` when all five beads are submitted in one group and the ledger holds the edges.

The current queue does not discover and add an epic's children. The submitter must name every item. Submission only uses dependency edges between submitted siblings for initial deferral. A dependency outside the group is not a queue item and does not become work by itself.

### Current stream guidance has drift

`specs/queue-model.md` says a stream can dispatch concurrently when capacity is greater than one. `internal/queue/state.go` supports this because it skips dispatched items and returns the next pending item on later scans. The socket-driven fan-out scenario proves this at runtime. The loaded `orchestrator-rules` and `harmonik-dispatch` skills still say stream mode has head-of-line blocking and needs wave mode for concurrency. The CLI does not expose a `queue submit --wave` flag. A custom queue JSON file is required to submit a wave. The generated skills are stale.

### What the submitting agent must do

The short form is:

```text
harmonik queue dry-run --beads A,B,C,D,E
harmonik queue submit --beads A,B,C,D,E
```

The agent must submit every child bead that it wants the queue to execute. The agent must create the Beads `blocks` edges first. The agent must not submit only the epic and expect child discovery. The simple `--beads` form creates one stream group. That group can still run independent ready items concurrently when the daemon and queue worker limits are greater than one.

Dry-run does not prove that the submitted set is complete. It examines dependency edges only when both endpoints are in the request. In a dry run that omitted A from `A -> [B, C, D] -> E`, it reported B, C, and D as pending. It reported only their three edges into E. The planning agent therefore owns submission-set completeness under the current contract.

The ledger prevents a dependency cycle at its write boundary. A real `br dep add` accepted `B depends on A` and rejected the closing `A depends on B` edge with exit 5. A following `br dep cycles --json` reported zero cycles. The queue does not need to infer or repair a cycle that the ledger refuses to store.

A JSON queue document is useful when the agent needs more than one ordered group, an explicit wave, a named queue, queue-specific workers, per-item context, or per-item workflow settings. The dependency graph remains in Beads. Queue groups add execution-plan barriers and append rules. They do not replace ledger dependencies.

### Epic integration branch composition and promotion

`specs/workspace-model.md` `WM-006` requires a parent bead to derive `harmonik/integration/<parent>`. Delta now composes that rule into the run plan and creates the branch when it does not exist.

Each child lands on that branch through the normal task-branch merge. The live graph proves that all five child changes collect there.

The daemon does not promote the completed epic branch to another branch. `maybeEmitEpicCompleted` emits the completion fact only. `WM-007` says that Harmonik's contract ends when the integration branch holds one commit per task. It leaves the next merge to developer or operator policy. The project branch rule also requires a human pull request into `main`.

This means the lack of automatic promotion is current policy, not a queue defect. A future contract can add mechanical epic-branch promotion into a project integration branch. It must keep the human boundary before `main`. It must also define the promotion target, validation gate, serialization, restart identity, and conflict path first.
