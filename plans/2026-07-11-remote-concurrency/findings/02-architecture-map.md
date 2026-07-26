# Remote-Worker Execution Path — Architecture Map & Serialization Audit

**Date:** 2026-07-12
**Scope:** How a bead run executes on the remote worker (gb-mbp, macOS, reached over Tailscale SSH), and every serialization / blocking / single-threaded point on that path.
**Method:** Read-only source trace of `internal/workers`, `internal/workspace`, `internal/daemon`, `internal/lifecycle/tmux`, `internal/queue/cli`, `cmd/harmonik`.

## Verdict (up front)

**The operator's hypothesis is correct: this is a design limit, not a resource wall.** The remote path is a stack of *global* serialization points layered on top of a *fresh-SSH-connection-per-operation* transport with SSH multiplexing **deliberately disabled**. At low concurrency the per-op SSH handshake latency hides behind the work; at ~6 concurrent runs the serialized sections (two global mutexes wrapping multi-hop SSH sequences, plus a daemon-wide `tmux new-window` mutex) queue up and the run budgets (30-min implementer, 90s agent_ready) start expiring. There is **no resident worker-side agent** — every git op, every tmux op, and every liveness/paste probe is its own short-lived `ssh host -- cmd` process.

The design already carries scar tissue proving this: `ControlMaster=no` was added (hk-zexsj) because the *shared* multiplexed connection truncated writes under concurrent churn (hk-cnp17), and `mergeMu` was then extended to wrap remote git ops (hk-lt091) because, without the shared connection serializing them at the remote OS, concurrent `git worktree add`/`git fetch` raced. In other words: the system oscillated between "share one connection (truncates under load)" and "one connection per op + a global mutex to re-serialize them." Both are symptoms of not having a resident worker agent.

---

## End-to-end sequence (numbered hops, file:line)

### Hop 0 — Worker selection & slot claim (dispatch time)
- Registry constructed once per daemon: `workers.NewRegistry(cfg)` — `internal/daemon/workloop.go:1245`. Stores only `cfg.Workers[0]` — `internal/workers/registry.go:22`.
- Remote-vs-local decision, hoisted to dispatch (hk-hs7ex) — `workloop.go:3000-3008`:
  - queue `LocalOnly` → skip selection → local.
  - queue `WorkerTarget` set → `SelectWorkerByName` — `registry.go:62`.
  - else → `SelectWorker` — `registry.go:38`.
  - nil result (no worker / disabled / slots full) → fall back to **local**.
- Slot claim = check-and-increment under the registry mutex: cap check `registry.go:47`, `r.inFlight++` `registry.go:50`. Released via `defer deps.workerRegistry.ReleaseSlot()` — `workloop.go:3391` (and `:3418` fallback).
- **Hard one-worker limit:** `currentVersion = 1` (`workers.go:34`); `parse` rejects >1 worker with `ErrTooManyWorkers` (`workers.go:282-284`). One HOST, but `MaxSlots` concurrent runs allowed on it via repeated increments.

### Hop 1 — Per-run reverse tunnel + worker prep (before any git)
For every remote run (`rbc != nil`), `beadRunOne` — `workloop.go:3458-3552`:
1. Allocate a box-A ephemeral port hint — `workloop.go:3461`.
2. `ensureWorkerHarmonikDir` (mkdir `.harmonik/` on worker) — **1 fresh SSH** — `workloop.go:3475`.
3. Start a **long-lived** `ssh -N -R <port>:<daemon.sock>` reverse-tunnel process — `workloop.go:3509-3510` (`reversetunnel.go`). This is the ONLY persistent daemon→worker link, and it is **per-run**, torn down in a defer (`:3519-3524`). Its purpose is only to let the run's hook-relay reach box A — it does not carry git/tmux commands.
4. Readiness gate: `waitWorkerSocketLive` (`nc -z` over SSH, retried) — **≥1 fresh SSH** — `workloop.go:3541`.

### Hop 2 — Remote worktree materialization (git over SSH) — SERIALIZED
`workloop.go:3623-3668`, all under **`mergeMu`** (global):
1. `deps.mergeMu.Lock()` — `workloop.go:3636`.
2. `ensureBaseOnWorker` — ensures base SHA present on worker; may `git fetch` worker←box-A over `ssh://`, or push from box A — **1+ fresh SSH** — `workloop.go:3648`.
3. `wtFactory` → `workspace.CreateWorktree` via `SSHRunner`, additionally guarded by **`worktreeCreateMu`** (`.WithCreateMutex(deps.worktreeCreateMu)` — `workloop.go:3602`). Inside `CreateWorktree` (`internal/workspace/createworktree.go`):
   - `mkdir -p <parentDir>` — **1 fresh SSH** — `createworktree.go:181`.
   - `git worktree add -b <branch> <path> <sha>` — **1 fresh SSH** — `createworktree.go:237`.
   - `git rev-parse HEAD` (empty-HEAD-race validation, `resolveWorktreeHEADViaRunner`) — **1 fresh SSH** — `createworktree.go:146-147, 257`.
   - Retry/cleanup path adds `rm -rf` + `worktree prune` + `branch -D` — **3 more fresh SSH** per retry — `createworktree.go:215, 222, 225`.
4. `deps.mergeMu.Unlock()` — `workloop.go:3666-3667`.
- **No `git fetch` of the branch in the create path** — the worker repo is assumed populated; `parentCommit` is added directly.

### Hop 3 — Per-launch artifact materialization (file writes over SSH)
`internal/workspace/remotematerialize.go` — each write is base64-piped over the runner:
- `settings.json` (hook-bridge) — `MaterializeClaudeSettingsVia` `remotematerialize.go:184` → `writeRemoteFile` `:78` = `sh -lc "mkdir…|base64 -d>…"` — **1 fresh SSH**.
- `.harmonik/agent-task.md` — `WriteAgentTaskVia` `:212` — **1 fresh SSH**.
- `~/.claude.json` trust upsert — `EnsureWorktreeTrustVia` `:256` runs a python3 program piped on stdin over SSH — **1 fresh SSH**. (Carries its own `flock(LOCK_EX)` on `~/.claude.json.lock` — `:328-396` — because 5 concurrent unlocked writers previously left only 1 worktree trusted; a cross-process lost-update race.)

### Hop 4 — tmux/pane spawn + agent launch (tmux over SSH) — SERIALIZED
`perRunSubstrate.spawnWindowRemote` — `internal/daemon/tmuxsubstrate.go:2157` (constructed `newPerRunSubstrate` `:2018`, called `workloop.go:4352`). Runner is the `SSHRunner{ControlMaster=no, ControlPath=none}` from `workloop.go:3389/3416`:
1. `EnsureSession` on worker (`tmux new-session -d`) — **1 fresh SSH** — `tmuxsubstrate.go:2183`.
2. `spawnWindowVia` → `callNewWindowBounded` (`tmux new-window`) under **`newWindowMu`** (daemon-global) — **1 fresh SSH** — `tmuxsubstrate.go:947, 1095`.
3. `WindowPaneID` — **1 fresh SSH** — `~tmuxsubstrate.go:1012`.
4. `WindowPanePID` — **1 fresh SSH** — `~tmuxsubstrate.go:1037`.
- **`agent_ready`** is detected via the worker-side hook-relay firing a SessionStart hook → over the per-run reverse tunnel → box-A daemon socket. The relay is a *short-lived* `harmonik hook-relay` subprocess spawned fresh per hook (`internal/hookrelay/hookrelay.go:3-6, 31-38`), NOT a resident agent. Timeout ~90s.
- Subsequent paste-inject / liveness probes (`WriteLastPane`, `CaptureLastPane`, `PaneHasActiveProcess`, `SendEnter/QuitToLastPane`) each = **1 fresh SSH apiece** — `tmuxsubstrate.go:2218-2388`.

### Hop 5 — Review.json read-back + merge-back
- Reviewer worktree is created **on box A** (not the worker) since fix-D/hk-fxy9, so `review.json` is read **locally** with a retrying reader — `reviewloop.go:1579` (`ReadReviewVerdictLocalRetry`). The comment at `reviewloop.go:1560-1569` records that reading it *via the worker runner* truncated "3/3 at 3 concurrent on real gb-mbp, clean single-slot" (hk-177oz / hk-cnp17) — direct evidence of concurrency-triggered failure.
- Merge-back: `preMergeSync` fetches the run branch box-A←worker over `ssh://` — `workloop.go:3574-3588` — **fresh SSH**; the final push box-A→GitHub uses box-A credentials. The merge itself is serialized under **`mergeMu`**.
- Remote worktree cleanup: `git worktree remove --force --force` + `git worktree prune` — **2 fresh SSH** — `workloop.go:3612-3615`.

---

## Serialization / blocking / lock table

| # | Point | file:line | Kind | Scope | Blocks whole daemon? | Notes |
|---|-------|-----------|------|-------|----------------------|-------|
| 1 | **`mergeMu`** wraps `ensureBaseOnWorker` (git fetch/push over SSH) **+** `CreateWorktree` (3 SSH) **+** all merges | `workloop.go:384,1161`; held `3636-3667` | `sync.Mutex` GLOBAL | All queues, all runs | **Yes** — every remote run's git materialization + every merge run strictly one-at-a-time | The dominant bottleneck. Holds the lock across a multi-hop, fresh-SSH-per-hop sequence to a Tailscale host. At 6 concurrent runs the 6 materializations fully serialize behind each other + behind any in-flight merge. |
| 2 | **`worktreeCreateMu`** wraps the worktree add+HEAD retry loop | `workloop.go:407,1162`; `createworktree.go:163-166` | `sync.Mutex` GLOBAL | All remote runs | **Yes** for worktree create | Nested inside `mergeMu` on the remote path — redundant serialization; added (hk-5qp7z) to stop empty-HEAD races once ControlMaster was disabled. |
| 3 | **`newWindowMu`** wraps every `tmux new-window` (+ optional `spawnStagger` sleep held under lock) | `tmuxsubstrate.go:118,1095-1117` | `sync.Mutex` GLOBAL | All spawns (local+remote) | **Yes** — every agent spawn serializes; a hung new-window holds it up to `newWindowTimeout` (60s) | Local and remote share one tmux-new-window lock. Under load, spawns queue; `spawnStagger` (0 by default, tuned 2-5s) *adds* held time. |
| 4 | **Fresh SSH per op, multiplexing OFF** | `runner.go:111-121`; `ControlMaster=no,ControlPath=none` `workloop.go:3389,3416` | Transport design | Every git/tmux/file/probe op | Indirect — inflates the time locks 1-3 are held | ~3 SSH for worktree, ~5 for artifacts, ~4 for spawn, N for probes → dozens of full TCP+SSH handshakes per run to a Tailscale host, none reused. |
| 5 | **Per-run reverse tunnel** `ssh -N -R` | `workloop.go:3509`; `reversetunnel.go` | Long-lived proc per run | Per run | No (per-run goroutine) | One extra persistent ssh process per concurrent run; also its own readiness-gate SSH probes. |
| 6 | `cacheReapMu` reap↔dispatch exclusion; dispatch holds `RLock` across run registration incl. slot claim | `workloop.go:894,1164,2971-2993` | `sync.RWMutex` | Dispatch vs reap | Briefly | Not a primary bottleneck; readers concurrent. |
| 7 | Registry `mu` guards worker config + `inFlight` slot counter | `registry.go:11` | `sync.Mutex` | Slot claim + report poll reads | No | Held only for trivial increments/reads; **never** across an SSH call. Not a bottleneck. |
| 8 | `localInFlight` local capacity sub-cap | `workloop.go:329,3010` | `atomic.Int32` | Local runs only | No | Lock-free; distinct from remote slots. |
| 9 | Report/breach poll = single goroutine, fresh `SSHRunner` per sweep, `wg.Wait` before next tick | `daemon.go:2280`; `report_poll.go:43-47,158,180-227` | Single goroutine | Observability only | No | breach.go/health.go/telemetry.go carry **no** locks; a slow/failing poll SSH is logged-and-dropped, never blocks dispatch. Not a bottleneck. |
| 10 | `spawnSem`/`nonTerminalSem` resizable spawn cap | `tmuxsubstrate.go:141,150,293` | counting semaphore | Local spawns | No | Remote runs **skip** this (`releaseSlotFn=no-op`, acquire skipped — `tmuxsubstrate.go:885-886`); remote is bounded by `MaxSlots` instead. |

**Per-run fresh-SSH-connection count (happy path, no retries):** tunnel-prep ~2 + worktree ~3 + artifacts ~3 + spawn ~4 + several paste/liveness probes + merge-back/cleanup ~3 = **~15-20+ discrete `ssh host -- cmd` invocations per remote run**, none reused, each a full handshake, and the git/merge subset (~6) runs under a **global** mutex.

**Why 6 concurrent falls over (mechanism):** Runs 1-6 all reach Hop 2 and queue on `mergeMu`. Each holder occupies the lock for the wall-clock duration of `ensureBaseOnWorker` + 3 serial SSH round-trips to a Tailscale host (each handshake is tens-to-hundreds of ms, more under contention). The 6th run waits behind 5 predecessors' full materialization sequences; meanwhile its 90s `agent_ready` / 30-min implementer budgets are already ticking from `run_started`. Independently, every spawn also serializes on `newWindowMu`. The failure is queueing-delay-induced budget expiry (agent_ready_timeout, empty-HEAD, verdict-truncation), exactly the class the scar-tissue beads (hk-zexsj/hk-lt091/hk-cnp17/hk-177oz) keep patching — not CPU/mem/disk exhaustion.

---

## Assessment: resident worker-agent binary (operator's proposed alternative)

**The operator is right, and the current code already contains every building block.**

**What exists today (no resident agent):**
- `harmonik` binary is already installed and invoked ON the worker — it runs `harmonik hook-relay` there (`workers.DefaultHarmonikPath = /Users/gb/go/bin/harmonik`, `workers.go:62`). So a worker-side `harmonik <serve>` would reuse that install.
- The reverse-tunnel + TCP-loopback endpoint machinery (`reversetunnel.go`, `resolveDialTarget` in `hookrelay.go`) already establishes a worker↔daemon socket channel — but it is per-run and torn down.
- The daemon already speaks a Unix-socket JSON request/response RPC (`sendRequest`/`handleResponse` in `internal/queue/cli/worker.go` — which, despite its name, is a **box-A client** that toggles the registry `enabled` flag over `daemon.sock`, `worker.go:60-73`; NOT a worker daemon).
- `cmd/harmonik/remote_control_prefix_cmd.go` is unrelated — it just prints a tmux session-label prefix from config; "side-effect-free, does not contact a daemon" (`:19,:57`).
- `hook-relay` / `hook-bridge` are **transient**: relay is a short-lived per-hook subprocess; bridge is the daemon-side socket. Neither is resident.

**What a resident worker-agent would eliminate:**
1. **The fresh-SSH-per-op transport (Point 4)** — a single persistent daemon↔agent connection (one TCP, one handshake) carrying framed RPCs replaces ~15-20 `ssh` handshakes per run. This directly shrinks the wall-clock time that `mergeMu`/`worktreeCreateMu`/`newWindowMu` are held, because the SSH-latency multiplier inside those critical sections collapses.
2. **The ControlMaster tug-of-war** — the truncation family (hk-cnp17/hk-177oz) came from sharing/not-sharing an SSH mux; a framed RPC over one connection has no partial-write-under-churn failure mode, so those patches (and the read-locally workarounds) become unnecessary.
3. **The remote git/tmux races** — with git and tmux executed **natively and locally on the worker** by the agent (which can hold its own local lock, or the daemon can pipeline non-conflicting ops), the empty-HEAD race and the reason `mergeMu` had to wrap remote git ops (hk-lt091) disappear; the daemon no longer needs a *global* mutex to compensate for the remote OS not serializing independent SSH connections.
4. **Per-run tunnel churn (Point 5)** — hooks can report over the already-open resident channel instead of standing up an `ssh -N -R` per run.

**Net:** a resident worker-agent (daemon↔worker RPC over one persistent connection, git/tmux run natively on the worker) would remove Points 4 and 5 entirely and drastically reduce the cost — and arguably the necessity — of the global mutexes at Points 1-3. That is a direct structural fix for the "falls over at 6 concurrent" design limit, not a tuning knob. The scaffolding (worker-side `harmonik` binary, a socket-RPC convention, a worker↔daemon channel) is already present; what is missing is a `harmonik worker serve`-style resident listener and a daemon-side RPC client to replace `SSHRunner`.

**Caveat / adjacent limit:** even with a resident agent, the v1 registry is hard-capped at **one worker host** (`workers.go:34,282-284`; `registry.go:22`). Concurrency on that host would still be `MaxSlots`, now bounded by the worker's real CPU/mem/disk rather than by SSH+mutex queueing. Multi-host scale-out is a separate v2 change.
