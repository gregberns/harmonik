# Research — Process lifecycle

## Questions

1. What command, RPC, selectors, and result identify one durable recovery?
2. When may the CLI report success?
3. What daemon-down result prevents a local fallback write?

## Findings

- `PL-003a` and `PL-028` list queue methods and defer `queue-resume`. The
  amendment must add it in both places and retain name and queue-ID selectors.
- `RunQueueResume_DaemonDown` already returns exit 17 without a daemon. Keep
  this result and state that it has not recovered a queue.
- The current drain command reports success after `operator_resuming`. Queue
  wiring consumes the event later. This cannot prove a durable failed-item
  transition.
- Submit and append show the socket pattern. Recovery needs a direct transaction
  result: queue ID, normalized name, re-armed IDs or count, and final status.

## Patterns to keep

- Report success only after the replacement transaction commits.
- Keep recovery per queue. Do not add a global recovery form.
- State that restart preserves a failed pause. It is not recovery.

## Risks and decisions

- Do not retain `queue resume` as an alias for `operator-resume`. It reports
  success while a failed queue stays unchanged.
- Do not reuse a global or per-queue drain-resume command for failure recovery.
  That would widen scope and hide the target queue.
