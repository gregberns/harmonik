# Next review work

## Immediate sequence

### 1. Draw the live bead state model

List the queue item, bead, run registry, run record, worktree, and merge states.
Define every valid combination.
Mark the current owner of each transition.

Stop when every process-death boundary has one recovery answer.

### 2. Define the target package boundary

Draft the smallest package graph for one bead path.
Use existing packages where they already fit.
Do not move code in this review step.

Stop when an import fence can state the boundary without file-name exceptions.

### 3. Finish the run machine design

Extend the current `runexec` direction to the full run lifecycle.
Define typed inputs, actions, and outcomes for each phase.
Keep DOT traversal as a child machine.

Stop when `beadRunOne` can become a thin shell or disappear.

### 4. Separate scheduler and supervisor

Make queue selection a pure decision.
Make dispatch transaction ownership explicit.
Make run goroutine ownership explicit.

Stop when the scheduler no longer knows worktree, harness, merge, or shutdown details.

### 5. Prove one vertical

Use one local Claude-compatible test substrate or a deterministic twin.
Submit one bead, claim it, run it, merge it, and close it.
Capture every state transition.

Break each critical transition and confirm that its named test fails.

## Daemon survival slice

After the core state model exists, run this fault matrix:

| Fault point | Required proof |
| --- | --- |
| Before queue reservation | No queue or bead state changes |
| After reservation, before claim | Restart returns the item to a dispatchable state |
| After claim, before run record | Restart does not double-dispatch |
| After run record, before launch | Restart repairs or resumes one run |
| During agent work | SIGTERM and SIGKILL have distinct defined results |
| After commit, before merge | Restart preserves and lands or reports the commit |
| After merge, before bead close | Restart closes without duplicate work |
| During shutdown merge | The daemon reports the owner and final result |

## Work to avoid during this review

- Do not add optional subsystem behavior to the scheduler.
- Do not fix isolated bugs unless they block architectural proof.
- Do not expand the core package list.
- Do not call a green suite proof until a deliberate break makes the right test fail.
- Do not redesign keeper or supervisor before the core ownership model is clear.
