# Event Payload Ownership — Change Design

Status: proposed. Alpha owns the final core decision. This design does not
authorize a production change.

## Current state

`specs/event-model.md` names one payload field list for `run_started` and
`handler_capabilities`. The run path writes a different `run_started` record.
The handler watcher publishes the raw handler progress message as the durable
`handler_capabilities` event. The core registry and replay paths therefore see
types that do not describe the bytes that producers wrote.

`liveness_halt` and `stale_open_bead_detected` have event type constants and
producers. They lack registered core payload records.

## Target state

### Common ownership rule

`specs/event-model.md` states that each registered event has one durable core
payload record. Producers must construct that record or call the boundary that
constructs it. The registry, journal, strict replay decoder, reconciliation
reader, and consumers use the same record.

The rule does not prohibit an input transport record. A transport record ends
at its boundary and must be translated before event publication when it differs
from the durable record.

### `handler_capabilities`

`specs/handler-contract.md` keeps the raw progress record:
`{type, supported_versions[], claude_session_id?}`. The design recommends that
the watcher translate it into the selected durable core record before it emits
the event. The watcher already owns the session context and has the run context
when a lifecycle machine is present.

The selected durable record is `core.HandlerCapabilitiesPayload`. It carries
required `run_id`, `session_id`, and `protocol_versions_supported`, plus
optional `claude_session_id`. The watcher converts the raw ordered integer
list to ordered decimal strings before it emits the registered core record.
`registerAgentEvents` remains its registry boundary. The optional Claude field
is additive. The payload remains version 1 with its current additive
`PayloadCompatEntry`. The raw wire record must not be republished as a
different core type.

### `run_started`

The selected design is neither an event-model retreat nor a late field fill.
The daemon resolves and validates one immutable `core.WorkflowDescriptor`
before it emits `run_started`. Its `WorkflowID` is a validated string that
equals the selected DOT graph's declared logical `workflow_id`. Its
`WorkflowVersion` is that graph's declared `version`. This pair carries only
workflow identity and declared version. The resolver returns it beside the
input reference, graph selection, resolved mode and policy, and audit
provenance. The event payload adds the resolved launch values and existing run,
queue, worker, and start-time observability fields to that core record.
Version 2 requires `started_at`, `worker_name`, and `worker_os`. The worker
fields are null for a local run. Queue fields remain optional additive fields.

The descriptor records selected graph identity only. It is also the input to
the DOT executor. It does not carry the graph body, graph configuration,
selection source, mode, or policy. This removes the current ordering defect
where `run_started` occurs before graph load and a later helper creates a
random workflow identifier. The graph parser and core type system must agree
on this representation. A graph's logical `workflow_id` must not be silently
converted into a random UUID. The flat JSON keys remain `workflow_id` and
`workflow_version`. Old UUID-form values remain readable, but no new DOT
execution creates a workflow UUID.

`workflow_id` becomes a required typed graph-level field. A new logical ID
matches `^[A-Za-z][A-Za-z0-9-]*$`. A legacy UUID text value remains readable.
`Graph.Name` and a source filename are not fallbacks: the name is optional and
the parser keeps the filename only for error messages. The graph must resolve before
`run_started`; the private `workloopRunStartedPayload` is then replaced with
the registered `core.RunStartedPayload` rather than maintained as a second
durable record.

The current one-shot `workflow:single` selection becomes an explicit no-review
DOT graph: an agentic implementer node followed by the successful `close`
terminal node. Its runtime artifact is `internal/daemon/no-review-bead.dot`.
Its byte-identical exemplar is `specs/examples/no-review-bead.dot`. It declares
`workflow_id="no-review-bead"` and `version="1.0"`, and the daemon registers
it beside `standard-bead.dot`. The reviewed `standard-bead.dot` graph remains
the default.

A legacy `workflow:single` label resolves this graph before `run_started` with
`workflow_selection_source=legacy_single_label`. A CLI-created or persisted
tier-0 queue item with `WorkflowMode=single` resolves the same graph with
`workflow_selection_source=queue_item_single_mode`. The raw queue field stays
for legacy audit. Both resolve to mode `dot`. Neither may use the imperative
single dispatcher.

`review_policy` is resolver-owned. Only the registered `no-review-bead` version
`1.0` descriptor, selected through one of those two legacy sources, may yield
`no_review`. A DOT attribute or custom graph cannot claim that policy. The
current single tail is deleted only after its remaining behavior is ported or
intentionally retired.

### `run_started` migration

The private daemon payload is legacy version 1. The registered core payload is
also version 1, but it has never described that writer. The unified typed core
record is version 2. Version 2 is a breaking per-type payload change with an
N-1 version-1 read window.

`registerRunLifecycle` must register the version-2 record. Its
`PayloadCompatEntry` must name current version 2, previous version 1, a
holding compatibility window, and non-additive classification. Tolerant replay
may read private version-1 bytes for run correlation. Restart reconciliation
keeps a read-only legacy-v1 decoder for bead and queue attribution. Strict
replay accepts the registered version-2 record only. The legacy decoder emits
nothing and is not a second payload definition.

### Unregistered payloads

`specs/event-model.md` adds typed records for `liveness_halt` and
`stale_open_bead_detected`. Alpha implementation then adds their core types and
registry entries before any producer conversion.

## Requirements traceability

- One owner and one JSON shape: Problem Space goals 1 to 3.
- Event specification names fields and version form: Problem Space goal 4.
- Raw handler boundary: `hk-b882r` evidence.
- Missing registered records: `hk-71dff` evidence.
- Alpha implementation boundary: Problem Space boundary and operator directive.

## Dependencies and implementation boundary

The workflow-identity change and the version-2 start-event migration are first.
The event-model amendment follows them.
The handler-contract wording follows the selected durable handler record.
`execution-model.md`, `workflow-graph.md`, and `process-lifecycle.md` need
coherent mode and identity wording. No production edit starts until Alpha
accepts this scope and gives a scoped handoff for its owned files.
