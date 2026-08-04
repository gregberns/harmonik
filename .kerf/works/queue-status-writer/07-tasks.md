# Implementation tasks

## Scope note

The spec changelog has no normative change. These tasks implement existing
queue-model requirements. The Kerf scenario and exploratory bead rule does not
apply because no operator or spec surface changes. The work uses focused Go
transaction tests and a source ratchet instead. No `br` command runs from this
worktree.

## Tasks

### T1. Move the transaction port to the queue package

- **Build:** Add queue-owned snapshot, request, result, and transaction-store
  types. Replace queuewiring's exported forms with aliases. Assert that
  `QueueStore` implements the queue port.
- **Spec trace:** `specs/queue-model.md` QM-001 and QM-060.
- **Files:** Add `internal/queue/transaction_store.go`. Change
  `internal/queuewiring/store.go` and its transaction tests.
- **Accept:** Existing transaction-store tests pass through the aliases. A
  queue-package fake can call the port without importing queuewiring. No daemon
  file changes.
- **Depends on:** none.

### T2. Add queue-owned semantic transition operations

- **Build:** Add named queue, group, and item transition helpers. Cover submit
  deferral, deferred recovery, resume and rearm, startup repair, stale group
  terminal state, queue pause, completion, and cancellation. Each helper checks
  its source state and changes coupled fields together. Keep no general status
  setter.
- **Spec trace:** `specs/queue-model.md` §§2, 5, and 8. QM-030, QM-051, and
  QM-052.
- **Files:** Add `internal/queue/status_transition.go`. Change
  `internal/queue/state.go`, `resume.go`, `rpc.go`, and focused queue tests.
- **Accept:** Tests cover valid and rejected transitions. Group completion
  rejects a non-terminal item set. Rearm clears attempts and failure reason.
  Deferred recovery stays event-free.
- **Depends on:** T1.

### T3. Convert detached startup and terminal candidate paths

- **Build:** Use the semantic helpers for all seven startup changes. Keep
  startup changes on a detached candidate before first QueueStore installation.
  Add `TerminalResult`, `CompleteAndUnlinkResult`, and
  `CancelQueueOnShutdownResult`. Clone before a terminal mutation and copy back
  only after the existing write succeeds.
- **Spec trace:** `specs/queue-model.md` QM-001, QM-002, QM-003, QM-030,
  QM-053, and QM-060.
- **Files:** Change `internal/lifecycle/startup_pl005_qm002.go` and
  `internal/queue/persistence.go`. Add or change lifecycle and queue tests.
- **Accept:** A failed startup write prevents installation. A failed completion
  or cancellation write leaves the supplied queue unchanged. Existing unlink
  and archive tests pass. No receipt binding behavior changes.
- **Depends on:** T2.

### T4. Convert the operator drain consumer to live transactions

- **Build:** Replace locked-pointer mutation plus `Persist` in the operator
  pause and resume consumer with the transaction-store form. Build the pause
  event after a committed result. Wake after a committed resume result.
- **Spec trace:** `specs/queue-model.md` QM-054, QM-055, QM-060, and QM-063.
  `specs/operator-nfr.md` ON-027.
- **Files:** Change `internal/queuewiring/operatorevents.go` and focused
  queuewiring tests.
- **Accept:** A rejected operation performs no namespace I/O. A failed write
  leaves the stored snapshot unchanged and emits no pause event. A successful
  resume wakes the store. Multi-queue behavior remains per-queue and ordered.
- **Depends on:** T1 and T2.

### T5. Add source-surface evidence

- **Build:** Add a Bravo-owned shrink-only source ratchet. It reports 16
  non-daemon direct assignments, 14 daemon direct assignments, and 18
  construction-path writes before conversion. After conversion, it permits
  assignments only in the queue transition owner and the fixed Alpha daemon
  baseline. It must not be added to `check-fast`.
- **Spec trace:** `specs/queue-model.md` QM-060.
- **Files:** Add a new script under `scripts/` and focused script test or shell
  fixture. Do not change `Makefile` or an existing gate script.
- **Accept:** Adding a direct assignment outside the allowed queue owner fails.
  The script reports the three denominators. The fixed daemon baseline can only
  shrink. `scripts/queuewiring-freeze-gate.sh` passes.
- **Depends on:** T2, T3, and T4.

### T6. Run Bravo verification

- **Build:** Run focused queue, queuewiring, and lifecycle tests. Run a race
  test for the operator transaction path. Run `go build ./...`, `go vet ./...`,
  and the permitted freeze gate. Capture the ratchet output.
- **Spec trace:** `specs/queue-model.md` QM-001, QM-030, QM-060, and QM-063.
- **Files:** Test files from T1 through T5. Store command output in the work
  handoff, not as a tracked test artifact.
- **Accept:** All listed commands pass. A reviewer can reproduce the assignment
  denominators from the ratchet output.
- **Depends on:** T3, T4, and T5.

### T7. Alpha handoff for deferred recovery

- **Build:** Alpha replaces the live `ReevaluateDeferred` call in
  `internal/daemon/scheduler.go` with the queue store form from T2. It removes
  the later bare persist that follows the old group-pointer mutation.
- **Spec trace:** `specs/queue-model.md` QM-001, QM-060, and QM-063.
- **Files:** Alpha-owned `internal/daemon/scheduler.go` and daemon tests.
- **Accept:** A failed write leaves the live queue unchanged. A successful
  deferred recovery installs the candidate before its next dispatch scan.
- **Depends on:** T1 and T2. This task is outside Bravo's commit.

## Dependency graph

```text
T1 → T2 → T3 → T5 → T6
      ├──→ T4 ───┘
      └──→ T7 (Alpha handoff)
```

## Parallelization plan

T3 and T4 can proceed in parallel after T2 because startup and terminal code
does not overlap the operator consumer. T5 begins after their call sites settle.
T6 follows all Bravo implementation tasks. T7 is independent Alpha work after
T2 and does not block Bravo's 15-path implementation.
