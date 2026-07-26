# PI-F2D — Adopt latch and finalization in DOT mode

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-L1A`, `PI-L1B`, `PI-L1C`, `PI-F0`
- Work type: serial DOT integration

## Objective

Replace the current “Codex fallback for every ProcessExit harness” in
`dispatchDotAgenticNode` with the shared harness-aware finalizer, ordered before
phase-complete and no-progress checks, and adopt the terminal latch.

## Exclusive lease

`internal/daemon/dot_cascade_core.go` and focused DOT tests. Exclusive DOT writer.

## Acceptance

Pi never calls Codex messaging/finalization; new HEAD and commit truth feed DOT
phase/no-progress logic; immediate `agent_end` is handled once.

## Verification

DOT Pi/Codex scenarios, race, lint/UBS, Sol review, check-fast.

## Escalate when

Stop if graph transition policy must change.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

