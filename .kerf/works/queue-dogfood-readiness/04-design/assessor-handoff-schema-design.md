# Change Design — Assessor handoff schema

## Current state

Schema version 2 requires branch, gate, evidence sources, report path, and sender.
An assessor consumes no normal queue work. Invalid frontmatter blocks scratch start.
The schema has no structured canary-limit, load, or artifact fields.

## Target state

Do not amend the schema unless field-gap review finds an unrepresentable fact.
The later mission body states branch, scope, and one local non-Pi canary.
It excludes remote, cross-repository, and wave work and sets concurrency one.
It names retained proof paths, assessor independence, and the report path.
Mission and registry changes require explicit operator authority.

The existing `spawned_by` field is the gate owner. The mission body names
candidate branch, scope label, canary limits, batch path, event-capture path,
matrix-result path, and controlled-load record. These are prose fields, not a
schema extension.

## Rationale

The schema already supplies gate identity and a durable report location.
The mission body can carry the evidence contract without new structure.

## Requirements traceability

- `02-components.md`: assessor handoff schema and mission/report.
- `03-research/assessor-handoff-schema/findings.md`.
