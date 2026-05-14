# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate (v5, post hk-tjl40 socket fix)

**Verdict:** RED — `agent_ready` still not delivered; `sess.Wait()` deadlocks on tmux window-name fallback; workloop capacity gate blocks forever
**Bead:** smoke-bsp (scratch repo)
**Follow-up filed:** hk-smuku (P0, in harmonik repo)
**Date:** 2026-05-13
**Runner:** automated smoke agent
**Procedure:** mirrors v4 (`docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v4.md`)

**Prior runs:**
- v1 RED: nil watcher panic → fixed hk-e2kwq (9dfd7e8)
- v2 RED: `harmonik` not on PATH in hook commands → fixed hk-kqdpf.6 (96de004)
- v3 RED: no task delivery — Claude launched idle, no CLAUDE.md, no `--print`
- v4 RED: socket never bound — `RunSocketListener` implemented but never called from composition root (hk-tjl40)
- v5 RED (this run): socket bound, but `sess.Wait()` deadlocks — tmux window-name target resolves to session active pane after `Kill`

---

## 1. Setup

### Git verification

```
git log --oneline -6

bb416e4 chore(beads): sync — close hk-tjl40
2104168 fix(daemon): wire RunSocketListener into daemon.Start — socket never bound (hk-tjl40)
f093c97 docs(smoke): dogfood-smoke-run v4 2026-05-13 — RED: socket never bound (hk-tjl40)
f6cd256 fix(workspace+spec): isolate EnsureWorktreeTrust from real ~/.claude.json (hk-lj1p9.3)
5e0acac docs(handoff): v37 — umbrella hk-lj1p9 complete, hk-lj1p9.3 P0 open
3ee426c fix(workspace+spec): CHB-028 amendment — agent-task.md overwritten per launch
```

HEAD is `bb416e4`. Both `2104168` (socket fix) and `bb416e4` (beads sync) are present. PASS.

### Preconditions

- `claude --version`: `2.1.141 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br --version`: `br 0.1.45` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS
- `go build -o /tmp/hk ./cmd/harmonik`: exit 0, timestamp `2026-05-13 23:33`, size `8324898` — PASS (rebuilt from HEAD, includes socket fix)

### Scratch directory

```
SMOKE_DIR=/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.wiPWSMFeYT
```

- `git init` + `git config user.email smoke@harmonik.local` + `git config user.name "Smoke Runner"` — PASS
- Initial commit: `README.md` + `marker.txt` (`initial\n`) — SHA `e82f72b` — PASS
- `br init --prefix smoke` — PASS
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels "workflow:single"` → **smoke-bsp**

---

## 2. Run

### Tmux session launch

```
tmux new-session -d -s smoke-1778740442 -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    2>&1 | tee $SMOKE_DIR/hk.log; sleep 120"
```

Session: `smoke-1778740442`.

---

## 3. Stage-by-stage analysis

### Stage 1: daemon_started — PASS

```json
{"type":"daemon_started","payload":{"binary_commit_hash":"unknown","pid":57261,"started_at":"2026-05-14T06:34:02Z"}}
```

Clean start, no crash.

### Stage 2: daemon_orphan_sweep_completed — PASS

```json
{"type":"daemon_orphan_sweep_completed","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-14T06:34:02Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
```

Clean sweep.

### Stage 3: run_started — PASS

```json
{"type":"run_started","payload":{"bead_id":"smoke-bsp","run_id":"019e2531-3531-7d4a-831a-acd87dff594f","started_at":"2026-05-14T06:34:02Z","workspace_path":".../.harmonik/worktrees/019e2531-3531-7d4a-831a-acd87dff594f"}}
```

Bead claimed, worktree created.

### Stage 4: handler_capabilities, session_log_location, skills_provisioned — PASS

All three pre-exec messages emitted correctly. Claude session ID `019e2531-35e7-72bf-b877-fc340bacdaa6` minted.

### Stage 4a: .claude/settings.json materialization — PASS

All five bridge hooks materialized with absolute binary path `/tmp/hk`:

```json
{
  "hooks": {
    "SessionStart": [{"hooks":[{"command":"/tmp/hk","args":["hook-relay","SessionStart"],"timeout":30,"type":"command"}],"matcher":""}],
    "Stop": ..., "SessionEnd": ..., "StopFailure": ..., "Notification": ...
  }
}
```

### Stage 4b: agent-task.md materialization — PASS

`$WORKTREE/.harmonik/agent-task.md` written with correct bead_id, title, run_id, workspace_path.

### Stage 4c: trust pre-seed — PASS

`~/.claude.json` contains `hasTrustDialogAccepted: true` for the worktree path. EnsureWorktreeTrust (hk-lj1p9.3) confirmed working.

### Stage 5: launch_initiated — PASS

```json
{"type":"launch_initiated","payload":{"claude_session_id":"019e2531-35e7-72bf-b877-fc340bacdaa6","session_id":"019e2531-3611-729f-a900-b6f350a3737e","type":"launch_initiated"}}
```

`handler.Launch` returned no `launchErr` — `Substrate.SpawnWindow` succeeded. A tmux window was created in session `smoke-1778740442`.

### Stage 6: daemon.sock bound — PASS (hk-tjl40 fix confirmed)

```
srw-------@ 1 gb  staff  0 May 13 23:34 /private/var/folders/.../tmp.wiPWSMFeYT/.harmonik/daemon.sock
```

Socket exists as a Unix-domain socket. `RunSocketListener` goroutine confirmed in the SIGQUIT goroutine dump:

```
net.(*UnixListener).Accept(0x237a143eea0)
```

The daemon is listening. **hk-tjl40 fix is confirmed working.**

### Stage 7: agent_ready timeout — FAIL (new failure mechanism)

After `launch_initiated`, the daemon enters `waitAgentReady`. HC-056 fires after 30 seconds:

```
daemon: workloop: waitAgentReady bead smoke-bsp run 019e2531-3531-7d4a-831a-acd87dff594f: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
```

The socket received **zero connections** during the 30-second window. The `RunSocketListener` goroutine remained blocked on `Accept`. No hook-relay connection was established.

Root investigation: why did the hook-relay not connect?

**Hypothesis A:** Claude started in the tmux window but the `SessionStart` hook fired before the socket was ready. Ruled out: the socket was bound before the workloop even started.

**Hypothesis B:** Claude started but failed immediately. Evidence: no claude session log directory was created in `~/.claude/projects/`. Claude leaves a session log on any successful session start (even 0-duration ones). The absence of the log suggests claude either crashed on startup or the hook was never reached.

**Hypothesis C:** The tmux window was created but claude received incorrect env vars (missing `HARMONIK_DAEMON_SOCKET`). Cannot fully confirm retroactively; however, the env var IS in the spec.Env generated by ClaudeEnvVars and injected via `tmux new-window -e`.

**Established:** The tmux window was created (SpawnWindow returned success). Claude may have started briefly. The SessionStart hook, if it fired, failed to connect to the socket (or the hook-relay received an error).

### Stage 7a: sess.Wait() deadlock — FAIL (new P0 bug: hk-smuku)

After HC-056, `beadRunOne` calls:
1. `sess.Kill(ctx)` → `KillWindow("smoke-1778740442:<worktree_path>")`
2. `sess.Wait(ctx)` → polls `WindowPanePID` every 500ms

`WindowPanePID` calls `tmux display-message -p -t "smoke-1778740442:<worktree_path>" "#{pane_pid}"`. When tmux cannot find a window named `<worktree_path>` (the window was killed or auto-renamed), tmux falls back to the session's active pane and returns its PID. Demonstrated:

```bash
$ tmux display-message -p -t "smoke-1778740442:/private/var/folders/.../019e2531-..." "#{pane_pid}"
57260   # PID of sleep 120 (the daemon's own window)
exit: 0
```

`runWait` sees PID 57260 (alive), continues polling. `sess.Wait` never returns. `beadRunOne` goroutine never reaches `return`. `defer deps.runRegistry.Unregister(runID)` never fires. `runRegistry.Len()` remains 1.

**Capacity gate deadlock:** The outer workloop loop at Step 2 checks `deps.runRegistry.Len() >= effectiveMax` (1 >= 1 = true). It sleeps and retries forever. Even after the bead was manually set to OPEN (`br update smoke-bsp --status open`), the workloop never polls for new beads.

Confirmed by SIGQUIT goroutine dump at T+5min: goroutine 30 (`beadRunOne`) is at `workloop.go:715` (`sess.Wait(ctx)`). Goroutine 86 (`runWait`) is at `tmuxsubstrate.go:308` in a `select` polling the ticker. The heartbeat goroutine (created at `workloop.go:661`) fired at T+300s, confirming the goroutine lived for >5 minutes.

### Stage 8: marker.txt, commit, bead close — NOT REACHED

`marker.txt` still contains only `initial\n`. No new git commit. Bead `smoke-bsp` remained `IN_PROGRESS` for the duration (daemon's own `ReopenBead` call ran `br update smoke-bsp --status open` but the bead status showed `IN_PROGRESS` when checked — possibly a `br` cache issue or the write conflicted with the workloop's claim state; manual `br update smoke-bsp --status open` succeeded at T+6min and confirmed the bead can be moved to OPEN). Bead was left OPEN after manual intervention.

---

## 4. Full hk.log (pre-SIGQUIT)

```
harmonik daemon starting in /private/var/folders/s9/.../tmp.wiPWSMFeYT
daemon: workloop: waitAgentReady bead smoke-bsp run 019e2531-3531-7d4a-831a-acd87dff594f: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
```

Two lines (identical to v4 — HC-056 fires, `(reopening)` logged). After SIGQUIT: 443 lines of goroutine dump.

---

## 5. Full events.jsonl

```jsonl
{"event_id":"...","type":"daemon_started","timestamp_wall":"2026-05-13T23:34:02.307641-07:00",...}
{"event_id":"...","type":"daemon_orphan_sweep_completed","timestamp_wall":"2026-05-13T23:34:02.390223-07:00",...}
{"event_id":"...","type":"run_started","timestamp_wall":"2026-05-13T23:34:02.594518-07:00",...}
{"event_id":"...","type":"handler_capabilities","timestamp_wall":"2026-05-13T23:34:02.641286-07:00",...}
{"event_id":"...","type":"session_log_location","timestamp_wall":"2026-05-13T23:34:02.641385-07:00",...}
{"event_id":"...","type":"skills_provisioned","timestamp_wall":"2026-05-13T23:34:02.641412-07:00",...}
{"event_id":"...","type":"launch_initiated","timestamp_wall":"2026-05-13T23:34:02.641434-07:00",...}
{"event_id":"...","type":"agent_heartbeat","timestamp_wall":"2026-05-13T23:39:02.664022-07:00","run_id":"019e2531-3531-7d4a-831a-acd87dff594f",...}
```

Eight events. The `agent_heartbeat` at T+300s (same run_id) confirms `beadRunOne` goroutine is alive at T+5min, i.e., `sess.Wait` is blocking.

---

## 6. Success criteria checklist

| Criterion | Result |
|-----------|--------|
| 1. No panic / SIGSEGV | PASS — no crash (SIGQUIT was sent by smoke agent for diagnostics) |
| 2. Tmux window spawned for Claude | PASS — `launch_initiated` emitted, `SpawnWindow` returned success |
| 3. `harmonik` absolute path in hooks (kqdpf.6) | PASS — `/tmp/hk` in settings.json |
| 4. agent-task.md written before launch (hk-9ow36) | PASS — file present with correct content |
| 5. EnsureWorktreeTrust called (hk-lj1p9.3) | PASS — `hasTrustDialogAccepted: true` in ~/.claude.json |
| 6. Synthetic agent_ready removed from pre-exec (hk-1rocd) | PASS — no pre-exec `agent_ready` event |
| 7. daemon.sock bound for hook-relay (hk-tjl40) | PASS — socket file exists, goroutine confirmed in Accept |
| 8. agent_ready via SessionStart hook relay | FAIL — socket received zero connections; claude session log never created |
| 9. sess.Wait() returns after Kill | FAIL — WindowPanePID falls back to session active pane, loops forever |
| 10. workloop dispatches retry after HC-056 | FAIL — runRegistry never decrements; capacity gate deadlocks |
| 11. marker.txt contains SMOKE-OK | FAIL — not reached |
| 12. Commit with SMOKE-OK on worktree branch | FAIL — not reached |
| 13. Bead closed with reason `done` | FAIL — not reached |

---

## 7. What the hk-tjl40 fix confirms

**hk-tjl40 is confirmed working.** `daemon.sock` is bound at the correct path before the workloop starts. The `RunSocketListener` goroutine is alive and waiting on `Accept`. The socket is accessible from the correct path (`$SMOKE_DIR/.harmonik/daemon.sock`). The v4 blocker (socket never bound) is resolved.

All prior umbrella deliverables remain confirmed:
1. **hk-9ow36 (agent-task.md):** Written correctly before tmux window creation.
2. **hk-lj1p9.3 (EnsureWorktreeTrust):** Trust pre-seeded in `~/.claude.json`.
3. **hk-1rocd (relay-synthesized agent_ready):** No synthetic `agent_ready` in pre-exec events.
4. **CHB-028 (agent-task.md overwrite per launch):** MkdirAll + atomic write confirmed.

---

## 8. Failure mode: WindowPanePID fallback deadlock (P0)

### Root cause

`tmuxSubstrateSession.SpawnWindow` stores the window handle as `"<session>:<window-name>"` where `<window-name>` is the worktree's absolute path (e.g., `/private/var/folders/.../019e2531-.../`). After HC-056:

1. `Kill(ctx)` → `KillWindow("smoke-1778740442:<worktree_path>")` — the window is killed or was already gone.
2. `Wait(ctx)` → `runWait` goroutine calls `WindowPanePID("smoke-1778740442:<worktree_path>")` every 500ms.
3. `tmux display-message -p -t "smoke-1778740442:<worktree_path>"` cannot find a window named `<worktree_path>` (gone or auto-renamed). Tmux falls back to the session's active pane and returns its PID (e.g., `57260` for `sleep 120`).
4. `runWait` sees a non-error PID and loops. `waitDone` is never closed. `Wait` blocks forever.
5. `beadRunOne` goroutine is stuck at `workloop.go:715`. `runRegistry.Unregister` never fires. `runRegistry.Len()` stays at 1 (`effectiveMax`). Capacity gate blocks all future dispatch.

Demonstrated directly:

```bash
$ tmux display-message -p -t "smoke-1778740442:/private/var/.../019e2531-3531-7d4a-831a-acd87dff594f" "#{pane_pid}"
57260   # returns active pane PID, not an error
$ echo $?
0
```

### Secondary issue: why did hook-relay never connect?

No claude session log was created, meaning claude did not complete even a session start. Two hypotheses remain open:
- Claude ran in the window but exited before firing the `SessionStart` hook (e.g., startup error in the worktree).
- The tmux window was created but the `claude` command itself failed silently (environment issue inside the window).

Not fully diagnosed: the window was alive and then killed by HC-056. A manual test in a different session confirms: when launched with correct env vars, the SessionEnd hook fires and reaches the hook-relay binary (which returned `bridge_malformed_hook_payload: required env var HARMONIK_WORKSPACE_PATH is absent` because test env was incomplete). This confirms the hook-relay mechanism works but requires the full env var set to be correctly injected via `tmux new-window -e`.

### Fix scope (hk-smuku)

Replace window-name-based targeting with window-index-based targeting in `tmuxSubstrateSession`:

1. `NewWindowIn` → parse the window index from tmux output (e.g., `tmux new-window` with `-P -F "#{window_index}"` to return the assigned index).
2. Store the handle as `"<session>:<index>"` (numeric, unambiguous, stable even after auto-rename).
3. `WindowPanePID` and `KillWindow` use the index-based target.

Alternatively: disable tmux auto-rename for the window (`-d` is already set; additionally pass `allow-rename off` or `-c "set-window-option automatic-rename off"` before spawning).

---

## 9. Follow-up bead

### hk-smuku (filed) — WindowPanePID falls back to session active pane for long window names — beadRunOne.Wait() deadlocks

**Type:** bug, **Priority:** P0, **Labels:** subsystem:daemon, subsystem:substrate
**Dependency:** `-> hk-lj1p9 (blocks)`

`tmuxSubstrateSession.Wait` polls `WindowPanePID` with a handle of `"session:<worktree-absolute-path>"`. After the claude window is killed or auto-renamed, `tmux display-message -t <handle>` falls back to the session's active pane and returns a valid PID. `runWait` loops forever. `beadRunOne` goroutine never exits. The workloop capacity gate deadlocks all future bead dispatch.

Fix: use window index (from `tmux new-window -P`) in the handle instead of the window name.

---

## 10. Cleanup

- Tmux session `smoke-1778740442` killed (sleep 120 window was the only remaining window after daemon SIGQUIT).
- Scratch directory `$SMOKE_DIR` left in place: `/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.wiPWSMFeYT`
- Bead `smoke-bsp` manually set to OPEN (`br update smoke-bsp --status open`).

---

*Prior RED run (socket never bound): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v4.md`*
*Prior RED run (no task delivery): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v3.md`*
*Prior RED run (harmonik PATH): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v2.md`*
*Prior RED run (nil-watcher panic): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate.md`*
