# DOT-GATE-01 — Decompose cognition-gate execution

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-01`, `ARCH-GATE`, `PS-02`, `DOT-02`
- Work type: serial DOT-gate decomposition

## Objective

Separate cognition-gate decision, agentic phase execution, and result mapping in
`dot_gate.go`; route process/session lifecycle through `PS-02`. Leave a thin
gate executor and explicitly decide whether reversible LIFT L9 is resumed or
superseded after decomposition.

## Evidence to verify first

Inspect `executeCognitionGate`, `dispatchDotGateNode`, current gate heartbeat and
paste-inject paths, LIFT L9 blockers, and normative cognition-gate semantics.

## Exclusive lease

`dot_gate.go`, exact new gate decision/executor owners, focused tests, and the
L9 compile probe. Sole `dot_spine` writer; no shared baseline file.

## Required work

1. Extract deterministic gate policy/result mapping from effects.
2. Route agentic lifecycle through the frozen phase adapter.
3. Preserve heartbeat, timeout, cancellation, and gate-file semantics.
4. Delete duplicate watcher/Wait/cleanup paths.
5. Meet exact approved symbol/file targets before any optional L9 move.

## Acceptance

- Gate policy is testable without a process.
- Process/session ownership matches other agentic modes.
- `dot_gate.go` and `executeCognitionGate` meet approved structural targets.
- Evidence records whether L9 is resumed or superseded; a move alone is not
  completion.

## Verification

Policy tables, local process, fault/race/repeat/leak tests, architecture gate,
compile probe, lint/UBS, and `make check-fast`.

## Escalate when

Stop if cognition-gate policy conflicts with the normative spec or needs a
change to the frozen phase contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
