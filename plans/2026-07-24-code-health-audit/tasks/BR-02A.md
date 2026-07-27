# BR-02A — Extract placement and worker acquisition

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `BR-01`
- Work type: serial beadRunOne resource decomposition

## Objective

Extract generic caller-authorized placement, worker-slot acquisition, and the
typed placement lease from `beadRunOne`. Harness-specific local/remote policy
remains `PI-R2`; tunnel, worktree, hook, session, and launch are excluded.

## Evidence to verify first

Use the immutable RunPlan, worker registry/capacity contracts, local-only Pi
fence, and exact current acquisition/release branches.

## Exclusive lease

Placement/worker sections of `workloop.go`, one placement lease owner, focused
tests. Sole `dispatch_spine` writer; no shared baseline file.

## Required work

1. Acquire from a complete RunPlan and return a typed lease.
2. Require a typed, already-approved placement intent; do not decide Pi policy.
3. Make slot release exactly once.
4. Delete inline selection/acquisition.
5. Meet the exact approved `beadRunOne` metric target.

## Acceptance

- Placement has one owner and one release path.
- Missing or refused placement intent cannot be bypassed.
- No Pi-specific policy is duplicated; `PI-R2` later migrates its fence here.
- No tunnel/worktree/session/process effect occurs here.

## Verification

Placement tables, fault/race/repeat tests, architecture gate, lint/UBS, and
`make check-fast`.

## Escalate when

Stop if placement requires a new scheduling or remote-security contract.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
