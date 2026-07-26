# PI-F2S — Adopt latch and finalization in single mode

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-L1A`, `PI-L1B`, `PI-L1C`, `PI-F0`
- Work type: serial workloop integration

## Objective

In `beadRunOne`, bind terminal delivery and run harness-aware finalization before
phase-complete and no-commit/terminal handling.

## Exclusive lease

Single-mode region of `internal/daemon/workloop.go` and focused tests. Conflicts
with `CQ-DEF-01`, `CQ-03`, `JR-01`, and `JR-03`; coordinator serializes them.

## Acceptance

Pi and Codex use their own finalizers; fallback-created commit advances reported
HEAD; immediate terminal event cannot disappear; Claude path is unchanged.

## Verification

Single-mode controlled process scenarios, race, lint/UBS, Sol review, check-fast.

## Escalate when

Stop on terminalization-policy changes owned by `JR-03`.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

