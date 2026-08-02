# Proposed Specification Changes

Status: draft only. The work has five complete target specification drafts.
The Step 13 cross-spec integration pass is complete. Independent final draft
review is required before finalization.

The complete target drafts are:

- `05-spec-drafts/event-model.md`
- `05-spec-drafts/execution-model.md`
- `05-spec-drafts/workflow-graph.md`
- `05-spec-drafts/process-lifecycle.md`
- `05-spec-drafts/beads-integration.md`

The component artifacts `event-payloads.md`, `workflow-convergence.md`, and
`step14-and-27a.md` are design inputs. They are not target specification
drafts. They must not be copied to `specs/`.

## `specs/event-model.md`

- Add EV-025. A cross-bus producer converts local or wire input to its one
  registered core payload before publication. A legacy decoder supports reads
  only. It is not a second producer shape.
- Change `run_started` to payload version 2. The core payload carries the
  descriptor keys, `workflow_mode = dot`, review policy, selection source,
  input reference, start time, and worker observations. Queue fields remain
  optional. Worker fields are null for a local run.
- Define `WorkflowDescriptor{WorkflowID, WorkflowVersion}` as selected graph
  identity only. Named IDs use `^[A-Za-z][A-Za-z0-9-]*$`. Historical UUID text
  remains readable during migration.
- Require graph resolution before `run_started`. A legacy `workflow:single`
  label selects the registered embedded `no-review-bead` DOT graph. It records
  `workflow_selection_source = legacy_single_label`. A tier-0 queue item with
  `workflow_mode = single` selects the same graph and records
  `queue_item_single_mode`. The raw queue value remains for audit. Neither can
  dispatch the imperative single path.
- Derive `review_policy` in the resolver. Only the exact registered
  `no-review-bead` version `1.0` descriptor, selected through either legacy
  source, may yield `no_review`.
- Define the v2/v1 migration. The normal v2 decode is strict. Replay and
  restart reconciliation can read legacy private v1 bytes for correlation
  during the explicit N-1 window.
- Complete `handler_capabilities` with the registered
  `core.HandlerCapabilitiesPayload`. The watcher converts wire integer protocol
  versions to ordered decimal strings before emission. The optional Claude
  session field is an additive version-1 change.
- Add schemas for `liveness_halt` and `stale_open_bead_detected`.

## `specs/execution-model.md`

- Make the resolved workflow descriptor the workflow identity in a new run.
  New runs use DOT execution. Historic `single` values remain readable.
- Resolve, parse, substitute typed attributes, and validate the selected graph before
  `run_started`. Seal the descriptor, selected graph, mode, review policy,
  selection source, and input reference for the run lifetime.
- Treat `workflow:single` and tier-0 queue `single` as legacy graph-selection
  requests. They select the registered embedded `no-review-bead` DOT graph
  with distinct audit sources. They do not select an imperative dispatcher.
- Require the version-2 `run_started` payload after graph validation. Add graph
  identity validation to the DOT validator. The parser exposes the typed graph
  identity rather than an unknown attribute.
- Use `WorkflowID` for sub-workflow pins and verify the same descriptor when a
  run resumes.

## `specs/workflow-graph.md`

- Add WG-055. Every graph must carry one graph-level `workflow_id`.
- Define the named-ID and legacy UUID-text grammar. The loader rejects missing,
  repeated, misplaced, or invalid identity before `run_started` or node
  dispatch.
- Require the parser to expose the identity in `Workflow.workflow_id`. It must
  not use a graph name or source filename as a fallback.
- Add `workflow_id` to the graph-level reserved attribute set. Pair it with
  graph `version` to form the identity-only workflow descriptor.
- Add WG-056. `review_policy` is reserved and rejected in DOT. The resolver
  alone binds no-review policy to the registered no-review graph.

## `specs/process-lifecycle.md`

- Restrict daemon configuration and defaults to reviewed DOT selection.
  `workflow_mode: single` and `workflow_mode: review-loop` fail at load.
- Define both legacy no-review requests: the per-bead `workflow:single` label
  and a tier-0 queue item with `workflow_mode = single`. Each selects the named
  no-review DOT graph before validation and `run_started`, with distinct audit
  provenance.
- Remove the retired review-loop fallback from daemon bootstrap. A graph-load
  failure is a loud run failure. It is not a mode fallback.

## `specs/beads-integration.md`

- Define the two legacy no-review inputs in BI-009a: a tier-1
  `workflow:single` label and a tier-0 queue item with `workflow_mode =
  single`.
- Require both inputs to select the registered `no-review-bead` version `1.0`
  graph, resolve to `dot`, and emit distinct selection provenance.
- Retain the raw queue value for legacy audit. Retire the stale
  `review_bypassed` audit reference. The `run_started` descriptor, policy, and
  selection-source tuple is the audit record.

No source specification changes are made by this planning work. The five drafts
remain subject to independent final draft review before they can advance.
