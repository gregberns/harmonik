# PI-L1B — Latch terminal signals until a session is bound

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-00`
- Work type: concurrency ownership primitive

## Objective

Add a small `internal/runlaunch` terminal-latch/session-bind helper so an
immediate `agent_end` observed before `Launch` returns is not lost. Binding and
terminal signaling are idempotent and trigger teardown exactly once.

## Exclusive lease

One new/existing `internal/runlaunch` helper and focused tests. No workflow giant
call sites in this task.

## Acceptance

signal-before-bind, bind-before-signal, duplicate signals, nil/error teardown,
and concurrent bind/signal all have deterministic once-only outcomes without
sleeps.

## Verification

Race/repeat tests, lint/UBS, Sol review, check-fast.

## Escalate when

Stop if the helper must own process wait/reap rather than only terminal signal
delivery.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

