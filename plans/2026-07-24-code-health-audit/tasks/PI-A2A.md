# PI-A2A — Parse supported Pi persisted-auth fixtures

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `pi_ralph`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-SPEC-01`
- Work type: pure security parser

## Objective

Implement a pure detector for the approved provider-keyed and retained legacy
auth fixtures. It returns allow, deny with reason, or malformed/unreadable
error; it performs no launch or environment work.

## Exclusive lease

`internal/harness/pi/billingguard.go` auth data types/parser and focused fixture
tests. Do not edit `BuildLaunchSpec`, live credentials, or workflow call sites.

## Acceptance

Approved API-key/OAuth cases follow the finalized policy; malformed, unreadable,
and unknown credential shapes fail closed; tests use fake values and versioned
fixtures; no secret is logged.

## Verification

Pure table tests, fuzz/property cases, lint/UBS, Sol security review.

## Escalate when

Stop on a credential kind absent from the approved spec.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

