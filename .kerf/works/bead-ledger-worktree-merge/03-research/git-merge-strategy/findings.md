# Research: Git-Level Merge Strategy (C1)

**Pass 3 — 2026-05-30**

## Research questions

1. Does `BD_DB=<main>/.beads/beads.db` cause `br sync` to write JSONL into main's working tree?
2. Does SQLite WAL mode serialize concurrent writes from multiple `br` processes safely?
3. How does `br sync --merge` handle `labels` and `dependencies` — union or LWW?
4. What env vars control JSONL output path when `BD_DB` is set?

---

## R1: BD_DB and JSONL output path

**Confirmed: JSONL always colocates with the DB.**

When `BD_DB` is set to main repo's `beads.db`, `br` derives the `.beads/` directory from the DB file's parent and writes JSONL there:

```
beads_dir=/Users/gb/github/harmonik/.beads
jsonl_path=/Users/gb/github/harmonik/.beads/issues.jsonl
```

There is a `BEADS_JSONL` env var that can redirect JSONL output to an external path, but it requires `--allow-external-jsonl` flag. Without that override, JSONL always lands in the same `.beads/` directory as the DB.

**Impact on Phase 2 (shared-DB) approach:** Worktree agents with `BD_DB` pointing at main's DB will flush JSONL to main's working tree automatically. This is **desirable** for the daemon's `br sync --flush-only` → `git add issues.jsonl` → commit pattern from main. However, it means worktree agents calling `br sync --flush-only` mid-run would dirty main's working tree — likely safe if agents only call `br create`/`br update` (which update SQLite directly) without calling `br sync`. The daemon owns the flush.

**Key constraint:** Phase 2 must ensure worktree agents do NOT call `br sync` (which would write to main's JSONL mid-run before the daemon's controlled flush). Agents should use only `br create`/`br update`/`br close` (SQLite-only writes).

---

## R2: SQLite WAL concurrent write safety

**WAL mode is confirmed active on both main and worktree DBs:**
- `PRAGMA journal_mode` returns `wal`.
- `beads.db-shm` and `beads.db-wal` sidecar files exist in main's `.beads/`.
- `PRAGMA busy_timeout` returns **0** — no retry window set.

**WAL concurrency semantics:** WAL allows multiple concurrent readers but serializes concurrent writers via SQLite's write-lock. With `busy_timeout=0`, a second concurrent writer that cannot immediately acquire the write-lock gets `SQLITE_BUSY` and fails immediately.

**Risk for Phase 2 multi-agent swarms:** Under N>1 concurrent agents all writing to main's shared DB, any agent calling `br create` or `br update` while another holds the write-lock will fail with `SQLITE_BUSY`. The `--lock-timeout <ms>` flag on `br` can set a retry window (e.g., `BR_LOCK_TIMEOUT=5000` for 5s). Without it, transient failures are expected under concurrent load.

**Mitigation required:** Phase 2 spec must mandate `BR_LOCK_TIMEOUT` env var (e.g., 5000ms) in the worktree agent environment alongside `BD_DB`. This converts "fail immediately" to "retry for 5s" — sufficient for the sparse concurrent write pattern in practice (agents rarely write beads at the exact same millisecond).

---

## R3 (partial): br sync --merge label/dependency handling

**No set-union for labels or dependencies.** `br sync --merge` uses three-way merge (with `beads.base.jsonl` as ancestor):

- Semantic conflicts (field changed in both DB and JSONL relative to base) require `--force-db`, `--force-jsonl`, or `--force` (newer timestamp) to resolve.
- `labels` and `dependencies` are treated as **opaque fields** subject to LWW semantics — not union/set-merge.
- Concurrent label additions on both sides → semantic conflict; one side's additions dropped.

**Impact on C1 (merge-driver):** The custom `harmonik beads-merge` driver must implement its own union logic for `labels` and `dependencies` arrays. Relying on `br sync --merge` alone is insufficient for these fields. This confirms the design requirement in BL-MRG-002 that the driver optionally union `labels` and `dependencies`.

**`beads.base.jsonl` does not exist** in the repo's `.beads/` — `br sync --merge` has never been run here. Using `br sync --merge` as a primary strategy would require the daemon to create and maintain this ancestor file at worktree-create time.

---

## Architecture recommendation update

Given the research findings, the **Phase 1 merge-driver remains the right first ship**. Phase 2 (shared-DB) is viable but requires:

1. `BR_LOCK_TIMEOUT` injection (to avoid `SQLITE_BUSY` under concurrent load).
2. Agent discipline to NOT call `br sync` (daemon owns flush, not agents).
3. Explicit documentation that mid-run `br sync` by an agent would corrupt the controlled flush cycle.

The merge-driver (Phase 1) has no such multi-writer concerns: each worktree writes its own JSONL copy, and the merge-driver handles union at merge time. Phase 1 is lower-risk for the initial ship.

**R4 finding for merge-driver design (C1):** Since `br sync --merge` doesn't union arrays, the custom merge-driver (BL-MRG-002) must explicitly union `labels` and `dependencies` arrays. This should be the default behavior (not behind a flag), since label/dep additions are always monotonic-additive in practice and LWW dropping labels is worse than occasionally carrying stale ones.
