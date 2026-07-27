# Durability Round-2 response

The appended Round-2 `R2-1` finding in the immutable durability reviewer
artifact is accepted.

Queue-model, process-lifecycle, research, designs, changelog, tasks, and CQ-02
evidence now preserve the literal shipped QueueCancelRequest JSON selector
`queue` and shipped `force`. Optional `queue_id` is an additive extension;
`queue` is not renamed to `name`. `queue` alone selects the normalized name,
`queue_id` alone selects exact identity, both must resolve to the same queue
or return `queue_selector_conflict`, and neither-present is invalid rather than
silently defaulting to `main`. N-1 `{queue,force}` requests therefore continue
to select the requested queue.

The cancellation implementation slice owns the exact request type, daemon
handler, CLI helpers, and N-1/dual-selector compatibility tests.

No normative `specs/` file, production code, task index, Beads ledger, commit,
or reviewer artifact was changed. Ready for focused durability final
re-review after the full validation set passes.
