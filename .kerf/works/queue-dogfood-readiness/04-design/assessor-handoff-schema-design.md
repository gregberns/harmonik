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

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Assessor handoff schema change design

#### Current state

The schema supports merge and deploy gates. It validates a branch and report
path but not a controlled-batch profile or durable proof bundle.

#### Target state

Add a `readiness` gate type and bump the schema version. Require a candidate
commit, branch context, reply target, distinct operator decision owner,
machine-readable canary profile, and durable artifact map. The profile fixes
one identified local stream item, no append, concurrency one, no remote, Pi,
cross-repository, or wave work, plus its repeat-safety limit. PASS authorizes
only that profile after the decision owner acts.

#### Rationale

A deploy encoding imports unrelated release rules. A dedicated type prevents
free-text widening of the canary.

#### Requirements traceability

Addresses assessor schema and controlled-activation requirements.
