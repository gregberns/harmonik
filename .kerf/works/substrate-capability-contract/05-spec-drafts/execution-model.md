# Execution Model Spec-Draft Disposition

## No normative change

`specs/execution-model.md` remains unchanged. Workflow selection stays sealed
before `run_started`. The removed run-session branch is unreachable and does
not change a run lifecycle guarantee.

The process-lifecycle draft records the observable capability timing. The
daemon design records the implementation boundary.
