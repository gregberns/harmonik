# Ownership decision

## Current ownership model

### Planning agent or crew

- Decompose the epic into child beads.
- Define every `blocks` edge before submission.
- Reduce avoidable file overlap when it understands the planned work.
- Put explicit branch overrides in bead bodies only when the project or epic needs them.
- Dry-run and submit every child ID in one request.

The queue does not discover an epic's children. Submitting only the epic is not enough.

### Deterministic core

- Validate queue and bead structure.
- Read ledger edges.
- Defer blocked items.
- Release items when all blockers resolve.
- Enforce global and per-queue concurrency limits.
- Create isolated run worktrees.
- Run the selected DOT graph.
- Serialize target-branch merges.
- Close successful beads and reopen failed beads.
- Pause a failed queue and emit typed facts.

This is mechanism under ZFC. None of these actions needs semantic judgment.

### DOT agents

- Interpret the bead and repository.
- Implement and review the change.
- Decide whether work is correct.
- Repair a merge conflict when the workflow assigns that action.
- Report a typed success, failure, or need for attention.

### Crew supervisor

The supervisor does not need to act between successful dependency transitions. It should observe at low frequency or query on demand. It becomes active when the queue pauses, a run becomes stale, a conflict needs judgment, the graph needs revision, or post-run verification finds a semantic problem.

The supervisor should not claim beads, close beads, move queue items, start the next ready child, or merge each normal completion. Those are core actions.

A daemon restart should also be a core recovery action when the stored facts give one safe answer. The current clean-stop path does not meet that goal. It cancels and archives the active queue, so an agent or operator must reconstruct and resubmit the remaining set. That is mechanical work and should not belong to the supervisor. An abrupt crash can contain ambiguous in-flight work. Deterministic reconciliation should classify it first. An investigator agent should run only when Git, Beads, and queue state do not give one safe action.

### Human or release owner

- Approve the integration branch into `main` or the release branch.
- Decide policy changes and unresolved semantic conflicts.

## Current gap

The desired per-epic integration branch is not automatic. The spec and pure workspace helper define it, but daemon composition does not query the parent edge. Until fixed, all children land on the project target branch unless planning writes the same branch override into every child bead.

Clean stop and start is also not an automatic continuation. Production archives the active queue as cancelled. The current queue and operator specs instead require a resumable `paused-by-drain` queue. This gap must be fixed before a long graph can tolerate normal daemon restarts without supervisor work.

Use this test for each action:

- If the action moves typed state or enforces a structural safety rule, prefer deterministic code.
- If the action needs meaning, judgment, prioritization, repair, or conflict resolution, assign it to an agent.
- If the action is routine observation with no decision, remove it or turn it into a query.
