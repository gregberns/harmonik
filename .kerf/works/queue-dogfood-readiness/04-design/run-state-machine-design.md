# Run state machine change design

## Current state

RSM-021 defines a shutdown drain path, but production shutdown does not call
it. Its state is process memory only.

## Target state

Keep RSM-021 as the only in-process drain terminal spine. Wire it only after
the terminal-recovery record is durable. The matrix has five rows: no commit,
unmerged commit, merge in progress, merged but unclosed, and merge failure.
Each row permits exactly one of redispatch, merge, close, or queue release.

## Rationale

One terminal spine prevents duplicate merge and close behavior.

## Requirements traceability

Addresses run-state-machine requirements and related execution research.
