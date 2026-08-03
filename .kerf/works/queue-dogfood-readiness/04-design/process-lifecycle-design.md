# Change Design — Process lifecycle

## Current state

`PL-003a` and `PL-028` defer `queue-resume`. The shipped `hk queue resume`
command sends `operator-resume`, which emits `operator_resuming`. Queue wiring
only releases `paused-by-drain`. The CLI can report success before a failed
queue changes.

## Target state

Add JSON-RPC `queue-resume` to `PL-003a` and add
`hk queue resume [--queue <name>] [--queue-id <uuid>]` to `PL-028` and
`PL-028c`. It calls the direct queue recovery transaction. It does not route
through `operator-resume`.

Selectors use name first, then queue ID, then main. The command remains
separate from a controlled batch, shutdown recovery, and the assessor gate. It
retains Step 9 and core-loop artifact paths before the assessor receives the
candidate. The assessor never consumes this queue work.

Keep daemon-down exit 17. It makes no local queue, intent, receipt, marker, or
event write. A successful reply returns queue ID, normalized name, re-armed
items or count, and final active status only after transaction commit. A typed
invalid-state or recovery-failure reply names the selected queue and observed
state. It rejects drained, active, completed, and cancelled queues.

## Rationale

The command and RPC must describe the durable operation truthfully. Failure
recovery is not drain release.

## Requirements traceability

- `02-components.md`: process lifecycle recovery command and RPC.
- `03-research/process-lifecycle/findings.md`.
- Session decisions on synchronous durability and distinct recovery.
