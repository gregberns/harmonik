# DOT-02 — Extract agentic DOT node execution

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `DOT-01`, `PS-02`, `BR-00`
- Work type: serial DOT lifecycle decomposition

## Objective

Extract agentic node plan/launch/ready/wait/finalize behavior from
`dispatchDotAgenticNode` behind the shared phase scope and a typed node result.
Tool, gate, and sub-workflow executors remain distinct adapters.

## Exclusive lease

`dispatchDotAgenticNode` in `dot_cascade_core.go`, one agentic executor owner,
exact adapters/tests. Sole `dot_spine` writer; no shared baseline file.

## Required work

1. Consume a complete node plan and phase scope.
2. Remove duplicated process/session lifecycle.
3. Preserve transport, cognition, no-work, finalization, and terminal facts in
   the typed result.
4. Delete the inline implementation and lower symbol metrics.
5. Prove the default production DOT path uses the new executor.

## Acceptance

- Agentic execution contains no graph-progression or queue terminal policy.
- Lifecycle ownership matches single/review modes.
- Production-call-site mutation fails the tests.
- `dispatchDotAgenticNode` becomes a thin adapter or disappears.

## Verification

Agentic outcome matrix, local real-process, fault/race/repeat/leak, architecture
gate, lint/UBS, and `make check-fast`.

## Escalate when

Stop if DOT requires lifecycle semantics incompatible with `PS-01`; amend the
shared contract rather than fork it silently.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
