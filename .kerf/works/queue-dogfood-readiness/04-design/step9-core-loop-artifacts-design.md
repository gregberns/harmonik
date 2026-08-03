# Change Design — Step 9 and core-loop artifacts

## Current state

Step 9 covers the live-daemon and real-agent gap.
The matrix keeps per-cell files but writes summary data to stdout.
`make core-loop-lt` defaults to Pi and removes its scratch path before each run.

## Target state

Add one local non-Pi readiness target for one seeded repeat-safe item.
It uses concurrency one with no remote-worker or feedback option.
It saves matrix output, branch, commit, canary reason, load, batch JSON, and events.
It records machine contention separately from product failure.
Do not change the broader core-loop target default.

## Rationale

The broader target proves a Pi path. Readiness needs a smaller repeatable path.

## Requirements traceability

- `02-components.md`: Step 9 and core-loop proof artifacts.
- `03-research/step9-core-loop-artifacts/findings.md`.
