# Ownership decision

## Current ownership model

### Planning agent or crew

- Decompose the epic into child beads.
- Define every `blocks` edge before submission.
- Reduce avoidable file overlap when it understands the planned work.
- Put explicit branch overrides in bead bodies only when the project or epic needs them.
- Dry-run and submit every child ID in one request.
- Treat dry-run as validation of the requested subgraph, not as proof that no child or blocker was omitted.

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

- Decide when a completed epic integration branch moves onward under the current policy.
- Approve the project integration branch into `main` or the release branch.
- Decide policy changes and unresolved semantic conflicts.

## Current gaps

The per-epic integration branch is wired and proven. The daemon reads the child record's parent edge. It derives and creates one integration branch for the parent when no higher branch setting overrides that field. The live graph landed all five children there.

Clean stop and start now continues without supervisor action in delta. A clean shutdown writes `paused-by-drain` with a one-shot restart intent. Startup consumes that intent before reconciliation and returns the queue to active. An explicit operator pause does not set the intent. It remains paused and keeps its pending work for a later operator resume.

Automatic promotion after `epic_completed` is not in the current contract. The core must not invent a target. A later design can make derived-branch to project-integration promotion mechanical after it defines one target and one conflict path. The project-integration to `main` boundary remains human.

Use this test for each action:

- If the action moves typed state or enforces a structural safety rule, prefer deterministic code.
- If the action needs meaning, judgment, prioritization, repair, or conflict resolution, assign it to an agent.
- If the action is routine observation with no decision, remove it or turn it into a query.
