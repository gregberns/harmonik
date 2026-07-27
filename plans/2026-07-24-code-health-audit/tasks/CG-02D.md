# CG-02D — Delete temporal graph construction

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `CG-02C`
- Work type: final composition cleanup

## Objective

Delete the remaining late injection, partial bundle population, compatibility
constructors, and dead `workLoopDeps` fields after all production readers have
migrated. Do not add behavior.

## Exclusive lease

`workLoopDeps` construction/injection, dead compatibility constructors/fields,
freeze/architecture gates, and focused tests. Sole `composition_spine` writer.

## Required work

1. Prove zero production readers for every deletion.
2. Remove late nil population and temporal injection.
3. Delete or reduce `workLoopDeps` to the exact approved outer-service target.
4. Ratchet forbidden compatibility symbols to zero.
5. Preserve one green commit.

## Acceptance

- Production graph is constructor-complete before work starts.
- No runnable partial bundle or generic service locator remains.
- All exact architecture targets pass.

## Verification

Zero-reader/freeze mutations, full composition/race/repeat, depguard and
architecture gates, lint/UBS, and `make check-short`.

## Escalate when

Stop if any compatibility reader remains; create a bounded caller card.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
