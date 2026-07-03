# Implementation Tasks — bead-ledger-worktree-merge

**Pass 7 — 2026-05-30**

## Dependency graph

```
T1 (spec: BI-010e, BI-011, BL-MRG) → T2 (beads-merge subcommand) → T5 (integration test)
T1 → T3 (daemon: remove --theirs, add --import-only)       → T5
T1 → T4 (reconciliation spec + detectors)                  → T5
T4 → T5
T0 (label-rename pre-condition) → T1 (must complete first)
T6 (event-model.md events) — parallel, no blockers
```

---

## T0 — Resolve label-prefix collision (pre-condition)

**What:** Rename `codename:hk-<parent-id>` → `parent:hk-<parent-id>` throughout all spec drafts and implementation code before implementation begins. The `codename:` prefix is already used for kerf work codenames (CLAUDE.md convention) — reusing it for bead lineage labels creates ambiguity.

**Spec sections:** BI-010e (constraints §1), Cat-BL1 (detection rule), 06-integration.md §G2.

**Deliverables:**
- Updated spec drafts (05-spec-drafts/) with `parent:hk-<parent-id>` throughout.
- No code changes (pre-implementation).

**Acceptance criteria:** No occurrence of `codename:hk-` in spec drafts or implementation code.

**Note:** This is a spec-text amendment, not a separate bead. The implementing agent for T1 applies this rename before writing spec text.

---

## T1 — Write spec text: beads-integration.md (BI-010e, BI-011 amendment, §BL-MRG)

**What:** Apply the three changes from `05-spec-drafts/beads-integration-bl-mrg.md` to `specs/beads-integration.md`:
1. Amend BI-011 (add permitted-write table, add `child-bead-spawn` and `parent-bead-label` categories).
2. Add BI-010e clause (child-bead-spawn creates, constraints including G2 rename).
3. Add §BL-MRG section (BL-MRG-001 through BL-MRG-006).

**Spec sections:** BI-010e (new), BI-011 (amended), §BL-MRG (new, 6 clauses).

**Deliverables:**
- `specs/beads-integration.md` — amended BI-011, new BI-010e, new §BL-MRG.

**Acceptance criteria:**
- `br list --label=parent:hk-<id>` is the documented idempotency-check pattern.
- `harmonik beads-merge %O %A %B %P` is the documented driver command.
- `br sync --import-only` is documented as mandatory post-merge step.
- `mergeRebaseAutoResolveBeadsLedger` is referenced for removal (BL-MRG-005).

**Depends on:** T0 (label rename).

---

## T2 — Implement `harmonik beads-merge` subcommand

**What:** New Go file `internal/cmd/beads_merge.go` implementing the `harmonik beads-merge %O %A %B %P` driver per BL-MRG-002/003.

**Algorithm:**
1. Parse three JSONL files (ancestor, ours, theirs) into `map[string]json.RawMessage` keyed by bead ID.
2. Union-by-ID with `updated_at` LWW for scalar fields, set-union for `labels` and `dependencies` arrays.
3. For semantic conflicts (same bead differs in both sides): take ours, append to `.beads/merge-conflicts.log`.
4. Write output ID-sorted to `%A` (ours path).
5. Exit 0 always.

**Also add:**
- `.gitattributes` entry: `.beads/issues.jsonl merge=beads-union`.
- `.gitignore` entry: `.beads/merge-conflicts.log`.
- Daemon startup: auto-configure `merge.beads-union.driver` in `.git/config` if absent.

**Spec sections:** BL-MRG-001, BL-MRG-002, BL-MRG-003.

**Deliverables:**
- `internal/cmd/beads_merge.go`
- `.gitattributes` (amended)
- `.gitignore` (amended)
- Daemon startup code (amended, likely `internal/daemon/setup.go` or equivalent)

**Acceptance criteria:**
- `harmonik beads-merge` is a registered subcommand.
- Union-by-ID test: ancestor has IDs {A}, ours adds {B}, theirs adds {C} → output has {A, B, C}.
- Child-bead-spawn test: ancestor has N rows, theirs adds 1 new ID → merged output has N+1 rows.
- LWW test: same bead differs in ours/theirs by `updated_at` → newer wins.
- Array union test: bead in ours has `labels=["x"]`, same bead in theirs has `labels=["y"]` → merged has `labels=["x","y"]`.
- Conflict log test: same bead status closed on ours, open on theirs, same `updated_at` second → `.beads/merge-conflicts.log` has one entry, ours (closed) wins.

**Depends on:** T1 (spec must be written before code).

---

## T3 — Daemon: remove `mergeRebaseAutoResolveBeadsLedger`, add `br sync --import-only`

**What:**
1. Delete (or stub) `mergeRebaseAutoResolveBeadsLedger` in `internal/daemon/workloop.go:2689`. With the merge-driver registered, this function's `git checkout --theirs .beads/issues.jsonl` override suppresses the driver.
2. In the normal merge path (`mergeRunBranchToMain`), after any merge touching `.beads/issues.jsonl`, add a `br sync --import-only` call. On failure: emit `bead_sync_failed` event per BL-MRG-004.

**Spec sections:** BL-MRG-004, BL-MRG-005.

**Deliverables:**
- `internal/daemon/workloop.go` (amended: remove `--theirs` call, add `--import-only` call + event emission).

**Acceptance criteria:**
- `mergeRebaseAutoResolveBeadsLedger` is deleted or contains no `--theirs` call.
- `br sync --import-only` is called after merge in the post-merge path.
- On `br sync --import-only` failure, `bead_sync_failed` event is emitted to `.harmonik/events/events.jsonl`.

**Depends on:** T1 (spec), T2 (driver must be registered so rebase doesn't fail).

---

## T4 — Write spec text and detectors: reconciliation/spec.md (Cat-BL1, BL2, BL3)

**What:**
1. Apply changes from `05-spec-drafts/reconciliation-bl-cats.md` to `specs/reconciliation/spec.md`:
   - Add §8.BL1 (Cat-BL1), §8.BL2 (Cat-BL2), §8.BL3 (Cat-BL3).
   - Update RC-003a priority ordering.
   - Update §8 summary table.
2. Implement Cat-BL1 and Cat-BL3 detectors in daemon reconciliation code:
   - Cat-BL1 detector: at startup, enumerate beads with `parent:hk-*` label, check git log for parent merge commits, close orphans.
   - Cat-BL3 detector: at startup, check if `.beads/merge-conflicts.log` is non-empty, emit `bead_ledger_conflict_audit` event.
3. Cat-BL2 is reactive (triggered by `bead_sync_failed` event from T3); no startup detector needed.

**Spec sections:** §8.BL1, §8.BL2, §8.BL3, RC-003a amendment.

**Deliverables:**
- `specs/reconciliation/spec.md` (amended).
- `internal/daemon/reconciliation.go` (amended: Cat-BL1 and Cat-BL3 detectors).

**Acceptance criteria:**
- Cat-BL1 detector closes a bead with `parent:hk-<id>` label when no `Refs: hk-<id>` merge commit exists.
- Cat-BL1 does NOT close a bead with `parent:hk-<id>` label when it is `in_progress` (escalates instead).
- Cat-BL3 detector emits `bead_ledger_conflict_audit` event when `.beads/merge-conflicts.log` is non-empty and truncates the file.

**Depends on:** T1 (spec), T3 (`bead_sync_failed` event is the Cat-BL2 trigger).

---

## T5 — Integration test: child-bead-spawn round-trip

**What:** Scenario test (in the scenario-harness or as a standalone script) that:
1. Creates a parent bead.
2. Starts a harmonik run for the parent bead.
3. Agent creates N child beads with `parent:hk-<parent-id>` label.
4. Agent commits and exits.
5. Daemon merges the run branch to main.
6. Verify: all N child beads survive in main's `.beads/issues.jsonl` and main's SQLite (via `br list --label=parent:hk-<parent-id>`).
7. Verify: `br sync --import-only` was called (check event log for expected sequence).

**Spec sections:** BI-010e (child-bead-spawn round-trip), BL-MRG-002 (union preserves child-bead creates), BL-MRG-004 (post-merge import).

**Deliverables:**
- Scenario test file (location TBD by test infrastructure conventions).

**Acceptance criteria:** Test passes end-to-end with 0 child beads dropped.

**Depends on:** T2, T3, T4.

---

## T6 — Register new events in event-model.md

**What:** Add two new event types to `specs/event-model.md §8.8` (or equivalent event registry):
- `bead_sync_failed{run_id, error, timestamp}`
- `bead_ledger_conflict_audit{run_id, bead_ids, conflicts, timestamp}`

**Spec sections:** BL-MRG-004 references `bead_sync_failed`; BL-MRG-003/Cat-BL3 references `bead_ledger_conflict_audit`.

**Deliverables:**
- `specs/event-model.md` (amended).

**Acceptance criteria:** Both event types appear in the event registry with field definitions.

**Depends on:** None (parallel).

---

## Bead creation plan

File 5 implementation beads in this order:

| Bead | Title | Priority | Depends on |
|------|-------|----------|-----------|
| B1 | `harmonik beads-merge` subcommand: union-by-ID JSONL merge driver | P1 | — |
| B2 | spec: amend beads-integration.md (BI-010e, BI-011, §BL-MRG) | P1 | — |
| B3 | daemon: remove mergeRebaseAutoResolveBeadsLedger --theirs, add br sync --import-only post-merge | P1 | B1 |
| B4 | spec+detectors: reconciliation Cat-BL1/BL2/BL3 + startup sweep | P2 | B2, B3 |
| B5 | spec: register bead_sync_failed + bead_ledger_conflict_audit in event-model.md | P2 | — |

B1 and B2 can be dispatched immediately in parallel. B3 depends on B1 (driver must exist before removing --theirs). B4 depends on B2 and B3.

- **What:** <TODO: what to build or change>
- **Spec sections:** <TODO: file.md §Section>
- **Deliverables:** <TODO: files created or modified>
- **Acceptance:** <TODO: how to verify the code matches the spec>
- **Depends on:** <TODO: other task IDs, or "none">

### <TODO: task ID>

- **What:** <TODO: ...>
- **Spec sections:** <TODO: ...>
- **Deliverables:** <TODO: ...>
- **Acceptance:** <TODO: ...>
- **Depends on:** <TODO: ...>

## Dependency Graph

<TODO: ASCII or bulleted edges. Must form a valid DAG.>

## Parallelization Plan

<TODO: Which tasks can run concurrently? Which serialize?>
