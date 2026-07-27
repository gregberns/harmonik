# PI-F2D — Adopt latch and finalization in DOT mode

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-L1A`, `PI-L1B`, `PI-L1C`, `PI-F0`, `PS-02`, `DOT-03`
- Work type: serial DOT integration

## Objective

In the extracted DOT agentic executor, replace the current “Codex fallback for
every ProcessExit harness” with the shared phase scope and harness-aware
finalizer, ordered before phase-complete and no-progress checks.

## Exclusive lease

The extracted DOT agentic executor and focused tests. Only the old DOT region
required to delete a superseded path may be edited. Exclusive DOT writer.

## Acceptance

Pi never calls Codex messaging/finalization; new HEAD and commit truth feed DOT
phase/no-progress logic; immediate `agent_end` is handled once.

## Verification

DOT Pi/Codex scenarios, race, lint/UBS, Sol review, check-fast.

## Escalate when

Stop if graph transition policy must change.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
