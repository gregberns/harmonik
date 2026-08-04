# Event model change design

## Current state

`specs/event-model.md` defines queue observations. Queue transactions are
event-free. Callers emit events after persistence.

## Target state

No normative event-model text changes in this work.

The new transition surface returns a durable result. The operator consumer uses
that result before it builds or emits `queue_paused`. Deferred recovery remains
event-free. Completion and cancellation preserve their current caller-owned
observation and cleanup order.

## Rationale

Event delivery is not recovery evidence. Returning a durable result keeps the
existing persist-before-emit rule clear and avoids bus access in the queue
package.

## Requirements traceability

| Requirement | Design response |
| --- | --- |
| QM-063 | Emit only after the committed result. |
| Deferred recovery | Do not add an event. |
| Event ownership | Keep the event bus outside queue transition code. |
