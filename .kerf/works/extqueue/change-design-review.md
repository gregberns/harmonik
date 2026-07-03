# extqueue — Change-Design Cross-Review

Scope: cross-review of the six 04-design docs against 01-problem-space (D1–D6), 02-components §1–§6, and the cited spec text. Current-state quotes spot-checked against `specs/execution-model.md:1021-1089`, `specs/beads-integration.md:253-268`, `specs/process-lifecycle.md:194,408-412`, `specs/operator-nfr.md:159,300,392,562`, `specs/event-model.md:79`. All quotes verified faithful.

## Findings

### F1 — `queue_completed` event is emitted but not registered (contradiction; load-bearing)
`queue-model-design.md:129` ("Complete" bullet in §8) states: *"Daemon emits `queue_completed{queue_id}`."* This event is NOT in `event-model-design.md`'s six-type §8.10 cohort and is not in the dropped-types rationale either. Either queue-model is emitting a phantom event, or event-model is missing a row. The event-model trim rationale only enumerates `queue_item_*` and `queue_resumed` as drops; `queue_completed` is unmentioned in both directions.

### F2 — `queue_paused.reason=operator_drain` path under-specified in queue-model
`event-model-design.md:28` (§8.10.4 row) defines `reason ∈ {group_failure, operator_drain}`. `operator-nfr-design.md` ON-027 step (1) rewrites drain to flip queue status to `paused-by-drain` per `[queue-model.md §5]`. But `queue-model-design.md §5` state machine table has no row for the `operator_drain` transition (only `complete-with-failures → paused`), and §8's lifecycle prose only covers pause-by-failure. The `paused-by-drain` queue status is declared in QueueStatus ENUM (§2 line 31) but no transition emits it. Spec writer cannot synthesize the drain emission path from queue-model alone.

### F3 — PL-013 retirement stub cites the wrong ON-NNN
`process-lifecycle-design.md:77` references `[operator-nfr.md §4.4 ON-026]` for the cadence-knob removal. The actual amendment is in **ON-004** (§4.1, line 159). The operator-nfr design even calls this out as a research typo correction. Update the PL-013 retirement stub.

### F4 — Cross-spec anchor drift: §5 vs §4 for `QM-010`
`event-model-design.md:106` (§6.5 co-ownership bullet) cites `[queue-model.md §5 QM-010, §6 QM-020..025, §7, §8]`. QM-010 lives in queue-model §4 Identity (line 74), not §5. Single-character fix.

### F5 — execution-model TS-1 pseudocode has unresolved placeholder
`execution-model-design.md:32` contains `[queue-model.md §1 QM-???]`. The "???" is a real placeholder — the queue-model spec author needs to either coin a QM-NNN for the in-memory authority claim (§1 currently has no numbered requirements; identity QM-010 is the closest match) or the cross-ref should be `[queue-model.md §4 QM-010]`. Spec writer cannot resolve this without a design decision.

### F6 — `queue-model §2 ItemStatus` includes a value that names an event-model artifact
ItemStatus enum (`queue-model-design.md:58`) includes `deferred-for-ledger-dep`. The event-model design's event type is `queue_item_deferred_for_ledger_dep` (§8.10.6). Same string, two semantics (item-state vs. event-type). Not a contradiction but worth a one-line note in queue-model §2 — the item-state and event-type are correlated but the item-state can recur silently when the dispatcher re-evaluates whereas the event is emitted once per QM-025 detection.

## Required changes

1. **queue-model-design.md §8 "Complete" + event-model-design.md §8.10** — pick one:
   - Add `queue_completed` (F-class) as §8.10.7 in event-model, with payload `{queue_id, completed_at}`, OR
   - Remove the "Daemon emits `queue_completed{queue_id}`" sentence from queue-model §8 and instead say *"queue status transitions to `completed`; no event emission (the last group's `queue_group_completed` is the durable landmark)."*
   Recommend the second (avoids a new F-class event for what is already evidenced by the last group's `queue_group_completed`).

2. **queue-model-design.md §5** — extend the state-machine table with a row for the `paused-by-drain` transition, OR add a §8 subsection "Pause-by-drain" mirroring "Pause-by-failure" and naming the entry condition (ON-008/ON-027 drain step 1) and the emission `queue_paused{reason:operator_drain}`. Without this, the spec writer cannot land `[queue-model.md §5]` as ON-027's cross-ref target.

3. **process-lifecycle-design.md:77** — change `[operator-nfr.md §4.4 ON-026]` to `[operator-nfr.md §4.1 ON-004]`.

4. **event-model-design.md:106** — change `[queue-model.md §5 QM-010, …]` to `[queue-model.md §4 QM-010, §5, §6 QM-020..025, §7, §8]`.

5. **execution-model-design.md:32** — replace `[queue-model.md §1 QM-???]` with `[queue-model.md §4 QM-010]` (the queue-id-bearing identity anchor is the right reference for "active in-memory queue authority"). Same fix at any other `QM-???` site in the pseudocode.

## Optional improvements

- **queue-model §2 ItemStatus / event-type collision** (F6): add one-line INFORMATIVE note distinguishing the recurring item-state from the one-shot event-type.
- **operator-nfr ON-027 step (1)** says *"the daemon stops advancing the queue"* but the queue-model design doesn't yet name an explicit "advance" verb in §5. Consider promoting the §5 INFORMATIVE block ("group lifecycle is mediated by the queue's overall status") to a normative QM-013 "Queue-advance gating" so ON-027 has a clean target.
- **event-model §8.10 emission-ordering paragraph (line 33)** says `run_completed`/`run_failed` "MUST precede `queue_group_completed`". This is asserted only here; queue-model §5 should mirror this ordering constraint (or cross-ref `[event-model.md §8.10 emission ordering]`) so the writer of queue-model spec has the constraint in-doc.
- **process-lifecycle PL-028c** allocates exit code 17 for daemon-down. The ON-side prose cascade is flagged as "owned by operator-nfr design pass" in process-lifecycle but the operator-nfr design doc does NOT include this cascade. Either add it to operator-nfr's amendment list, or note explicitly that the prose update is deferred to spec-draft.

## Coverage check

Every 02-components.md §1–§6 requirement maps to at least one amendment (verified via traceability tables in each design). No orphan amendments found — every TS-* / Amendment / QM-NNN / ON edit traces to a numbered requirement or D1–D6 decision. Method-name consistency (`queue-submit`/`queue-append`/`queue-status`/`queue-dry-run`, bare-kebab, no dotted form) holds across all six designs. `enqueue` is retired (no alias) consistently across PL-003a, PL-028, ON-013a, ON-050. The `.harmonik/queue.json` write-at-mutation / read-at-step-8a / unlink-on-completion triple is consistent across queue-model, process-lifecycle, and operator-nfr ON-018. Concurrency primitives EM-NNN-A/B/C compose with queue groups per queue-model §9 — no conflict. Current-state quotes spot-checked at five sites all match the live specs verbatim.

## Writability

Five of six designs pass the "could a spec writer with no design context produce final text?" bar: execution-model TS-1 specifies line-range replacement; operator-nfr supplies verbatim replacement quotes; event-model supplies full §8.10 table + YAML schemas + ordering paragraph; process-lifecycle gives PL-005 step 8a, PL-028 bullets, and PL-028c verbatim; beads-integration uses "After:" blocks throughout. Queue-model is the weakest on writability: §8 "Complete" emits a phantom event (F1), §5 lacks the drain transition (F2), and §1 has no numbered QM identifier that cross-refs can land on (F5). All three are addressed by Required changes 1, 2, 5.

## Verdict

**REQUEST_CHANGES**

The five required changes are small and mechanical but two of them (F1, F2) are real contradictions between designs — not polish. Fixing them in queue-model is a 30-minute pass; once landed, the cross-spec anchors and event surface lock cleanly. No new amendments needed; no requirements uncovered; no contradictions in method naming, concurrency story, enqueue retirement, or queue.json discipline.
