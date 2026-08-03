# Event model change design

## Current state

Queue events show group completion and pause. Queue status cannot prove a
failed recovery request or result.

## Target state

Add Class-F failed-recovery requested and completed or rejected events. Their
payload includes queue, group, item count, prior and current state, recovery
receipt, retired run identities, and reason. Emit only after durable commit.
Add one correlation field from terminal recovery to the existing workspace and
run observations. Keep `run_*` as the only item terminal events.

## Rationale

An assessor needs audit evidence without treating event log as authority.

## Requirements traceability

Addresses recovery observation and replay-proof requirements.
