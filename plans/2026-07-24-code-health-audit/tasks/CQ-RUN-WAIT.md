# CQ-RUN-WAIT — Wait on authoritative queue state

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `terra_high` (`gpt-5.6-terra`, high)
- Reviewer profile: `sol_high`
- Depends on: `CQ-RECEIPT`
- Lease family: `run_via_daemon_waiter`

## Objective

Make daemon-backed `harmonik run` terminate from exact queue-ID live/receipt
state even when `queue_group_completed` is absent.

## Exact ownership

Own only `cmd/harmonik/run_via_daemon.go`
`runBeadSubcommandViaDaemon` watcher call site,
`viaWatchGroupCompletion`, the smallest `viaQueueStatusByID` or injected
`viaStatusQuery` seam backed by a separate `viaSendRequest` connection, and
focused tests.

## Non-goals

Do not edit submit/append mutation, QueueStatus server, subscription transport,
event bus/JSONL, terminal production, or same-name selection policy.

## Required behavior and proof

- Query exact accepted queue ID and watched group immediately after
  accept/subscription setup and after every heartbeat; never multiplex RPC on
  the subscription connection.
- Pending/active continues. Live non-final success or a covering final receipt
  succeeds. Failed group, paused, cancelled, missing group, bad receipt range,
  integrity error, or transport failure exits 1.
- Preserve complete own-bead event attribution; when observations are
  incomplete, use durable group/receipt truth.
- Prove fresh-submit and shared-append paths with final event suppressed,
  final receipt-only success, non-final success, absent pause observations,
  same-name reuse before each query, and all integrity failures.

## Accepted intermediate state

Both waiter branches are authoritative-state safe; optional final observation
delivery remains disabled until `JR-03`.

## Completion and rollback

Focused deterministic tests, mutation fixtures that remove each status query,
scoped UBS/vet, `make check-fast`, and Sol review pass. Roll back only the
watcher call site/signature/status seam/tests. **COMMIT EXPLICITLY.**
