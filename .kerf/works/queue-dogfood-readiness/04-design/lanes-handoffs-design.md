# Change Design — Lanes and handoffs

## Current state

Alpha owns daemon shutdown and daemon-side release tests.
Bravo owns recovery, queue wiring, CLI, non-daemon proofs, ratchets, and scripts.
The fleet daemon remains stopped. The lane plan lacks a release-contract table.

## Target state

Record the release-contract table and ownership in the lane plan and handoffs.
Do so before either lane changes a shared boundary.
Order work as contract, repairs, controlled test, snapshot, scratch proof,
operator authority, assessor gate, and operator go or no-go decision.

## Rationale

The queue and daemon meet at the release boundary.
Contract-first order prevents incompatible fixes and leaves a reviewable record.

## Requirements traceability

- `02-components.md`: lane plan and live handoffs.
- `03-research/lanes-handoffs/findings.md`.
