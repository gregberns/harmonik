# Workflow Identity Decision

Status: accepted design input for the Step 13 spec draft. This file does not
authorize a production or normative-spec change.

## Decision

The durable workflow identity is the immutable pair:

```go
core.WorkflowDescriptor{
    WorkflowID:      core.WorkflowID,
    WorkflowVersion: core.WorkflowVersion,
}
```

`WorkflowID` is a validated named string. Its value is the graph's declared
logical `workflow_id`, such as `standard-bead`. It is not a generated UUID.
`WorkflowVersion` is the graph's declared `version`. The pair is resolved,
substituted, parsed, and validated once before `run_started` emits. The same
pair goes to the run record, `RunStartedPayload`, DOT executor, replay, and
restart reader.

The JSON field names stay `workflow_id` and `workflow_version`. The descriptor
is a Go construction boundary. It does not add a nested JSON object or a
second durable identifier.

The descriptor carries only `WorkflowID` and `WorkflowVersion`. The resolver
returns it beside the input reference, selected graph, resolved mode and
policy, and audit provenance. Those values describe one resolved launch, but
they are not workflow identity and must not become descriptor fields.

## Source evidence

- `internal/daemon/standard-bead.dot` declares
  `workflow_id="standard-bead"` and `version="1.0"`.
- `internal/workflow/dot/parser.go` `buildGraph` puts `workflow_id` into
  `Graph.UnknownAttrs`. `dot.Graph` has no typed identity field.
- `internal/workflow/dot/parser.go` `Parse` documents its `filename` input as
  error-message-only. `Graph.Name` is optional. Neither is a durable identity.
- `internal/daemon/workloop.go` `emitRunStarted` runs before DOT graph load
  and emits the private `workloopRunStartedPayload`.
- `internal/daemon/dot_cascade_core.go` `driveDotWorkflow` then assigns
  `core.WorkflowID(uuid.New())` to its synthesized run.
- `internal/core/runstartedpayload.go` and `internal/core/workflowid.go`
  currently require a UUID-backed `workflow_id`. This conflicts with the
  graph artifacts and is the contract that must change.
- `cmd/harmonik/run.go` writes `--workflow-mode single` to the queue item's
  `WorkflowMode`. `internal/queue/rpc.go` retains that field.
- `internal/daemon/workloop_runplan.go` gives a valid item mode tier-0
  precedence over normal mode resolution. The existing CLI and persisted
  records therefore require an explicit migration path.
- `internal/daemon/standardgraph.go` defines the existing embedded-artifact
  and byte-identical exemplar pattern that the no-review graph follows.

## Required contract changes

1. Change `core.WorkflowID` from UUID-backed to a validated string. The
   accepted graph-source grammar is the union of named ID
   `^[A-Za-z][A-Za-z0-9-]*$` and canonical UUID text. This is based on the
   current declared corpus: named IDs including mixed-case
   `spec-R1-R2-cycle`, plus the UUID-form ID in
   `scenarios/_workflows/smoke-one-node.dot`. Both forms stay readable. UUID
   text is a legacy compatibility form, not a generated canonical identity.
   New DOT execution does not create UUID workflow identities.
2. Add `core.WorkflowDescriptor` as the identity part of workflow resolution.
   The resolver returns the descriptor beside input reference, graph selection,
   resolved mode and policy, and audit provenance. Keep the existing flat
   `workflow_id` and `workflow_version` JSON fields in run records and event
   payloads.
3. Promote DOT `workflow_id` to a typed field on `dot.Graph`. Make it a
   graph-level reserved attribute. DOT validation rejects a missing or invalid
   identity before a run starts.
4. Resolve the graph and descriptor before `emitRunStarted`. Replace the
   private `workloopRunStartedPayload` with the registered
   `core.RunStartedPayload`. Its core definition must also own each retained
   start-time, queue, worker, and mode field, or the producer must stop writing
   that field.
5. Pass the descriptor to `driveDotWorkflow`. Delete its random workflow-ID
   assignment. Random `RunID` and `StateID` allocation is outside this decision.
6. Update every current UUID statement for workflow identity: the event schema,
   execution model, workflow graph, handler launch contract, workspace
   sidecar, scenario input, reconciliation records, and sub-workflow pin.
   A sub-workflow pin must retain the resolved logical identity and version;
   a derived UUID may be a legacy lookup aid only, never the canonical value.

7. Resolve a legacy `workflow:single` label to a named no-review DOT graph
   before `run_started`. If that graph cannot resolve and validate, reject the
   run before it emits `run_started`. The path must not enter the imperative
   single dispatcher.
8. Define the canonical no-review graph as the embedded
   `internal/daemon/no-review-bead.dot` artifact and the byte-identical
   `specs/examples/no-review-bead.dot` exemplar. It declares
   `workflow_id="no-review-bead"` and `version="1.0"`. Register it beside
   `standard-bead.dot` in the daemon embedded-graph registry.
9. Map a tier-0 persisted queue item with `WorkflowMode=single` to that same
   graph before validation and `run_started`. It records
   `workflow_selection_source=queue_item_single_mode`. The raw queue field
   remains for legacy audit. This preserves CLI and persisted-record
   compatibility while removing the imperative executor path.
10. Derive `review_policy` in the resolver. Only the exact registered
    `no-review-bead` version `1.0` descriptor, selected by
    `legacy_single_label` or `queue_item_single_mode`, may yield `no_review`.
    A DOT graph must not self-declare its review policy.

## Compatibility and migration

The wire keys do not change. A string-backed `WorkflowID` reads historical
UUID text and new graph IDs. This gives old event logs and scenario data a
read path while new events expose the meaningful graph identity.

This is still a contract amendment. UUID-only validation, UUID-oriented
documentation, generated fixture values, and APIs that compare
`WorkflowID` with `uuid.Nil` must migrate together. Do not add a parallel
`workflow_ref` field beside the old UUID field. Two durable identities would
leave replay and reconciliation with no canonical choice.

`run_started` has two incompatible historical shapes. The daemon's private
writer is legacy payload version 1. The registered core payload is also version
1, but it was never the live writer shape. The new unified core record is
`run_started` payload version 2. It is a breaking per-type change, with an
explicit N-1 window for legacy version 1.

The implementation must register version 2 in `registerRunLifecycle` and
update the `run_started` `PayloadCompatEntry` to current version 2, previous
version 1, a holding compatibility window, and non-additive classification.
Tolerant replay may decode historical private version-1 bytes for run
correlation. Restart reconciliation must retain a read-only legacy-v1 decoder
for bead and queue attribution. Strict replay accepts the registered version-2
core record only. The legacy decoder is migration input, never a producer or a
second registered payload owner.

## Rejected options

- A UUIDv5 derived from graph ID avoids random allocation, but the event still
  hides the logical graph ID. It is not the canonical representation.
- Graph filename or DOT `digraph` name is not valid fallback identity. The
  filename is not retained and the graph name is optional.
- Keeping a UUID `workflow_id` and adding `workflow_ref` preserves code in the
  short term but creates two durable truths.
- A random UUID created in the DOT executor is invalid because it is neither
  graph identity nor dispatch-time truth.

## Verification

1. Parser and validator tests prove that `workflow_id` is typed, required,
   valid, and no longer produces an unknown-attribute warning.
2. Default, project, and explicit DOT launches prove `run_started` contains
   the exact graph ID and version used by the DOT executor.
3. A negative test proves a missing or invalid graph identity prevents
   `run_started` and node dispatch.
4. A `workflow:single` label proves one typed start event with a named
   no-review descriptor and audit provenance. It proves that no imperative
   single dispatcher runs.
5. A CLI-created and a pre-existing persisted queue item with
   `WorkflowMode=single` each prove the same descriptor and `dot` execution.
   Each records `queue_item_single_mode` while retaining the raw queue value.
6. A custom graph that supplies `review_policy=no_review`, and a non-canonical
   descriptor paired with `no_review`, each fail before `run_started`.
7. Strict replay decodes a new version-2 `run_started` event through the
   registered core record. Tolerant replay and restart reconciliation decode
   historical private version-1 bytes. The latter still obtains bead and queue
   attribution.
8. Compatibility tests decode historical UUID-form workflow IDs.
9. A source or behavior test proves the DOT path does not allocate a workflow
   UUID. Update typed test fixtures that currently cast generated UUIDs to
   `WorkflowID`.
