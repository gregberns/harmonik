# Spec Draft Status — 2026-08-02

The Kerf work is in `Integration`. The Step 13 integration pass is complete.
The draft is not ready for finalization.

Five complete target specification drafts now exist:

- `05-spec-drafts/event-model.md`
- `05-spec-drafts/execution-model.md`
- `05-spec-drafts/workflow-graph.md`
- `05-spec-drafts/process-lifecycle.md`
- `05-spec-drafts/beads-integration.md`

Together, these drafts define one Step 13 contract:

- A selected workflow has a typed `WorkflowDescriptor` with graph identity and
  version. The descriptor does not carry graph data, selection source, mode, or
  policy.
- Every DOT graph has a required typed `workflow_id`. New named IDs use
  `^[A-Za-z][A-Za-z0-9-]*$`. Legacy UUID text remains readable. The loader
  rejects invalid identity before `run_started` or node dispatch.
- The resolver selects, parses, substitutes typed attributes, and validates the DOT graph before
  `run_started`. It returns the descriptor with the graph, input reference,
  resolved mode, review policy, and selection source. The result is sealed for
  the run lifetime.
- New `run_started` records use payload version 2 and `workflow_mode = dot`.
  They include required start and worker observations. The worker observations
  are null for a local run. The v1 private record has a defined N-1 read path
  for replay and restart reconciliation.
- A legacy `workflow:single` label selects the registered embedded
  `no-review-bead` DOT graph. It records `workflow_selection_source =
  legacy_single_label`. A CLI-created or persisted tier-0 queue item with
  `WorkflowMode=single` selects the same graph with
  `queue_item_single_mode`. The raw queue value remains for audit. Both resolve
  to `dot` and cannot use an imperative single dispatcher. Daemon defaults and
  configuration use reviewed DOT selection only.
- `review_policy` is resolver-derived. Only the exact registered
  `no-review-bead` version `1.0` descriptor, selected through one of those two
  legacy sources, may yield `no_review`. DOT input cannot self-claim it.
- The `handler_capabilities` core payload and the eager-refill provenance
  events have defined event-model schemas.
- BI-009a defines both legacy no-review inputs. It retires the stale
  `review_bypassed` reference. The durable audit is the resolved start-event
  descriptor, policy, and selection source.

The integration review found no remaining Step 13 contradiction. The remaining
requirement is the final independent spec-draft review before finalization. It
must verify all five drafts against the workflow-identity decision and their
cross-references. The disclosed `sub-workflow-dispatch.md` forward reference is
outside Step 13 and remains a finalization prerequisite for workflow-graph.
This remains a planning boundary. It does not authorize an Alpha-owned
production edit.
