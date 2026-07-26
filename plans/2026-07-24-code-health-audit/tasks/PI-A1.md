# PI-A1 — Fail closed on configured Pi key files and safe models config

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-SPEC-01`
- Work type: security implementation

## Objective

Implement the finalized credential materialization contract. A configured key
file that is unreadable or empty must return a typed error without ambient-env
fallback. Generated models config must never violate the approved secret-at-rest
rule.

## Exclusive lease

`internal/harness/pi/launchspec.go`, its focused tests, and exact config error
types approved by the spec. No billing-guard, workloop, SSH, or queue edits.

## Acceptance

Configured-file read/empty errors fail before launch; absent file configuration
retains the specified env path; selected child env receives only the intended
key; no secret value appears in argv, errors, events, test output, or committed
fixtures; generated dir/file modes match the contract.

## Verification

Focused tests with sentinel secrets plus worktree secret scan, lint/UBS, security
review, check-fast.

## Escalate when

Stop on any spec ambiguity or required edit outside the lease.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
