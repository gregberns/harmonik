# Research — Process lifecycle

## Questions

1. What command, RPC, selectors, and result identify one durable recovery?
2. When may the CLI report success?
3. What daemon-down result prevents a local fallback write?

## Findings

- `PL-003a` and `PL-028` list queue methods and defer `queue-recover`. The
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

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Process lifecycle research findings

#### Questions

1. Does `queue resume` recover failed work or only drain-paused work?
2. Which RPC and CLI contract must failed recovery expose?
3. Who owns committed-but-unmerged work at graceful stop?

#### Findings

`PL-011` treats `just-checkpointed` work as quiescent and releases leases.
`PL-012` defers recovery to startup. `PL-003a` and `PL-028` own socket methods
and CLI registration. They do not define failed-item recovery.

`RunQueueResume` sends `operator-resume`, while the consumer resumes only
`paused-by-drain`. `ResumeFromFailure` is unwired. `exitClean` in
`internal/daemon/scheduler.go` falls through to cancellation and archive after
its wait limit.

#### Patterns and risks

The CLI is a thin socket client. Queue mutation belongs in the daemon-owned
consumer and must persist before it reports success. A post-commit run needs a
state distinct from an ordinary checkpoint.

#### Design constraints

- Specify an explicit failed-recovery command and RPC with accepted states,
  no-op or rejection cases, errors, and durable response fields.
- It invokes the durable queue transaction and cannot form a second terminal
  route.
- Lifecycle orders process recovery. Execution owns run reconstruction. Queue
  owns item and group mutation.
- Add a real drain-window stop test with exactly one later terminal action.
