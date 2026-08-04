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

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Scratch daemon runbook change design

#### Current state

The scratch harness is isolated and general. It builds its current checkout and
retains batch JSON, but not candidate proof, complete events, or a readiness
evidence bundle.

#### Target state

Add a controlled-readiness section. It fetches, checks out, verifies, and
records the candidate commit before build. It requires a manifest with the
candidate, effective configuration, controlled-load result, Step 9 result,
core-loop result, daemon log, complete event trace, batch JSON, and assessor
report. It uses a one-item submission path and rejects a broad input profile.
It never calls `feedback` for a passing proof.

#### Rationale

The assessor must audit the exact candidate and a retained, narrow proof.

#### Requirements traceability

Addresses isolated-proof and first-canary requirements.
