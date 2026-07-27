# BR-02C — Compose the prepared-run resource scope

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `terra_high`
- Reviewer profile: `sol_xhigh`
- Depends on: `BR-02B`
- Work type: serial typed-resource composition

## Objective

Compose placement, worker, tunnel, and worktree leases into one immutable
prepared-run input for mode execution. Do not acquire or own hook, session,
launch, Wait, or terminal resources.

## Exclusive lease

The narrow prepared-resource construction/call sites in `workloop.go`, resource
lease types, and focused tests. Sole `dispatch_spine` writer.

## Required work

1. Construct only from complete acquired leases.
2. Make reverse-order close explicit and idempotent.
3. Pass the prepared input to the mode boundary.
4. Delete compatibility locals and duplicate cleanup.
5. Meet the exact approved metric target.

## Acceptance

- Invalid or partial prepared-run values cannot be constructed.
- Cleanup order is mechanically inspectable.
- Phase/session and terminal ownership remain outside this scope.

## Verification

Constructor-negative, fault/race/repeat tests, architecture gate, lint/UBS, and
`make check-fast`.

## Escalate when

Stop if composition needs mode policy or phase-session ownership.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
