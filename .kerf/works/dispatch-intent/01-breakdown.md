# Dispatch intent breakdown

## Source contract

Use `specs/live-bead-state.md` LB-002, LB-004, and LB-007 through LB-009.
This work implements only C20. C21 owns persistence and startup replay.

## Work units

1. Add a pure `internal/dispatch` package.
2. Define the intent phase and immutable dispatch binding.
3. Validate every phase before marshal and after unmarshal.
4. Define the transaction result classes from LB-008.
5. Add exhaustive value and JSON tests.
6. Add a mutation check for each phase guard.

## Package boundary

`internal/dispatch` can import `internal/core`, `internal/queue`, and the
standard library. Queue owns queue-name validation. The new package cannot
import daemon, queue wiring, the Beads adapter, run storage, or I/O packages.
C20 defines values only. C21 will own the durable adapter.

## Type plan

The immutable base binding contains the queue ID, normalized queue name, group
index, item index, bead ID, run ID, and claim transition ID.

The phase data is additive:

- `prepared` has only the base binding.
- `claim_durable` has the base binding and no new identity.
- `run_durable` adds the exact durable run-record identity.
- `handoff_durable` adds the exact session and worktree lease identities.

Validation rejects a later-phase field in an earlier phase. Validation also
rejects a missing field that the current phase requires.

## Result plan

The result class is one of `committed`, `replayable`, `refused`, or
`repair-required`. A committed or replayable result carries a valid intent.
A refused result carries no intent and one typed refusal reason. A
repair-required result carries no replacement intent and one typed conflict.

## Test units

- Table tests cover each valid phase and every missing or early field.
- JSON tests reject unknown fields, unknown phases, wrong schema, and partial
  nested records.
- Round-trip tests preserve exact valid values.
- Result tests reject contradictory class, intent, refusal, and conflict data.

## Dependency graph

`types -> validation -> JSON tests -> result tests -> review`

## Bead status

The follow-up review records an active bead-creation process blocker. This work
does not create or mutate beads. C20 remains the tracked execution unit in
`CHARLIE-BACKLOG.md`. The test units above stay in the same reviewed change.
