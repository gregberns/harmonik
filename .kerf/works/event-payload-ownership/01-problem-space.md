# Problem Space — One Definition per Event Payload

## Summary

One event type must have one registered payload definition. Producers, the
event registry, replay, and consumers must use that definition.

The program has two known forms of divergence. `handler_capabilities` is
registered to `core.HandlerCapabilitiesPayload`, but the handler protocol emits
`handlercontract.HandlerCapabilitiesMsg`. Their JSON key and version element
type differ. `run_started` is registered to `core.RunStartedPayload`, but the
daemon emits `workloopRunStartedPayload`.

The two old direct JSONL envelope writers are not part of this work. They now
write `core.Event` envelopes with event IDs and schema versions.

## Goals

- Define one payload owner for each event type.
- Make registered decoding agree with emitted bytes.
- Make replay and all runtime consumers decode the same payload shape.
- Add payload definitions for registered event types that have none.
- State the wire contract in the event specification.

## Evidence in Scope

- `hk-b882r`: the registered `handler_capabilities` decoder does not match the
  bytes emitted by the handler protocol.
- `hk-71dff`: `liveness_halt` and `stale_open_bead_detected` have event types
  but no registered payload definitions.
- `run_started`: `internal/core` and `internal/daemon` define different shapes.
- Five further direct payload writers shadow existing core payload types:
  `agent_failed`, `session_keeper_idle_crew`, `decision_required`, and two
  `decision_acknowledged` paths.

## Boundaries

Bravo owns Step 13 planning only. Alpha owns the final `internal/core` decision
and implementation. Bravo must not edit Alpha-owned emitters without a scoped
handoff.

This work does not change the two repaired JSONL envelope writers. It does not
change Step 14. Step 14 is source inventory only until Step 10 is complete.
Step 27a is deferred until Step 10 is complete.

## Decision

The work rejects both earlier options. It does not make the specification match
an incomplete live record. It does not make the current path fill required
fields with a generated value.

The selected direction is a resolved immutable workflow descriptor. The daemon
must resolve and validate the workflow before it emits `run_started`. The
descriptor supplies the durable workflow identity, version, input reference,
and selected graph. `run_started`, the registry, replay, and reconciliation
then use the one core record derived from that descriptor.

The user-facing single-shot choice becomes a named no-review DOT graph. It does
not keep a second imperative executor. The current normal default remains the
reviewed `standard-bead.dot` graph. The no-review graph remains an explicit,
audited selection.

Alpha owns the core implementation and the final scoped handoff. This planning
decision does not authorize production edits.

## Success Criteria

- Each registered event type has one payload type and one wire shape.
- Registry decoding, replay decoding, and live consumers agree on that shape.
- The event specification names the fields and version rule for changed types.
- The implementation plan separates Alpha-owned emitters from Bravo planning.

## Spec Areas

- `specs/event-model.md`
- `specs/handler-contract.md` for the handler progress message boundary
- `specs/execution-model.md` only if the final run-started decision changes its
  declared record
