# reap — Analysis (current-state map of the territory)

Factual map of the code/spec as it exists at `2e49a8df` (main). **Key correction to the assessment doc's framing:** much of what the assessment called "missing" (lost issue #5) has in fact landed since the incident's code snapshot — the stale-`in_progress` reset, the `*-flywheel` tmux session kill, and worktree lease-lock clearing are all implemented and wired into the production daemon-start path. The genuine remaining reap gaps are narrower and are isolated below per affected area.

## A. Orphan sweep — `internal/daemon/orphansweep.go` + `internal/lifecycle/orphansweep*.go`

**Status: largely built and WIRED in production.** `RunOrphanSweep` (`internal/daemon/orphansweep.go:198`) is called from `daemon.Start` at `internal/daemon/daemon.go:743`, BEFORE socket bind, per PL-005 step 3 / PL-006. It performs, in order:

- (a) **Tmux session kill** — `lifecycle.SweepOrphanTmuxSessions` + `ltmux.SweepOrphanTmuxSessions` kill sessions whose name has the exact `harmonik-<12-char-hash>-` prefix (`internal/lifecycle/tmux/orphansession.go:12-21`). This **already covers `harmonik-<project_hash>-flywheel` sessions** — they match the prefix. Provenance is the prefix; a different project's `harmonik-<otherhash>-` session is NOT touched (test `orphansession_test.go:225`).
- (a') **Tmux window kill** — `ltmux.SweepOrphanTmuxWindows` for the PL-021c $TMUX-reuse case.
- (b) **Worktree lease-lock clear** — `workspace.SweepStaleLeaseLocks` removes stale `<worktree>/.harmonik/lease.lock` files. **NOTE: this clears the LOCK, it does not REMOVE the worktree directory.** `result.LocksCleared` counts removed locks.
- (c-i) **Handler subprocess kill** — `lifecycle.SweepOrphanHandlers` (SIGTERM→SIGKILL, init-reparented + provenance-marked).
- (c-ii) **`br` subprocess kill** — `lifecycle.SweepOrphanBr` (BI-029 surface; `BrSubprocessesKilled` undercounts — flagged for a follow-up to return (killed,survived)).
- (d) **Stale intent enumeration** — `EnumerateStaleIntents` (count only; left for Cat 3a per PL-006).
- (e) **Reconciliation-lock clear** — `SweepStaleReconciliationLocks`.
- (f) **Stale `in_progress` bead reset** — `lifecycle.SweepStaleInProgressBeads`, gated on `cfg.BeadLedger != nil && cfg.BeadResetter != nil`. **This is wired in production** (daemon.go:684-703: when `cfg.BrPath != ""`, `beadResetter = brAdapter`, `beadCat3cCloser = brAdapter`). Landed in `9779f725` (hk-iuaed.4) + broadened in `bf2db814` (hk-sc3o4) + SIGKILL-queue-provenance in `80d2ef64` (hk-2ty0g) + Cat 3c close in `5cb0532a` (hk-lgtq2). `BeadInProgressReset` and `BeadCat3cClosed` counts roll into the event.
- (g) **`.claude/worktrees/` sweep** — `SweepClaudeWorktrees` is **DRY-RUN BY DEFAULT** (`internal/daemon/claudeworktreesweep.go:23-24`); removal requires `HARMONIK_SWEEP_CLAUDE_WORKTREES=1`. This is the sub-agent worktree path, parallel to `.harmonik/worktrees/`.
- (h) **Queue-archive accumulation sweep** — `SweepQueueArchives` (hk-pycay).
- Emits `daemon_orphan_sweep_completed` (event-model §8.7.14) with all counts.

**Provenance seams** (all in `internal/lifecycle/orphansweepbeads.go:154-301`): `BeadResetter`, `BeadCat3cCloser`, `ProvenanceChecker` (left **nil** in production — MVH fallback is intent-log + queue-owned provenance), `MergeCommitScanner` (production `GitMergeCommitScanner`), `InFlightBeadLedger`, plus exclusion sets `QueueDispatchedSet`/`QueueOwnedSet`/`IntentClaimSet`/`IntentMutationSet`/`IntentProvenanceSet`. The bead-reset write is idempotency-keyed `<project_hash>:<bead_id>:reset:<daemon_start_ns>` and intent-logged per BI-030.

**Reset exclusions** applied in order: (a) live run reattached / claim-intent present / queue-dispatched; (b) close/reopen intent present (Cat 3a); (c) merged commit present (Cat 3c → close, not reset, when `Cat3cCloser` non-nil). If none apply → `ResetBead` (in_progress→open).

**Why the incident reset 0:** the 6 stuck beads likely fell outside the provenance signal — the daemon auto-pulled `br ready` into a **wave** queue (not a stream queue), and a completed/removed `queue.json` plus drained claim-intent files leaves no provenance handle, so the sweep correctly (conservatively) skipped them. `ProvenanceChecker` being nil means there is no project_hash-audit-actor fallback. This is the **reachability gap** reap must close: a durable per-project provenance signal that survives queue.json removal AND intent-log drain (e.g., a project-scoped run-ledger or the audit-actor checker).

**Tests:** extensive — `orphansweep_pl006_test.go`, `orphansweepbeads_test.go`, `bi010d_sweep_idempotency_test.go`, `claudeworktreesweep_hkyhq3m_test.go`, `orphansweepbr_bi014a_test.go`, `provenance_pl006a_test.go`. Pattern: fake adapters injected via the seam interfaces; table-driven exclusion tests.

### Gaps in area A (genuine reap work)
1. **Worktree DIRECTORY removal** — the sweep clears stale lease-locks but never `git worktree remove` / `RemoveAll`s the orphaned `.harmonik/worktrees/<run_id>/` directory. The incident left **11 orphaned run-worktrees**; lock-clearing alone does not reclaim them. (Sub-agent `.claude/worktrees/` removal exists but is dry-run-gated and is a different path.)
2. **Provenance reachability for the reset** — the nil `ProvenanceChecker` + queue/intent-drain means real stuck beads escape the reset (the incident's "observed 6, reset 0"). Needs a durable provenance signal.
3. **Dispatched-queue-item reconciliation** — `QueueDispatchedSet` is consumed only as a reset-EXCLUSION (don't reset a bead the queue thinks is live). There is NO path that RE-QUEUES a `dispatched` item whose run did not survive back to `pending`, nor terminally fails it. A `dispatched` item with a dead run stays `dispatched` forever (phantom in-flight). `LoadQueueAtStartup` (PL-005 step 8a) needs to reconcile these.

## B. Supervisor — `internal/supervise/supervisor.go` + `cmd/harmonik/supervise/*`

**Status: restart backoff + crash-loop detection BUILT, but scoped to in-process child restart.** `Supervisor.Run` (`supervisor.go:185`) spawns one child, applies `PolicyOnFailure`, exponential backoff with jitter (`backoffWithJitter`, base 1s→cap 60s, ×2), restart cap (`MaxRestarts=5`), and dual crash-loop detection (absolute cap + sliding-window). PL-019(f). The child is placed in its own PGID (`Setpgid: true`, `buildCmd`:342). `terminateChild` does SIGTERM→bounded-wait→SIGKILL (PL-011).

**`supervise start`** (`cmd/harmonik/supervise/start.go`): probes daemon socket (exit 17 if down — so supervise STRICTLY depends on a live daemon), acquires `supervisor.lock` via `flock(LOCK_EX|LOCK_NB)` (exit 25 if held), writes the PL-006d sentinel, writes `config.json`, creates the `harmonik-<project_hash>-flywheel` tmux session with `remain-on-exit on`, launches the `_shim`. **`supervisor.lock` IS the single-supervisor-per-project lock** for the Pi/flywheel layer (the shim re-acquires it blocking). It is an fd-lifetime advisory lock, kernel-auto-released on death (PL-002a discipline).

**`supervise stop`** (`stop.go`): reads `supervisor.pid`, SIGTERM→wait→SIGKILL. **Only kills the supervisor PID, NOT its descendant tmux/claude tree** — relies on PGID for forwarding, but the spawned `claude` sessions live in tmux panes whose processes are not in the supervisor's PGID.

**Env passthrough:** `buildCmd` uses `cmd.Env = append(os.Environ(), s.spec.Env...)` (supervisor.go:336) and the shim uses `syscall.Exec(resolved, command, os.Environ())` (`shim.go:103`) — the unscrubbed-key path. **This is `credfence`'s scope, NOT reap's** (noted to avoid double-ownership).

**Tests:** `supervisor_test.go` (backoff, crash-loop, stop-timeout, heartbeat).

### Gaps in area B (genuine reap work)
4. **Reap-on-exit of the child claude+tmux tree** — when the supervisor/daemon dies, the spawned `claude` sessions in tmux panes survive (the incident: 8 live `claude --session-id` + a `pi`). The supervisor's PGID covers its direct child, but tmux-substrate-hosted `claude` panes are NOT in that group. Need an exit hook (supervisor `Run` return + `stop.go`) that enumerates+kills this project's provenance-marked `claude`/tmux children. The orphan-sweep on the NEXT boot catches them, but reap-on-exit closes the live-spend window between death and next boot.
5. **Concurrent-spawn cap** — the supervisor manages exactly ONE child; the spawn-fan-out that needs capping is the DAEMON spawning N implementer/reviewer `claude` sessions (the `--max-concurrent` capacity gate at EM-049 / `workloop.go:596`). A hard upper bound (independent of `--max-concurrent`, a safety ceiling) that REFUSES the (C+1)th spawn with a typed signal does not exist. The `--max-concurrent` gate throttles but is operator-set and unbounded-by-default in the auto-pull path.
6. **Single-flywheel-per-project across daemon+Pi** — `supervisor.lock` prevents a second `supervise start`, and the daemon `pidfile` lock (PL-002, `pidfilelock_*.go`, fd-lifetime flock) prevents a second daemon. BUT the incident's dual-orchestrator collision was a second *daemon* (auto-pulling) tripping a separate *Pi* — the two locks are independent. There is no single lock asserting "this project has exactly one flywheel (daemon ⊕ supervised-Pi) owner." The pidfile lock + supervisor lock together nearly cover it, but the collision arose because killing the daemon and the live Pi were not mutually exclusive — they are different processes holding different locks. reap must define the relationship (likely: the supervisor lock is the canonical flywheel lock; a daemon boot for the flywheel topology checks it, or a unified flywheel-lock).

## C. Daemon restart backoff (boot-level) — NEW, no current implementation

The supervisor's backoff (area B) throttles **child restarts within one supervisor process**. It does NOT throttle **fresh daemon boots** — the incident was 10 separate `harmonik --project` invocations across a day, each a new OS process with no memory of the prior boot. There is **no durable last-boot marker** and **no boot-time backoff gate**. `daemon.Start` (`internal/daemon/daemon.go`) emits `daemon_started` and proceeds immediately. The pidfile lock (PL-002) prevents *concurrent* daemons but not *rapid sequential* re-boots after each crash.

### Gap in area C (genuine reap work)
7. **Boot-level restart backoff** — a durable `.harmonik/daemon.last-boot` (or similar) timestamp; on `daemon.Start`, if the prior boot was within the backoff window AND exited abnormally, delay (or refuse) the boot. Must be crash-safe (file-based, not in-memory) and must distinguish operator-intended restart from crash-loop.

## D. Constraints to preserve

- **PL-006a provenance is load-bearing and non-negotiable** — every kill/reset/remove gates on the project_hash marker (env var Linux / PGID darwin / tmux prefix). reap MUST NOT introduce a path that touches a resource lacking a valid marker. (`provenance.go`, `provenance_pl006a_test.go`.)
- **PL-006d coordinator exclusion** — the remediating tmux reaping MUST skip a LIVE flywheel session (sentinel + `kill(pid,0)`). Reaping `*-flywheel` means orphaned ones only. The sentinel/skip path exists (`orphansweepactiveheld_pl007_test.go`); reap's worktree-removal + reap-on-exit must honor the same live-supervisor check.
- **Sweep error non-fatal** — `RunOrphanSweep` errors do NOT abort `daemon.Start` (daemon.go:763-769). reap's additions (worktree removal, dispatched reconcile) must keep this best-effort posture.
- **Idempotency + intent-log** — bead writes via BeadResetter carry the `:reset:<daemon_start_ns>` key + BI-030 intent log. New write paths must follow the same discipline.
- **fd-lifetime advisory locks only** — pidfile + supervisor locks use `flock`, kernel-auto-released on crash (PL-002a). POSIX `F_SETLK` is FORBIDDEN. The single-flywheel lock and the restart-backoff marker must respect this (the backoff marker is a timestamp file, not a lock; the flywheel lock is an flock).
- **darwin-first** — no `/proc`; PGID provenance fallback (OQ-PL-008), `tmux list-panes` polling at 100ms.
- **Additive event payloads** — new counts on `daemon_orphan_sweep_completed` are additive-tolerated per event-model §6.3 N-1 (precedent: `tmux_windows_killed`, `coordinator_sessions_skipped`, `bead_in_progress_reset`).

## E. Conventions to follow

- **Seam-injection for testability** — every external interaction (br, git, tmux, fs) is behind an interface with a production impl + a test fake (`BeadResetter`, `MergeCommitScanner`, `TmuxLister`/`TmuxKiller`, `HandlerLister`). reap's new behaviors (worktree removal, dispatched reconcile, reap-on-exit, spawn cap, flywheel lock, boot backoff) follow this pattern.
- **Spec-ref + bead-ref comments** — code carries `Spec ref:` and `Bug ref:`/`Bead ref:` comments tying lines to PL/EM/QM clauses and bead IDs.
- **Counts on the completion event** — observability is a count field on `daemon_orphan_sweep_completed`, not a side log.
- **Exit codes from operator-nfr §8** — 17 (daemon down), 25 (supervisor running), 5 (second daemon); reap's new refusals (spawn-cap-exceeded, single-flywheel-refused) need allocated codes from ON §8.
- **Naming** — `Sweep*` for sweep steps, `Orphan*` for orphan types, PL-clause-suffixed test files (`*_pl006_test.go`).

## F. Recent git history (affected areas)

- `9779f725` hk-iuaed.4 — stale `in_progress` orphan-reset (lifted the imrest deferral; the assessment doc's "post-mvh-deferred" framing is now stale).
- `bf2db814` hk-sc3o4 — broadened orphan-sweep provenance to any intent op.
- `80d2ef64` hk-2ty0g — orphan sweep resets `in_progress` using queue.json after SIGKILL (the QueueOwned/QueueDispatched seams).
- `5cb0532a` / `cf0d0d04` hk-lgtq2 / hk-im9tw — Cat 3c auto-reconciler (close subsumed beads).
- `943ffb03` hk-yhq3m — orphan sweep walks `.claude/worktrees/` (dry-run-gated removal).
- `9d9a372a` hk-pycay — queue-archive sweep on startup.
- `fc3c7a96` / `54b3525f` hk-qx702 — `supervise` CLI surface (start/stop/status/lock/sentinel).
- `fc015bdc` — internal/supervise package + `--watch-restart` shim (backoff/crash-loop).

**Implication for decomposition:** reap is NOT greenfield. ~70% of the orphan-sweep machinery exists and is wired. The work is (1) close three sweep gaps (worktree-dir removal, provenance reachability, dispatched reconcile), (2) add reap-on-exit + spawn cap on the supervise/daemon side, (3) add boot-level restart backoff, (4) define+wire the single-flywheel-per-project lock relationship. Spec work is mostly small amendments to existing PL clauses plus a few new normative requirements (boot backoff, spawn cap, flywheel-lock).
