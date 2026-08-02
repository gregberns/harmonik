# Integration Review — Event Payload Ownership

Status: Integration corrections complete. The Step 13 target set is coherent.

## Corpus checked

The complete target drafts are:

- `05-spec-drafts/event-model.md`
- `05-spec-drafts/execution-model.md`
- `05-spec-drafts/workflow-graph.md`
- `05-spec-drafts/process-lifecycle.md`
- `05-spec-drafts/beads-integration.md`

The review also checked the workflow-identity decision, change design,
changelog, and status record. External checks covered the corresponding source
specs, `cmd/harmonik/run.go`, `internal/queue/rpc.go`,
`internal/daemon/workloop_runplan.go`, and `internal/daemon/standardgraph.go`.

## Coherent contract

1. `WorkflowDescriptor{workflow_id, workflow_version}` is selected graph
   identity only. New DOT execution does not create a UUID identity.
2. The resolver completes selection, parsing, typed-attribute substitution,
   validation, descriptor creation, mode resolution, policy derivation, and
   provenance selection before `run_started` version 2 emits.
3. New starts use `workflow_mode=dot`. The old version-1 reader is read-only
   migration support for replay and restart reconciliation.
4. The canonical no-review graph is the registered `no-review-bead` version
   `1.0` graph. Its planned runtime artifact is
   `internal/daemon/no-review-bead.dot`. Its planned byte-identical exemplar
   is `specs/examples/no-review-bead.dot`.
5. A tier-1 `workflow:single` label maps to that graph with
   `legacy_single_label`. A tier-0 queue item with `workflow_mode=single` maps
   to the same graph with `queue_item_single_mode`. The raw queue value remains
   for audit. Both execute as `dot`.
6. `review_policy` is resolver-owned. Only that exact registered descriptor,
   through either legacy source, can yield `no_review`. DOT input cannot
   self-claim the policy.

The tier-0 compatibility path is source-backed. The CLI writes the mode to the
queue item. The queue RPC retains it. The run planner gives it tier-0
precedence. The queue-model contract already defines the item fields.

## Integration findings and corrections

### IR-001 — Resolved: DOT substitution order

EM-055 now follows WG-046. The order is read source, parse, substitute typed
attributes, validate, then dispatch. The EM text preserves WG-045's
context-specific quoting rule for `tool_command`.

### IR-002 — Resolved: Beads compatibility contract

`05-spec-drafts/beads-integration.md` is now a complete target draft. BI-009a
defines both legacy no-review inputs and their distinct selection sources. It
retains the raw queue value for audit. It retires the stale `review_bypassed`
reference because the event catalog has no such registered event. The durable
audit is the `run_started` descriptor, policy, and selection-source tuple.

### IR-003 — Resolved: Main-loop input propagation

The EM main-loop pseudocode now passes the complete queue item into workflow
resolution. It carries the resolved result through validation and `create_run`.
The tier-0 mode, reference, and parameters cannot be dropped on this path.

### IR-004 — Resolved: Validator test coverage

The EM-057 test obligation now names all nine checks. It includes missing or
invalid graph identity and invalid review-policy binding.

### IR-005 — Resolved: Draft records

All five target drafts now use `status: draft`. The execution-model revision
history includes its `0.10.4` Step 13 row. The Beads draft has its `0.9.3`
revision row.

### IR-006 — Resolved: UUID wording

The v2 `run_started` schema now calls `workflow_id` a logical graph ID and
states that UUID text is legacy input only. This matches the descriptor grammar.

## Changelog and status review

`05-changelog.md` and `spec-draft-status.md` now name all five complete target
drafts. They include the Beads coordination amendment and the replacement of
the stale audit reference.

## Remaining issue outside Step 13

`sub-workflow-dispatch.md` remains a disclosed forward reference in the
workflow-graph draft. It is not changed by Step 13. It must be resolved or
re-anchored before that workflow-graph draft can be finalized.

## Coherence verdict

**Step 13 integration passes.** No Step 13 contradiction remains among the
five target drafts. The remaining forward reference is outside this work. No
production source or normative `specs/` file changed during integration.
