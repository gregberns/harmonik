# Queue model change design

## Current state

`specs/queue-model.md` defines the queue, group, and item state machines. It
also requires the QueueStore transaction owner for live mutation. The code has
direct status assignments in several packages. The live operator consumer
changes memory before it persists. Startup changes a detached queue before the
store exists. Completion and cancellation use their existing namespace steps.

## Target state

No normative queue-model text changes in this work.

The implementation will add a queue-owned semantic transition surface. It will
move neutral transaction request, result, snapshot, and port types into
`internal/queue`. `internal/queuewiring` will retain aliases and make
`QueueStore` satisfy the port. This avoids a package cycle.

The surface will use named operations. It will not expose a general status
setter. Each operation validates its source state and updates all coupled
fields. Examples include deferred recovery, failure resume, group completion,
queue pause, and terminal queue state.

The implementation has three paths:

1. Loaded operator queues use `TransactionStore`.
2. Startup reconciliation changes a detached candidate before first store
   installation.
3. Completion and cancellation clone their supplied queue, persist the terminal
   candidate, copy it back only after commit, then perform their existing
   unlink or archive step.

The status assignment scan will distinguish 16 non-daemon source assignments,
14 daemon assignments, and 18 pre-publication construction-path writes. Bravo
will give durable coverage to 15 source assignments. Deferred recovery remains
an Alpha handoff because its only live caller is in `internal/daemon`.

## Rationale

QM-001, QM-060, and QM-063 require durable queue facts before a successful
live mutation is observed. The transaction port gives queue-owned logic access
to this behavior without importing the registry package. The startup adapter is
valid only before store installation. Terminal helpers must retain their
current unlink and archive behavior. QM-005 receipt binding is not part of this
work because the current transaction record fixes that field to nil.

## Requirements traceability

| Requirement | Design response |
| --- | --- |
| State machines in §§2, 5, and 8 | Named semantic operations validate source and coupled fields. |
| QM-001, QM-060, QM-063 | Live changes use the moved transaction port. Startup and terminal paths retain their bounded behavior. |
| QM-030 | Group completion stays in the existing terminal-item helper. |
| No daemon edit | Deferred recovery becomes an Alpha handoff. The 14 daemon assignments stay unchanged. |
| No receipt or event change | Terminal and event ordering stay as they are. |
| Bypass evidence | The ratchet reports assignment and construction denominators separately. |
