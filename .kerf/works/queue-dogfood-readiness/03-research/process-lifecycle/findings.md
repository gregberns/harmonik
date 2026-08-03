# Process lifecycle research findings

## Questions

1. Does `queue resume` recover failed work or only drain-paused work?
2. Which RPC and CLI contract must failed recovery expose?
3. Who owns committed-but-unmerged work at graceful stop?

## Findings

`PL-011` treats `just-checkpointed` work as quiescent and releases leases.
`PL-012` defers recovery to startup. `PL-003a` and `PL-028` own socket methods
and CLI registration. They do not define failed-item recovery.

`RunQueueResume` sends `operator-resume`, while the consumer resumes only
`paused-by-drain`. `ResumeFromFailure` is unwired. `exitClean` in
`internal/daemon/scheduler.go` falls through to cancellation and archive after
its wait limit.

## Patterns and risks

The CLI is a thin socket client. Queue mutation belongs in the daemon-owned
consumer and must persist before it reports success. A post-commit run needs a
state distinct from an ordinary checkpoint.

## Design constraints

- Specify an explicit failed-recovery command and RPC with accepted states,
  no-op or rejection cases, errors, and durable response fields.
- It invokes the durable queue transaction and cannot form a second terminal
  route.
- Lifecycle orders process recovery. Execution owns run reconstruction. Queue
  owns item and group mutation.
- Add a real drain-window stop test with exactly one later terminal action.
