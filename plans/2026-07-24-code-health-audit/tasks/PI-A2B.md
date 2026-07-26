# PI-A2B — Guard the effective Pi agent directory before launch

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-A1`, `PI-A2A`
- Work type: bounded security wiring

## Objective

Derive the exact effective child agent directory, including a custom
`PI_CODING_AGENT_DIR`, and run the approved disk-auth guard before process
start.

## Exclusive lease

`internal/harness/pi/launchspec.go`, billing-guard call wiring, and focused
launch tests. Serialize with `PI-A1` and `PI-R1`.

## Acceptance

Default/custom dirs inspect the same location the child uses; deny/error causes
zero process start; allowed state preserves argv/env; no global `~/.pi` guess
survives.

## Verification

Focused launch tests, no-process-start oracle, lint/UBS, security review,
check-fast.

## Escalate when

Stop if effective-directory precedence is not settled by `PI-SPEC-01`.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

