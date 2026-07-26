# PI-L1C — Bound TERM, KILL, wait, and reap

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-00`
- Work type: process lifecycle ownership

## Objective

Replace `context.Background()` teardown and unbounded TERM wait with one bounded
owner for TERM → grace → KILL → Wait/reap.

## Exclusive lease

`internal/handler/session.go`, `internal/harness/pi/harness.go`, and controlled
subprocess tests. No workflow call sites.

## Acceptance

A TERM-ignoring child is killed and reaped within a bounded test clock/deadline;
already-exited and repeated teardown are safe; wait has one owner; no orphan or
goroutine leak remains.

## Verification

Real local subprocess, race/repeat/leak assertions, lint/UBS, Sol review,
check-fast.

## Escalate when

Stop if full descendant-process-tree ownership is required; that is a broader
process-lifecycle contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

