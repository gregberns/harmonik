# Change Design — Handler pause

## Current state

`HP-040` changes only handler state. `HP-043` and `HP-009` say handler resume
does not clear failed or drained queue state. A handler pause holds pending
items at dispatch.

## Target state

Add a recovery cross-link to `HP-043` and `HP-009`. `queue-resume` does not
call `HandlerPauseController`, clear handler state, emit `handler_resumed`, or
bypass the handler gate. It may re-arm an item to pending while its handler
remains paused. Normal held-item and deduplicated held-event behavior applies
on the next dispatch check.

Conversely, handler resume does not invoke queue recovery, re-arm failed items,
or emit `queue_recovered`.

## Rationale

Queue state is per queue and handler state is daemon-wide. Each control action
must be safe without changing the other state machine.

## Requirements traceability

- `02-components.md`: handler-pause compatibility requirements.
- `03-research/handler-pause/findings.md`.
- Session decision that recovery and handler resume stay separate.
