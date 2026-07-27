# Durability Round-1 response

The Round-1 `REQUEST_CHANGES` is accepted. The immutable reviewer artifact is
unchanged.

## Dispositions

- **D1 — resolved.** QM-001 now pins
  `.harmonik/queues/<normalized-name>.replace-intent`, its canonical v1 fields,
  no-replace installation, ordinary intent unlink plus queue-parent sync, and
  exhaustive exact prior/candidate/temp restart and ambiguity classifiers.
  QM-007 now pins
  `.harmonik/queues/<normalized-name>.archive-intent`, its canonical v1
  predecessor/source/destination bindings, parseable and corrupt-source
  selection, no-replace behavior, predecessor/successor cleanup, and every
  rename/unlink/parent-sync classifier.
- **D2 — resolved.** Active PL-004, PL-013, and PL-028 language now treats
  `.harmonik/queue.json` only as migration input, inventories the named
  QueueStore surface, and removes singleton live-write/overwrite behavior.
  Queue-model's cancelled enum and glossary now retain ownership through the
  linked archive transaction instead of later overwrite.
- **D3 — resolved.** QueueSubmitRequest preserves shipped `groups`,
  `schema_version`, `name`, `workers`, `spend_cap_usd`, and
  `default_harness`. QueueAppendRequest preserves `queue_id`, `name`,
  `group_index`, and `bead_ids`, with ID/name/main precedence. Status and
  cancel request/response records and selector semantics are explicit and
  preserve existing response fields.
- **D4 — resolved.** QM-052 and EM-015f require failed-group terminal state
  plus `paused-by-failure` in one authoritative queue replacement before
  observation attempts.

Design, task, changelog, and CQ-02 evidence artifacts were updated to carry the
same paths, schemas, classifications, and implementation ownership. No
normative `specs/` file, production code, task index, Beads ledger, commit, or
review artifact was changed.

## Round-2 gate

Ready for an independent durability Round-2 final review after the exact
draft/card/EM/DAG/wire/caller validations pass.
