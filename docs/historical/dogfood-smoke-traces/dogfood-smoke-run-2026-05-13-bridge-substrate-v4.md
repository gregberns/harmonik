# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate (v4, post umbrella hk-lj1p9)

**Verdict:** RED — socket never bound; hook-relay cannot reach daemon; `agent_ready` timeout on every run
**Bead:** smoke-zc2 (scratch repo)
**Follow-up filed:** hk-tjl40 (P0, in harmonik repo)
**Date:** 2026-05-13
**Runner:** automated smoke agent
**Procedure:** mirrors v3 (`docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v3.md`)

**Prior runs:**
- v1 RED: nil watcher panic → fixed hk-e2kwq (9dfd7e8)
- v2 RED: `harmonik` not on PATH in hook commands → fixed hk-kqdpf.6 (96de004)
- v3 RED: no task delivery — Claude launched idle, no CLAUDE.md, no `--print`
- v4 RED (this run): socket never bound — `RunSocketListener` implemented but never called from composition root

---

## 1. Setup

### Git verification

```
git log --oneline -6

f6cd256 fix(workspace+spec): isolate EnsureWorktreeTrust from real ~/.claude.json (hk-lj1p9.3)
5e0acac docs(handoff): v37 — umbrella hk-lj1p9 complete, hk-lj1p9.3 P0 open
3ee426c fix(workspace+spec): CHB-028 amendment — agent-task.md overwritten per launch
fb86274 fix(workspace): WriteAgentTask MkdirAll .harmonik/ before atomic write
a5c2739 feat(workspace+daemon): write .harmonik/agent-task.md before claude launch (hk-9ow36)
5199aa4 fix(bridge): agent_ready observation switches to relay-synthesized claude signal (hk-1rocd)
```

HEAD is `f6cd256` (hk-lj1p9.3 EnsureWorktreeTrust env-var-override + flock). PASS.

### Preconditions

- `claude --version`: `2.1.141 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br --version`: `br 0.1.45` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS
- `go build -o /tmp/hk ./cmd/harmonik`: exit 0 — PASS

### Scratch directory

```
SMOKE_DIR=/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.9FfO6auLcM
```

- `git init` + `git config user.email smoke@harmonik.local` + `git config user.name "Smoke Runner"` — PASS
- Initial commit: `README.md` + `marker.txt` (`initial\n`) — SHA `20b35ac` — PASS
- `br init --prefix smoke` — PASS
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels "workflow:single"` → **smoke-zc2**

---

## 2. Run

### Tmux session launch

```
tmux new-session -d -s smoke-1778739405 -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    2>&1 | tee $SMOKE_DIR/hk.log; sleep 120"
```

Session: `smoke-1778739405`. Daemon launched inside the session so `tmux display-message -p "#{session_name}"` resolves to `smoke-1778739405`.

---

## 3. Stage-by-stage analysis

### Stage 1: daemon_started — PASS

```json
{"type":"daemon_started","payload":{"binary_commit_hash":"unknown","pid":53759,"started_at":"2026-05-14T06:16:46Z"}}
```

Clean start, no crash.

### Stage 2: daemon_orphan_sweep_completed — PASS

```json
{"type":"daemon_orphan_sweep_completed","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-14T06:16:46Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
```

Clean sweep.

### Stage 3: run_started — PASS

```json
{"type":"run_started","payload":{"bead_id":"smoke-zc2","run_id":"019e2521-6536-7be2-af13-77aa831d311b","started_at":"2026-05-14T06:16:46Z","workspace_path":".../.harmonik/worktrees/019e2521-6536-7be2-af13-77aa831d311b"}}
```

Bead claimed, worktree created.

### Stage 4: handler_capabilities, session_log_location, skills_provisioned — PASS

All three pre-exec messages emitted correctly. Claude session ID `019e2521-65ea-7d0b-b428-8b39e6e40300` minted.

### Stage 4a: .claude/settings.json materialization — PASS (hk-kqdpf.6 still working)

All five bridge hooks materialized with absolute binary path `/tmp/hk`:

```json
{
  "hooks": {
    "SessionStart": [{"hooks":[{"command":"/tmp/hk","args":["hook-relay","SessionStart"],"timeout":30,"type":"command"}],"matcher":""}],
    "Stop": [{"hooks":[{"command":"/tmp/hk","args":["hook-relay","Stop"],"timeout":30,"type":"command"}],"matcher":""}],
    "SessionEnd": [{"hooks":[{"command":"/tmp/hk","args":["hook-relay","SessionEnd"],"timeout":30,"type":"command"}],"matcher":""}],
    "StopFailure": [{"hooks":[{"command":"/tmp/hk","args":["hook-relay","StopFailure"],"timeout":30,"type":"command"}],"matcher":""}],
    "Notification": [{"hooks":[{"command":"/tmp/hk","args":["hook-relay","Notification"],"timeout":30,"type":"command"}],"matcher":""}]
  }
}
```

### Stage 4b: agent-task.md materialization — PASS (hk-9ow36 fix confirmed)

`$WORKTREE/.harmonik/agent-task.md` was written before tmux window creation:

```
# Harmonik Task

bead_id: smoke-zc2
title: Add SMOKE-OK marker line to marker.txt and commit
phase: 
iteration: 1
run_id: 019e2521-6536-7be2-af13-77aa831d311b
workspace_path: /private/var/folders/.../019e2521-6536-7be2-af13-77aa831d311b

## Task Description

Add SMOKE-OK marker line to marker.txt and commit
```

**hk-9ow36 is confirmed working.** The v3 gap (no task delivery) is resolved at the file-materialization level.

### Stage 5: launch_initiated — PASS (tmux window created)

```json
{"type":"launch_initiated","payload":{"claude_session_id":"019e2521-65ea-7d0b-b428-8b39e6e40300","session_id":"019e2521-660f-7dc7-8509-9ba15ffb9dce","type":"launch_initiated"}}
```

`handler.Launch` returned no `launchErr` — `Substrate.SpawnWindow` succeeded. A tmux window was created in session `smoke-1778739405`.

Note on v3 vs v4 difference: v3 had a synthetic `agent_ready` as pre-exec event (7th event). v4 does **not** — commit `5199aa4` (hk-1rocd relay-synthesis fix) correctly changed the `agent_ready` signal to come only from the hook-relay, not from pre-exec emission. This is the right behavior; it just means the tap can only succeed if the socket is live.

### Stage 6: agent_ready timeout — FAIL (root cause: socket never bound)

After `launch_initiated`, the daemon enters `waitAgentReady`. The claude process (spawned in a tmux window) fires the `SessionStart` hook, which executes `/tmp/hk hook-relay SessionStart`. The hook-relay command reads `HARMONIK_DAEMON_SOCKET` from its environment and connects to the unix socket at `$SMOKE_DIR/.harmonik/daemon.sock`.

**That socket file does not exist.** Inspection of `$SMOKE_DIR/.harmonik/` shows only:

```
daemon.pid
beads-intents/
events/
worktrees/
```

No `daemon.sock`. The hook-relay connection fails silently; no `agent_ready` event reaches the daemon. After 30 seconds (defaultAgentReadyTimeout, HC-056):

```
daemon: workloop: waitAgentReady bead smoke-zc2 run 019e2521-6536-7be2-af13-77aa831d311b: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
```

### Root cause: RunSocketListener never called

`internal/daemon/socket.go` implements `RunSocketListener` — it binds a Unix domain socket, accepts connections, and dispatches hook-relay messages. However, inspection of `internal/daemon/daemon.go::Start()` shows the function is **never invoked**. The composition root:

1. Calls `runWorkLoop` in a goroutine
2. Blocks on `<-loopDone`

There is no goroutine calling `RunSocketListener`. The comments in `daemon.go` and `workloop.go` reference it as if it exists in the wiring, but the actual call is missing. This is a wiring gap — the socket implementation is complete, the composition root call is not.

Evidence: `grep -rn "RunSocketListener" internal/ | grep -v "_test.go"` returns references only in comments and in `socket.go` itself. No production caller.

### Stage 7: HC-056 reopen — PASS (bead requeued)

After HC-056, `ReopenBead` is called. The daemon begins polling again. A second run attempt was pending when the session was killed (events showed `agent_heartbeat` from the second run starting). No new window appeared during observation (the first window had already exited after claude failed to connect).

Claude session log directory was never created in `~/.claude/projects/` — confirming claude never completed a `SessionStart` hook acknowledgment (the hook-relay failed before the session could register).

### Stage 8: marker.txt, commit, bead close — NOT REACHED

`marker.txt` still contains only `initial\n`. No new git commit. Bead `smoke-zc2` was `IN_PROGRESS` at session kill. No `outcome_emitted`. No `run_completed`.

---

## 4. Full hk.log

```
harmonik daemon starting in /private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.9FfO6auLcM
daemon: workloop: waitAgentReady bead smoke-zc2 run 019e2521-6536-7be2-af13-77aa831d311b: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
```

Two lines. Same structure as v3. No panic.

---

## 5. Full events.jsonl

```jsonl
{"event_id":"019e2521-64b5-7bcf-8955-a73337cd9cf7","schema_version":1,"type":"daemon_started","timestamp_wall":"2026-05-13T23:16:46.005774-07:00","source_subsystem":"eventbus","payload":{"binary_commit_hash":"unknown","pid":53759,"started_at":"2026-05-14T06:16:46Z"}}
{"event_id":"019e2521-6518-7e48-b2a3-bb99a55b3b51","schema_version":1,"type":"daemon_orphan_sweep_completed","timestamp_wall":"2026-05-13T23:16:46.104936-07:00","source_subsystem":"eventbus","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-14T06:16:46Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
{"event_id":"019e2521-65e6-7b62-8c7b-e7da97052a11","schema_version":1,"type":"run_started","timestamp_wall":"2026-05-13T23:16:46.310747-07:00","source_subsystem":"eventbus","payload":{"bead_id":"smoke-zc2","run_id":"019e2521-6536-7be2-af13-77aa831d311b","started_at":"2026-05-14T06:16:46Z","workspace_path":"/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.9FfO6auLcM/.harmonik/worktrees/019e2521-6536-7be2-af13-77aa831d311b"}}
{"event_id":"019e2521-6610-7003-a358-8f6bc3620f5b","schema_version":1,"type":"handler_capabilities","timestamp_wall":"2026-05-13T23:16:46.352001-07:00","run_id":"019e2521-6536-7be2-af13-77aa831d311b","source_subsystem":"eventbus","payload":{"claude_session_id":"019e2521-65ea-7d0b-b428-8b39e6e40300","supported_versions":[1],"type":"handler_capabilities"}}
{"event_id":"019e2521-6610-7157-876d-6cb411c81e63","schema_version":1,"type":"session_log_location","timestamp_wall":"2026-05-13T23:16:46.352088-07:00","run_id":"019e2521-6536-7be2-af13-77aa831d311b","source_subsystem":"eventbus","payload":{"agent_type":"claude-code","log_format":"jsonl","log_path":"/Users/gb/.claude/projects/private-var-folders-s9-kq5h9q_17t571w_xq_1q9f2r0000gp-T-tmp.9FfO6auLcM-.harmonik-worktrees-019e2521-6536-7be2-af13-77aa831d311b/019e2521-65ea-7d0b-b428-8b39e6e40300.jsonl","node_id":"bead/smoke-zc2","run_id":"019e2521-6536-7be2-af13-77aa831d311b","session_id":"019e2521-660f-7dc7-8509-9ba15ffb9dce","type":"session_log_location"}}
{"event_id":"019e2521-6610-71d4-a261-243f03239386","schema_version":1,"type":"skills_provisioned","timestamp_wall":"2026-05-13T23:16:46.35212-07:00","run_id":"019e2521-6536-7be2-af13-77aa831d311b","source_subsystem":"eventbus","payload":{"run_id":"019e2521-6536-7be2-af13-77aa831d311b","session_id":"019e2521-660f-7dc7-8509-9ba15ffb9dce","skills":[],"type":"skills_provisioned"}}
{"event_id":"019e2521-6610-722e-971e-c7175b49c5c1","schema_version":1,"type":"launch_initiated","timestamp_wall":"2026-05-13T23:16:46.352143-07:00","run_id":"019e2521-6536-7be2-af13-77aa831d311b","source_subsystem":"eventbus","payload":{"claude_session_id":"019e2521-65ea-7d0b-b428-8b39e6e40300","session_id":"019e2521-660f-7dc7-8509-9ba15ffb9dce","type":"launch_initiated"}}
```

Seven events (no synthetic `agent_ready` — hk-1rocd relay-synthesis change confirmed correct). Followed by `agent_heartbeat` events from the second run attempt before session kill.

---

## 6. Success criteria checklist

| Criterion | Result |
|-----------|--------|
| 1. No panic / SIGSEGV | PASS — no crash |
| 2. Tmux window spawned for Claude | PASS — `launch_initiated` emitted, `SpawnWindow` returned success |
| 3. `harmonik` absolute path in hooks (kqdpf.6) | PASS — `/tmp/hk` in settings.json |
| 4. agent-task.md written before launch (hk-9ow36) | PASS — file present with correct content |
| 5. EnsureWorktreeTrust called (hk-lj1p9.3) | PASS — `.claude/settings.json` present in worktree |
| 6. Synthetic agent_ready removed from pre-exec (hk-1rocd) | PASS — no pre-exec `agent_ready` event |
| 7. daemon.sock bound for hook-relay | FAIL — `RunSocketListener` never called from daemon.Start |
| 8. agent_ready via SessionStart hook relay | FAIL — socket absent; hook-relay connection fails |
| 9. Paste-inject kick-off delivered | CANNOT VERIFY — depends on agent_ready first |
| 10. marker.txt contains SMOKE-OK | FAIL — not reached |
| 11. Commit with SMOKE-OK on worktree branch | FAIL — not reached |
| 12. Bead closed with reason `done` | FAIL — not reached |

---

## 7. What the umbrella hk-lj1p9 fixes confirm working

The following umbrella deliverables are confirmed structurally correct through this run:

1. **hk-9ow36 (agent-task.md):** File written to `$WORKTREE/.harmonik/agent-task.md` with correct bead title, phase, iteration, run_id, workspace_path before tmux window creation. The v3 "no task delivery" gap is resolved at the file level.
2. **hk-lj1p9.3 (EnsureWorktreeTrust flock + env override):** `.claude/settings.json` present in worktree with correct hook commands. Trust mechanism functional.
3. **hk-1rocd (relay-synthesized agent_ready):** Pre-exec event sequence no longer emits synthetic `agent_ready`. Event log has 7 events, not 8. The relay path is the only path — correct.
4. **CHB-028 (agent-task.md overwrite per launch):** File overwrites correctly even on re-run (same run_id in this case, but MkdirAll + atomic write confirmed).

---

## 8. Failure mode identified: socket wiring gap (P0)

### Root cause

`internal/daemon/socket.go` exports `RunSocketListener(ctx, sockPath, h, hr)`. This function binds the unix socket, accepts connections, dispatches hook-relay messages to `hr` (a `HookRelayHandler`).

`internal/daemon/daemon.go::Start()` **never calls `RunSocketListener`**. The composition root:

```go
// daemon.go Start():
loopDone := make(chan error, 1)
go func() {
    loopDone <- runWorkLoop(ctx, deps)
}()
<-loopDone
```

No goroutine starts the socket. `daemon.sock` is never created. The comments at lines 363 ("The same instance is forwarded to RunSocketListener") and workloop.go line 136 describe the intended wiring, but the actual `go RunSocketListener(...)` call is absent.

This is a simple wiring omission — the socket implementation is complete and correct, and the hook-relay client is correct. The single missing line is the goroutine launch + error check in `daemon.Start`.

### Fix scope

Wire `RunSocketListener` into `daemon.Start`:
1. Derive `sockPath` from `lifecycle.SocketPath(cfg.ProjectDir)`
2. Launch `go RunSocketListener(ctx, sockPath, noopRequestHandler, hookStore)` before `runWorkLoop`
3. Add a `socketDone` channel to drain the goroutine on shutdown (or let ctx cancel it)
4. Handle bind errors as fatal (socket failure = daemon cannot receive hook signals)

The `hookStore` is already constructed in `daemon.Start` at line 368, making it available to pass as `hr`. No structural changes required — this is an additive wiring change.

---

## 9. Follow-up bead

### hk-tjl40 (filed) — Wire RunSocketListener into daemon.Start

**Type:** bug, **Priority:** P0, **Labels:** subsystem:daemon, subsystem:bridge
**Dependency:** `-> hk-lj1p9 (blocks)`

`daemon.Start` constructs `hookStore` (line 368) and documents that it is "forwarded to RunSocketListener" — but never calls `RunSocketListener`. The unix socket `daemon.sock` is never bound. The hook-relay subprocess (called by claude's `SessionStart` hook) cannot connect, so `agent_ready` is never delivered, and every run hits the HC-056 timeout regardless of whether claude starts and runs successfully.

Fix: add `go RunSocketListener(ctx, sockPath, ...)` in `daemon.Start` before `runWorkLoop`, with proper shutdown drain.

---

## 10. Cleanup

- Tmux session `smoke-1778739405` killed.
- Scratch directory `$SMOKE_DIR` left in place: `/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.9FfO6auLcM`
- Claude process (session `019e2521-65ea-7d0b-b428-8b39e6e40300`) terminated via HC-056 Kill() before session kill.

---

*Prior RED run (no task delivery): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v3.md`*
*Prior RED run (harmonik PATH): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v2.md`*
*Prior RED run (nil-watcher panic): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate.md`*
