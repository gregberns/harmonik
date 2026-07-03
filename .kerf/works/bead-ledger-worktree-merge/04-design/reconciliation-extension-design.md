# Design: Reconciliation Extension (C4)

**Pass 4 — 2026-05-30**

---

## Current state

`specs/reconciliation.md` has 11 categories (Cat 0–6b). No category explicitly covers:
- Child bead exists, parent run discarded (orphan-bead).
- Bead-ledger SQLite import failure post-merge.
- `.beads/merge-conflicts.log` has entries requiring operator review.

The spec classifies runs, not bead hierarchies. Parent/child bead relationships are not modeled.

---

## Target state

### Three new categories in `specs/reconciliation.md`

**Cat-BL1: Child-bead orphan.**

*Trigger:* A bead with label `codename:hk-<parent-id>` exists in `open` or `in_progress` state, AND there is no merge commit on main with `Refs: hk-<parent-id>` (parent run was discarded or never completed).

*Investigator action:* Emit `orphaned_child_bead` event to `.harmonik/events/events.jsonl`. Collect all affected child IDs. Default verdict: `br close <child-id> --reason="Orphaned: parent bead hk-<parent-id> run discarded"`. Investigator MAY escalate to human if child bead has already been claimed (in_progress) by a downstream agent.

*Detection point:* Runs at daemon startup sweep (automatic). Also triggered on-demand via `harmonik reconcile`.

*Rationale:* With BI-010e enabling child-bead-spawn, failed/discarded parent runs will leave orphaned open child beads. Without sweeping them, the bead ledger accumulates phantom open items that pollute `kerf next` output and `br ready` feeds.

---

**Cat-BL2: Bead-ledger import failure.**

*Trigger:* Daemon's `br sync --import-only` call fails after a merge that touches `.beads/issues.jsonl` (per BL-MRG-004).

*Daemon action:* Emit `bead_sync_failed` event with error details to `.harmonik/events/events.jsonl`. Do NOT continue with downstream `br` operations (e.g. terminal-transition `br close`) that depend on DB state.

*Investigator action:* Retry `br sync --import-only` once. If still fails: emit `bead_ledger_corrupt` escalation event, route to Cat 6b (operator must restore `.beads/` from git history or `br sync --flush-only` from known-good state).

*Rationale:* BL-MRG-004 makes the import-only call a required post-merge step. Without a reconciliation category for its failure, the daemon would silently continue with a SQLite state that diverges from the merged JSONL, causing phantom state in subsequent `br` operations.

---

**Cat-BL3: Merge-conflict-log has entries.**

*Trigger:* `.beads/merge-conflicts.log` is non-empty after a merge (written by `harmonik beads-merge` per BL-MRG-003).

*Daemon action:* Emit `bead_ledger_conflict_audit` event with the log contents. Do NOT block the merge or subsequent workflow steps — conflicts were resolved (by taking ours/main) and logged for review.

*Investigator action:* Parse `.beads/merge-conflicts.log`, extract conflicted bead IDs, surface to operator as an `operator_escalation_required` event with severity `low` (no data loss — ours/main won; this is an audit notification). Clear the log after emitting.

*Rationale:* BL-MRG-003 guarantees the merge never fails, but the conflict log records cases where the LWW resolution may have lost intent (e.g. one side closed a bead, the other reopened it at the same second). The operator needs visibility to manually audit and correct if needed.

---

## Implementation sites

- `specs/reconciliation.md` — add Cat-BL1, Cat-BL2, Cat-BL3 sections.
- `internal/daemon/workloop.go` (post-merge path) — emit `bead_sync_failed` event on `br sync --import-only` failure (feeds Cat-BL2 trigger).
- `internal/daemon/reconciliation.go` (or equivalent) — add detectors for Cat-BL1 and Cat-BL3 to the startup sweep.
- `.beads/merge-conflicts.log` — referenced by Cat-BL3; daemon should gitignore this file (it's ephemeral operator output, not a ledger artifact).
