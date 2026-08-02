# Alpha Implementation Surface — 2026-08-01

This is a source map for the accepted Step 13 direction. It does not authorize
a production edit.

## Descriptor boundary

`beadRunOne` calls `emitRunStarted` before its workflow-mode switch. Its DOT
branch only loads and validates the selected graph after that event. The new
descriptor boundary must therefore sit before `emitRunStarted`.

The descriptor needs one resolved graph source, after parameter substitution,
parse, and validation. It must carry the graph identity, graph version, input
reference, selected graph, execution policy, and the audit provenance for an
explicit no-review selection.

`dot.Graph` keeps a DOT `workflow_id` in `UnknownAttrs`. The checked-in
`standard-bead.dot` value is `standard-bead`. `core.WorkflowID` is a UUID type.
`driveDotWorkflow` currently creates a random `WorkflowID` to construct a
`core.Run`. Alpha must define the canonical representation before it changes
the core record. A random helper value is not an accepted descriptor identity.

## `run_started` change set

| Role | Current symbol | Required disposition |
|---|---|---|
| Live producer | `internal/daemon/workloop.go` `emitRunStarted` and `workloopRunStartedPayload` | Construct the one core payload from the descriptor. Keep queue, worker, and start-time observability when the final record requires it. |
| Core record | `internal/core/runstartedpayload.go` `RunStartedPayload` | Define one record with the chosen identity representation and the descriptor fields. |
| Registry | `internal/core/eventreg_hqwn59.go` `registerRunLifecycle` | Keep `run_started` registered to that one record. |
| DOT executor | `internal/daemon/dot_cascade_core.go` `driveDotWorkflow` | Receive the descriptor or its derived `core.Run`. Remove `WorkflowID(uuid.New())`. |
| Strict replay | `internal/replay/replay.go` `Replay`; `internal/replay/runcheckers.go` `runKey` | Decode the core record through the registry. Preserve run correlation. |
| Restart recovery | `internal/daemon/runinflightreconcile_hkr73qr.go` `reconcileOrphanedRunsOnResume` | Decode the same core record. It needs bead and queue routing only. |
| In-process consumers | `internal/daemon/notifystream.go` `NotifyStreamConsumer.handleRunStarted`; `internal/daemon/spendmeter_hkk3f8g.go` `DaemonSpendMeter.handleRunStarted` | Keep their minimal read set valid. They must not restore a second payload definition. |

Tests that create or decode the old daemon-only payload must move with the
producer. This includes the terminated-run scenario and remote-worker event
assertions. Event-sequence tests that inspect only the event name do not define
the payload shape.

## `handler_capabilities` change set

| Role | Current symbol | Required disposition |
|---|---|---|
| Wire message | `internal/handlercontract/versionnego_hc009.go` `HandlerCapabilitiesMsg` | Keep this transport record at the handler boundary. |
| Publisher | `internal/handlercontract/watcher_hc011.go` watcher event publication | Translate the wire message before durable publication when it differs from the chosen core record. |
| Core record | `internal/core/agentevents_hqwn59.go` `HandlerCapabilitiesPayload` | Own the durable JSON names and version element type. |
| Registry | `internal/core/eventreg_hqwn59.go` `registerAgentLifecycle` | Continue to register only the selected durable record. |

The translation needs run and session context from the watcher. It must retain
the version-negotiation meaning and the Claude session identifier where the
handler contract needs it. Raw handler bytes must not be published under the
core event type.

## Other payload owners

`liveness_halt` from `movementGovernor.onHalt` and
`stale_open_bead_detected` from `emitStaleOpenBeadDetected` need core payload
records and registry entries before their emitters move. The watcher failed
payload, keeper idle event, and sentinel decision events already have related
core records. Their producers need only convert to those registered records.

## One executor migration

The accepted target is one DOT executor. The reviewed `standard-bead.dot`
remains the normal graph. An explicit no-review selection must resolve a named
DOT graph and write audit provenance for that graph selection.

Before deletion, Alpha must trace the current single tail in `beadRunOne`. The
known tail-only behavior covers launch context, post-exit classification,
escape and no-commit guards, session and orphan handling, audit semantics, and
merge retry budget. Port needed behavior to the graph path. Record an explicit
retirement decision for behavior that does not belong in the target. Then
delete the single tail and the mode-dispatch branch.

## Required proof

1. A normal DOT bead emits a typed `run_started` record from the descriptor.
2. An explicit no-review bead emits the same record form and has graph-selection
   audit evidence.
3. Strict replay and restart reconciliation decode that record without a daemon
   shadow payload.
4. The DOT executor uses the descriptor identity. It creates no random helper
   workflow identity.
5. Handler capabilities decode from the registered core payload after watcher
   translation.
6. No executable imperative single tail remains after the migration.
