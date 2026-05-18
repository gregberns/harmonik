# extqueue 07-tasks.md — Tasks-Pass Review

Reviewer: tasks-pass review sub-agent
Date: 2026-05-14
Artifact under review: `.kerf/projects/gregberns-harmonik/extqueue/07-tasks.md`

## Findings

### 1. Changelog → task coverage (criterion 1)

Walked every bullet in `05-changelog.md` "Per-spec change inventory" against the 07-tasks.md spec-traceability table. Coverage is complete:

- queue-model.md (NEW) → T10, T11, T30, T31, T32, T40, T41, T42, T70, T83 cover §§1–9. ✓
- execution-model.md TS-1/TS-2/TS-3/TS-4/TS-5/TS-6 → T50 (TS-1/TS-2), T21 (TS-3), T51 (TS-4); TS-5/TS-6 are doc-only and correctly marked. ✓
- beads-integration.md BI-013/013a/013b/013c, §4.5a, §4.10 intent-log scope note → T40 covers the submit-time read surface; the demotion of BI-013 / relocation of BI-013a is pure spec text landed by T02. ✓ (acceptable, but see Finding 7 below.)
- process-lifecycle.md PL-003a / PL-028 / PL-028c / PL-005 step 8a / PL-013 retire / PL-004 / PL-027(iii) / PL-008a → T60, T62, T31, T71, T50 + T84. ✓
- event-model.md §8.10, §6.3 run_* extensions, §8.7.16 enum, source_subsystem → T20, T21, T84, T12. ✓
- operator-nfr.md ON-004 / ON-009a / ON-013a / ON-015 / ON-018 / ON-027 / ON-041 / ON-050 / ON-INV-001 / §7.2 drain pseudo → all landed by T02 (spec-only); ON-013a/ON-041/ON-050 conformance asserted by T84. ✓
- §6.3 run_* `queue_id` / `queue_group_index` additive — T21 covers shape; **gap (minor): no explicit task to emit the fields at run dispatch.** T50's workloop rewrite implicitly does it, but acceptance criteria do not mention populating QM-011/QM-012. See Required change R1.

### 2. Task → spec ref traceability (criterion 2, spot-check)

- T11 → queue-model.md §2 ✓ (RECORDs exist).
- T30 → queue-model.md §3 QM-001, WM-026 ✓.
- T40 → queue-model.md §6 ✓ (QM-020..QM-027 + QM-029a all present).
- T50 → execution-model.md §7.4 + EM-015f ✓ (per changelog).
- T62 → process-lifecycle.md PL-028 + PL-028c ✓.
- T84 → PL-003a + ON-013a + ON-041 + ON-050 — all four anchors verified to exist in `05-spec-drafts/{process-lifecycle,operator-nfr}.md`. ✓

### 3. Dependency graph (criterion 3)

Re-derived DAG from per-task `Deps:` lines and compared to the explicit graph section. **One inconsistency**: T50 `Deps:` cites `T41, T42, T70` (line 144) but the explicit DAG (line 264 / 266) routes T50 from T41 → T50 and T42 → T50 only — T70 is not drawn as a predecessor in the diagram. Either Deps or the diagram is correct, not both. See Required change R2. No cycles in either form.

### 4. Acceptance criteria (criterion 4, spot-check)

- T11 — concrete: round-trip + `schema_version` reject. ✓
- T30 — concrete: atomic-or-old, 1 MiB enforced. ✓
- T40 — concrete: 8 per-rule tests + order test + QM-025 emission test. ✓
- T62 — concrete: subcommand callable, code-17 on daemon-down, both flag shapes. ✓
- T80 — concrete: 2 wave-groups, all-terminal advance, unlink on completion, events.jsonl order. ✓
- **T51 weak**: "code comments cite EM-049/050/051; conformance tests for these (§10.2) pass." The conformance-test obligations referenced (§10.2 of execution-model.md) are not enumerated in T51 — an implementer agent would need to chase §10.2 to know whether tests already exist or must be authored. See Optional improvement O1.

### 5. Granularity (criterion 5)

- T40 is on the high end: 8 rules + order test + parallelism-narrowed-emission test ≈ 1 day. Acceptable; splitting would proliferate trivial beads.
- T03 (archival) is trivial — could fold into T02's commit. Optional improvement O2.
- T50 and T62 are the two largest items. T50 is bounded by the cited workloop region and is acceptable as a single bead; T62 is bounded by 4 verbs × ~30 LOC each + dispatch.

### 6. Wave-3 parallel-safety (spot-check directive)

Wave 3 lists T30 / T40 / T41 / T70 as concurrently runnable. File analysis:

- T30 → `internal/queue/persistence.go` (new)
- T40 → `internal/queue/validation.go` (new)
- T41 → `internal/queue/state.go` (new)
- T70 → `internal/daemon/daemon.go`, `internal/daemon/workloop.go` (existing)

No file overlap among T30 / T40 / T41. T70 touches `workloop.go` but only the constructor/handle-wiring region, NOT the dispatch region rewritten by T50 in a later wave — safe to run in parallel with T30/T40/T41. ✓ However: T70's `Deps:` lists T40, T41, T42 (line 187), which conflicts with the Wave-3 placement (T42 is Wave 4). See Required change R3.

### 7. Workloop line-range verification (spot-check directive)

T50 cites `workloop.go:391-421`. Actual:
- Line 360-370: claim semaphore (T51 cites this — ✓).
- Line 372-389: cancellation + capacity gate.
- **Line 391-416: br ready poll + nothing-ready sleep + first-record pick.**
- Line 418-422: pick-first + RunID generation.
- Line 425-495: claim + pre-claim status guard + semaphore acquire + ClaimBead.

T50's "391-421" range covers Step 3 (poll), Step 4 (sleep on empty), Step 5 (pick-first + RunID gen). The actual `br ready` → pick → claim region spans 391-495 if the rewrite must also replace the pre-claim ShowBead guard (hk-p4xbw) and ClaimBead invocation. Recommend T50 expand the cited range. See Required change R4.

### 8. T84 ON-target verification (spot-check directive)

Verified all 4 anchors in `05-spec-drafts/operator-nfr.md`:
- ON-013a — line 275, queue-* methods enumerated ✓
- ON-041 — line 526, "queue (with subcommands ...)" present ✓
- ON-050 — line 562, inline subset is `{pause, resume, stop}` (enqueue removed) ✓
- PL-003a — `05-spec-drafts/process-lifecycle.md` line 195 ✓

All 4 targets resolve. T84 is well-anchored.

## Required changes

**R1.** Add to T50's Scope+Acceptance: "On every run dispatched from a queue item, populate `queue_id` and `queue_group_index` on the run_started / run_completed / run_failed event emissions per QM-011 / QM-012." Currently QM-011/012 are typed (T21) but no task actually emits them.

**R2.** Reconcile T50 deps with the DAG diagram. Either add the T70 → T50 edge to the diagram block (line 264) or remove T70 from T50's `Deps:` on line 144. The diagram is the more authoritative reference per kerf convention — recommend adding the edge.

**R3.** Reconcile T70's deps with its Wave-3 placement. Either remove T42 from T70's `Deps:` (line 187) — T70 only needs the types (T11) and the validator/state-machine API surface (T40/T41), not T42's append implementation — or move T70 to Wave 4. Recommend removing T42 from T70 deps; daemon-wiring should not block on the append implementation.

**R4.** Expand T50's cited line range to `workloop.go:391-495` to cover the full pick → ShowBead → ClaimBead block that the queue-pull rewrite must replace. (Lines 391-421 as currently written omit the hk-p4xbw guard and ClaimBead call.)

## Optional improvements

**O1.** T51 acceptance: enumerate the EM-049/050/051 conformance assertions from execution-model.md §10.2 (e.g., "test asserts capacity-gate matches `effectiveMax`, semaphore depth equals `effectiveMax`, `--max-concurrent` flag plumbed end-to-end") instead of pointing at §10.2 by reference. Reduces implementer-chase.

**O2.** Fold T03 (archival) into T02's commit as a single chore. T03 is ~5 minutes of work; standalone bead overhead exceeds the task value.

**O3.** Add a brief note in T84 that the conformance assertion should also test the §8.7.16 `operator_command_failed.command` enum (changelog C2). Currently T84 covers PL-003a / ON-013a / ON-041 / ON-050 but the spec-traceability table assigns §8.7.16 to T84 (line 343) without acceptance-criteria coverage.

## Verdict

**REQUEST_CHANGES**

R1 (QM-011/012 emission has no implementing task) is load-bearing — without it, the dispatched-from-queue runs would not carry queue identity on their event payloads, violating QM-011/012. R2/R3/R4 are dep-graph and line-range corrections that prevent implementer confusion. R1+R2+R3+R4 are all single-line edits to 07-tasks.md; resolution does not require a re-review pass — apply the edits and proceed.

If R1–R4 are applied in-place, the tasks artifact is APPROVE-grade. The spec-coverage matrix is otherwise complete, the DAG is acyclic, granularity is appropriate, Wave-3 parallel-safety holds, and T84's four conformance targets all resolve in the drafted spec text.

Word count: ~870.
