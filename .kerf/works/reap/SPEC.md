# reap — Assembled Change Spec

**Work:** reap — Crash recovery & orphan remediation · **Jig:** plan (v1)
**Source of truth:** `docs/flywheel/2026-05-30-lifecycle-feasibility-and-gaps.md` (the 2026-05-30 credit-burn incident).
**Relates to / lifts:** hk-iuaed (imrest — post-mvh orphan-reset deferral, lifted here into a real implementation).
**Sibling works:** `credfence` (credential scrub + spend ceiling — safety prerequisite, lands first), `pilot` (quiet daemon / Pi dispatch / pause — independent). reap is the failure-mode hardening tier.

This is the single self-contained document an implementing agent reads first. It assembles the six component change-specs (`05-specs/C1..C6`) and the integration contract (`06-integration.md`) faithfully — it adds no requirements and changes no decisions. Per-component detail (full Approach, Files-and-changes tables, Verification commands, Error-handling) lives in `05-specs/`; this assembly carries the normative spec text, the acceptance criteria, and the cross-component integration obligations.

---

## 1. What reap does (and does not)

On 2026-05-30 a flywheel daemon crash-looped through 10 boots in a day (the last five inside ~14 minutes). The 14:56 boot's orphan sweep OBSERVED 951 stale intents, 6 beads stuck `in_progress`, and 24 stale `*-flywheel` tmux sessions — and remediated ZERO. When the dead daemon stopped, 8 `claude` sessions + a `pi` were still alive and spending, and a separate live `harmonik-pi` was doing the same work (dual-orchestrator collision). reap makes the boot sweep ACTUALLY REMEDIATING, adds reap-on-exit + a concurrent-spawn cap, adds restart backoff, and adds a single-flywheel-per-project lock.

**In scope:** remediating worktree removal (C1), durable provenance for the in_progress reset (C2), dead-run dispatched-item reconcile (C3), reap-on-exit + spawn cap (C4), single-flywheel lock (C5), boot-level restart backoff (C6).

**Out of scope (sibling works):** credential scrubbing + the per-day-USD/max-runs spend meter (`credfence`); the quiet/no-auto-pull daemon, Pi-driven curated dispatch, and pause/resume control (`pilot`); dry-run mode; the review-loop retry budget; re-architecting in_progress claim semantics; new abstraction frameworks (reap wires production impls into the existing `internal/lifecycle/` seams).

## 2. Hard invariants (apply to every component)

- **Provenance-gated, never blind.** Every kill/reset/remove is gated on this project's PL-006a marker (env var on Linux / PGID on darwin / tmux `harmonik-<project_hash>-` prefix) for processes & tmux, and on the PL-006e path-gate (`realpath` under `<repo>/.harmonik/worktrees/`) for worktree dirs. A resource bearing a DIFFERENT project's marker is NEVER touched (success-criterion 9, the negative test).
- **Honor PL-006d.** A live coordinator/flywheel session owned by a running supervisor (sentinel + `kill(pid,0)`) is SKIPPED, never reaped. Reaping `*-flywheel` means reaping ORPHANED ones only.
- **Idempotent & crash-safe.** Re-running any boot-path action does not double-act or corrupt state. Durable markers (run-ledger, boots.jsonl) + fd-lifetime advisory locks (kernel-auto-released) survive process crash.
- **Best-effort posture.** Remove/kill errors are logged + summarized, never abort `daemon.Start`.
- **No regression of the live path.** The sweep + reap-on-exit MUST NOT kill a healthy concurrent run or the operator's own Claude Code session; provenance + liveness checks are the guard.
- **darwin-first.** PGID-based provenance fallback + `tmux list-panes` polling work on macOS (no `/proc`).

## 3. Shared integration contract (read before any component)

These four shared resources are the load-bearing cross-component glue (full rationale in `06-integration.md`):

1. **One pre-sweep active-run set (C1 ⟷ C3).** The daemon calls `lifecycle.DiscoverActiveRuns` (EM-031a) ONCE at **PL-005 step 3** — Beads non-terminal query + git task-branch-tip scan, which does NOT require the step-7 in-memory model (not wired at MVH per `orphansweepbeads.go:50-58`). This single set is the survive-check source for BOTH C1's worktree reap (step 3) and C3's dispatched reconcile (step 8a, set threaded into `LoadQueueAtStartup`). Matches the existing pre-sweep `queue.Load` pattern at `daemon.go:705-741`. On `ErrBeadsUnavailable`, degrade to git-branch-tips for both consumers.
2. **One `daemon_orphan_sweep_completed` payload (C1 + C2 + C3).** Additive integer fields only (N-1 tolerated): existing `bead_in_progress_reset` (C2 widens source) + existing `coordinator_sessions_skipped` (PL-006d) + new `worktree_dirs_removed` (C1) + new `dispatched_items_reconciled` split (C3). No field renamed/removed.
3. **PL-005 step labels: 1.5 = flywheel-lock (C5), 1.6 = boot-backoff (C6).** Distinct fractional insertions between step 1 (pidfile lock) and step 3 (sweep); both land together; no integer step renumbered.
4. **Ordered exit-code obligation (C5 + C6).** §8 registry: absorb 25 (`supervisor-already-running`) → add `CommandSupervise` `CommandName` → add 24 (`flywheel-already-running`) → add 26 (`daemon-boot-throttled`). `VerifyCommandExitCodeSets` must resolve 24/25/26 contiguously. Code-17 dual-meaning is a separate non-blocking reconciliation.

Build order: **A** C2 run-ledger + C6 boot-log (durable markers + `daemon_generation`) → **B** pre-sweep `DiscoverActiveRuns` + payload plumbing → **C** C1 worktree reap + C2 reset widening → **D** C3 dispatched reconcile → **E** C5 lock + ordered §8 codes → **F** C6 backoff gate + code 26 → **G** C4 reap-on-exit + spawn cap. Hard constraints: C5 before C4 (§5.3); step 1.5 before 1.6; §8 25→24→26.

---

## 4. Normative change specs (the spec text to land)

### C1 — Remediating worktree-directory reaping → `specs/process-lifecycle.md` §4.5 PL-006e

> **PL-006e — Remediating worktree-directory removal.** The orphan sweep of §PL-006 MUST remove orphaned run-worktree DIRECTORIES, not merely clear their lease-locks. After the lease-lock clear, for each worktree discovered under `<project_root>/.harmonik/worktrees/`, the daemon MUST remove the directory when ALL of:
> - (i) the worktree's lease-lock is stale or absent per [workspace-model.md §4.3 WM-013a] (`LeaseLock == nil`, or the recorded PID does NOT respond to `kill(pid, 0)`); AND
> - (ii) the worktree's `run_id` (directory basename) is NOT in the **pre-sweep active-run set** computed at §PL-005 step 3 by `DiscoverActiveRuns` (EM-031a). Because this removal runs at step 3 — BEFORE the step-7 model rebuild — the step-7 re-attached set does not yet exist; the survive-check source is the step-3 pre-sweep `DiscoverActiveRuns` (Beads non-terminal query + git task-branch-tip scan, supported independently of step 7), invoked exactly as the daemon's existing pre-sweep `queue.Load` precedes `LoadQueueAtStartup`. On Beads-unavailable, degrade to the git-branch-tip set alone; AND
> - (iii) the worktree does NOT carry an unprocessed session sidecar awaiting Cat-3 reconciliation (a worktree classified "bare-worktree-with-unreconciled-sidecar" per [workspace-model.md WM-003a] / `internal/workspace/crashevidence.go` MUST be preserved until §PL-005 step 8 consumes the evidence; no-sidecar or fully-reconciled-sidecar is removable).
>
> Removal MUST use `git -C <project_root> worktree remove --force --force <path>` then `git worktree prune`; on a "not a working tree" error, fall back to `os.RemoveAll(<path>)` + the terminal `prune`. Removal MUST be path-gated: the `realpath`-expanded `<path>` MUST have the prefix `realpath(<project_root>/.harmonik/worktrees/)`; a path resolving outside is NEVER removed regardless of `--worktree-root`. Removal runs at §PL-005 step 3 (before socket bind / dispatch, within the §PL-002 single-daemon invariant); its survive-check (ii) MUST be fed by the step-3 pre-sweep set, not the step-7 set. Idempotent (absent dir = no-op). Errors are non-fatal (best-effort). The removed-dir count is `worktree_dirs_removed` on `daemon_orphan_sweep_completed` (additive, [event-model.md §8.7.14]). Behind a `WorktreeReaper` seam; nil ⇒ step skipped, count 0.
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

Also: event-model §8.7.14 gains `worktree_dirs_removed (PL-006e)`.

**Acceptance:** K stale worktrees (lease-stale, run_id ∉ pre-sweep set) → all gone, `git worktree list` clean, `worktree_dirs_removed==K` (AC1). Live-lease or in-pre-sweep-set worktree preserved (AC2/AC2b). Path outside root never removed (AC3). Unprocessed-sidecar worktree preserved, bare-stale removed (AC4). Field N-1-tolerated (AC5). `git worktree remove` failure logged, daemon still reaches ready (AC6). Second clean boot → count 0 (AC7).

### C2 — Durable run-ledger provenance → `specs/process-lifecycle.md` §4.5 PL-006f

> **PL-006f — Durable run-ledger for stale-`in_progress` provenance.** To establish provenance that survives `queue.json` removal (QM-003) AND BI-031 intent drain, the daemon MUST maintain an append-only JSONL run-ledger at `.harmonik/run-ledger.jsonl` (within the §PL-004 surface), one row `{bead_id, run_id, claimed_at, daemon_generation, project_hash}` per claim. The append MUST occur at the workloop run-registration site, BEFORE the `br` claim write, and MUST be `fsync`'d. A crash between append and claim leaves a harmless row (bead not `in_progress`), pruned per the lifecycle rule.
>
> **Consumption (ownership).** During the §PL-006 stale-`in_progress` sweep, a bead is "owned by this project" if it has a ledger row whose `project_hash` matches the current daemon's, OR it satisfies the pre-existing owner signals (intent file of any op-type per BI-030, queue-owned, or a positive `ProvenanceChecker`). The ledger is an ADDITIONAL owner signal; it MUST NOT override reset-exclusions (a) live-run-reattached, (b) pending close/reopen intent, (c) merged commit. A bead with no ledger row, no intent, not queue-owned, `ProvenanceChecker` nil/false MUST NOT be reset (conservative-skip).
>
> **Lifecycle (stale-authorization guard).** Prune a bead's ledger rows when it reaches a terminal state (a `close` write completing, or a merge commit bearing `Harmonik-Bead-ID:` per PL-006c). Each row's `daemon_generation` (the §PL-005c boots.jsonl counter) records the writing boot. The sweep treats a row only as an OWNERSHIP signal; a re-claimed bead gets a fresh row so a stale row cannot authorize resetting a re-claimed bead. The ledger MUST be bounded (prune-on-terminal; optional window-prune-on-boot).
>
> **`ProvenanceChecker` reservation.** The seam stays nil-in-production, reserved for the future Beads audit-actor `project_hash` checker; the run-ledger is the MVH-grade provenance.
>
> **Reset count** rolls into the existing `bead_in_progress_reset`; no new field, no schema change.
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

Also: PL-004 inventory gains `.harmonik/run-ledger.jsonl`; PL-006 sixth bullet gains "OR a matching row in the durable run-ledger per §PL-006f."

**Acceptance:** Reproduce-first (AC1): bead `in_progress`, no intent, no queue.json, `ProvenanceChecker` nil, ledger row present → RESET, `bead_in_progress_reset==1`; ledger row ABSENT → SKIPPED, `==0` (both branches, one table-driven test). Cross-project ledger row → not reset (AC2). Exclusions a/b/c still gate (AC3). Crash between append and claim → no spurious reset, row pruned (AC4). Post-close prune (AC5). No new payload field (AC6).

### C3 — Dead-run dispatched-item reconciliation → `specs/queue-model.md` QM-002c

> **QM-002c — Dead-run dispatched-item reconciliation on boot.** §QM-002a reverts a `dispatched` item to `pending` ONLY when Beads reports its bead `open` (claim-write-lost). A `dispatched` item whose run died AFTER claiming (bead still `in_progress`, no live re-attached run, no in-flight worktree+lease, no recovering claim-intent) is NOT covered by §QM-002a and would otherwise strand. The daemon MUST reconcile these on boot.
>
> During `LoadQueueAtStartup` (§PL-005 step 8a), after §QM-002a, for each item still `dispatched`, determine whether its run SURVIVED. Survived iff ANY of: (i) its `run_id` is in the **pre-sweep active-run set** computed at §PL-005 step 3 by `DiscoverActiveRuns` (the SAME set §PL-006e (ii) uses, threaded into `LoadQueueAtStartup` so C1 and C3 share one survive-check source); (ii) an in-flight worktree with a live lease-lock exists; (iii) a recovering `claim` intent file exists (BI-031). A SURVIVED item is left `dispatched` and re-driven by normal recovery (no double-dispatch).
>
> For a NON-surviving item, transition by evidence in order: (a) merge commit bearing `Harmonik-Bead-ID:` exists (Cat-3c, `MergeCommitScanner`) → `completed`, reason `dead_run_merged`; (b) worktree with NO `Refs:` commit + dead run (poisoned/failed per `internal/workspace/crashevidence.go` WM-003a, whose evidence §PL-006e (iii) preserves) → `failed`, reason `dead_run_failed`; (c) otherwise (no terminal artifact) → `pending`, reason `dead_run_requeued`. Each transition persists the queue before emitting (persist-before-emit per §QM-063; `queue_item_reconciled` is class-F fsync-backed per [event-model.md §8.10.7]) and emits `queue_item_reconciled` with the reason. Record a `dispatched_items_reconciled` summary split (`requeued`/`failed`/`completed`).
>
> **Ordering.** Runs at step 8a, strictly AFTER the §PL-006 sweep (step 3). The sweep's stale-`in_progress` reset excludes queue-dispatched beads (exclusion (a)), so a still-`dispatched` bead is not reset by the sweep; this reconcile re-queues it the same boot. Idempotent across boots (reverted item is `pending`, terminal item is `completed`/`failed`, neither re-enters the dispatched scan).
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

Also: execution-model EM-031a cross-ref note (the `DiscoverActiveRuns` set is the shared survive-check input for §PL-006e and §QM-002c); event-model §8.10.7 + §6.3 extend `queue_item_reconciled.reason` enum with `dead_run_merged`/`dead_run_failed`/`dead_run_requeued` (class F unchanged).

**Acceptance:** dead-run → pending (AC1); terminal-failure → failed (AC2); merged → completed, not re-queued (AC2b); live-run (in pre-sweep set) → unchanged, no event (AC3); idempotent on second boot (AC4); summary split (AC5); non-interference with C2 exclusion (a) (AC6).

### C4 — Reap-on-exit + concurrent-spawn cap → `specs/process-lifecycle.md` PL-014b + PL-019(i)

> **PL-014b — Concurrent-spawn safety cap.** In addition to the §PL-014a FD-budget ceiling and the operator-set `--max-concurrent` gate (EM-049), the daemon MUST enforce a hard SAFETY cap on concurrently-LIVE spawned child `claude` sessions (a blast-radius ceiling). Finite default, operator-configurable. The cap MUST count ALL spawned `claude` — implementer, reviewer, resume — not only `RunRegistry`-tracked runs; a spawn path bypassing `RunRegistry` (reviewer-worktree) MUST still count (a unified live-child counter at the OS-spawn boundary). The count is a LIVE gauge: incremented at spawn, decremented on watcher reap (§PL-014 / [handler-contract.md §4.3 HC-011] — one watcher per session, its `Wait`/reap is the decrement; HC-024a covers socket-EOF); NOT monotonic. When a spawn would exceed the cap, the daemon MUST REFUSE (not launch) and emit `spawn_cap_exceeded{cap, live_count, run_id?, bead_id?}`. Refusal is distinct from the at-`--max-concurrent` sleep-and-retry: a refused spawn's item is deferred, re-evaluated on a child-exit slot-free; no busy-spin. The cap composes with `--max-concurrent` (below the cap), §PL-014a, and `credfence`'s cumulative spend meter (a different gate reap does NOT own).
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

> **PL-019(i) — Reap-on-exit.** When the supervisor exits via any interceptable path (`Supervisor.Run` returning — ctx-cancel, `Stop`, crash-loop, clean exit — and on `harmonik supervise stop`), it MUST enumerate and kill every `claude` process and tmux session bearing THIS project's PL-006a marker (SIGTERM → 5s bounded wait per HC-018 → SIGKILL) BEFORE returning; an unmarked process/session is NEVER killed. Reap-on-exit MUST honor §PL-006d: a LIVE different supervisor's `harmonik-<project_hash>-flywheel` session (sentinel + live PID) is SKIPPED (incrementing `coordinator_sessions_skipped`). The safety of the project-prefix enumeration on the exit path relies on the single-flywheel invariant (§PL-002 + §PL-019(c)/§PL-002c — at most one daemon + one supervisor per project). The DAEMON's spawned `claude` sessions are reaped by (1) the next-boot sweep (authoritative, covers uninterceptable SIGKILL) AND (2) a best-effort reap on the §PL-011 graceful-drain path (closes the clean-death window). SIGKILL of the daemon relies solely on the next-boot sweep (documented boundary).

Also: event-model §8.7 gains `spawn_cap_exceeded` (class O).

**Acceptance:** marked child killed, other-project child survives (AC1); live different-supervisor session skipped (AC2); `supervise stop` clears the child tree (AC3); daemon drain best-effort reaps, SIGKILL → next-boot sweep (AC4); (cap+1)th spawn refused + `spawn_cap_exceeded`, no busy-spin (AC5); child exit frees slot (AC6); reviewer spawn counts against cap (AC7); cap behavior observably distinct from `--max-concurrent` (AC8).

### C5 — Single-flywheel-per-project lock → `specs/process-lifecycle.md` PL-002c + operator-nfr §8

> **PL-002c — Single-flywheel-per-project lock.** The pidfile lock (§PL-002) prevents a second DAEMON; the supervisor lock (§PL-019(c)) prevents a second SUPERVISOR; they are orthogonal, so "one daemon + one independent supervisor on the same beads" (the 2026-05-30 collision) is permitted by neither. To assert AT MOST ONE flywheel owner, the daemon (flywheel topology) AND `harmonik supervise start` MUST both acquire a single canonical flywheel lock before proceeding. **Surface:** a fd-lifetime advisory lock at `.harmonik/flywheel.lock` via `flock(LOCK_EX|LOCK_NB)`; POSIX `F_SETLK` FORBIDDEN; kernel auto-releases on termination. The marker records `{pid, layer, started_at, project_hash}` (layer ∈ {daemon, supervisor}). **Scope:** acquired ONLY for flywheel-topology boots (daemon under a `--flywheel` flag/config; `supervise start` unconditionally); a plain `harmonik --project` daemon MUST NOT acquire it. **Acquire ordering:** daemon path acquires at §PL-005 **step 1.5** — after the pidfile lock (step 1), before the §PL-005c boot-backoff gate (step 1.6), the sweep (step 3), queue-load, auto-pull, dispatch; supervise path acquires before sentinel/config/tmux writes. `LOCK_NB` ⇒ a second acquirer gets `EWOULDBLOCK` and exits before mutating state — first owner undisturbed. **Refusal:** a held-by-live-owner lock ⇒ refuse with [operator-nfr.md §8] code 24 (`flywheel-already-running`) + a stderr diagnostic naming the live owner (PID, layer, lock path). The pidfile-lock (exit 5) and supervisor-lock (exit 25) refusals are unchanged. **Stale reclaim:** flock OK + recorded PID dead (ESRCH) ⇒ remove marker + proceed; flock OK + PID alive ⇒ ambiguous ⇒ refuse 24; `EWOULDBLOCK` ⇒ in active use, authoritative.
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

> **PL-019(c) amend:** the supervisor singleton lock is narrower than the §PL-002c single-flywheel lock; `harmonik supervise start` MUST acquire BOTH (flywheel lock first, then supervisor lock), refusing with code 24 on flywheel contention and 25 on supervisor contention.

**Ordered §8 obligation (must precede the 24 add):** (1) absorb code 25 `supervisor-already-running` into operator-nfr §8 (value already at `start.go:23`); (2) add a `CommandSupervise` `CommandName` + `CommandExitCodeSet` in `commandcodes.go`; (3) add code 24 `flywheel-already-running` to §8 + `CommandSupervise` + `CommandDaemon` (`--flywheel`). Then `VerifyCommandExitCodeSets` resolves 24/25. PL-004 inventory gains `.harmonik/flywheel.lock`. Code-17 dual-meaning (`start.go:19` local `ExitCodeDaemonDown=17` vs registry code-17) is a separate non-blocking reconciliation.

**Acceptance:** daemon-up → supervise start refused exit 24 naming daemon (AC1); supervise-up → daemon refused exit 24 naming supervisor (AC1b); stale reclaim, no operator action (AC2); first owner byte-identical after refused attempt (AC3); flock auto-released on SIGKILL, no `F_SETLK` (AC4); plain non-flywheel daemon unaffected (AC5); `VerifyCommandExitCodeSets` passes with 24 AND 25 contiguous (AC6).

### C6 — Boot-level restart backoff → `specs/process-lifecycle.md` PL-005c + operator-nfr §8

> **PL-005c — Boot-level restart backoff.** The pidfile lock prevents CONCURRENT daemons but NOT rapid SEQUENTIAL re-boots. The daemon MUST throttle rapid boots with a crash-aware backoff backed by a durable marker. **Boots log:** an append-only `.harmonik/boots.jsonl` (within §PL-004), one row `{boot_id, started_at, daemon_generation, exit_disposition}` per boot; `exit_disposition ∈ {clean, abnormal, unknown}`; `daemon_generation` is the monotonic counter (max prior + 1) referenced by §PL-006f and §PL-002c. Append a row `exit_disposition=unknown` early in `daemon.Start` (survives crash); the §PL-011 drain updates it to `clean` before exit; a crash leaves it `unknown`. **Backoff gate** runs at **step 1.6** — after the pidfile lock (step 1) and the §PL-002c flywheel-lock gate (step 1.5, when flywheel topology), before the sweep (step 3). It computes `k` = rows with `started_at > now - WINDOW` AND `exit_disposition ∈ {abnormal, unknown}` (a `clean` prior row is NOT counted — operator-intended restart never throttled). Rows older than `WINDOW` are pruned each boot (self-healing). Given `k` and a finite envelope (`base=1s`, `cap=60s`, `WINDOW=10m`, `CEILING=5`, `×2` jittered, operator-overridable, mirroring §PL-019 supervisor knobs): if `k < CEILING` → DELAY `min(base × 2^(k-1), cap)` then PROCEED; if `k >= CEILING` → REFUSE with [operator-nfr.md §8] code 26 (`daemon-boot-throttled`) + a retry-after diagnostic. Emit `daemon_boot_throttled{delay_ms, recent_abnormal_boot_count, window_ms, action}` (`action ∈ {delayed, refused}`); log per ON-035. The start-row is written BEFORE the gate so a refused boot still counts toward the next window. Crash-safe (file-based) + window-bounded.
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

> **PL-005 step list amend:** insert step **1.5** (flywheel-lock acquire, §PL-002c, C5) and step **1.6** (boot-backoff gate, §PL-005c) between step 1 and step 3; existing integer steps unchanged. **PL-011 amend:** the graceful-drain MUST update the current boot's row to `exit_disposition=clean` before the event-bus flush.

Also: operator-nfr §8 code 26 (added AFTER C5's 25-absorption + `CommandSupervise`, registered on `CommandDaemon`); event-model §8.7 `daemon_boot_throttled` (class O), payload `{delay_ms, recent_abnormal_boot_count, window_ms, action, throttled_at}`.

**Acceptance:** reproduce-first crash-loop (AC1): injected `k` abnormal rows + injected clock; `k=1..CEILING-1` DELAYS the asserted duration then proceeds; `k>=CEILING` REFUSES exit 26 + retry-after + `daemon_boot_throttled{action=refused}`. Clean prior rows → not throttled (AC2). Stale rows (older than window) → pruned, boot proceeds (AC3). SIGKILL-`unknown` row counted abnormal (AC4). Observable on delay and refusal (AC5). Gate after pidfile lock + flywheel gate, before sweep (AC6). `VerifyCommandExitCodeSets` passes with 24/25/26 contiguous (AC7).

---

## 5. Files & code anchors (summary; full tables in `05-specs/`)

| Area | Files |
|---|---|
| Boot sweep (C1/C2) | `internal/daemon/orphansweep.go`, `internal/daemon/daemon.go` (~705-741 pre-sweep block), `internal/lifecycle/orphansweepbeads.go`, new `internal/lifecycle/worktreereap.go` + `runledger.go`, `internal/workspace/{orphansweep.go,crashevidence.go}` (WM-003a), `internal/daemon/runregistry.go:94` (claim-time ledger append) |
| Queue reconcile (C3) | `internal/lifecycle/startup_pl005_qm002.go` (after `reconcileDispatchedItems` ~169), `internal/lifecycle/activerun_em031a.go:314` (`DiscoverActiveRuns`), `internal/queue/...` (summary count) |
| Reap + cap (C4) | `internal/supervise/supervisor.go` (`Run` defer), `cmd/harmonik/supervise/stop.go`, `internal/daemon/workloop.go` (~595 spawn site), new `spawncounter.go`, `internal/workspace/createworktree.go` (reviewer spawn), `internal/lifecycle/tmux/orphansession.go` |
| Lock (C5) | new `internal/lifecycle/flywheellock.go` (modeled on `pidfilelock.go`), `cmd/harmonik/supervise/start.go`, `internal/operatornfr/{exitcode.go,commandcodes.go}` |
| Backoff (C6) | new `internal/lifecycle/{bootlog.go,bootbackoff.go}`, `internal/daemon/daemon.go`, `internal/operatornfr/{exitcode.go,commandcodes.go}` |
| Specs | `specs/process-lifecycle.md` (PL-002c, PL-005c, PL-006e, PL-006f, PL-014b, PL-019(i), PL-004/PL-006/PL-011/PL-005-step-list amends), `specs/queue-model.md` (QM-002c), `specs/execution-model.md` (EM-031a cross-ref), `specs/event-model.md` (§8.7.14, §8.7 new events, §8.10.7 reason enum), `specs/operator-nfr.md` (§8 codes 24/25/26 + `CommandSupervise`) |

## 6. Testing (full strategy in `06-integration.md` §7)

- **Scenario (hk-a31od):** one `harmonik daemon` boot remediates C1+C2+C3 and throttles under C6; asserts `daemon_orphan_sweep_completed{worktree_dirs_removed, bead_in_progress_reset}` + `queue_item_reconciled{reason=dead_run_*}` + `daemon_boot_throttled{action}`.
- **Exploratory (hk-izs8s):** `supervise stop` reaps child tree (C4); second `supervise start` refused exit 24 + diagnostic (C5); crash-looping `daemon` refused exit 26 + retry-after (C6).
- **Reproduce-first (C2-AC1, C6-AC1):** with-fix and without-fix branch in one table-driven test, locking the fix.
- **Negative/cross-project:** a different-project marker / out-of-path worktree is never reset/killed/removed (success-criterion 9).
- **Registry conformance:** `VerifyCommandExitCodeSets()` after the ordered §8 add (24/25/26 contiguous).

## 7. Traceability

Every success criterion (1–9) traces to a component + change-spec section (`06-integration.md` §8); every change-spec section traces to a requirement + goal (`03-components.md` coverage table). The six change-spec critical-review must-fixes are resolved and encoded as the §3 shared integration contract. No contradiction remains between component specs.
