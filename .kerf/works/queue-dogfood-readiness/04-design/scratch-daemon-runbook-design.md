# Change Design — Scratch daemon runbook

## Current state

The scratch runner isolates the fleet but permits remote work, waves, and feedback.
Batch files remain under the scratch clone and can disappear during cleanup.

## Target state

Add a readiness-only procedure with a preflight record.
Reject gate evidence with more than one item or concurrent run.
Reject Pi, remote worker, cross-repository target, wave queue, and feedback use.
Copy batch JSON and event capture to retained evidence before cleanup.
Do not restrict the generic scratch command outside the readiness gate.

## Rationale

Canary limits are release controls, not general queue rules.
Artifact retention protects evidence from scratch cleanup.

## Requirements traceability

- `02-components.md`: scratch daemon runbook.
- `03-research/scratch-daemon-runbook/findings.md`.
