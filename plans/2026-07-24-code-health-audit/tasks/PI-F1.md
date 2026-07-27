# PI-F1 — Adopt latch and finalization in review-loop

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-L1A`, `PI-L1B`, `PI-L1C`, `PI-F0`, `PS-02`, `RL-03`
- Work type: serial review-loop integration

## Objective

In the extracted review phase shells, bind the shared phase scope to the
launched session and finalize ProcessExit work before phase-complete,
no-commit, or reviewer decisions.

## Exclusive lease

Extracted review phase shells and one focused scenario test. Only the old
reviewloop region required to delete a superseded path may be edited. No
single/DOT files. Exclusive reviewloop writer.

## Acceptance

Immediate `agent_end` is not lost; finalizer-created/amended commit updates HEAD;
phase-complete reports final truth; no-commit/reviewer sees the updated HEAD.

## Verification

Immediate-event and edit-without-commit scenarios, race, lint/UBS, Sol review,
check-fast.

## Escalate when

Stop if review-loop must change its verdict or iteration policy.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
