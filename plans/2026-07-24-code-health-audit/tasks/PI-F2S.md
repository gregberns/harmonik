# PI-F2S — Adopt latch and finalization in single mode

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-L1A`, `PI-L1B`, `PI-L1C`, `PI-F0`, `PS-02`, `BR-03`
- Work type: serial workloop integration

## Objective

In the extracted single-mode executor, bind terminal delivery through the
shared phase scope and run harness-aware finalization before phase-complete and
no-commit/terminal handling.

## Exclusive lease

The extracted single-mode executor and focused tests. Only the old single-mode
region of `workloop.go` required to delete a superseded path may be edited.
Conflicts with other workloop tasks; the coordinator serializes them.

## Acceptance

Pi and Codex use their own finalizers; fallback-created commit advances reported
HEAD; immediate terminal event cannot disappear; Claude path is unchanged.

## Verification

Single-mode controlled process scenarios, race, lint/UBS, Sol review, check-fast.

## Escalate when

Stop on terminalization-policy changes owned by `JR-03`.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
