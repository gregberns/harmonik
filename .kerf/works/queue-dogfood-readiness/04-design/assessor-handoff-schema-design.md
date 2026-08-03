# Assessor handoff schema change design

## Current state

The schema supports merge and deploy gates. It validates a branch and report
path but not a controlled-batch profile or durable proof bundle.

## Target state

Add a `readiness` gate type and bump the schema version. Require a candidate
commit, branch context, reply target, distinct operator decision owner,
machine-readable canary profile, and durable artifact map. The profile fixes
one identified local stream item, no append, concurrency one, no remote, Pi,
cross-repository, or wave work, plus its repeat-safety limit. PASS authorizes
only that profile after the decision owner acts.

## Rationale

A deploy encoding imports unrelated release rules. A dedicated type prevents
free-text widening of the canary.

## Requirements traceability

Addresses assessor schema and controlled-activation requirements.
