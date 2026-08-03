# Scratch daemon runbook change design

## Current state

The scratch harness is isolated and general. It builds its current checkout and
retains batch JSON, but not candidate proof, complete events, or a readiness
evidence bundle.

## Target state

Add a controlled-readiness section. It fetches, checks out, verifies, and
records the candidate commit before build. It requires a manifest with the
candidate, effective configuration, controlled-load result, Step 9 result,
core-loop result, daemon log, complete event trace, batch JSON, and assessor
report. It uses a one-item submission path and rejects a broad input profile.
It never calls `feedback` for a passing proof.

## Rationale

The assessor must audit the exact candidate and a retained, narrow proof.

## Requirements traceability

Addresses isolated-proof and first-canary requirements.
