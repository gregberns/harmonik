# CQ-CALLER-BOOTSTRAP — Migrate inline queue bootstrap

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-02I`, `CQ-01`
- Work type: CLI composition caller migration

## Objective

Replace the inline queue bootstrap in `cmd/harmonik.runBeadSubcommandIO` with
the reviewed queue admission/transaction API. The CLI must not create a second
persistence implementation.

## Evidence to verify first

Inspect `cmd/harmonik/run.go`, production CLI routing, CQ-00A ingress evidence,
CQ-01 admission contract, and existing CLI tests.

## Exclusive lease

`cmd/harmonik/run.go` bootstrap region and focused tests. No queue persistence
implementation, workloop, or unrelated CLI router edits.

## Required work

1. Add durability failure and existing-queue tests.
2. Call one supported queue composition API.
3. Remove inline filesystem/persistence/bootstrap logic.
4. Preserve CLI exit/error/output behavior truthfully.

## Acceptance

- CLI bootstrap uses the same durable transaction as supported admission.
- Failure creates no partial queue or false success.
- Existing queue behavior remains idempotent.

## Verification

CLI composition/fault/repeat tests, scoped vet/lint, UBS, and `make check-fast`.

## Escalate when

Stop if no supported API can preserve CLI behavior; amend CQ-01/CQ-06 first.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
