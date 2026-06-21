# Phase 2 — Transport & Lifecycle Grounding

Ground truth for "live resource-breach alerts pushed from a remote worker to the central daemon DURING a running job." All claims read from code, not the design sketch.

---

## 1. The remote-run hook-relay / tunnel mechanism, end to end

### The reverse tunnel (one per run, long-lived)
- A remote run spawns the implementer via a **detached** ssh (`tmux new-window -d`, returns immediately), so a `-R` flag riding that ssh would die before the first hook. The tunnel is therefore a **SEPARATE long-lived `ssh -N -R` process** keyed to the run and held open for the run's lifetime. (`internal/daemon/reversetunnel.go:1-35`, argv built at `buildReverseTunnelArgs` `reversetunnel.go:163-170`.)
- Form: `ssh -N -R 127.0.0.1:<port>:<daemonSock> -o ExitOnForwardFailure=yes <opts> <host>`. sshd binds a **TCP loopback listener** on the worker (`127.0.0.1:<port>`) and forwards it back to box A's daemon unix socket (`<projectDir>/.harmonik/daemon.sock`).
- **Why TCP loopback, not a unix socket (hk-ege6):** on macOS sshd runs as root, so a `-R` StreamLocal unix bind is root-owned 0600 and the unprivileged hook subprocess gets `connect: permission denied` → silent `agent_ready_timeout`. TCP loopback has no filesystem permission bits. (`reversetunnel.go:20-31`.)
- Port is allocated as a free-port HINT on box A (`allocateReverseTunnelPort` `reversetunnel.go:131-138`); `ExitOnForwardFailure=yes` makes a worker-side port collision fail fast; a connect-probe gate (`waitWorkerSocketLive`, `nc -z` over SSH **as the worker user**, `reversetunnel.go:252-284`) blocks launch until the listener is actually connectable.
- The worker-side endpoint `tcp://127.0.0.1:<port>` is injected as `HARMONIK_DAEMON_SOCKET` into the agent's spawn env (`resolveAgentDaemonSocket` `reversetunnel.go:196-201`). Wiring lives in `workloop.go` ~2436-2530.

### How hooks call the relay
- `MaterializeClaudeSettingsVia` writes `.claude/settings.json` ON THE WORKER (via the SSHRunner) with command-type hooks whose `command` field is the **worker's** harmonik path (`workerHarmonikPath` `reversetunnel.go:58-63`; threaded `claudelaunchspec.go:266-270`). Hooks invoke `<worker-harmonik> hook-relay <EventKind>`.
- The relay (`internal/hookrelay/hookrelay.go`) reads Claude's hook JSON on stdin, builds ONE NDJSON envelope, dials `HARMONIK_DAEMON_SOCKET`, writes one line, reads one ACK line, **closes**. `resolveDialTarget` (`hookrelay.go:508-513`) keys off the `tcp://` prefix to pick TCP vs unix.

### PERSISTENT vs ONE-SHOT — the key answer
- **The tunnel (the `ssh -N -R`) is persistent for the whole run.** But **each event is a fresh one-shot dial** through it: `sendToSocket` dials, writes one line, reads the ACK, closes the connection (`hookrelay.go:519-605`, regime documented `hookrelay.go:1-12` "one-shot NDJSON … dial timeout ≤5s … then close"). There is no resident worker process holding a channel open — the relay is a short-lived subprocess spawned per Claude hook fire.

### Event types the relay accepts/forwards TODAY
- Relay reads only Claude hook kinds: `SessionStart, Stop, SessionEnd, StopFailure, Notification` (`hookrelay.go:33-39`) and maps them to wire types: `agent_ready` (from SessionStart), `outcome_emitted` (Stop/StopFailure), `agent_rate_limited`, `agent_heartbeat` (from Notification), no-op (SessionEnd). (`buildMessage` `hookrelay.go:252-279`.)
- Daemon-side accept switch (`dispatchHookRelayEnvelope` `internal/daemon/hookrelay_chb025.go:363-405`): explicit cases for `outcome_emitted`, `agent_ready`, `agent_rate_limited`, `agent_rate_limit_cleared`, and a **`default: → {status:"ok"}`** arm that accepts any other/future type without state update (`hookrelay_chb025.go:401-404`).

### Could a worker-side process emit a NEW `resource_breach` through the SAME path?
**Yes, cheaply.** The envelope (`hookRelayMessage` `hookrelay.go:65-74`: `type, run_id, claude_session_id, handler_session_id, emitted_at_ns, payload`) and the TCP tunnel are type-agnostic. A worker-side emitter would need:
- The same env values (`HARMONIK_RUN_ID`, `HARMONIK_DAEMON_SOCKET` = the `tcp://127.0.0.1:<port>` endpoint, `HARMONIK_CLAUDE_SESSION_ID`, `HARMONIK_HANDLER_SESSION_ID`) — already exported into the run env.
- Note: the relay's stdin path is **Claude-hook-shaped** and enforces `session_id == HARMONIK_CLAUDE_SESSION_ID` + `hook_event_name == argv` (`hookrelay.go:182-209`). So a `resource_breach` would NOT reuse `hook-relay <kind>` verbatim — it would be a **new tiny worker subcommand** (e.g. `harmonik emit-breach`) that reuses `sendToSocket`'s one-shot dialer + envelope but skips the hook-JSON validation.
- Daemon change: add an explicit `case "resource_breach"` to `dispatchHookRelayEnvelope` (`hookrelay_chb025.go:371`) that emits a new bus event; today it would already ACK `ok` via the default arm but be inert (no bus emit). Add a `core.EventType` + payload type. That's the entire forwarder change.

---

## 2. How/where Claude is spawned on the worker — sampler attach point

- Remote spawn routes `tmuxSubstrate.spawnWindowVia(..., remote=true)` (`tmuxsubstrate.go:600`) through the **SSH-backed adapter**, issuing `tmux new-window -P -F "#{pane_id}" -d -t <workerSession>:` on the worker (`osadapter.go:134-140`). Claude runs in **one tmux window inside a worker-side session whose cwd is the worker repo_path**. The worker session is ensured up front (`perRunSubstrate.SpawnWindow`).
- **Attach points for a sampler whose lifetime is bound to the run (best → worst):**
  - **(a) A second tmux window/pane in the SAME worker session, opened at spawn and `kill-window`'d at teardown.** Symmetric to how claude's window is created; the SSHRunner/adapter already does `tmux new-window` and `tmux kill-window` (`osadapter.go:175-192`). Cleanest worker-side option — but it IS a second resident worker process.
  - (b) A background process in claude's own window — fragile; dies/orphans unpredictably with the pane shell.
  - (c) Launched from the SessionStart hook — but hooks are one-shot subprocesses, so this can't host a long-lived sampler without daemonizing (orphan risk).
- **Teardown today:** the run's cleanup (deferred in `beadRunOne`, `workloop.go:2610-2618`) removes the **remote git worktree** over SSH (`git worktree remove --force`), and the tunnel ssh is ctx-scoped. There is **no explicit worker-side `kill-window` of the claude window in the cleanup path** — completion is *detected* by the worker pane disappearing (see §3), not by the daemon killing it. A worker-side sampler would need an **explicit teardown** (its own `kill-window` / pkill over SSH), or it orphans — and this project has documented orphaned-claude pain. Orphan sweep (`internal/lifecycle/orphansweep.go`, `tmuxsubstrate.go:124` spawnedHandles) is box-A-oriented; it does not GC arbitrary worker side-processes.

---

## 3. Existing periodic signal to piggyback?

- **`agent_heartbeat` does NOT sample the worker.** Two heartbeat sources, both useless for worker resources: (1) relay maps Claude's `Notification` hook → `agent_heartbeat{phase}` (`hookrelay.go:476-490`) — event-driven, not periodic, carries only a phase string; (2) the daemon emits `agent_heartbeat{phase:"reasoning"}` **on box A's behalf** for claude-code (`internal/daemon/claudeheartbeat.go`, HC-057) — generated centrally, never touches the worker. No worker-side periodic emitter exists.
- **Chani's just-landed liveness poll (commit d64c6602, hk-r1zq) DOES already poll the worker periodically during a run.** `tmuxSubstrateSession.runWait` (`tmuxsubstrate.go:1978-2084`) runs a **500ms ticker**; for a remote session (`s.remote=true`) the local-kill fast path is gated off (`&& !s.remote`, line 2023) and it falls through to `s.adapter.WindowPanePID(ctx, s.panePIDTarget())` (line 2070) — which for a remote run resolves `#{pane_pid}` **on the worker's tmux server over the run's SSHRunner**. So **central already issues a per-run SSH command to the worker every 500ms while the job runs**, purely to detect pane-gone. This is a ready-made high-cadence, run-scoped, worker-touching poll that Phase 2 can extend.

---

## 4. The fork, decided

### Recommendation: **(B) central-side faster polling during a run** — extend chani's runWait liveness poll to also sample worker resources.

**Rationale.** Phase 1 deliberately kept the worker dumb (a slow central-side `CollectReport` poll over SSH, `internal/workers/report_poll.go`, no worker resident process). Option (A) reverses that: it needs a NEW long-lived worker-side sampler process *plus* a NEW worker subcommand to drive the one-shot relay dialer — and crucially a NEW explicit worker-side teardown, because the current cleanup only removes the worktree and lets completion be *inferred* from the pane vanishing; nothing kills extra worker processes, so a sampler is a prime orphan candidate in a codebase that already fights orphaned claude. Option (C) is dead: no periodic worker-side signal exists to piggyback — `agent_heartbeat` is either event-driven (Notification) or daemon-synthesized on box A, neither sampling the worker.

Option (B) costs almost nothing structurally and aligns with the dumb-worker preference: chani's `runWait` (`tmuxsubstrate.go:1989`, 500ms ticker) **already** SSH-polls the worker per run for liveness. Phase 2 piggybacks a resource sample onto that existing tick (or a slower divisor of it — e.g. sample every Nth tick) by issuing one extra cheap worker command (`ps`/`top -l1`/the Phase-1 collector probe scoped to the run's pane PID), comparing against thresholds **centrally on box A**, and emitting a `resource_breach` bus event directly — no tunnel, no relay, no worker process, no orphan surface. The sampler's lifetime is automatically the run's lifetime because `runWait` already starts at spawn and returns at completion. "Silence when no job runs" is free: the poll only exists while a run's `runWait` goroutine is alive.

The one tradeoff vs (A): breach latency is bounded by the central poll cadence (500ms tick or chosen divisor) and adds SSH round-trips, rather than a worker pushing the instant a threshold trips. For an "is CPU pegged >80% for N seconds" breach that tolerance is fine, and it avoids standing up — and having to reliably tear down — a worker resident process. If sub-second push latency ever becomes a hard requirement, (A) is the fallback, and the transport homework in §1 (envelope is type-agnostic, daemon switch has a default-ok arm, one explicit `case` + a new `core.EventType` + a small worker `emit-breach` subcommand reusing `sendToSocket`) is already mapped.
