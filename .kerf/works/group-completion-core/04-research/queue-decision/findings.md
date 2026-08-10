# Queue decision research

## Questions

1. Can a queue value be copied safely?
2. Which input cases are no-change cases?
3. Which timestamp fields change today?

## Findings

`Queue` contains group slices.
Each group contains item slices.
Items contain maps and pointers.
A plain structure copy shares nested storage.

`internal/queue/rpc.go` and `internal/queuewiring/store.go` already deep-copy queue values.
The new function needs one queue-owned clone helper or an explicit consumed-pointer contract.
The project goal favors a detached result.

A stale expected queue ID is an expected race and should return no change.
A missing group, duplicate group index, negative item index, or large item index indicates corrupt or invalid input.
Those cases should return typed errors.

The current state function changes group status but does not populate `StartedAt` or `CompletedAt`.
This work must preserve those fields unchanged.

## Risks

An already-terminal item can cause duplicate completion events if admitted twice.
A repeated matching outcome returns no change.
A conflicting outcome returns a typed conflict.

Final success also needs the QM-005 completion receipt ID in the final `queue_group_completed` payload.
The transaction owner must prebind that ID before replace-intent durability.
The completion decision cannot create the ID or add it after it returns.
This blocks the decision contract until the receipt prebinding input exists.
