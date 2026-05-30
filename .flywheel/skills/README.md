# .flywheel/skills/

Markdown procedure files the orchestrator fetches on demand via the `read_skill(name)` tool — the Yegge "fat skills" pattern. The model decides when to read; the harness does not bake procedures into the prompt.

## Catalog

- `triage-failure.md` — what to do after a `run_failed` event (CL-055, CL-072 guard #3)
- `investigate-run.md` — when the same bead fails twice or a reviewer emits BLOCK
- `compose-batch.md` — how to compose the next dispatch batch from `kerf next` (CL-071–CL-073)
- `escalate.md` — when and how to surface a decision to the operator (CL-092 security class)
- `reconcile-state.md` — what to do at startup before the first batch (CL-051, CL-054)

Each skill is a single markdown file ≤200 lines. The orchestrator reads it on demand via
`read_skill(name)` (registered in `.pi/extensions/flywheel/index.ts`). The tool returns
`{content, sha}` where `sha` is a SHA-256 content hash (first 12 hex chars) — pin the sha
in any `note()` call that cites the skill to identify the version consulted.

Skills live-reload: editing a skill file takes effect on the next `read_skill()` call without
a process restart (L3b volatility, per the layered-instructions design).

See: `/Users/gb/.kerf/projects/gregberns-harmonik/flywheel/03-research/layered-instructions/findings.md`
