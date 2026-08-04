# Change Design — Process lifecycle

## Current state

`PL-003a` and `PL-028` defer `queue-recover`. The shipped `hk queue resume`
releases a drain pause only, so recovery needs its own verb.
command sends `operator-resume`, which emits `operator_resuming`. Queue wiring
only releases `paused-by-drain`. The CLI can report success before a failed
queue changes.

## Target state

Add JSON-RPC `queue-recover` to `PL-003a` and add
`hk queue recover [--queue <name>] [--queue-id <uuid>]` to `PL-028` and
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

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Process lifecycle change design

#### Current state

The queue CLI sends `operator-resume`, which resumes only drain-paused queues.
Shutdown treats post-commit work as an ordinary checkpoint.

#### Target state

Specify a distinct failed-recovery RPC and CLI command. Define accepted queue
states, rejected and no-op results, receipt fields, and durable success before
response. Define committed-but-unmerged work as a drain state: drain its
existing ladder or persist recovery before exit. Define the controlled batch as
a separate assessor-gated lifecycle operation.

#### Rationale

The command must not silently claim failed recovery when it only resumes drain.

#### Requirements traceability

Addresses lifecycle command, drain, and assessor-boundary requirements.
