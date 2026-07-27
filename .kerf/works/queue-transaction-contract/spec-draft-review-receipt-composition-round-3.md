# Spec-draft composition review — focused Round 3

**Verdict: APPROVE**

This review is limited to the three Round-2 composition corrections and
regression checks for the already-passed status, failure-transaction, event,
and execution-model boundaries.

## Focused findings

- **R2-C1 is resolved.** The queue-model and process-lifecycle drafts,
  research, design, changelog, task plan, and evidence preserve the shipped
  `QueueCancelRequest` JSON fields `queue` and `force`. Optional `queue_id` is
  additive. A lone selector is valid, dual selectors must identify the same
  queue or fail with `queue_selector_conflict`, neither-present is invalid,
  and no cancel path silently defaults to `main`. N-1 `{queue,force}` remains
  valid.
- **R2-C2 is resolved.** The 13 critical production symbols checked for this
  round each map to exactly one slice: the seven requested cancel
  request/handler/CLI helpers; both
  `QueueOperatorEventConsumer` transition methods; `eagerRefillEval`;
  `crewHandlerImpl.ensureQueue`; `reconcileOrphanedRunsOnResume`; and
  `adoptLiveRunSession`. The operator assignment names
  `internal/queuewiring/operatorevents.go`, and the eager, crew, and cancel
  assignments name their literal production symbols in both `07-tasks.md`
  and `CQ-02.yaml`.
- **R2-C3 is resolved.** JR-04 owns the typed recovery/adoption implementation
  and depends on CQ-04 plus JR-03. WL-REC-01 depends on JR-04 (with its
  existing architecture/workloop prerequisites) and owns only the later
  `workloop.go` recovery/adoption call-site migration. Generic workloop
  maintenance owns only deferred reevaluation, claim-skipped dependency,
  cross-queue duplicate, and max-attempt regions; it does not own adoption.
  The shared workloop lease/conflict declarations serialize the remaining
  same-file work.

## Mechanical and regression checks

- `CQ-02.yaml` retains the exact 23-key schema and 69-row crash matrix, now
  with 20 unique implementation slices. Every slice has prerequisites, lease,
  writes, failing-first proof, accepted intermediate state, rollback boundary,
  and conflicts. The dependency graph is acyclic.
- Status/startup/terminal ownership remains unique for the
  `QueueStatus*`/status CLI symbols, the three CQ-04 reconciliation symbols,
  and the JR-03 terminal symbols.
- QM-052 and EM-015f still require one durable candidate/commit containing
  failed-group terminal state and `paused-by-failure` before observations.
- Optional group observations remain diagnostic-only, non-persistent,
  non-replayed, and unable to gate authoritative state, cleanup, admission, or
  waiter termination. No global event migration/replay slice was introduced.
- The execution-model draft diff remains confined to frontmatter version/date,
  EM-015f, and one qualifying revision row. No regression was found in the
  previously approved receipt authority, waiter ordering, startup recovery,
  cancellation audit, or event payload composition.
- `git diff --check` passes for the work artifacts and CQ-02 evidence.

No composition blocker remains for the focused Round-3 scope.
