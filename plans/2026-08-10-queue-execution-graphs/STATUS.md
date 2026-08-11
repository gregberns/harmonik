# Status

Updated: 2026-08-11

## Current stage

Research and runtime proof. Serial and fan-out or fan-in graphs passed through the real daemon and Unix-socket CLI without supervisor action.

## Completed

- Fast-forwarded delta over alpha's 34 newer integration commits at `7cf5c73df`.
- Reconciled the new live-bead model, dispatch intent store, and C21 startup-replay design with this investigation.
- Re-ran delta's three real queue graph scenarios on alpha's integration head. They passed.
- Corrected the embedded dispatch skill and generated mirror. Streams now describe dependency-ready concurrency and idle wake correctly.
- Loaded the project charter, principles, state, handoff, orchestration rules, dispatch rules, bead rules, branch practices, and ZFC concept.
- Created an isolated delta worktree at `/Users/gb/github/harmonik-wt/delta` on `work/delta-queue-execution`.
- Defined serial, fan-out and fan-in, failed dependency, conflict, restart, and epic branch scenarios.
- Traced submit-time dependency deferral and dispatch-time re-evaluation.
- Ran the existing real-daemon two-bead dependency scenario. It passed.
- Confirmed that parent-derived epic branch naming exists as an unwired workspace helper.
- Found drift between the dispatch skills and current stream concurrency contract.
- Replaced the test-only submit adapter with the real Unix-socket CLI path.
- Proved `A -> [B, C, D] -> E` in the normal stream group with real ledger edges and concurrent branch runs.
- Proved all successful child runs land on the configured integration branch while `main` stays unchanged.
- Re-ran the existing real-Git conflict and serialized-merge scenarios. Both passed.
- Proved a failed root never launches its dependent. The queue pauses for an explicit recovery decision.
- Confirmed that a clean daemon stop archives an active queue as cancelled. This conflicts with the specified durable `paused-by-drain` transition.
- Confirmed that abrupt-crash recovery has tested run-session adoption and durable reservation release parts. A full killed-process graph run is still missing.
- Proved the clean-stop gap through two real daemon starts. The first stop archived the active queue. The second start had no graph to continue.
- Wired parent-child edges into the run plan. A child now uses one parent-derived integration branch when no higher branch field sets another value.
- Added an atomic branch creation helper. Concurrent children now converge on one integration branch.
- Added focused tests for branch creation and field-by-field branch precedence.
- Proved the epic fan graph lands all five children on one parent-derived integration branch.
- Replaced clean-shutdown cancellation with a durable one-shot restart drain.
- Proved a second real daemon continues the pending queue without resubmit or supervisor action.

## Next

- Run an abrupt-crash graph recovery scenario. Keep it separate from the confirmed clean-stop cancellation gap.
- Run the abrupt-crash graph recovery scenario after alpha's durable dispatch replay contract lands.
- Coordinate the abrupt-crash scenario with C21. Do not pin the old session-only recovery path as the final dispatch contract.
- Run the focused package and scenario gates.

## Constraints

- The shared checkout belongs to lane alpha and is dirty. Delta does not edit it.
- Kerf cannot create a work from this worktree because its global project link points to the shared checkout.
- The default macOS Bash 3 cannot run one script test because it lacks `mapfile`. Bash 5 is installed at `/opt/homebrew/bin/bash`. `PATH=/opt/homebrew/bin:$PATH make fast` is green: 8,342 tests passed and 45 existing tests were skipped.
- `PATH=/opt/homebrew/bin:$PATH make full` ran 75,687 tests across 108 packages. All tests passed and 54 tests were skipped. The final repository-wide lint allow-list step failed on ten file and linter pairs outside delta's diff. The files belong to other active lanes and were present in delta's base. Delta did not change them or weaken the allow list.
- Delta has three saved commits for the graph tests, clean-stop proof, and parent branch wiring.
- Free disk recovered above the watermark. The parent branch and clean restart scenarios now pass.
