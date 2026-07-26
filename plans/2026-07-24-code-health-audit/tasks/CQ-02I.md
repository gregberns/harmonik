# CQ-02I — Implement the reviewed queue transaction primitive

## Dispatch metadata

- Group / priority: core queue / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `CQ-02`, `CQ-MIG-01`
- Work type: durability implementation

## Objective

Implement the exact mutation/persist/install primitive approved by `CQ-02`,
with injected failures at every durable boundary. Do not migrate all callers in
this task.

## Exclusive lease

The exact queue persistence/transaction files named by the finalized contract,
plus focused fault tests. RPC and workloop caller migrations are forbidden.

## Acceptance

Clone → mutate → persist → install order is enforced by API shape; failure
cannot expose memory ahead of disk; generation/lock rules match the spec; every
fault cut has a deterministic oracle.

## Verification

Targeted fault tests, race where applicable, vet/lint/UBS, review, check-fast.

## Escalate when

Stop on any divergence from the finalized contract or compatibility plan.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**

