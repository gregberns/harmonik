# CQ-02 cross-spec composition and implementation-DAG review — Round 2

## Verdict

**REQUEST_CHANGES**

Round 1 C2 and C3 are resolved: the QueueStatus server/request/operator-client
surface is assigned end to end, and failed-group terminal state plus
`paused-by-failure` is now one authoritative replacement before either
observation attempt. The receipt/waiter/startup/terminal dependency spine
remains acyclic and safe.

Round 1 C1 and C4 are not fully resolved. The claimed “preserved” cancel wire
does not match the literal shipped request, and the caller plan assigns one
production caller to the wrong file while creating an unordered overlap with
the existing recovery/adoption cards.

## Verification performed

I reviewed the Round 1 response against the corrected four drafts,
`05-changelog.md`, `07-tasks.md`, `CQ-02.yaml`, the approved CQ-02/CQ-00/JR-00
evidence, the existing CQ-03/JR-04/WL-REC-01 cards, and the literal production
symbols.

The following checks pass:

- the evidence file retains the exact 23-key schema, 69 crash rows, and 19
  declared slices;
- every declared slice has prerequisites, a lease, writes, conflicts, a
  failing-first proof, an accepted intermediate state, and a rollback field;
- the declared dependency graph is acyclic;
- execution-model changes remain limited to version/date, EM-015f, and one
  qualifying revision row;
- QM-052 and EM-015f now agree on one failure-group-plus-pause transaction;
- `CQ-RECEIPT` exactly assigns `QueueStatusRequest`/`QueueStatusResponse`,
  `HandleQueueStatus`, `findQueueByID`, `HandlerAdapter.HandleQueueStatus`,
  `RunQueueStatus`, `renderQueueStatusText`, watched-index-zero behavior, and
  live/receipt text/JSON proofs;
- `CQ-04` now names `reconcileDispatchedItems`, `reconcileThreeWay`, and
  `reconcileQueueTerminalState`;
- queue-event payload, validation, registry, compatibility, cohort, and count
  symbols are assigned;
- `CQ-RUN-WAIT` still gates `JR-03`, and `CQ-04` also gates `JR-03`, so no
  crash-optional group observation can precede both waiter fallback and
  capable startup recovery;
- optional group observations remain diagnostic, non-authoritative,
  non-replayed, and unable to hang the waiter or roll back/retain queue state;
  and
- no task owns `ScanAfter`, ReplayCursorV2, segmented JSONL, global reader
  migration, or another stale global event scope.

The supplied structural validator passes these shapes but does not compare
wire fields or task paths/symbols against production. Direct production-aware
checks expose the failures below.

## Required changes

### R2-C1 — The cancel request is a replacement, not preservation of the shipped wire

The corrected queue and process drafts, changelog, design, evidence, and
`CQ-CALLER-CANCEL` all claim that the shipped cancel request is
`name`, `queue_id`, `force`, migrated “additively.”

Literal production disagrees:

- `internal/queue/types.go` `QueueCancelRequest` has fields `Queue string`
  tagged `json:"queue"` and `Force bool` tagged `json:"force,omitempty"`;
- `internal/queue/cli/cancel.go` `tryDaemonQueueCancel` sends
  `{op, queue, force}`; and
- `HandlerAdapter.HandleQueueCancel` decodes that current request.

The CLI's `--queue-id` is resolved locally by `cancelFindByID`; the accepted
daemon request still carries the resolved `queue` name, not `queue_id`.
Dropping `queue` while adding `name`/`queue_id` is not additive and does not
preserve the shipped wire. An N-1 client would send `queue`, which the proposed
record does not define.

Required correction: either retain the current `{queue, force}` request and
document the CLI's ID-to-name resolution, or define an explicitly compatible
extension that continues decoding `queue` and gives deterministic precedence
for `queue`, `name`, and `queue_id`. Assign the exact
`QueueCancelRequest`, `tryDaemonQueueCancel`, `cancelFindByID`,
`HandlerAdapter.HandleQueueCancel`, and compatibility tests. Make the research,
design, both drafts, changelog, evidence, and task plan use the same actual
wire. Strengthen the validator to compare the declared retained fields with
the production JSON tags.

### R2-C2 — Literal caller ownership is still incomplete and one assignment names the wrong subsystem

`CQ-CALLER-OPERATOR` is assigned to operator pause/resume regions in
`internal/daemon/workloop.go`. Those production mutations are actually:

- `internal/queuewiring/operatorevents.go`
  `QueueOperatorEventConsumer.transitionToPausedByDrain`; and
- `internal/queuewiring/operatorevents.go`
  `QueueOperatorEventConsumer.transitionToActive`.

The incorrect path leaves both real direct `queue.Persist` callers unowned.
It also declares a fictitious `workloop.go` overlap and a
`queue_operator_events` lease that cannot serialize the stated
`dispatch_spine` conflict.

Several other supposedly exact evidence slices still name only a file or
region even though CQ-00 already pins the literal symbols:

- `CQ-CALLER-EAGER` omits `eagerRefillEval`;
- `CQ-CALLER-CREW` omits `crewHandlerImpl.ensureQueue`; and
- the cancel slice omits the helper symbols listed in R2-C1.

Required correction: regenerate every slice from CQ-00's production caller
inventory. Put the operator task on `internal/queuewiring/operatorevents.go`
with both exact methods and file-disjoint conflict semantics. Name
`eagerRefillEval`, `crewHandlerImpl.ensureQueue`, and every cancel helper in
both `07-tasks.md` and `CQ-02.yaml`. Re-run a validator that compares every
production-reachable caller tuple `(path, symbol)` with exactly one task.

### R2-C3 — Adoption/recovery has an unordered overlapping owner

`CQ-CALLER-WORKLOOP-MAINTENANCE` now claims the
`adoptLiveRunSession` stale-session reset direct-persist region. That conflicts
with existing reviewed task ownership:

- `JR-04.md` exclusively leases `run_session_adoption.go`,
  `adoptLiveRunSession`, and `runinflightreconcile_hkr73qr.go`; and
- `WL-REC-01.md`, which depends on JR-04, exclusively owns the
  `workloop.go` recovery/adoption call sites and removes inline adoption
  policy.

The corrected CQ-02 representation of JR-04 instead claims
`reconcileQueueTerminalState`, `evaluateGroupAdvanceWithOutcome`, and
`drainCancelledQueue` restart seams, while omitting JR-04's actual exclusive
files/symbols `runinflightreconcile_hkr73qr.go`,
`reconcileOrphanedRunsOnResume`, `run_session_adoption.go`, and
`adoptLiveRunSession`.

There is no dependency between `CQ-CALLER-WORKLOOP-MAINTENANCE` and JR-04 or
WL-REC-01. Both maintenance and JR-04 can therefore be dispatched after
JR-03 and edit/own adoption behavior concurrently. The graph is acyclic but
not conflict-safe.

Required correction: preserve the existing JR-04 and WL-REC-01 exclusive
leases, remove adoption from the generic maintenance slice, and represent the
actual recovery owner plus later workloop caller migration with their existing
prerequisites. If CQ-02 requires an earlier transaction-only adaptation,
serialize it explicitly with both cards and bound it to a disjoint named
symbol/region; do not claim the whole `adoptLiveRunSession` policy in two
parallel-ready tasks. Replace “JR-04 existing card” with an exact rollback
boundary matching the actual card.

## Round 2 conclusion

The cross-spec receipt/event semantics, one-transaction failure pause,
QueueStatus fallback, startup recovery, and terminal ordering are ready. The
remaining work is production-composition accuracy: preserve the actual cancel
wire and make the caller-to-task graph a bijection over literal paths/symbols.
Re-review after R2-C1 through R2-C3 and the production-aware validators are
corrected.
