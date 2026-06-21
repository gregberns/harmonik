# Remote-Substrate — Current Architecture (Ground Truth, 2026-06-20)

Established by reading the code, not docs. File:line refs throughout. The design
question this answers: *"To run remote jobs we apparently need a harmonik
instance/daemon on the remote box. If so, maybe we collect RAM/CPU telemetry,
send it back, and let the central node decide how many instances run on each box."*

Short answer to the premise: **a long-lived harmonik *daemon* is NOT required on
the remote. The harmonik *binary* must be present (the Claude hook subprocess
shells out to it), but it is invoked one-shot per hook event over SSH — there is
no remote daemon, no remote scheduler, and today no remote→central telemetry of
any kind.**

---

## 1. What runs on the remote box TODAY

**No persistent harmonik process. Everything is spawn-on-demand over one-shot
`ssh` invocations.** The daemon runs only on the central box ("box A").

The seam is `CommandRunner` (`internal/lifecycle/tmux/runner.go:15-17`):
- `LocalRunner` (`runner.go:21-26`) — direct `exec.CommandContext`, the local path.
- `SSHRunner` (`runner.go:84-98`) — wraps **every** command as
  `ssh [opts] <host> -- <name> <args...>`. One fresh `ssh` process per call; no
  connection pool, no persistent session. Argv tokens are passed discretely (the
  `#{pane_id}` comment-truncation fix single-quotes remote tokens).

What the central daemon invokes ON the worker, all one-shot via `SSHRunner`:
- **git** — `git fetch origin <ref>`, `git -C <repo> worktree add`, `git push origin run/<id>`
  (`internal/workspace/createworktree.go:147-171`, `codesync_rs_b8.go`).
- **tmux** — `tmux new-session -d` (EnsureSession) and `tmux new-window`
  (`internal/daemon/tmuxsubstrate.go` remote path, `spawnWindowVia` →
  `perRunSubstrate.SpawnWindow` when `p.runner != nil`, ~1549-1605).
- **file materialization** — base64-decode shell one-liners that write
  `.claude/settings.json`, `.harmonik/agent-task.md`, and a `~/.claude.json`
  trust entry onto the worker's filesystem (`internal/workspace/remotematerialize.go:74-87`).
- **claude** — the agent itself runs as a tmux pane *on the worker's tmux server*.
- **harmonik hook-relay** — when the worker's claude fires a hook, claude's
  `.claude/settings.json` hook command is the **worker-side harmonik binary**:
  `<worker-harmonik-path> hook-relay <eventKind>` (`claudelaunchspec.go:266-269`).

**Therefore the worker needs: ssh access + tmux + git + claude + the harmonik
binary present at a known path.** The binary is needed ONLY as the hook handler;
it is never run as a daemon. The health probe enforces exactly this toolset
(`internal/workers/health.go:101-106`: `tmux -V`, `claude --version`,
`git rev-parse HEAD`, and a fail-closed check that `ANTHROPIC_API_KEY` is unset).

---

## 2. Control-plane / data-flow: central ↔ remote

Two channels per remote run, both set up by the central daemon, both per-run:

**(a) Reverse tunnel — remote→central, for hook events.**
`internal/daemon/reversetunnel.go` + `workloop.go:~2505-2640`:
1. Box A allocates an ephemeral loopback TCP port (`allocateReverseTunnelPort` →
   `net.Listen("tcp","127.0.0.1:0")`).
2. Box A starts a long-lived `ssh -N -R 127.0.0.1:<port>:<box-A-daemon.sock>
   -o ExitOnForwardFailure=yes <host>` (`buildReverseTunnelArgs`,
   reversetunnel.go:140-170). This is the ONLY long-lived ssh process, and it
   runs ON BOX A, not the worker. Stored as `rbc.tunnelCmd`; killed on run
   completion (`workloop.go:~2604-2608`).
3. A readiness gate probes the worker side as the unprivileged user
   (`ssh <worker> -- nc -z 127.0.0.1 <port>`, 10s/300ms,
   `waitWorkerSocketLive` reversetunnel.go:221-284). Fail → `worker_tunnel_failed`
   event + bead reopened, agent never launches.
4. The worker's claude gets `HARMONIK_DAEMON_SOCKET=tcp://127.0.0.1:<port>` in
   its env (`resolveAgentDaemonSocket`, reversetunnel.go:183-201). The hook-relay
   binary detects the `tcp://` prefix and dials TCP loopback instead of the unix
   socket (`hookrelay.go:504-513`).
   - **Why TCP, not a unix-socket -R forward (the gap-7 / hk-ege6 fix):** macOS
     sshd runs as root, so a `-R` unix socket bind is root-owned 0600 and the
     unprivileged hook user gets EACCES → silent `agent_ready_timeout`. TCP
     loopback has no filesystem perms.

What flows back over (a): a fixed, narrow set of hook events as one-shot NDJSON
messages — `agent_ready` (from SessionStart), `outcome_emitted` (from Stop),
`agent_rate_limited`, `agent_heartbeat` (`hookRelayMessage`,
`internal/hookrelay/hookrelay.go:67-74`). Each is dial→send-one-line→read-ACK→
close (one-shot, 5s timeout, ≤1 MiB; CHB-015). The daemon socket listener
(`socket.go:305-404`) routes them to the hook-relay handler. **This is the only
remote→central data channel that exists.**

**(b) Completion / liveness — central polls the worker over SSH.**
There is NO callback for "the run finished." The central daemon's `runWait`
goroutine (`tmuxsubstrate.go:~1954-2038`) polls on a 500 ms ticker. For a remote
run, `PaneHasActiveProcess` / pane-PID resolution route through the SSH-backed
adapter (`tmuxsubstrate.go:~1715-1740`), i.e. repeated `ssh worker -- pgrep/ps/
tmux display-message`. Exit code is inferred from process/pane presence
(ESRCH → exitCodeClean=0; pane gone while PID known → exitCodeUnknown=-1). The
Stop hook's `outcome_emitted` (channel a) provides the authoritative outcome with
a 3 s grace wait (`waitsocketgrace.go:86-144`); the poll is the backstop.

So: completion detection is **central-daemon polling the worker's tmux over
repeated one-shot ssh calls**, not a remote agent reporting in.

---

## 3. Worker-health / telemetry that exists today

**Data model** (`internal/workers/workers.go:36-52`), all static from
`.harmonik/workers.yaml`:
```go
type Worker struct {
    Name, Transport, Host, OS, RepoPath, HarmonikPath string
    MaxSlots int   // static capacity declaration
    Enabled  bool  // gate
}
```
There is no dynamic capacity field on the worker — `MaxSlots` is a constant.

**"Health" = reachability + tooling verification, NOT resource telemetry**
(`health.go:50-106`). Four sequential SSH probes: `tmux -V`, `claude --version`,
`git rev-parse HEAD`, and a fail-closed `ANTHROPIC_API_KEY` unset check. Output is
a single boolean → `Registry.SetEnabled(true/false)`. No CPU, no RAM, no load.

**Slot accounting is central and static** (`registry.go`): the `Registry` holds an
in-memory `inFlight int` counter under a mutex. `SelectWorker()` reserves a slot
(returns nil when `inFlight >= MaxSlots` or worker disabled, `registry.go:35-50`);
`ReleaseSlot()` decrements on run completion (registry.go:54-60). **The remote box
has zero visibility into this count — `MaxSlots` is the only brake, and it is a
number a human typed into workers.yaml.**

**Offline/disable transitions are in-memory only, never persisted**
(`offline.go`, `tunnelfailed.go`): `worker_unhealthy` (health probe fail),
`worker_offline` (SSH exit-255 during spawn/liveness), `worker_tunnel_failed`
(tunnel never came up → reopen bead). All flip the in-memory `Enabled` flag;
workers.yaml is never rewritten; everything re-probes on daemon restart. Event
payloads carry only name/host/phase/error strings — **no resource metrics
anywhere**.

**Confirmed: zero CPU/RAM/load telemetry flows remote→central today.** Grep of
`internal/workers/*` finds no `cpu`/`load`/`memory`/`sysinfo`/`statm` references.

---

## 4. Is the operator's premise TRUE?

**Partly — and the distinction is the whole design opening.**

- **FALSE as stated:** running remote jobs does *not* require a harmonik *daemon*
  (or any long-lived harmonik process) on the remote. The remote is a passive SSH
  target. The only long-lived process for a remote run is the `ssh -N -R` reverse
  tunnel, and that runs on the **central** box.
- **TRUE in a weaker form:** the harmonik *binary* must be installed on the worker
  — but solely because Claude's hook config shells out to `harmonik hook-relay`
  one-shot per event. It is a hook handler, not a daemon.

So "we need a harmonik instance there" is an **evolution, not a description of
today.** Today the central node already makes 100% of the scheduling decisions and
the worker is dumb. The operator's telemetry/autoscale idea would require adding
something the architecture currently does NOT have: a remote→central resource
signal. There are two shapes for that:

1. **No remote daemon needed.** The central daemon already runs arbitrary one-shot
   `ssh worker -- <cmd>` (it does this constantly). A periodic
   `ssh worker -- <load/mem probe>` (e.g. `vm_stat`/`sysctl`/`uptime` on darwin)
   folded into the existing `health.go` probe loop would give central the CPU/RAM/
   load it needs to size `MaxSlots` dynamically — with ZERO new process on the
   remote. This is the smaller-step, more-consistent-with-current-code option.
2. **A remote daemon** would only be justified if you want the worker to push
   telemetry autonomously, self-throttle, or run without per-probe ssh latency —
   a heavier evolution that adds a process to supervise on every box.

**The single most important architectural fact for the design discussion:**
The control plane is already fully central — the worker is a passive SSH endpoint
with no harmonik process, and capacity (`MaxSlots`) is a static human-entered
number with no measurement behind it. Adding resource-aware autoscale therefore
needs exactly ONE new thing — a resource signal from worker to central — and the
existing one-shot-ssh health-probe loop (`health.go`) can carry it without any
remote daemon. The premise's "daemon on the remote" is not a prerequisite; it is
one (heavier) of two ways to get telemetry, and the lighter way reuses machinery
that already exists.
