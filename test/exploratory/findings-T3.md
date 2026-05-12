# T3 Exploratory Testing Findings — Daemon Lifecycle Boundary Cases

Tester: T3
Date: 2026-05-12
Scope: `EXPLORATORY_TESTING_PLAN.md §T3` — daemon lifecycle boundary cases
Test file: `internal/daemon/t3_exploratory_test.go`

---

## Scenario Results

### T3-01: Double invocation (same project dir)

**Status: PASS** (with one noted gap)

`daemon.Start` returns `lifecycle.ErrPidfileLocked` when a second invocation
attempts to start against the same project directory while the first daemon holds
the pidfile flock. The flock mechanism works correctly — the second call is rejected
immediately before any work loop activity.

**Gap noted (hk-b6m3h, P3):** `cmd/harmonik/main.go` returns exit code 1 for all
errors. PL-008a mandates exit code 5 for `ErrPidfileLocked`. External callers
(systemd, orchestrators) cannot distinguish pidfile contention from other startup
failures via exit code.

Test: `TestT3_DoubleInvocation`

---

### T3-02: SIGINT mid-run

**Status: FINDING** — bead stuck in `in_progress` after shutdown

When SIGINT arrives while the handler subprocess is sleeping (in-flight):

1. `daemon.Start` returns `nil` (clean, correct).
2. The bead is **left in `in_progress` status** and is NOT dispatched on the next run.
3. The git worktree created by `workspace.CreateWorktree` is left on disk.

**Root cause (hk-wdeen, P2):** `brcli.ReopenBead` calls `br reopen <bead_id>` which
only transitions `closed -> open`. When the handler is killed by signal cancellation,
the bead is in `in_progress` state. `br reopen` on an `in_progress` bead is silently
skipped by br ("already in_progress"). The bead is stranded; `brcli.Ready()` returns
only `open` beads so the next daemon will not dispatch it.

**Fix direction:** ReopenBead must use `br update --status open` for the
`in_progress -> open` transition (not `br reopen`). Alternatively, the work loop
needs to detect the current bead status and choose the correct rollback command.

**Worktree gap (hk-j4avq, P3):** The left-behind worktree is the known gap hk-fgdgz.
Confirmed: worktree exists at `.harmonik/worktrees/<run_id>/` after SIGINT.

Test: `TestT3_SIGINTMidRun`

---

### T3-03: SIGTERM mid-run

**Status: FINDING** — same as T3-02

SIGTERM behaves identically to SIGINT: `daemon.Start` uses
`signal.NotifyContext(SIGINT, SIGTERM)` so both cancel `loopCtx`. The bead is left
in `in_progress` and the worktree is left on disk.

**Root cause:** Same as T3-02 (hk-wdeen).

Test: `TestT3_SIGTERMMidRun`

---

### T3-04: Stale pidfile (PID no longer exists)

**Status: PASS**

When a stale pidfile is present (PID 99999999 written manually to simulate a crash
residue), a new `daemon.Start` call succeeds by acquiring the flock directly.
`flock(LOCK_EX|LOCK_NB)` on the existing file succeeds because the crashed process
released the lock on death. `AcquirePidfile` then truncates and rewrites the file
with the new PID.

Note: `daemon.Start` does NOT call `lifecycle.ProbePidfileLock` before
`AcquirePidfile` (the two-step probe mandated by PL-002a). The flock mechanism
handles the common case correctly without the probe, but the ambiguity path
(PID reuse after reboot) is not covered by the current startup sequence.

Test: `TestT3_StalePidfile`

---

### T3-05: Stale worktree on disk — orphan sweep

**Status: NOT CONFIRMED** (test fixture error — manual investigation result recorded)

**Test fixture issue:** The test seeded a lease-lock file at
`${worktreePath}/.lease-lock` (wrong path). The canonical lease-lock path per
WM-013a is `${worktreePath}/.harmonik/lease.lock`. The worktree was also not
git-registered (no `git worktree add`). Both errors prevented the orphan sweep
from finding or sweeping anything.

**Manually verified behavior (via code reading):**

- `workspace.SweepStaleLeaseLocks` discovers worktrees via `DiscoverWorktrees`
  which requires `git worktree list --porcelain` registration.
- For a git-registered worktree with a stale lease-lock (dead PID), the sweep DOES
  remove the lease-lock file. This is the correct MVH behavior.
- The **MVH work loop never calls `workspace.WriteLeaseLockAtomic`**, so worktrees
  created by the work loop have NO lease-lock. Signal-killed worktrees therefore
  appear as `WM-003a bare-worktree-no-lease` orphans. The orphan sweep skips them
  (no lease to sweep). They require reconciliation routing, not simple sweep.

**Bead filed (hk-5dade, P4):** Records this behavior for the lease-lock write gap.

---

### T3-06: Signal before handler launch (stub-based)

**Status: PASS**

When the context is cancelled immediately after `ClaimBead` (before the handler is
launched), the work loop correctly calls `ReopenBead`. The stub confirms the work
loop's intent is correct — the gap is in the `br` CLI semantics mismatch (see T3-02).

Test: `TestT3_SignalBeforeHandlerLaunch`

---

## Bead Summary

| Bead ID  | Severity | Title |
|----------|----------|-------|
| hk-wdeen | P2       | ReopenBead cannot recover in_progress bead after SIGINT/SIGTERM mid-run |
| hk-j4avq | P3       | Worktree left behind after SIGINT/SIGTERM (expected gap hk-fgdgz) |
| hk-b6m3h | P3       | main.go exit code 1 for ErrPidfileLocked (PL-008a requires 5) |
| hk-5dade | P4       | PL-006 sweep skips lease-lock-free worktrees; they need reconciliation |

---

## Key Finding: `brcli.ReopenBead` / `br reopen` semantics mismatch

`brcli.ReopenBead` is used in the work loop for failure rollback. In ALL failure
cases the bead is `in_progress`, not `closed`. But `br reopen` (used by
`brcli.ReopenBead`) only works on `closed -> open`. The correct br command for
`in_progress -> open` is `br update --status open`.

This means ReopenBead is broken for ALL non-zero-exit-code scenarios, not just
the SIGINT/SIGTERM case. The smoke test passes because the happy path never calls
ReopenBead.
