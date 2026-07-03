# Integration Review — bead-ledger-worktree-merge

**Pass 6 — 2026-05-30**

## Cross-reference checks

### beads-integration.md drafted changes vs. existing specs

**BL-MRG-001 (merge driver registration):**
- References `.gitattributes` — no existing spec covers `.gitattributes` conventions. No conflict.
- Driver command `harmonik beads-merge` — new subcommand; no existing spec names it. No conflict.

**BL-MRG-004 (post-merge `br sync --import-only`):**
- Cross-references `bead_sync_failed` event — must be added to `specs/event-model.md §8.8` event registry. Gap: the spec draft does not include an event-model.md change. Add to tasks: file a bead to register `bead_sync_failed` and `bead_ledger_conflict_audit` events in event-model.md.
- The `br sync --import-only` call site is in `mergeRunBranchToMain` — same function that BL-MRG-005 removes the `--theirs` call from. No circular dependency; safe.

**BI-010e (child-bead-spawn):**
- References BI-010 (terminal-transition prohibition) and BI-011 (permitted intra-run writes) — consistent.
- References `codename:hk-<parent-id>` label convention — TERMINOLOGY COLLISION: `codename:` prefix is used for two purposes (kerf work codenames AND parent-bead lineage labels). Recommend using `parent:hk-<parent-id>` instead. Flag for resolution before implementation.

**BI-025e (concurrent `br` invocation):**
- BI-025e adapter retry policy (BI-025c) coexists with Phase 2 `BR_LOCK_TIMEOUT` env var (SQLite WAL busy timeout). Both operate at different layers. No conflict.

### reconciliation/spec.md drafted changes vs. existing specs

**Cat-BL1 (after Cat 5):** Cat 5 handles orphaned branches; BL1 handles orphaned beads. Distinct objects. No overlap.

**Cat-BL2 (after Cat 6b):** BL2 is event-driven (reactive to `bead_sync_failed`), not strictly an integrity-violation category. Placement is conservative but not wrong; Cat 6a investigators don't use `br` so BL2 import failure doesn't invalidate them. Minor concern only.

**Cat-BL3 (after Cat 2):** Audit-only, low severity. Appropriate placement.

**RC-003a:** Updated ordering documented with rationale. Consistent with existing ordering logic.

## Gaps and open questions

| # | Gap | Blocking? | Resolution |
|---|-----|-----------|-----------|
| G1 | `bead_sync_failed` + `bead_ledger_conflict_audit` events not in event-model.md | No | Implementation bead |
| G2 | `codename:hk-<parent-id>` collides with kerf work codename labels | YES | Rename to `parent:hk-<parent-id>` before implementation |
| G3 | Cat-BL2 priority placement may be too high | No | Revisit at implementation |
| G4 | `.beads/merge-conflicts.log` needs `.gitignore` entry | No | Include in merge-driver implementation bead |

## G2 resolution (required before implementation)

Rename `codename:hk-<parent-id>` → `parent:hk-<parent-id>` throughout:
- BI-010e constraints
- Cat-BL1 detection rule
- Daemon orphan-sweep detector

This change is captured in the tasks pass as a pre-condition on the spec-update beads.

## Verdict

Spec drafts are internally consistent and consistent with existing specs. G2 (label prefix collision) MUST be resolved before implementation. G1, G3, G4 are implementation-level items filed as beads.

## Contradictions Found

<TODO: For each: which specs disagreed, what the conflict was, how it was resolved.>

## Consistency Issues Found

<TODO: Terminology drift, naming mismatches, structural inconsistencies — and resolutions.>

## Cross-Reference Validity

<TODO: Confirm every `[text](file.md)` link in the drafted specs resolves and the linked content is accurate.>

## Changelog Verification

<TODO: Confirm 05-changelog.md matches the actual drafts.>

## Final Assessment

<TODO: One paragraph on overall coherence of the spec corpus after this change.>
