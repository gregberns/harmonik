# PI-F0 — Define a harness-aware ProcessExit finalizer

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-00`
- Work type: finalizer policy/port plus unit tests

## Objective

Create a small dispatcher with injected harness finalizer ports returning typed
`Outcome`, new HEAD, and commit-landed truth. Pi calls Pi fallback, Codex calls
Codex fallback, non-ProcessExit is not applicable, and unknown ProcessExit fails
loud. Define finalization-error policy.

## Exclusive lease

New/existing harness-shared finalizer policy and unit tests; harness commit
adapters may be wrapped, not rewritten. No workflow call sites.

## Acceptance

All harness/completion combinations are exhaustive; git I/O is behind injected
ports; returned HEAD/truth is the sole input to later phase/no-commit decisions.

## Verification

Pure dispatcher tests plus adapter conformance, lint/UBS, Sol review, check-fast.

## Escalate when

Stop if a harness lacks a defined finalization contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
