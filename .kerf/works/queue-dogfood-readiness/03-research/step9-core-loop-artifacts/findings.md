# Research — Step 9 and core-loop proof artifacts

## Questions

1. What gap does the live pass cover?
2. Which script produces batch evidence?
3. Where does the matrix result live?
4. Does the current default match the narrow canary?

## Findings

- `DECOMPOSITION-MAP.md` Step 9 says scenario tests do not start a shipped
  daemon process or a real agent. A scratch live pass covers that process gap.
- `scripts/core-loop-matrix.sh` creates one batch artifact per cell. With
  `--assert`, it keeps event streams under
  `<scratch>/.harmonik/matrix-captures/`.
- The matrix emits `MATRIX_SUMMARY` and optional `MATRIX_JSON` on stdout. It
  does not create one durable matrix-summary file.
- `make core-loop-lt` creates a fresh scratch clone and removes its configured
  scratch path before each run.
- The current core-loop target defaults to `pi:local`. This conflicts with the
  no-Pi first-canary limit.

## Patterns to keep

- Retain the branch, commit, canary, load record, batch JSON, event captures,
  and final matrix JSON or captured stdout.
- Record machine contention separately from a product failure.

## Risks and decisions

- Do not use the broad default core-loop target unchanged for the first canary.
- Define an explicit local, non-Pi, single-item invocation and its repeat-safe
  reason before the proof run.
