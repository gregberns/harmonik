# Step 13 Component Map

## Producers

`internal/core` owns the registered event type and payload registry. The run
path has a second producer in `internal/daemon/workloop.go`.

The two old direct JSONL envelope writers are already fixed. They are
`internal/queue/cli/cancel.go` `emitQueueCancelEvent` and
`cmd/harmonik/handler.go` `emitHandlerResumedEvent`. They are not part of this
payload ownership decision.

## Payload definitions

`internal/core/agentevents_hqwn59.go` defines `HandlerCapabilitiesPayload`.
`internal/handlercontract/versionnego_hc009.go` defines the wire message
`HandlerCapabilitiesMsg`. The field names and version element types differ.

`internal/core/runstartedpayload.go` defines `RunStartedPayload`.
`internal/daemon/workloop.go` defines `workloopRunStartedPayload`. The daemon
form is the live run-path payload. It has queue and worker fields. The core
form has workflow fields.

Five other producers reuse a core payload type. The remaining untyped
producers build payload maps for `liveness_halt` and `stale_open_bead_detected`.

## Registry, replay, and consumers

`internal/core/eventreg_hqwn59.go` is the registry entry point. The replay
path includes `internal/replay/replay.go`, `internal/replay/checkers.go`, and
the `RunStartedPayload` type switch in `internal/replay/runcheckers.go`.
`internal/daemon/runinflightreconcile_hkr73qr.go` is the other production
reader of the durable `run_started` bytes. It reads them with the daemon shadow
type.

The complete production reference set for `run_started` is the core registry
and type, `workloop.go`, `runinflightreconcile_hkr73qr.go`, and
`runcheckers.go`. The `handler_capabilities` set is the core registry and type,
the Claude handler producer, and `versionnego_hc009.go`. The event sink, JSONL
journal, and strict replay decode are shared consumers of every registered
event. They must use one registered shape for each event name.

## Ownership boundary

Alpha owns the final `internal/core` decision and implementation. Bravo owns
this planning work only. Bravo must not change Alpha-owned emitters without a
scoped handoff.

## Specification areas

### `specs/event-model.md`

This is the required behavior owner. It must describe one durable payload shape
for each registered event. It must state the selected `run_started` fields,
define the durable `handler_capabilities` boundary, and add the records for
`liveness_halt` and `stale_open_bead_detected`.

It depends on Alpha selecting the final `run_started` option. The handler
message boundary depends on `specs/handler-contract.md` agreeing on where the
raw progress record ends.

### `specs/handler-contract.md`

This spec owns the handler progress stream. It must distinguish that transport
message from the durable event when their shapes differ. It must say whether the
watcher translates the message before publication and which context it adds.

It depends on the `event-model` durable record. It does not define the
`run_started` record.

### `specs/execution-model.md`

This file already requires `workflow_id`, `workflow_version`, and `input_ref`
for `run_started`. It must require the resolved descriptor before emission. It
must not permit an event to use a random helper identity.

It depends on the canonical workflow identity definition in
`specs/workflow-graph.md` and the selected `event-model` record.

### `specs/workflow-graph.md` and `specs/process-lifecycle.md`

These specs must define the graph identity source and distinguish a no-review
graph selection from a separate dispatch mode. They depend on the descriptor
shape. No new specification file is needed.
