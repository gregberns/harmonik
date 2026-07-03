# C1 — Remediating worktree-directory reaping — Research findings

Component: extend the boot orphan sweep to REMOVE orphaned `.harmonik/worktrees/<run_id>/`
directories, not just clear their lease-locks. Bead hk-9eury, analysis gap #1, goal G1.

Anchors verified at `2e49a8df` (main). All file:line references re-checked against the live tree.

## RQ1 — What does the existing sweep do with worktrees today, and where is the lock-only behavior?

**Finding:** The boot sweep clears stale lease-LOCKS and prunes git bookkeeping, but never removes the worktree DIRECTORY.

- `internal/daemon/orphansweep.go:235-239` — `RunOrphanSweep` calls `workspace.SweepStaleLeaseLocks(ctx, projectDir, NoWorktreeRootOverride())` and sets `result.LocksCleared = len(sweepResult.Removed)`. `Removed` is the list of worktree paths whose **lease-lock file** was removed — NOT the worktree dir.
- `internal/workspace/orphansweep.go:60-98` — `SweepStaleLeaseLocks`: for each discovered worktree whose lease-lock PID is dead (`isPIDDead`, `os.FindProcess`+`Signal(0)`), it calls `ReleaseLeaseLock(leaseLockPath)` (removes `${wt}/.harmonik/lease.lock`) and appends the path to `result.Removed`. After the loop it runs `git -C <repoRoot> worktree prune` (line 96). **`prune` only drops `.git/worktrees/<name>/` admin entries for directories already gone — it does NOT delete a still-present worktree directory** (and it skips locked entries). So a crashed run's `.harmonik/worktrees/<run_id>/` dir with its working files persists indefinitely; only its lease-lock and (if the dir were already deleted) git admin entry are reclaimed.

This is exactly the incident's "11 orphaned run-worktrees": lock-clearing + prune left the directories themselves.

## RQ2 — Is there an existing `git worktree remove --force` helper to reuse?

**Finding:** Yes — `internal/workspace/createworktree.go:32-37` documents and the `CreateReviewerWorktree` cleanup closure calls `git worktree remove --force --force` then `git worktree prune`, idempotent and safe to call >once. This is the canonical removal idiom in-repo and the model C1 should follow. `--force --force` (double) is required to remove a worktree with a dirty/locked working tree.

**Tradeoff (removal mechanics):**
- **Option A — `git worktree remove --force --force <path>` then `git worktree prune`.** Pro: git-aware, updates admin records atomically, matches existing reviewer-cleanup idiom. Con: fails if the path is not a registered worktree (admin entry already pruned, dir survives) — needs a fallback.
- **Option B — `os.RemoveAll(<path>)` then `git worktree prune`.** Pro: always reclaims the directory even when git no longer knows about it. Con: leaves git admin records until the follow-up prune; bypasses git's locked-worktree guard.
- **Recommendation:** A-then-B fallback — try `git worktree remove --force --force`; on "not a working tree"/non-registered error, `os.RemoveAll` the dir; always finish with one `git worktree prune`. Mirrors the reviewer cleanup but is robust to the prune-already-ran case.

## RQ3 — How is "this worktree is still live / re-attached" determined (C1-R2)?

**Finding:** Two signals already exist; both must be honored before removal:
1. **Lease-lock liveness** — `workspace.IsLeaseLockStale(pid)` / `isPIDDead(pid)` (`internal/workspace/orphansweep.go:104-132`): a worktree whose lease-lock PID is ALIVE is in use; skip. `DiscoveredWorktree.LeaseLock` (`discoverworktrees.go:54-56`) is nil when the lock file is absent (WM-013a "not leased").
2. **Re-attached live run** — PL-005 step 7 active-run reconstruction (`internal/lifecycle/activerun_em031a.go`, EM-031a) produces the set of run_ids re-attached to live in-flight runs. A worktree whose `run_id` is in that set MUST NOT be removed. C1 must consume this set (same one C2/C3 read per the decomposition dependency graph).

So C1-R2's "live" = (lease-lock PID alive) OR (run_id in EM-031a re-attached set). Removal fires only when BOTH say dead/absent.

## RQ4 — Provenance boundary (C1-R3): how do we know a worktree is THIS project's?

**Finding:** Provenance is by filesystem location + git-worktree registration. `SweepStaleLeaseLocks` only iterates worktrees discovered under this repo's `<repoRoot>/.harmonik/worktrees/` root (via `DiscoverWorktrees` + `WorktreeRootConfig`/`NoWorktreeRootOverride`). A directory outside this root, or a git worktree registered to a different repo, is never enumerated. This satisfies C1-R3 WITHOUT the PL-006a env/PGID/tmux-prefix marker — the marker gates process/tmux kills (C4), not filesystem worktrees whose provenance is structural. **Risk to flag:** confirm `DiscoverWorktrees` is rooted strictly at the project's worktree root and cannot follow a symlink/`--worktree-root` override into another project; change spec should assert removal is gated to paths with prefix `<repoRoot>/.harmonik/worktrees/`.

## RQ5 — Observability + non-fatal posture (C1-R4, R5, R6)

**Finding:** Payload struct is `OrphanSweepResult` → `ToPayload()` → `core.DaemonOrphanSweepCompletedPayload` (`internal/daemon/orphansweep.go:24-92`). Existing additive integer fields: `TmuxSessionsKilled`, `LocksCleared`, `StaleIntentsObserved`, `BeadInProgressReset`, `BeadCat3cClosed`, `ClaudeWorktreesSwept`. C1-R4 adds one more additive integer, e.g. `WorktreeDirsRemoved`, N-1-tolerated per event-model §6.3 (precedent: every field above was added additively). Non-fatal posture (C1-R5) is already the contract: `RunOrphanSweep` accumulates step errors into `errs []string` and `daemon.Start` (`daemon.go:743-769`) does NOT abort on sweep error; C1's removal step appends to the same slice. Idempotency (C1-R6): `git worktree remove`/`RemoveAll` of an already-absent dir is a no-op → count 0 on second boot; the `cleanup` closure idiom is documented safe to call >once.

## Risks / unknowns
- **R1 (locked worktree):** a `git worktree lock`-ed worktree resists `remove` with single `--force`; the double `--force --force` is needed (reviewer-cleanup precedent already uses it).
- **R2 (concurrent boot race):** removal runs BEFORE socket bind (PL-005 step 3 ordering) so no concurrent dispatch can re-create the dir mid-sweep; safe within the single-daemon pidfile-lock invariant.
- **R3 (sidecar/session-log retention) — needs a change-spec decision:** `crashevidence.go` classifies bare-worktree-no-lease vs sidecar-without-lease states (Cat-3 evidence). The change spec must decide whether a worktree carrying an un-reconciled session sidecar should be PRESERVED for reconciliation rather than removed. Recommend: reap only bare-stale worktrees; preserve worktrees with unprocessed sidecars until Cat-3 routing consumes the evidence.

## No-blocker assertion
No unresolved blocker prevents a C1 change spec. One change-spec decision required (RQ-R3: preserve-sidecar-evidence vs remove-all-stale) — both codeable; recommend preserve-sidecar.
