# Change Design — Readiness evidence

## Current state

The scratch runner protects the fleet but permits broad work.
Batch evidence lives in the scratch clone.
The core-loop target defaults to Pi and writes summary data to stdout.
Ratchets do not prove durable storage.
The lane plan names owners but has no release-contract table.

## Target state

Add a readiness-only scratch procedure.
It rejects evidence from more than one item or concurrent run.
It rejects Pi, remote worker, cross-repository target, wave queue, and feedback use.
It copies batch JSON and event capture before cleanup.

Add a local non-Pi core-loop readiness target.
It saves matrix output, branch, commit, canary reason, load, batch JSON, and events.
It classifies machine contention separately from product failure.

Create a release-contract table before implementation.
Each edge names owner, durable state observed, production test, and fault test.
A claimed durable test fails when its write is removed.

Record the table and ownership in the lane plan and both handoffs.
Order work as contract, repairs, controlled test, snapshot, scratch proof,
operator authority, assessor gate, and operator go or no-go decision.

## Rationale

The canary is a temporary release control.
It needs evidence that survives cleanup and clear ownership at shared boundaries.

## Requirements traceability

- `02-components.md`: scratch runbook, durability proof, Step 9, ledger events, and lanes.
- Research: scratch runbook, durability proof, Step 9, ledger events, and lanes findings.
