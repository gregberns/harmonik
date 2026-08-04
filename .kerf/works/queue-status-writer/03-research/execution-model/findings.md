# Execution model findings

## Questions

1. Which run outcomes drive item transitions?
2. Which non-run transitions must the API support?
3. What remains outside this lane?

## Findings

- EM-015b and EM-015c map terminal run outcomes to `run_completed` and
  `run_failed`. `queue-model.md` §2.7 maps those observations to item
  `completed` and `failed`.
- Startup repair, pre-claim rejection, reservation failure, forced reaping, and
  retry/resume are valid non-run causes. The API must name transition causes
  instead of accepting only a run result.
- Cancellation leaves a dispatched item unchanged. QM-002a later resets it to
  pending only after startup observes an open Beads record.
- Daemon run-outcome writers remain outside this slice. The new boundary must
  not change Beads terminal-write ownership.

## Patterns to follow

- Validate the concrete state-machine guard before durable work.
- Model coupled terminal effects as domain operations, not field updates.

## Risks

- A run-outcome-only API cannot represent valid startup and recovery paths.
- Replacing the bounded startup adapter with a live store can violate boot
  ordering.

