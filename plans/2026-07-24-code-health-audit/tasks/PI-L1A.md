# PI-L1A — Fire Pi NDJSON callbacks outside parser locks

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `pi_ralph`
- Reviewer profile: `terra_high`
- Depends on: `PI-00`
- Work type: narrow concurrency fix

## Objective

Change the interceptor so it parses/updates once-state under its mutex but
invokes session-ID and `agent_end` callbacks after unlocking.

## Exclusive lease

`internal/harness/pi/ndjsonparser.go` and focused parser tests only.

## Acceptance

Callbacks fire once, in stream order, can re-enter safely, never run under the
parser mutex, and bytes pass through unchanged.

## Verification

Deterministic channel tests, race detector, lint/UBS, review.

## Escalate when

Stop if a call-site/session binding change is needed.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

