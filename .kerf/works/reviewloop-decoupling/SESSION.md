# SESSION — reviewloop-decoupling

## State

Kerf plan is complete and independently approved through research, change design, spec draft,
integration, and tasks. Operator signoffs were explicitly waived by the operator. Status is `ready`;
implementation proceeds autonomously on branch `phase1-session-restart-substrate`.

## Locked direction

- Decompose reviewloop in place before relocation.
- Pure cycle/allowance policy; I/O through narrow owner ports.
- Phase-owned close/join barrier.
- One leased run workspace plus unleased exact-SHA reviewer projections.
- Awaitable continuity checkpoint; Minted checkpoint before `version_selected`, Captured before Retask.
- Thin coordinator moves only after the L8 dependency probe reaches zero.
- No generic god port, full reducer/effect interpreter, DOT kernel conversion, or L12/L13.

## Implementation order

Read `07-tasks.md`. Begin R0 characterization, then execute the documented DAG. All scenario and
exploratory beads are terminal dependencies. Preserve the pre-existing `STATUS.md` deletion and do not
push without explicit operator request.

