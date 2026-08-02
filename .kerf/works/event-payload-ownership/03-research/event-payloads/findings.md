# Event Payload Evidence — 2026-08-01

## Measured scope

The core registry has a `handler_capabilities` entry for
`HandlerCapabilitiesPayload`. The handler contract sends raw
`HandlerCapabilitiesMsg` bytes through the watcher. The core form uses
`run_id`, `session_id`, and `protocol_versions_supported` as strings. The
handler form uses `type`, `supported_versions` as integers, and optional
`claude_session_id`. The watcher publishes the raw handler line with the same
event name. This is one event name with competing payload definitions.

The run path emits `workloopRunStartedPayload`. Its durable fields include queue
identity, queue index, worker name, worker operating system, and mode. The
registered core `RunStartedPayload` expects workflow identity and workflow
version. The current single-mode run path has no `workflow_id` source. It must
not create one only to fit a payload type.

The registry and replay decoder use the core registration path. A producer that
uses a different shape creates an event that is not described by its registered
payload owner.

The source inventory also found these map-shaped Step 13 producers:

- `internal/daemon/movementgovernor.go` `onHalt` for `liveness_halt`.
- `internal/daemon/eagerfill_em063.go` `emitStaleOpenBeadDetected` for
  `stale_open_bead_detected`.
- `internal/handlercontract/watcher_hc011.go` `buildWatcherFailedPayload` for
  `agent_failed`.
- `internal/keeper/step.go` `stepIdleRestartTick` for
  `session_keeper_idle_crew`.
- `internal/sentinel/trip_ev043b.go` for the decision-required,
  decision-acknowledged, and legitimate-halt acknowledgment events.

The `agent_failed`, session keeper, and sentinel cases have related core
payload types. `liveness_halt` and `stale_open_bead_detected` need registered
payload definitions.

## Existing evidence

hk-b882r and hk-71dff are evidence in this work. They are not separate defect
work. They establish that event type, emitted JSON, registry decoding, and
consumer expectations must agree.

## Rejected options

### A. Amend the core payload specification to match emitted bytes

This option makes the live run-path payload the owned shape for
`run_started`. It needs an event-model amendment that replaces required
`workflow_id`, `workflow_version`, and `input_ref` with the fields emitted by
the work loop. The existing optional queue fields remain. It adds the worker
and start-time fields emitted today. It also needs aligned core registry,
replay, and reconciliation decoding. It needs no invented `workflow_id`
because the single-mode run path does not have one.

### B. Change the run path to emit the current core specification payload

This option preserves the current core `RunStartedPayload` definition. It needs
a real source for `workflow_id`, `workflow_version`, and `input_ref`, plus a
run-path mapping that uses them. It needs a decision about missing worker and
start-time observability fields. The current single-mode path has no
`workflow_id`. It must either gain a real workflow identity or the
specification must permit its absence.

Neither option is sufficient. `beadRunOne` emits `run_started` before it loads
the DOT graph. `driveDotWorkflow` later creates a random `WorkflowID` only to
satisfy an in-memory helper. The graph parser retains `workflow_id` only as an
informational string, while `standard-bead.dot` declares `standard-bead` rather
than the core UUID type. Neither value is a durable workflow identity for the
event.

## Selected direction

Resolve and validate an immutable workflow descriptor before `run_started`.
The descriptor must own a real identity source, declared version, input
reference, graph selection, and workflow mode. The core event record is emitted
from that descriptor. It removes the random helper identity and makes replay
read the same record the run used.

The current `workflow:single` selection is used only twice in the plan's
observed event window. It becomes an explicit no-review DOT graph with
`implement → close`. The normal default stays the reviewed `standard-bead.dot`
graph. Once the tail-only behavior is ported or deliberately retired, the
single-mode tail and its dispatch enum branch are deleted.

This direction agrees with the program's Step 7: make single-shot a graph and
delete the single-mode tail. Alpha must implement the descriptor and grant a
scoped handoff before any producer changes.
