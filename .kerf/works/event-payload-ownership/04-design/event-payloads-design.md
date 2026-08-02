# Event Payloads — Change Design

## Current state

The durable event registry names core payloads for `run_started` and
`handler_capabilities`. The live writers use incompatible shapes. Two emitted
event types, `liveness_halt` and `stale_open_bead_detected`, have no registered
core payload.

## Target state

`event-model.md` defines one registered core payload for every cross-bus event.
The full target text is `05-spec-drafts/event-model.md`.

`run_started` becomes payload version 2. It uses the resolved
`WorkflowDescriptor` identity and retains the run-start observations:
`started_at`, `worker_name`, and `worker_os`. Worker fields are required and
null for local runs. Queue fields remain optional. The v2 registry entry has an
N-1 reader window for legacy private version-1 bytes. Strict decode accepts
v2. Replay and restart reconciliation use the legacy reader only for old data.

`handler_capabilities` uses `core.HandlerCapabilitiesPayload`. The daemon
watcher converts the raw wire record before emission. The durable payload keeps
required run and session correlation, ordered decimal protocol versions, and
optional `claude_session_id`. This is an additive version-1 field change.
`registerAgentEvents` remains the registry boundary.

`liveness_halt` and `stale_open_bead_detected` receive registered core payloads
and concrete event-model schemas. The latter becomes a declared eager-refill
provenance event.

## Rationale

One event name must have one durable shape. The registry, journal, strict
replay, reconciliation, and consumers then agree on the same bytes. The source
evidence is in `03-research/event-payloads/findings.md`.

## Requirements traceability

| Requirement | Target state |
|---|---|
| One durable shape per event | One-owner rule and registered core payloads |
| Truthful run start record | Version-2 `run_started` with resolved identity and retained observations |
| Handler wire boundary | Watcher conversion to `HandlerCapabilitiesPayload` |
| Missing event records | Registered schemas for `liveness_halt` and `stale_open_bead_detected` |
