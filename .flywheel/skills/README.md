# .flywheel/skills/

Markdown procedure files the orchestrator fetches on demand via the `read_skill(name)` tool — the Yegge "fat skills" pattern. The model decides when to read; the harness does not bake procedures into the prompt.

## Planned initial catalog (not yet authored)

- `triage-failure.md` — what to do after a `run_failed` event
- `investigate-run.md` — when the same bead fails twice
- `compose-batch.md` — how to compose the next dispatch batch from `kerf next`
- `escalate.md` — when and how to surface a decision to the operator
- `reconcile-state.md` — what to do at startup before the first batch

Each skill is a single markdown file. The orchestrator reads it verbatim on demand.

See the layered-instructions design at `/Users/gb/.kerf/projects/gregberns-harmonik/flywheel/03-research/layered-instructions/findings.md`.
