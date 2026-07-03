# C1 — Remediating worktree-directory reaping — Change Spec

**Component:** Extend the boot orphan sweep to REMOVE orphaned `.harmonik/worktrees/<run_id>/` directories, not just clear their lease-locks.
**Bead:** hk-9eury · **Goal:** G1 · **Analysis gap:** #1
**Spec home:** `specs/process-lifecycle.md` §4.5 PL-006 (new sub-clause **PL-006e**); `specs/event-model.md` §8.7.14 (additive field).

---

## Requirements (carried forward from 03-components.md)

- **C1-R1** — On boot, for each `.harmonik/worktrees/<run_id>/` directory whose lease-lock is stale (WM-013a) AND whose `run_id` is not re-attached to a live in-flight run rebuilt at PL-005 step 7, the sweep MUST `git worktree remove --force` (or prune + `RemoveAll`) the directory and prune the registered git worktree entry.
- **C1-R2** — A worktree whose `run_id` IS re-attached to a live run, OR whose lease-lock is held by a live process, MUST NOT be removed.
- **C1-R3** — A worktree directory outside this project's `<repo>/.harmonik/worktrees/` root MUST NOT be touched.
- **C1-R4** — The removed-worktree count MUST appear as a new additive integer field (`worktree_dirs_removed`) on `daemon_orphan_sweep_completed`.
- **C1-R5** — Removal errors MUST be non-fatal: logged + summarized, never aborting `daemon.Start`.
- **C1-R6** — Re-running the sweep with no orphaned worktrees remaining MUST be a no-op (count 0).

## Research summary (from 04-research/C1)

The boot sweep clears stale lease-LOCKS (`workspace.SweepStaleLeaseLocks`, `internal/workspace/orphansweep.go:60-98`) and runs `git -C <repoRoot> worktree prune`, but `prune` only drops admin entries for directories already gone — it never deletes a still-present worktree dir. The incident's 11 orphaned run-worktrees survived for exactly this reason. The canonical removal idiom already in-repo is the reviewer-cleanup closure (`internal/workspace/createworktree.go:32-37`): `git worktree remove --force --force <path>` then `git worktree prune`, idempotent and safe to call more than once; the double `--force` is required to remove a worktree with a dirty/locked working tree. Liveness is established by TWO existing signals that BOTH must say dead before removal: lease-lock PID liveness (`IsLeaseLockStale`/`isPIDDead`) AND the EM-031a re-attached run set (`internal/lifecycle/activerun_em031a.go`). Provenance (C1-R3) is structural: `DiscoverWorktrees` only enumerates under this repo's `<repoRoot>/.harmonik/worktrees/` root, so the PL-006a env/PGID marker is not required — but removal MUST be path-gated to the prefix `<repoRoot>/.harmonik/worktrees/`.

## Approach

Add a new sweep step **after** the lease-lock clear (PL-006 worktree bullet) and a new normative sub-clause **PL-006e** governing it. The step:

1. Enumerates worktrees via the existing `DiscoverWorktrees(projectDir, NoWorktreeRootOverride())` — already rooted at `<repoRoot>/.harmonik/worktrees/`.
2. For each discovered worktree, computes a removal-eligibility verdict from two liveness signals:
   - **lease-lock dead/absent** — `LeaseLock == nil` (not leased, WM-013a) OR `IsLeaseLockStale(pid)` true; AND
   - **run not re-attached** — `run_id` (the worktree directory basename) NOT in the **pre-sweep active-run set** (see the survive-check source note below). A worktree is removal-eligible iff BOTH are dead/absent. If EITHER says live, SKIP (C1-R2).
3. **Sidecar-evidence preservation:** a removal-eligible worktree that carries an UNPROCESSED session sidecar (per the `internal/workspace/crashevidence.go` Cat-3 / WM-003a crash-evidence classification — a bare-worktree-with-unreconciled-sidecar state, identified by the WM-022 sidecar walk in `internal/workspace/workspace.go`) MUST be PRESERVED, not removed, until Cat-3 routing (PL-005 step 8 reconciliation) consumes the evidence. A worktree with no sidecar OR a fully-reconciled sidecar is bare-stale and removable.

**Survive-check source (resolving the step-3-before-step-7 ordering).** PL-006e's worktree-dir removal runs at PL-005 step 3, BEFORE the full in-memory model is rebuilt at PL-005 step 7. The EM-031a re-attached-run set produced by step 7 therefore does NOT exist at sweep time — `internal/lifecycle/orphansweepbeads.go` lines 50-58 already document that "at MVH the in-memory model rebuild (PL-005 step 7) is not yet wired as a distinct phase." To give the step-3 sweep a live-run survive-check WITHOUT the step-7 model, the daemon MUST run `lifecycle.DiscoverActiveRuns` (EM-031a; `internal/lifecycle/activerun_em031a.go:314`) as a **pre-sweep input** at step 3. `DiscoverActiveRuns` builds the active-run set from the Beads non-terminal query + the git task-branch-tip scan ALONE — it does not require the step-7 model build — exactly the lightweight pre-sweep discovery pattern the daemon already uses for queue-provenance (`internal/daemon/daemon.go:705-741` does a raw `queue.Load` BEFORE `LoadQueueAtStartup` to feed the bead-reset sweep). The pre-sweep `DiscoverActiveRuns` result is the survive-check source for PL-006e (ii) AND for C3/QM-002c. When Beads is unavailable (`ErrBeadsUnavailable`), the pre-sweep falls back to the git-branch-tip set alone and the worktree reap proceeds conservatively (a worktree with a live task-branch tip is treated as re-attached and skipped).
4. Removes via the A-then-B fallback: try `git -C <repoRoot> worktree remove --force --force <path>`; on a "not a working tree"/non-registered error, `os.RemoveAll(<path>)`; always finish with a single `git -C <repoRoot> worktree prune`.
5. Path-gate (C1-R3): removal MUST assert `<path>` has the prefix `realpath(<repoRoot>/.harmonik/worktrees/)` and reject any path that resolves (after symlink expansion) outside it. A `--worktree-root` override that points outside the project root MUST NOT be followed for removal.
6. Increments `WorktreeDirsRemoved` on the sweep result; rolls into `daemon_orphan_sweep_completed.worktree_dirs_removed`.

The step is behind a `WorktreeReaper` seam (production impl wraps the `git worktree remove` + `RemoveAll` calls; tests inject a fake recording removals) consistent with the existing sweep seam-injection convention.

### Spec text to add — PL-006e (process-lifecycle.md §4.5, after PL-006d)

> **PL-006e — Remediating worktree-directory removal**
>
> The orphan sweep of §PL-006 MUST remove orphaned run-worktree DIRECTORIES, not merely clear their lease-locks (the lease-lock clear of the §PL-006 worktree bullet reclaims the lock file but leaves the working-tree directory and its git admin entry; the incident of 2026-05-30 left 11 such directories). After the lease-lock clear, for each worktree discovered under `<project_root>/.harmonik/worktrees/`, the daemon MUST remove the directory when ALL of the following hold:
>
> - (i) the worktree's lease-lock is stale or absent per [workspace-model.md §4.3 WM-013a] (`LeaseLock == nil`, or the recorded PID does NOT respond to `kill(pid, 0)`); AND
> - (ii) the worktree's `run_id` (the directory basename) is NOT in the **pre-sweep active-run set** computed at step 3 by `DiscoverActiveRuns` (EM-031a). Because this worktree-dir removal runs at §PL-005 step 3 — BEFORE the in-memory model is rebuilt at step 7 — the step-7 re-attached-run set does NOT yet exist; the survive-check source is the step-3 pre-sweep `DiscoverActiveRuns` (the Beads non-terminal query + git task-branch-tip scan, which EM-031a supports independently of the step-7 model build), invoked exactly as the daemon's existing pre-sweep `queue.Load` provenance read precedes `LoadQueueAtStartup`. When Beads is unavailable, the pre-sweep degrades to the git-branch-tip set alone (a worktree with a live task-branch tip is treated as re-attached and skipped); AND
> - (iii) the worktree does NOT carry an unprocessed session sidecar awaiting Cat-3 reconciliation (a worktree whose crash-evidence classification per [workspace-model.md §4.x WM-003a] / `internal/workspace/crashevidence.go` is "bare-worktree-with-unreconciled-sidecar" MUST be preserved until §PL-005 step 8 reconciliation consumes the evidence; a worktree with no sidecar or a fully-reconciled sidecar is removable).
>
> Removal MUST be performed via `git -C <project_root> worktree remove --force --force <path>` followed by `git -C <project_root> worktree prune`; on a "not a working tree" / non-registered-worktree error from `git worktree remove`, the daemon MUST fall back to `os.RemoveAll(<path>)` and still issue the terminal `git worktree prune`. The double `--force` is REQUIRED to remove a worktree with a dirty or locked working tree.
>
> Removal MUST be path-gated: the resolved (symlink-expanded) `<path>` MUST have the prefix `realpath(<project_root>/.harmonik/worktrees/)`; a path resolving outside this root MUST NOT be removed, regardless of any `--worktree-root` override. This is the structural provenance boundary for worktree directories (filesystem location + git-worktree registration under this repo); the PL-006a env/PGID/tmux-prefix marker gates process and tmux kills, NOT filesystem-resident worktrees.
>
> Removal runs at §PL-005 step 3 (BEFORE socket bind and before any dispatch can re-create the directory mid-sweep, within the single-daemon pidfile-lock invariant of §PL-002). Because step 3 precedes the step-7 model rebuild, the step's survive-check (clause (ii)) MUST be fed by the step-3 pre-sweep `DiscoverActiveRuns` set, NOT by the step-7 re-attached-run set (which does not yet exist). Removal of an already-absent directory is a no-op (idempotent); a second boot with no orphaned worktrees yields a count of 0. Removal errors (locked file, git failure) are non-fatal: they are appended to the sweep's error accumulator and logged; they MUST NOT abort `daemon.Start` (the §PL-006 best-effort posture). The count of directories removed is recorded as `worktree_dirs_removed` on `daemon_orphan_sweep_completed` (additive-tolerated per [event-model.md §6.3] N-1; see §8.7.14).
>
> The removal path is abstracted behind a `WorktreeReaper` interface (`lifecycle.WorktreeReaper` or `workspace`-package equivalent); production satisfies it with the `git worktree remove`/`RemoveAll` implementation, tests inject a fake. When the reaper is nil, the removal step is SKIPPED and `worktree_dirs_removed` is 0 (safe-but-incomplete, mirroring PL-006b's nil-BeadResetter posture).
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### Spec text to amend — event-model.md §8.7.14

Add `worktree_dirs_removed` to the additive field list of the §8.7.14 `daemon_orphan_sweep_completed` row, annotated `(PL-006e)`, consistent with the `bead_in_progress_reset` (PL-006) and `coordinator_sessions_skipped` (PL-006d) precedents. No schema bump; consumers tolerate unknown integer fields per EV-029.

## Files & changes

| File | Change | Why |
|---|---|---|
| `specs/process-lifecycle.md` | Add PL-006e (above) after PL-006d (~line 327); changelog row. | Normative contract for worktree-dir removal. |
| `specs/event-model.md` | Add `worktree_dirs_removed (PL-006e)` to §8.7.14 additive field list; changelog row. | Payload field registration. |
| `internal/daemon/orphansweep.go` | After the `SweepStaleLeaseLocks` call (~line 235-239), invoke the new worktree-removal step; add `WorktreeDirsRemoved` to `OrphanSweepResult` and map it in `ToPayload()` (~line 24-92). | Wire the step + count. |
| `internal/workspace/orphansweep.go` (or new `internal/lifecycle/worktreereap.go`) | New `WorktreeReaper` seam + production impl (`git worktree remove --force --force` → `RemoveAll` fallback → `prune`), path-gate, lease-lock + pre-sweep `DiscoverActiveRuns` liveness check, sidecar-preservation check via `internal/workspace/crashevidence.go` (WM-003a). | Removal mechanics behind a seam. |
| `core/DaemonOrphanSweepCompletedPayload` | Add `WorktreeDirsRemoved int` field. | Additive payload field. |
| `internal/daemon/daemon.go` (~line 705-741, the existing pre-sweep block) | Add a pre-sweep `lifecycle.DiscoverActiveRuns` call alongside the existing raw `queue.Load`, BEFORE `RunOrphanSweep` (step 3); thread the resulting `ActiveRunSet` + a non-nil `WorktreeReaper` into `RunOrphanSweep` at the call site (~line 743). The same pre-sweep `ActiveRunSet` is threaded into `LoadQueueAtStartup` for C3/QM-002c. | Production wiring (so the survive-check has a source at step 3 and the step is not nil-skipped). |

## Acceptance criteria

- **AC1 (C1-R1):** Given K worktree dirs under `.harmonik/worktrees/<run_id>/` with stale/absent lease-locks and run_ids NOT in the pre-sweep `DiscoverActiveRuns` set, after boot all K dirs are gone (`os.Stat` → `ENOENT`), `git worktree list` shows none of them, and `daemon_orphan_sweep_completed.worktree_dirs_removed == K`.
- **AC2 (C1-R2 live-lease):** A worktree whose lease-lock PID is alive is NOT removed; it persists across the sweep.
- **AC2b (C1-R2 re-attached):** A worktree whose run_id IS in the pre-sweep `DiscoverActiveRuns` set (a live Beads non-terminal bead OR a live git task-branch tip) is NOT removed.
- **AC3 (C1-R3):** A worktree path resolving outside `<repoRoot>/.harmonik/worktrees/` (e.g., a symlinked or `--worktree-root`-overridden path) is never removed; the path-gate rejects it.
- **AC4 (sidecar preservation):** A removal-eligible worktree carrying an unprocessed session sidecar is preserved until Cat-3 reconciliation; a bare-stale worktree (no sidecar) is removed.
- **AC5 (C1-R4):** `worktree_dirs_removed` appears on the emitted event with the correct count; N-1 consumer reading the event without the field does not error.
- **AC6 (C1-R5):** A `git worktree remove` failure on one dir is logged + summarized in the sweep error field and does NOT abort `daemon.Start`; the daemon reaches `ready`.
- **AC7 (C1-R6):** A second boot with no orphaned worktrees yields `worktree_dirs_removed == 0` and no error.

## Verification

```bash
go test ./internal/daemon/...   -run 'OrphanSweep|Worktree'   -count=1
go test ./internal/workspace/... -run 'Sweep|Worktree|LeaseLock' -count=1
go test ./internal/lifecycle/...  -run 'Worktree|ActiveRun|EM031' -count=1
```

Manual: create a stale `.harmonik/worktrees/<run_id>/` (committed dirty file + stale lease.lock PID), boot the daemon, confirm the directory is gone and `git worktree list` is clean; tail `.harmonik/events/events.jsonl | grep daemon_orphan_sweep_completed` for `worktree_dirs_removed`.

## Error handling & edge cases

- **Locked worktree** — `git worktree lock`-ed dir resists single `--force`; the double `--force --force` handles it (reviewer-cleanup precedent).
- **prune-already-ran** — git admin entry already gone but dir survives → `git worktree remove` errors "not a working tree" → `os.RemoveAll` fallback reclaims it.
- **Concurrent boot race** — impossible within the single-daemon pidfile-lock invariant + step-3 ordering (removal precedes socket bind, before any dispatch).
- **Symlink escape** — path-gate uses `realpath` expansion before the prefix check; a symlink pointing outside the root is rejected.
- **Partial removal** — if `os.RemoveAll` partially fails (e.g., a sub-file held open), the error is logged + summarized; the next boot retries (idempotent).

## Migration / backwards compatibility

Additive only. The new sweep step + payload field do not change existing behavior; a daemon built without the `WorktreeReaper` (nil) behaves exactly as today (lock-clear only, count 0). N-1 event consumers tolerate the new field per EV-029.

## Test beads

- **Scenario:** hk-9eury covers the orphan-sweep end-to-end; the C1 scenario-test bead (see test-beads section in C2 spec / shared) asserts the worktree-removal terminal condition. CLI under test: `harmonik daemon` (boot path). Lifecycle state: orphaned run-worktree present at boot. Observable terminal condition: directory absent + `daemon_orphan_sweep_completed.worktree_dirs_removed == K` in `.harmonik/events/events.jsonl`.
- See the shared test-bead block in `C6-boot-backoff-spec.md` §Test beads for the filed bead IDs.
