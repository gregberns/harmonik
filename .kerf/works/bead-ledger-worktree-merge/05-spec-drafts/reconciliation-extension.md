# Spec Draft: reconciliation/spec.md — Cat-BL1, Cat-BL2, Cat-BL3

**Pass 5 — 2026-05-30**
**Target file:** `specs/reconciliation/spec.md`

---

## Changes

### 1. Add three new categories after §8.11a Cat 6b

Insert the following sections after the Cat 6b section (~line 228 in specs/reconciliation/spec.md):

```
### 8.BL1 Cat-BL1 — Child-bead orphan

**Detection rule.** A bead exists in the Beads ledger with label `codename:hk-<parent-id>` AND status `open` or `in_progress`, AND `git log --all --grep="Refs: hk-<parent-id>"` returns no merge commit on main (parent run was discarded or never completed). The detector enumerates all beads carrying any `codename:hk-*` label, extracts the parent-ID suffix, and checks git for a corresponding `Refs:` commit.

**Priority.** Cat-BL1 runs after Cat 5 in the priority ordering (RC-003a). Rationale: the orphan sweep (Cat 5 + PL-006) runs first to clean up run-level artifacts; Cat-BL1 then sweeps bead-level orphans that the run-level sweep does not reach.

**Default response.** Dispatch a Cat-BL1 investigator workflow:
1. Collect all orphaned child bead IDs (label `codename:hk-<parent-id>` + no git `Refs:` commit for parent).
2. For each orphan: emit `orphaned_child_bead{bead_id, parent_id}` event.
3. Default verdict: `br close <child-id> --reason="Orphaned: parent bead hk-<parent-id> run discarded (Cat-BL1)"`.
4. Exception: if the child bead is `in_progress` (another run has claimed it), escalate to human rather than auto-closing.

**Detection point.** Daemon startup sweep (automatic), after PL-006 orphan sweep. Also triggered on-demand via `harmonik reconcile`.

Tags: mechanism

### 8.BL2 Cat-BL2 — Bead-ledger import failure

**Detection rule.** Daemon received a `bead_sync_failed` event in `.harmonik/events/events.jsonl` (emitted by the post-merge `br sync --import-only` call per BL-MRG-004). The event carries the run-id, error message, and timestamp.

**Default response.**
1. Retry `br sync --import-only` once.
2. If retry succeeds: emit `bead_ledger_recovered{run_id}` event; resume normal operations.
3. If retry fails: emit `bead_ledger_corrupt{run_id, error}` escalation event. Route to Cat 6b (auto-escalate to operator without investigator). Operator must restore `.beads/issues.jsonl` from git history (`git checkout HEAD -- .beads/issues.jsonl && br sync --import-only`) or repair.

**Detection point.** Triggered by `bead_sync_failed` event arrival — reactive, not polling. Runs at next startup if daemon crashed before handling the event.

Tags: mechanism

### 8.BL3 Cat-BL3 — Merge-conflict-log audit

**Detection rule.** `.beads/merge-conflicts.log` is non-empty after a merge that touches `.beads/issues.jsonl` (written by `harmonik beads-merge` per BL-MRG-003).

**Default response.**
1. Parse `.beads/merge-conflicts.log`; extract conflicted bead IDs and field values.
2. Emit `bead_ledger_conflict_audit{bead_ids, conflicts}` event with severity `low`.
3. Emit `operator_escalation_required` with the conflict list (audit notification; no data loss — ours/main won in all cases per BL-MRG-003).
4. Truncate `.beads/merge-conflicts.log` after emitting (it is ephemeral; conflicts are now in the event log).

**No auto-resolution.** Cat-BL3 is purely an audit notification. The merge already resolved without error (BL-MRG-003 guarantees exit 0). The operator reviews the conflict list and manually corrects if needed (e.g., `br update <id> --status=open` if a bead was incorrectly closed by LWW).

**Detection point.** Daemon startup sweep (check for non-empty log file). Also triggered after each merge via the post-merge hook if implemented.

Tags: mechanism
```

---

### 2. Update the priority ordering table

**Current priority ordering (RC-003a):**
```
Cat 0 → Cat 6b → Cat 6a → Cat 5 → Cat 3c → Cat 3b → Cat 3a → Cat 3 → Cat 2 → Cat 4 → Cat 1
```

**Updated priority ordering:**
```
Cat 0 → Cat 6b → Cat-BL2 → Cat 6a → Cat 5 → Cat-BL1 → Cat 3c → Cat 3b → Cat 3a → Cat 3 → Cat 2 → Cat-BL3 → Cat 4 → Cat 1
```

Rationale:
- Cat-BL2 (ledger import failure) after Cat 6b: both are infrastructure-class failures; BL2 is reactive (event-driven) rather than integrity-violation, but must precede normal classification since a corrupt SQLite state would invalidate all subsequent `br` reads.
- Cat-BL1 (orphan child beads) after Cat 5: run-level orphan sweep (Cat 5) runs first; bead-level orphan sweep (BL1) follows.
- Cat-BL3 (merge conflict audit) after Cat 2: purely an audit notification; low severity; does not block classification of in-flight runs.

---

### 3. Update the summary table (§8 table)

Add rows for the three new categories to the existing response-summary table:

```
| Cat-BL1 (child-bead orphan) | investigator workflow | No | `br close` (orphan) / `escalate-to-human` (if in_progress) | No (investigator required) |
| Cat-BL2 (ledger import failure) | retry → Cat 6b escalation | No (retry auto) | — | Yes (retry); then operator |
| Cat-BL3 (merge-conflict audit) | operator notification only | No | — | Yes (no-op; audit notification) |
```

---

## Changelog entry (`05-changelog.md`)

```
## reconciliation/spec.md

- **Cat-BL1** (new): Child-bead orphan detector. Triggers when a bead with `codename:hk-<parent-id>` label exists with no parent-run merge commit on main. Auto-closes orphans; escalates if orphan is in_progress.
- **Cat-BL2** (new): Bead-ledger import failure. Triggered by `bead_sync_failed` event. Retries once; routes to Cat 6b on repeated failure.
- **Cat-BL3** (new): Merge-conflict-log audit notification. Surfaces semantic conflicts logged by `harmonik beads-merge` per BL-MRG-003; no auto-resolution.
- **RC-003a** (amended): Priority ordering updated to include Cat-BL1 (after Cat 5), Cat-BL2 (after Cat 6b, before Cat 6a), and Cat-BL3 (after Cat 2).
- **§8 summary table** (amended): Added rows for Cat-BL1, Cat-BL2, Cat-BL3.
```
