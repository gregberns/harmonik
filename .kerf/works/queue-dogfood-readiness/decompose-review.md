# Decompose Review — Queue dogfood readiness

## Round 1 — REQUEST_CHANGES

The review found five gaps:

1. Add `run-state-machine.md`. Its shutdown-drain edge conflicted with the
   required remote branch synchronization.
2. Expand `process-lifecycle.md` for the recovery command and RPC.
3. Move first-canary limits out of the general queue model.
4. Add durability-proof, Step 9, core-loop, event-input, and assessor-record
   areas.
5. State concrete durable-boundary and controlled-load requirements.

All findings were corrected in `02-components.md`.

## Round 2 — APPROVE

An independent reviewer confirmed that the merged process-lifecycle area covers
failed-item recovery, daemon-down behavior, handler separation, the controlled
batch boundary, the separate assessor, and proof-artifact retention.

The map lists the affected existing specs with concrete target requirements and
dependencies. It maps every problem-space goal to a spec or operational area.
The first-canary limits are scoped to the scratch procedure, mission, handoff,
and assessor report. No new normative spec is needed unless Research finds a
missing contract field.

## Verdict

APPROVE — advance to Research.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Decompose review — Queue dogfood readiness

#### Round 1 — BLOCK

Three independent reviews found that the first component map did not define the
failed-item recovery transition. It also omitted the current owners of ordered
drain, worktree retention, recovery observation, and assessor handoff.

The reviewers also found that `beads-integration.md` was not justified by the
listed operational work. The first-canary limits were too vague and were placed
wrongly in the general queue model.

#### Resolution

`02-components.md` now defines the required durable recovery properties. It
adds `operator-nfr.md`, `workspace-model.md`, `event-model.md`,
`assessor-handoff-schema.md`, and a conditional `run-state-machine.md` area.
It moves the first-canary limits to the controlled activation gate, runbook,
mission, and assessor report. It removes `beads-integration.md` unless research
finds a specific Beads transition.

The work needs no new spec. The integration pass must publish one recovery
matrix across the existing state owners.

#### Round 2 — PASS

Three independent reviewers approved the corrected component map. It now maps
every problem-space goal to a justified area. No reviewer found an unresolved
component or dependency gap.
