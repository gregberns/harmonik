# Tasks

## T1 — Plan and normative contract

Update the planning record and `specs/session-keeper.md`. Define the three bands, Stop semantics,
two safe restart paths, operator guard ownership, and the removal of timeout escalation.

## T2 — Message renderer and configuration

Add the selected NOTICE default and configurable NOTICE, WARN, and HARD templates. Set the first
trial thresholds to 170k, 200k, and 220k.

## T3 — Pending request state

Replace no-handoff abort with a durable pending outcome. Preserve request identity and accept a
late marked handoff. Remove timeout-driven force restart.

## T4 — First-class Stop event

Feed Stop into the pure engine. Make a Stop after the handoff marker enter the clear tail. Keep the
assistant transcript observation as a fallback when Stop is unavailable.

## T5 — Shared explicit restart entry

Route validated `restart-now` into the same clear and resume transition where practical. Preserve
the current synchronous CLI contract during migration.

## T6 — Observability and replay tests

Record message submission, Stop, marked handoff, wait reasons, clear, and session change. Add fake
clock scenarios and negative controls.

## T7 — Focused and full verification

Run keeper tests, command tests, `make fast`, and `make full`. Build a keeper binary from this
branch. Deploy only after the branch passes the merge decision.

