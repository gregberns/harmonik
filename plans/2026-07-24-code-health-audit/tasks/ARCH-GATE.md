# ARCH-GATE — Ratchet structural complexity and ownership

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `ARCH-00`, `ARCH-01`
- Work type: deterministic architecture gate

## Objective

Add a local, reviewable ratchet for the six giant run-path functions, their
file spans, suppressions, raw service-locator reads, and forbidden dependency
directions. The gate must prevent moving or renaming debt from being reported as
decomposition.

## Evidence to verify first

Use the exact commands and baselines in `ARCH-00`; inspect existing runloop
freeze/emitter gates and Makefile gate conventions before designing another
script.

## Exclusive lease

- one new architecture-gate script and its mutation test
- the narrow Makefile wiring for `check-fast` and `check-short`
- initial tracked baseline/target data copied exactly from approved `ARCH-01`
- no production Go files

After this task integrates, target/baseline updates are coordinator-owned.

## Required work

1. Pin symbol complexity and span for `runWorkLoop`, `beadRunOne`,
   `runReviewLoop`, `driveDotWorkflow`, and `dispatchDotAgenticNode`.
2. Pin `executeCognitionGate`, `dot_gate.go`, and `workLoopDeps` field
   count/production reach.
3. Reject new complexity suppressions or forbidden daemon back-edges.
4. Store coordinator-approved per-task complexity/span/reach/import targets
   produced by `ARCH-01`; cards cannot become ready without an exact target.
   Pure moves may preserve but never claim a reduction.
5. Add mutations for symbol rename, comment decoy, path move, baseline increase,
   and forbidden dependency.

## Acceptance

- Current reviewed baseline passes.
- Every mutation fails for the intended reason.
- The gate is deterministic, fast, and wired into both local merge gates.
- A reviewer confirms it cannot be bypassed by comments or file relocation.

## Verification

Shell syntax/static checks, gate, mutation harness, `make check-fast`, scoped
UBS where supported, and `git diff --check`.

## Escalate when

Stop if the only implementation requires network access, an unstable analyzer,
or an unverifiable text-count proxy.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
