# Decompose review — Queue dogfood readiness

## Round 1 — BLOCK

Three independent reviews found that the first component map did not define the
failed-item recovery transition. It also omitted the current owners of ordered
drain, worktree retention, recovery observation, and assessor handoff.

The reviewers also found that `beads-integration.md` was not justified by the
listed operational work. The first-canary limits were too vague and were placed
wrongly in the general queue model.

## Resolution

`02-components.md` now defines the required durable recovery properties. It
adds `operator-nfr.md`, `workspace-model.md`, `event-model.md`,
`assessor-handoff-schema.md`, and a conditional `run-state-machine.md` area.
It moves the first-canary limits to the controlled activation gate, runbook,
mission, and assessor report. It removes `beads-integration.md` unless research
finds a specific Beads transition.

The work needs no new spec. The integration pass must publish one recovery
matrix across the existing state owners.

## Round 2 — PASS

Three independent reviewers approved the corrected component map. It now maps
every problem-space goal to a justified area. No reviewer found an unresolved
component or dependency gap.
