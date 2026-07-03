# Design: Git-Level Merge Strategy (C1)

**Pass 4 — 2026-05-30**

---

## Current state

`specs/beads-integration.md` has no §BL-MRG section. The merge conflict workaround lives in:

- `internal/daemon/workloop.go:2689` — `mergeRebaseAutoResolveBeadsLedger`: calls `git checkout --theirs .beads/issues.jsonl` when JSONL is the sole conflicting file.
- `HANDOFF.md:~55` — documents this as a known workaround (lossy).

There is no `.gitattributes` entry for `.beads/issues.jsonl`. The `harmonik` binary has no `beads-merge` subcommand.

---

## Target state

### New spec section: `specs/beads-integration.md §BL-MRG` (Bead Ledger Merge Contract)

Add a new normative section after BI-025e:

**BL-MRG-001. Merge driver registration.** `.gitattributes` MUST contain:
```
.beads/issues.jsonl merge=beads-union
```
`.git/config` (or `~/.gitconfig`) MUST configure the driver:
```
[merge "beads-union"]
    driver = harmonik beads-merge %O %A %B %P
    name = Bead Ledger Union Merge
```
**Note:** `.gitattributes` is repo-tracked; the git config entry is per-install. Daemon setup MUST configure the driver on first run if not already present.

**BL-MRG-002. Driver algorithm.** `harmonik beads-merge` MUST implement union-by-ID:
1. Parse ancestor `%O`, ours/main `%A`, theirs/run-branch `%B` as `map[id]row`.
2. For each ID in `union(O, A, B)`:
   - Present in only one of A or B beyond O → take it (covers child-bead-spawn creates).
   - Present in both A and B, equal → take either.
   - Present in both and differ → pick row with larger `updated_at`.
3. **Array field union:** For `labels` and `dependencies` arrays, perform set-union from both A and B sides (not LWW). Rationale: research confirmed `br sync --merge` treats these as opaque LWW; the driver must compensate. Label/dep additions are monotonic-additive in practice.
4. Write rows back ID-sorted to `%A`, exit 0.
5. Emit one line per semantic conflict to `.beads/merge-conflicts.log` (BL-MRG-003).

**Rationale for array union:** `br sync --merge` applies LWW to `labels` and `dependencies` as opaque fields — concurrent label additions on both sides would drop one side's additions. The driver must explicitly union these arrays.

**BL-MRG-003. Unresolvable conflicts.** Same bead closed on A, reopened on B within the same `updated_at` second → pick A (ours/main) and append a line to `.beads/merge-conflicts.log`: `<timestamp> CONFLICT bead=<id> field=status a=<A_value> b=<B_value>`. Do NOT fail the merge (exit 0 always). The reconciliation investigator reads this log (Cat-BL3).

**BL-MRG-004. Post-merge SQLite refresh.** Daemon MUST call `br sync --import-only` against its SQLite after any merge that touches `.beads/issues.jsonl`, before any subsequent `br` operations (e.g. terminal-transition `br close`). This ensures the daemon's SQLite reflects the merged JSONL state.

**BL-MRG-005. Supersession of `mergeRebaseAutoResolveBeadsLedger`.** With the merge-driver registered, the explicit `git checkout --theirs .beads/issues.jsonl` fallback in `mergeRebaseAutoResolveBeadsLedger` MUST be removed. The driver runs automatically during rebase/merge; the `--theirs` override would suppress it. The function should be deleted or reduced to a logging-only stub.

**BL-MRG-006 (Phase 2 migration path, informative).** If `BD_DB` env var is set in the worktree agent environment pointing at main's `beads.db`, worktrees MAY skip JSONL tracking entirely. Research findings:
- JSONL always colocates with the DB — `BD_DB` pointing at main's DB causes all `br sync` flushes to write main's JSONL directly.
- Agents MUST NOT call `br sync` with Phase 2 active (daemon owns the flush cycle).
- `BR_LOCK_TIMEOUT=5000` (or higher) MUST be set alongside `BD_DB` to avoid `SQLITE_BUSY` under concurrent write load (SQLite WAL has `busy_timeout=0` by default, causing immediate-fail on write contention).
- When Phase 2 is active, BL-MRG-001–005 are no-ops for that worktree (no JSONL in worktree to merge).

Phase 2 is not required for child-bead-spawn safety (the merge-driver covers that). Phase 2 is required only for stale-at-fork resolution (agent's `br show <parent>` fails if parent was created after the worktree fork). Phase 2 is a follow-up bead.

---

## Rationale

The merge-driver solves the primary bug (child-bead creates silently dropped) with minimal spec surface and no architectural risk. Research confirmed:
- The union-by-ID algorithm is correct for the dominant conflict class (child-bead-spawn).
- `br sync --merge` cannot replace the driver (treats labels/deps as LWW, not union).
- Phase 2 (shared-DB) introduces `SQLITE_BUSY` risk at `busy_timeout=0` and requires agent discipline to not call `br sync` mid-run; defer to a follow-up bead.

---

## Implementation sites

- `.gitattributes` — add `merge=beads-union` entry.
- `internal/cmd/beads_merge.go` (new) — `harmonik beads-merge` subcommand.
- `internal/daemon/workloop.go:2689` — delete `mergeRebaseAutoResolveBeadsLedger` or remove the `--theirs` call.
- `internal/daemon/workloop.go` (post-merge path) — add `br sync --import-only` call (BL-MRG-004).
- Daemon setup code — configure `merge.beads-union.driver` in git config on startup.
