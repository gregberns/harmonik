# CQ-CALLER-REVIEW — Migrate review-failure charging

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-02I`, `JR-02`
- Work type: queue transaction adapter migration

## Objective

Migrate `internal/daemon/runports.go` `ChargeReviewLoopFailure` and its narrow
port to the reviewed queue
transaction so the failure count and any resulting budget state become durable
before visibility. Preserve the existing `BudgetPort` consumer contract unless
truthful error propagation requires a reviewed amendment.

## Evidence to verify first

Inspect `internal/daemon/runports.go`, `runloop.BudgetPort`, its production
caller/result use, CQ-00A evidence, and budget/terminal specs.

## Exclusive lease

`daemonBudget.ChargeReviewLoopFailure`, the narrow BudgetPort signature only if
required, and focused tests. No `workloop.go` mode/terminal edits.

## Required work

1. Add persist-failure, limit-crossing, and retry tests.
2. Mutate through clone → persist → install.
3. Propagate a truthful typed result; never report charged on failed durability.
4. Preserve one production caller and delete fallback mutation.

## Acceptance

- Failed persistence does not increment installed state.
- Retry charges once and returns the correct budget decision.
- Production caller cannot mistake failure for exhaustion or success.

Accepted intermediate: review charging is durable; terminal policy remains
`JR-03`. Roll back only the named port/region/tests.

## Verification

Fault/property/race/repeat and port-conformance tests, lint/UBS, and
`make check-fast`.

## Escalate when

Stop if changing error propagation alters normative review terminal policy.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
