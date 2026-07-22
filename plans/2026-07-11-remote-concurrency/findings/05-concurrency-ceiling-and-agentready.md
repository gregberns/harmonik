# Concurrency ceiling & what `agent_ready_timeout` actually waits on

**Question (operator's challenge):** the standing story blames "cold start" for reviewer
`agent_ready_timeout` at 6 concurrent slots (hk-5z1f0: raise 90→150s + spawn semaphore).
The operator rejects that: the box is warm, the harness is warm, the caches are warm — so
what is the daemon *actually* waiting on, and why does the system fall over at 6 when real
systems do thousands of tx/sec?

**Verdict up front:** the operator is right. `agent_ready_timeout` is **not** waiting on a
cold machine. It is waiting for a hook callback fired *from inside* the freshly-launched
`claude` process to travel back to the daemon — and the daemon can only *get* that process
launched by pushing every remote run through **~11–13 serialized, un-multiplexed SSH
round-trips gated by THREE daemon-global mutexes, each held across live SSH network I/O**,
on top of **one shared tmux-server command lock on the worker**. That is a serialization
pipeline, i.e. a **design limit**, not a resource wall. "Cold start" is a symptom the
timeouts were tuned around; the disease is a single-file remote-op pipeline.

---

## 1. What `agent_ready` / `agent_ready_timeout` actually is

**`agent_ready` is a hook callback from inside `claude`, not an observation the daemon makes.**

- `waitAgentReady` (`internal/daemon/agentready.go:154-207`) runs an observer goroutine that
  calls `adapter.DetectReady(ev)` on each run-scoped bus event; returns `nil` on match,
  `ErrAgentReadyTimeout` after the window.
- The claude adapter's `DetectReady` returns true **only** when `event.Type == agent_ready`
  and explicitly false for `launch_initiated` (`internal/handler/adapter_claudecode.go:99-105`,
  HC-041).
- The `agent_ready` event originates **inside the claude process**:
  1. claude boots far enough to fire its `SessionStart` hook (wired in `.claude/settings.json`).
  2. The hook invokes `harmonik hook-relay SessionStart`, a short-lived worker-side subprocess
     that **synthesizes** `agent_ready` (`internal/hookrelay/hookrelay.go:255-303`) and ships one
     NDJSON line to the daemon socket (`sendToSocket`, ~line 519) — a unix socket locally, or
     `tcp://127.0.0.1:<port>` **over the per-run reverse SSH tunnel** for a remote worker
     (~lines 492-513).
  3. The daemon socket acceptor's `case "agent_ready"` calls `notifyAgentReady` →
     per-session `agentReadyCallback` (`internal/daemon/hookrelay_chb025.go:328-388`), which feeds
     the event into the tap `waitAgentReady` is reading.

So between `launch_initiated` and `agent_ready` the daemon is blocked on:
**(a)** the `ssh … tmux new-window` returning so the pane exists, **then** **(b)** the `claude`
process inside that pane booting to its first `SessionStart` hook, **then** **(c)** that hook's
`hook-relay` subprocess connecting back over the reverse tunnel to the daemon socket. There is
**no model-API-handshake gate** in this path; `SessionStart` is simply "the first
claude-originated lifecycle signal under the tmux substrate"
(`internal/handlercontract/readystate_hc039.go:16-18`).

The stall diagnostic confirms the named failure modes are pipeline/plumbing, not CPU: the
`agent_ready_stall_detected` causes are "the claude process never started, the -default session
was orphaned, or the relay never synthesized agent_ready"
(`internal/core/agentreadystall_hk1s1or.go:27-28`) — i.e. the pane never came up, or the hook
never reached the daemon.

---

## 2. The serialization points on the concurrent critical path

Every one of these is on the path from dispatch to `agent_ready` for a **remote** run, and
each is either a global lock or a fresh un-multiplexed SSH connection.

**SSH transport has no connection reuse.** `SSHRunner.Command` spawns a fresh `ssh` process per
op (`internal/lifecycle/tmux/runner.go:111-121`), and production explicitly *disables*
multiplexing: `-o ControlMaster=no -o ControlPath=none`
(`internal/daemon/workloop.go:3389,3416`; `reversetunnel.go:238-243`). So every remote op pays
full TCP + auth setup, sequentially.

**Three daemon-global mutexes are held ACROSS SSH network calls:**

| Mutex | Held across | Cite |
|---|---|---|
| `newWindowMu` (per-daemon global) | the SSH `tmux new-window` call **plus** the `spawnStagger` sleep, both inside the held lock | `internal/daemon/tmuxsubstrate.go:1095-1096` (lock) → `callNewWindowBounded` → `adapter.NewWindowIn` at ~1144; remote adapter is SSH-backed (`tmuxsubstrate.go:2166`). Comment even admits "multi-second holds" (1110-1112). |
| `mergeMu` (global) | `ensureBaseOnWorker` (git fetch + cat-file = 2 SSH, +push fallback) **and** `CreateWorktree` (mkdir + worktree add + rev-parse = 3 SSH) — ~5 SSH round-trips | `internal/daemon/workloop.go:3637` (lock) … 3667 (unlock) |
| `worktreeCreateMu` | the SSH `git worktree add` + HEAD-resolve retry loop (nested inside mergeMu) | `internal/workspace/createworktree.go:163-166`; threaded at `workloop.go:3602` |

**One shared tmux server on the worker.** All implementer/reviewer windows live in a single
tmux session on a single tmux server, so concurrent `tmux new-window`s contend on the tmux
server's **global command lock** — documented as able to "crawl ~16 min behind under
MaxConcurrent>1" (`internal/daemon/tmuxsubstrate.go:107-117`). `newWindowMu` exists precisely
to serialize the daemon side so runs don't pile onto that worker-side lock — but the cure is
itself a global mutex held across the network call, so remote launches are single-file either way.

**SSH round-trip count for ONE remote run, dispatch → agent_ready (~11–13, serial):**
`mkdir -p` worker dir (1) · reverse tunnel `ssh -N -R` spawn (1, long-lived) · `git fetch` +
`git cat-file` (2, under mergeMu) · `mkdir` + `worktree add` + `rev-parse` (3, under
mergeMu/worktreeCreateMu) · materialize settings + trust + agent-task (3) · `nc -z` socket-live
probe (≥1) · `tmux new-window` (1, under newWindowMu) · pane-PID resolve then poll (1+). Then
the wait for claude to boot to `SessionStart` and the hook to dial back over the tunnel.

**Why it "falls over at exactly 6":** this is the textbook mutex-held-across-network-I/O
signature. With N=6 remote runs, each run's `newWindowMu` + `mergeMu` sections queue behind the
other five. A single `tmux new-window` over SSH under worker load is multi-second; five of them
in front of you plus the base-sync/worktree-add serialized behind mergeMu pushes the *last*
run's pane-creation minutes late — and the 90s (now 150s/210s) `agent_ready` clock for that run
started at dispatch. The run times out not because claude is slow to boot but because it spent
the budget **queued behind other runs' SSH calls holding global locks**. hk-5z1f0's fixes
(bigger timeout, spawn semaphore, stagger) *raise the ceiling and smooth the burst* but the
semaphore/stagger literally add serialization; they treat the symptom.

---

## 3. Cold-start vs. design-limit — the verdict

**Design limit.** The evidence:

- The wait is a socket callback from inside claude, gated behind a **serial SSH pipeline** and
  **three global mutexes held across the network**, not behind CPU/disk saturation. Warm box,
  warm caches — irrelevant to a mutex you're queued behind.
- The timeout knobs themselves encode the admission: the local default was raised to 150s and a
  **remote** default of 210s exists specifically because "a remote spawn clears
  reverse-SSH-tunnel readiness … and competes … up to agentSpawnSem's cap-3 concurrent
  cold-starts" (`agentready.go:66-80`). That is describing contention on a serialized resource,
  not per-process cold-start.
- `newWindowMu`'s own doc-comment concedes concurrent new-windows "can crawl ~16 min behind
  under MaxConcurrent>1" (`tmuxsubstrate.go:107-117`). Sixteen minutes of queueing is a
  serialization pathology, full stop.

The "thousands of tx/sec" intuition is correct: the per-run *work* is small. What's expensive is
that harmonik expresses each unit of remote work as a fresh SSH process and funnels the
create/spawn/merge steps through daemon-global locks. The ceiling is the width of that pipeline
(effectively 1), not the worker's capacity.

---

## 4. The resident worker-binary proposal — assessment

**Would a resident worker binary on gb-mbp, speaking RPC to the daemon over ONE persistent
connection and executing git/tmux/agent-launch natively, remove the bottleneck? Yes — directly,
on both axes.**

- **Kills the serialized-SSH-round-trip cost.** The ~11–13 fresh `ssh` connections per run
  (each un-multiplexed, `ControlMaster=no`) collapse into local `exec` on the worker behind one
  persistent RPC channel. No per-op TCP+auth. The injection point already exists: the
  `tmux.CommandRunner` seam with `LocalRunner`/`SSHRunner` (`internal/lifecycle/tmux/runner.go:16-27`)
  — a worker-resident agent would run the `LocalRunner` path natively instead of the daemon
  driving `SSHRunner` remotely.
- **Relieves the global-mutex-across-network problem.** `newWindowMu`, `mergeMu`, and
  `worktreeCreateMu` were made daemon-global largely to tame contention on the *shared tmux
  server command lock* and to serialize slow SSH calls. Move new-window/worktree creation to
  native worker-local exec and those locks no longer straddle a multi-second network call — the
  daemon side stops being the single-file gate. (The worker's tmux-server command lock still
  exists, but it's a fast local IPC lock, not a lock wrapped around SSH.)
- **The reverse tunnel could collapse too.** Today each run holds a dedicated `ssh -N -R`
  (`reversetunnel.go`) so the worker-side `hook-relay` can reach the daemon socket; `agent_ready`
  can't fire until `nc -z` confirms that listener is live. A persistent worker↔daemon RPC channel
  carries the `agent_ready` signal directly, removing both the per-run tunnel setup and the
  socket-live probe from the critical path.

**What already exists toward it (building blocks, all reusable):**
- A real, extensible JSON-RPC request/response surface + socket dispatcher
  (`internal/queue/rpc.go`; dispatch in `internal/daemon/socket.go`) — but today it is
  **local CLI→daemon only**, unidirectional, over the daemon's own unix socket.
- The `CommandRunner` seam (`runner.go:16-27`) — the exact swap point for native worker exec.
- A worker registry with slot accounting + enable/disable + health/offline/telemetry events
  (`internal/workers/`), and `harmonik` is already required to be installed on the worker
  (`HarmonikPath`, `workers.go:46-62`).
- A precedent for worker-resident harmonik code speaking NDJSON to the daemon socket:
  `harmonik hook-relay` (`internal/hookrelay/hookrelay.go`) — though it is a one-shot
  per-hook subprocess, not resident.

**What's missing (net-new):**
- No resident worker process — everything is fresh SSH per op; there is no `harmonik worker serve`.
- No persistent bidirectional channel — RPC only flows CLI→daemon over the *local* socket; there
  is no daemon→worker direction and no long-lived multiplexed link.
- No worker-hosted RPC/native-exec surface — the git/tmux/agent-launch method set would be new.
- No dynamic worker register/heartbeat handshake — workers are static YAML (max 1 in v1);
  liveness is daemon-initiated SSH polling.

**Note on naming — two red herrings:** `internal/queue/cli/worker.go` is *not* a resident
worker; it's the one-shot `harmonik worker enable/disable` CLI that flips a registry flag over
the daemon socket. `cmd/harmonik/remote_control_prefix_cmd.go` is *not* a control channel; it
just prints the tmux/claude session-label prefix and is explicitly side-effect-free. Neither is a
step toward the resident binary despite the suggestive names.

**Bottom line for the design:** the resident-worker-RPC architecture attacks the actual ceiling
(serial SSH + global locks across network I/O + per-run tunnel), whereas hk-5z1f0's
timeout-bump + spawn-semaphore only widen the tolerance around the symptom. The seams are in
place; the resident process, the persistent bidirectional channel, and the worker-side exec
surface are the genuine build.
