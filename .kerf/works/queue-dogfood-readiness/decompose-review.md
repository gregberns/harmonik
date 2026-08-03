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
