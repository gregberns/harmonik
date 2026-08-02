# Workflow Convergence — Change Design

## Current state

`run_started` is emitted before DOT graph resolution. The parser retains the
graph's logical ID as an unknown attribute. A later DOT helper assigns a random
UUID. The imperative `workflow:single` tail remains separate from DOT
execution.

## Target state

Workflow resolution validates the selected DOT graph before `run_started`.
It returns `WorkflowDescriptor{WorkflowID, WorkflowVersion}` beside the input
reference, selected graph, resolved mode, resolved policy, and audit
provenance. The descriptor records selected graph identity only. It does not
carry graph data or launch-selection fields.

New graph IDs match `^[A-Za-z][A-Za-z0-9-]*$`. Legacy UUID text remains
readable. DOT `workflow_id` becomes a required typed graph attribute. Filename
and graph name are not identity fallbacks.

The legacy `workflow:single` label resolves a named no-review DOT graph before
the start event. A failed resolution rejects the run. The label must not select
the imperative dispatcher. The reviewed standard graph remains the default.

The eventual target specs are `execution-model.md`, `workflow-graph.md`, and
`process-lifecycle.md`. This design does not supply their final spec text. It
does not authorize the Step 7 tail deletion.

## Rationale

The run record, durable event, replay, reconciliation, and DOT executor need
one truthful identity. The source evidence and the tail-porting risk are in
`03-research/workflow-convergence/findings.md`.

## Requirements traceability

| Requirement | Target state |
|---|---|
| Identity comes from selected graph | Required typed DOT identity and descriptor |
| Start event precedes no execution | Resolve and validate before `run_started` |
| Legacy single selection remains auditable | Named no-review graph and provenance |
| Tail behavior is not lost | Step 7 retains ownership of port or retire decisions |
