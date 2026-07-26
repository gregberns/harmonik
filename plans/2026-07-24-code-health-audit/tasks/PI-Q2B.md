# PI-Q2B — Enforce effective-Pi rules on submit and append

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-Q2A`, `CQ-01`
- Work type: bounded queue admission implementation

## Objective

Implement the approved effective-harness resolver/label port and reject unsafe
Pi submit/append without mutating or persisting the queue.

## Exclusive lease

`internal/queue/rpc.go`, exact request/provenance types, narrow ledger port, and
focused RPC tests. No workloop or Pi launch files.

## Acceptance

Label-selected and queue-default Pi obey the same named-queue/explicit-cap rule;
append rechecks new beads; rejection leaves disk/memory/events unchanged;
non-Pi golden paths remain.

## Verification

RPC table/race tests, lint/UBS, Sol review, check-fast.

## Escalate when

Stop if current persisted schema cannot represent the approved provenance.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

