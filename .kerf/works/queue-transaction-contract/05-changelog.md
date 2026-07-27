# Queue transaction contract — planned four-spec changelog

This spec-draft changelog inventories the four complete modified files under
`05-spec-drafts/`. It is not permission to copy any draft to `specs/`; that
requires both final reviews and coordinator finalization.

## `queue-model.md`

- **Status:** modified.
- **Design:** `04-design/queue-model-design.md`.
- **Source frontmatter:** literal `version: 0.1.4`,
  `last-updated: 2026-05-31`.
- **Existing history drift:** the source already contains an embedded v0.1.5
  history entry. The draft therefore targets **0.1.6** and must not claim a
  literal frontmatter `0.1.5 → 0.1.6` edit.
- Reconcile singleton wording to canonical named queues and migration-only
  legacy `main`.
- Define QueueStore’s immutable snapshot/generation transaction ownership.
- Add typed persistence results and universal event-free replace intent.
- Define archive intent and exact linked cancellation handoff: predecessor
  prebinds origin/kind/source digest/destination/successor bytes and remains
  through successor durability; classify replace-only, exact pair,
  archive-only, and mismatch states.
- Pin exact normative paths
  `.harmonik/queues/<normalized-name>.replace-intent` and
  `.harmonik/queues/<normalized-name>.archive-intent`, canonical v1 fields,
  no-replace/exact-byte rules, ordinary intent unlink/root-sync, parseable and
  corrupt archive-source identity, and exhaustive definite/ambiguous restart
  cleanup classifiers.
- Replace QM-033 event landmark with queue-owned completion receipt authority.
- Define intent-bound receipt identity/content, flat root establishment,
  no-replace install, QM-053 order, queue-ID plus completed-digest CAS cleanup,
  same-name reuse, and an immutable post-cleanup/post-release marker whose
  trusted-UTC 720-hour boundary gates receipt-first crash-safe GC.
- Extend QueueStatus exact-ID selection with exact live state then exactly one
  identity-consistent retained receipt; zero/multiple/corrupt/unsupported/
  disagreeing candidates fail deterministically.
- Preserve shipped submit/append/status/cancel wire fields and selector
  semantics, including literal cancel JSON `queue` plus `force`; add optional
  cancel `queue_id` compatibly, require both selectors to agree, and reject
  neither-present rather than silently selecting main. Preserve optional
  watched group index, `max_concurrent`, and receipt-completed status output.
- Make failed-group terminal status and queue `paused-by-failure` one
  authoritative replacement transaction before observation attempts.
- Add implementation/conformance composition without global event migration.

## `process-lifecycle.md`

- **Status:** modified.
- **Design:** `04-design/process-lifecycle-design.md`.
- **Source frontmatter:** `version: 0.6.0`,
  `last-updated: 2026-07-14`.
- **Planned target:** 0.6.1 with current integration date.
- Inventory exact submit/append/status/cancel wire fields, selector precedence,
  operator watched-group flag, and live/receipt status render ownership.
- Make daemon-unreachable cancellation exit 17 with zero local writes.
- Insert receipt-root establishment and queue-owned recovery before readiness.
- Define the complete replace/archive-intent plus receipt capability as the
  unit of startup/upgrade/downgrade/rollback compatibility, including
  completion-release marker v1, clock-regression refusal, and receipt-first
  GC.
- Preserve shutdown ordering while distinguishing live operator audit.
- Remove active `.harmonik/queue.json` live-write/singleton/overwrite wording
  from PL-004, PL-013, and PL-028; it remains migration-only input.
- Do not add event repair/replay or a generic intent service.

## `event-model.md`

- **Status:** modified.
- **Design:** `04-design/event-model-design.md`.
- **Source frontmatter:** `version: 0.7.1`,
  `last-updated: 2026-07-14`.
- **Planned target:** 0.7.2 with current integration date.
- Add optional `completion_receipt_id` to `queue_group_completed`.
- Require it exactly for final `complete-success`; omit it for non-final
  success and every `complete-with-failures`.
- Clarify Class F as one normal-path fsync-backed attempt while queue state and
  receipt remain authoritative.
- Add typed Class O `queue_cancelled_operator` with parseable/corrupt XOR and
  post-release/pre-response ordering.
- Preserve EV-021/EV-022 and all JSONL/EventID/replay/cursor/writer/consumer
  mechanics unchanged.

## `execution-model.md`

- **Status:** modified.
- **Design:** `04-design/execution-model-design.md`.
- **Source frontmatter:** `version: 0.9.3`,
  `last-updated: 2026-07-22`.
- **Planned target:** 0.9.4 with current integration date.
- Preserve EM-015f's post-commit uninterrupted normal-path attempt and
  no-crash-surviving-delivery clarification, now scoped per named queue.
- Align the two queue glossary entries, EM-062 through EM-065, §6.5 queue
  lifecycle, §7.4 queue selection/capacity, §9.3 queue dependencies, and §10.1
  queue conformance with complete QueueStore snapshots and per-name submit.
- Require all-queue duplicate pre-screen, empty-named-set-only `br ready`,
  eligible active queue selection, global `--max-concurrent` plus selected
  queue `workers` gates, and QM-067 cursor advance on every selection.
- Correct only the stale EM-066/EM-067 singleton branch references, enable the
  §10.2 pause fixture with `--auto-pull`, add the adjacent
  nonempty-ineligible no-fallback fixture, and place QM-062 in the §9 capacity
  dependency row while lifecycle remains a §8 citation.
- Preserve Run/workflow/checkpoint/failure/event-schema and non-queue clauses.
  Permit only the card-enumerated regions and four exact fallback/test
  replacements, frontmatter version/date, and one qualifying history row;
  compare the title/opening preamble literally.

## Cross-spec compatibility

- Queue-model owns completion, cleanup, restart, same-name admission, status,
  retention, and GC authority.
- Event-model owns only observation payload/class/order.
- Execution-model owns semantic Run/group event order, not delivery recovery.
- Process-lifecycle owns readiness and process capability handoff.
- No effect key, outbox, segmented event storage, ReplayCursorV2,
  `ScanAfter`/reader migration, full-log lookup, truncation, repair CLI, event
  lock, or writer census is introduced.
