# Spec Draft Review — Declared Substrate Capability Contract

## Round 1

BLOCK. PL-021b item 6a allowed the unreachable independent run-session path to
remain as a declared capability. The accepted design requires removal.

## Round 2

APPROVE. PL-021b item 6a now requires removal of the unreachable path. The
full-copy draft changes only the selected process-lifecycle contract. It keeps
the handler boundary narrow, workflow selection sealed, and pane capture
observation-only. The changelog preserves the Step 12 serialization and the
full run-session removal closure.
