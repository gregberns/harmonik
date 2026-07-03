# Changelog — bead-ledger-worktree-merge

**Pass 5 — 2026-05-30**

## specs/beads-integration.md

- **BI-011** (amended): Renamed "No intra-run writes" → "Permitted and prohibited intra-run writes". Added permitted-write table with `claim` (existing), `child-bead-spawn` (new BI-010e), and `parent-bead-label` (new). Added explicit failure contract for prohibited terminal writes from inside worktrees.
- **BI-010e** (new): Child-bead-spawn creates. Defines constraints: lineage label (`codename:hk-<parent-id>`), idempotency check, terminal-transition prohibition, merge-driver preservation guarantee.
- **§BL-MRG** (new section, 6 clauses): Normative bead-ledger merge contract.
  - BL-MRG-001: Merge driver registration in `.gitattributes` and `.git/config`.
  - BL-MRG-002: Union-by-ID algorithm with set-union for `labels` and `dependencies` arrays.
  - BL-MRG-003: Semantic conflict logging to `.beads/merge-conflicts.log`.
  - BL-MRG-004: Post-merge `br sync --import-only` requirement.
  - BL-MRG-005: Removal of `mergeRebaseAutoResolveBeadsLedger` / `--theirs` workaround.
  - BL-MRG-006: Phase 2 shared-DB migration path (informative).

## specs/reconciliation/spec.md

- **Cat-BL1** (new §8.BL1): Child-bead orphan. Detects beads with `codename:hk-<parent-id>` label and no parent-run merge commit. Auto-closes orphaned open beads; escalates if in_progress.
- **Cat-BL2** (new §8.BL2): Bead-ledger import failure. Triggered by `bead_sync_failed` event (from BL-MRG-004 failure). Retries once; routes to Cat 6b on repeated failure.
- **Cat-BL3** (new §8.BL3): Merge-conflict-log audit. Surfaces semantic conflicts logged by `harmonik beads-merge` per BL-MRG-003. Operator notification only; no auto-resolution.
- **RC-003a** (amended): Priority ordering updated: `Cat 0 → Cat 6b → Cat-BL2 → Cat 6a → Cat 5 → Cat-BL1 → Cat 3c → Cat 3b → Cat 3a → Cat 3 → Cat 2 → Cat-BL3 → Cat 4 → Cat 1`.
- **§8 summary table** (amended): Added rows for Cat-BL1, Cat-BL2, Cat-BL3.

## Implementation sites (not spec changes)

- `.gitattributes` — add `.beads/issues.jsonl merge=beads-union`.
- `internal/cmd/beads_merge.go` (new) — `harmonik beads-merge` subcommand.
- `internal/daemon/workloop.go:2689` — delete `mergeRebaseAutoResolveBeadsLedger`.
- `internal/daemon/workloop.go` (post-merge path) — add `br sync --import-only` call + `bead_sync_failed` event.
- `internal/daemon/reconciliation.go` — add Cat-BL1 and Cat-BL3 detectors to startup sweep.
- `.gitignore` — add `.beads/merge-conflicts.log` (ephemeral operator output).
- Daemon setup — auto-configure `merge.beads-union.driver` in `.git/config` on startup.
