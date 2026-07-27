# CQ-CALLER-GROUP-ACTIVATION — Remove standalone normal activation

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `sol_xhigh`
- Model / effort: `gpt-5.6-sol` / `xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-03`
- Conflicts with: every active `dispatch_spine` card
- Lease family: `dispatch_spine`

## Objective

Remove standalone normal group activation now that submit and group advance
install activation transactionally. Retain only explicitly classified legacy
recovery activation.

## Exact ownership

`internal/daemon/workloop.go` `activateFirstPendingGroup`,
`activateFirstPendingGroupLocked`, their callers, and focused tests.

## Non-goals

No reservation, terminal, claim/Run, deferred maintenance, adoption, or queue
schema changes.

## Proof, intermediate state, rollback

Fail first by proving submit/advance persists once, no normal active queue has
all groups pending, and legacy activation requires classified state plus a
current generation. Mutation tests re-enable an ordinary standalone caller and
must fail. Accepted intermediate: normal standalone activation is gone while
terminal logic is untouched. Pass targeted race/restart tests, UBS, check-fast,
and Sol review. Roll back only both helpers/callers/tests. **COMMIT EXPLICITLY.**
