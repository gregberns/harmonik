# Runtime scenarios

## Harness rule

Use an isolated throwaway repository, bead ledger, daemon state directory, harness process, and branch namespace. Run the real daemon binary and real CLI path. Use a deterministic test harness only for the agent endpoint. Do not mock the queue, ledger adapter, worktree manager, scheduler, or merge path.

Each test must prove that the machinery ran. Negative checks alone do not count.

## Scenario S1: serial graph

Graph: `A -> B -> C`.

Submit all three once. Record queue state and events. Prove A starts first. Prove B starts only after A closes and merges. Prove C starts only after B closes and merges. Prove one target branch contains the three changes in order.

## Scenario S2: fan-out and fan-in

Graph: `A -> [B, C, D] -> E`.

Submit all five once with concurrency three. Prove only A starts first. Prove B, C, and D become runnable after A. Prove at least two run at the same time. Prove E does not start until all three close and merge.

Run each child through the canonical implement, commit-gate, review, and close topology. Use a small real `make full` target that checks the implementer commit and writes durable gate evidence outside the disposable worktree. Prove five gate passes before accepting the graph result.

## Scenario S3: failed dependency

Use the S2 graph and make C fail validation. Prove E never starts. Prove the queue reaches a typed terminal or paused state that names the cause. Prove no supervisor action silently changes a bead state.

## Scenario S4: merge conflict

Make B and C edit the same line. Prove deterministic code detects and reports the conflict. Prove it does not create a semantic resolution. Record whether unrelated D and downstream E continue, pause, or fail.

## Scenario S5a: clean stop and start

Send the daemon its normal graceful stop after A starts and before B starts. Start it again with the same project. Prove whether the canonical queue survives, whether `queue resume` can continue it, and whether B runs without a new submit. This scenario currently has a confirmed contract and code disagreement.

## Scenario S5b: abrupt crash recovery

Kill the daemon process without running its clean-exit path after A merges or while B is in flight. Start it again with the same project. Prove reconstruction uses the queue, Git, and Beads state. Prove the next valid bead runs, fan-in does not start early, and no successful merge occurs twice. A retried `run_started` is acceptable only when the prior attempt did not land.

Run this as a child-process scenario after C21 scheduler wiring and replay land. The parent test owns the throwaway project and sends SIGKILL to the child daemon. It then starts a new child against the same files.

Use two cuts:

1. Kill after A has a durable queue terminal result and before any branch item has a prepared intent. On restart, B, C, and D can dispatch. E must stay deferred until all three terminal results are durable.
2. Kill while B has `handoff_durable`, C and D are either handed off or still eligible, and no branch merge exists. On restart, each exact live session is adopted once. Each dead session resumes with its bound run and worktree. E must not start from event history or elapsed time.

For each cut, assert these durable facts before the kill and after restart:

- canonical queue item status and bound run ID
- dispatch intent phase and full queue, bead, run, session, and lease identity
- Beads status
- durable run record
- worktree lease
- immutable release claim when present
- target-branch completion evidence

The result assertion is stronger than an event count. No bead can have two target-branch completion commits. A retried or adopted attempt must keep the same durable identity where C21 requires it. Startup must finish intent replay before it emits ready or allows a new dispatch.

## Scenario S6: epic branch

Submit child beads under one epic. Prove how the target branch is selected and created. Prove whether the parent relation has any runtime effect. Prove the system cannot push a protected release branch.
