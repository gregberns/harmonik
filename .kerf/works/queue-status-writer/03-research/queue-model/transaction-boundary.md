# Transaction boundary decision

## Decision

`internal/queue` owns the transition rules. `internal/queuewiring` owns the
live registry and its lock. The queue package must not import queuewiring.

Move the neutral transaction port into `internal/queue`:

- `QueueSnapshot` holds a detached queue and its process-local generation.
- `TransactionRequest` and `TransactionResult` describe clone, mutate, durable
  replace, and install.
- `TransactionStore` exposes `Snapshot` and `Transact`.

`queuewiring.QueueStore` implements that port. Keep aliases in queuewiring for
the exported transaction names. This keeps current daemon callers source
compatible. It also lets queue transition helpers take the port without an
import cycle.

The port is not a generic status setter. Each queue helper represents one
existing transition. Its input carries the facts that the state machine needs.
For example, rearming also clears attempts and failure reason. A terminal
group transition checks all items and sets its completion time. A dispatched
item transition keeps its run ID coupled to its status. The helpers reject an
invalid source state before namespace I/O.

`ReevaluateDeferred` gains two forms. The candidate form applies the checked
transition to a detached queue. The store form takes a queue name, group index,
ledger, and `TransactionStore`; it applies the same candidate form inside one
transaction. Keep the current group-pointer form as a compatibility wrapper
for the unchanged daemon caller. It calls the same queue-owned primitive, so
the direct assignment leaves `state.go`, but it cannot gain transaction safety
until alpha changes that daemon call. That named handoff is part of this work's
completion record.

## Three persistence paths

### 1. Live store transaction

Use `TransactionStore` for a loaded queue. The helper takes a snapshot, builds
a semantic mutation callback, and calls `Transact`. Queuewiring then does:

1. verify generation and snapshot bytes under its write lock;
2. clone and apply the queue-owned transition;
3. write the replacement and wait for a durable result;
4. install the candidate only after commit; and
5. wake only when the operation requires it.

The two operator-drain writers use this path. They must stop installing a
changed queue before `Persist`. The pause event is emitted only after the
committed result. Resume wakes only after the committed result.

### 2. Detached startup candidate

Startup reconciliation loads a queue before `QueueStore` exists. It cannot
take a live-store transaction. It uses the same queue-owned semantic helpers
on that detached value, then persists the candidate before the caller installs
it in the new store. A failed write discards the candidate. This is the bounded
QM-060 exception. It is valid only before first store installation.

The seven writers in `startup_pl005_qm002.go` use this path: dispatched item
revert, ledger-derived completed or failed item results, stale active group
completion, and queue pause after terminal failures. The all-success path
uses the completion helper below and never installs the queue.

### 3. Terminal namespace sequence

Completion and shutdown cancellation have a named namespace sequence after
the completed or cancelled durable record. Completion unlinks the canonical
file. Cancellation renames it into its archive. Neither operation can be
reduced to an ordinary live-store install.

`CompleteAndUnlink` and `CancelQueueOnShutdown` will first clone the supplied
queue, apply the queue-owned terminal transition to the clone, and make the
existing durable write. They copy the candidate back to the supplied queue
only after that write commits. Completion then unlinks. Cancellation then
renames the canonical file. A failed write leaves the supplied queue unchanged.
This preserves existing caller signatures, avoids a daemon edit, and removes
the live-pointer-before-persist defect.

The terminal sequence remains event-free. This item preserves the current
completion and cancellation protocol. It does not add a QM-005 receipt binding
to `ReplaceIntentV1`, because the current transaction type fixes that field to
nil. Making the receipt and release protocol executable is separate work. It
must not be implied by this status-writer slice.

The terminal boundary has two queue-owned entry points:

```text
CompleteAndUnlinkResult(ctx, projectDir, source) -> TerminalResult
CancelQueueOnShutdownResult(ctx, projectDir, source, archiveTime) -> TerminalResult
```

`TerminalResult` reports whether the terminal candidate committed, a commit
error, and a post-commit cleanup error. The completion function performs the
existing unlink only after a successful completed write. The cancellation
function performs the existing archive rename only after a successful
cancelled write. The old exported wrappers keep their error return for source
compatibility and map `TerminalResult` to it. This gives callers a testable
failure boundary without changing the receipt or archive protocol.

## Writer map

| Source | Existing change | Path |
| --- | --- | --- |
| `internal/queue/rpc.go` | submit candidate deferred status | pre-publication construction path, measured separately |
| `internal/queue/state.go` | deferred item to pending | excluded from Bravo's durable-coverage denominator: queue owns the primitive and exposes a live-store form, but Alpha must replace the daemon compatibility call |
| `internal/queue/resume.go` | failed group to active, failed item to pending, queue to active | queue semantic helpers; the caller persists the resulting candidate as today |
| `internal/queue/persistence.go` | completed and cancelled queue | terminal namespace sequence |
| `internal/lifecycle/startup_pl005_qm002.go` | seven recovery and terminal changes | detached startup candidate |
| `internal/queuewiring/operatorevents.go` | active to paused-by-drain, paused-by-drain to active | live store transaction |

The transition helpers own every source assignment in the 16-source
measurement. Bravo provides durable ordering for 15 of them. Deferred recovery
is the explicit exception: the unchanged daemon call mutates a live group and
persists it later. Alpha must use the supplied store form before that writer
can join the durable-coverage denominator. The implementation keeps the 14
daemon assignments unchanged. The direct-assignment check reports both sets.
It also reports construction sites separately.

## Construction measurement

At this tip, the construction scan finds 18 pre-publication status writes
outside tests. It consists of 17 composite-literal fields plus the submit
candidate's deferred adjustment in `rpc.go`. It excludes response fields and
unrelated records named `Status`.

| Area | Sites |
| --- | ---: |
| `internal/queue/append.go`, `validation.go`, and `rpc.go` | 10 |
| `internal/queue/cli/helpers.go` request document | 2 |
| `cmd/harmonik/run.go` | 3 |
| `cmd/harmonik/run_via_daemon.go` | 2 |
| `internal/queue/rpc.go` deferred adjustment before first persist | 1 |
| Total | 18 |

The command used a non-test search for `Status:` and then retained only
`Queue`, `Group`, `Item`, or their queue-submit document values. It excludes
`QueueSubmitResponse.Status` and `QueueDryRunResponse.Status`, because they
report an already constructed queue. The literal-only count is 17. Adding the
pre-publication deferred adjustment gives the plan's construction-path count of
18. The ratchet records 16 outside-daemon assignments, 14 daemon assignments,
and 18 construction-path writes as three separate numbers.

## Rejected alternatives

- Import `queuewiring` from `queue`. This creates a package cycle.
- Keep transaction types only in queuewiring. This forces queue transition code
  to know its implementation package.
- Add `SetStatus`. It permits illegal state pairs and misses coupled fields.
- Treat startup as a live transaction. There is no live store before startup
  completes.
- Convert completion or cancellation to a normal install. This loses unlink or
  archive handoff behavior.

## Required tests

- A valid live operator transition commits, installs, and emits only after the
  durable result.
- A rejected semantic transition performs no namespace I/O.
- A failed durable write leaves the store snapshot unchanged.
- A failed terminal write leaves the supplied queue unchanged.
- A startup write failure prevents store installation.
- The bypass report shows the fixed 14 daemon assignments and the independently
  measured construction count. It is shrink-only and is not added to
  `check-fast` in this lane.
