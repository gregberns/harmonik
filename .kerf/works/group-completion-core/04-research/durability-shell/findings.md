# Durability and shell research

## Questions

1. What happens after persist and cleanup failures today?
2. What event order must remain stable?
3. Which effects run for each disposition?

## Findings

`evaluateGroupAdvanceWithOutcome` persists intermediate and paused state before event emission.
It suppresses events after a persist failure but keeps the in-memory mutation.
It wakes dispatch after both intermediate success and persist failure.

The final path calls `CompleteAndUnlink` before event emission.
It clears memory and fires both cancel hooks even when completion returns an error.
This flattens commit failure and cleanup failure.

`CompleteAndUnlinkResult` distinguishes legacy commit failure from cleanup failure.
It is useful evidence but is not the target interface.
The target depends on the QM-001 and QM-005 transaction owner and its receipt-aware result.
That result must report the achieved QM-053 phase.
A durable receipt alone does not authorize memory clear or cancellation.
Only a result that reached ownership release authorizes those effects.
A later release-marker failure must not reacquire the released name.

Current intent order is:

1. Current group completion.
2. Queue pause after a failed group.
3. Successor group start after a successful group.

The current queue specification resolves the comment and code conflict.
QM-053 requires a durable completed canonical value and completion receipt before the final observation.
It then requires cleanup, ownership release, and the completion-release marker.
The live helper does not implement that contract.

## Options

Preserving the live failure behavior keeps known recovery defects.
Adapting the legacy helper would also harden the wrong completion mechanism.
The full shell change must wait for the receipt-aware transaction result.

## Risks

The store lock currently spans filesystem writes.
This work can preserve that writer rule, but it should not add more work under the lock.
A later transaction-owner change can reduce the lock duration.

## Conclusion

Only the event-intent prerequisite is unblocked.
The completion decision can proceed as a value design but cannot replace the live shell.
The durability policy and daemon shell remain blocked on the queue transaction owner.
