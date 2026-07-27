# BR-02B — Extract tunnel and worktree acquisition

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `BR-02A`
- Work type: serial beadRunOne resource decomposition

## Objective

Extract tunnel and worktree preparation from `beadRunOne` into typed,
reverse-order-cleaned leases. Hook, session, launch, wait, and terminal effects
remain excluded.

## Evidence to verify first

Inspect existing tunnel/codesync, workspace creation/trust, branch-tip, dirty
worktree safeguards, and partial-failure cleanup.

## Exclusive lease

Tunnel/worktree sections of `workloop.go`, exact transport/workspace adapters,
one resource owner, focused tests. Sole `dispatch_spine` writer.

## Required work

1. Acquire only after a valid placement lease.
2. Record tunnel and worktree ownership independently.
3. Unwind partial failure without deleting ambiguous/dirty state.
4. Delete inline resource acquisition.
5. Meet the exact approved metric target.

## Acceptance

- Every acquired tunnel/worktree has one bounded cleanup.
- Dirty or ambiguous state fails closed and is preserved.
- No hook/session/process/terminal ownership enters the resource owner.

## Verification

Per-cut fault tests, local git fixture, race/repeat, architecture gate,
lint/UBS, and `make check-fast`.

## Escalate when

Stop if remote cleanup needs a new protocol or cleanup would delete ambiguous
state.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
