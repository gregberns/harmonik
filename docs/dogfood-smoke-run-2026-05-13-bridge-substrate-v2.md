# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate (v2, post hk-e2kwq fix)

**Verdict:** RED (new failure mode — different from v1)
**Bead:** hk-kqdpf.5 (re-run)
**Date:** 2026-05-13
**Runner:** automated smoke agent
**Procedure:** same as `docs/dogfood-smoke-run-2026-05-13-bridge-substrate.md`
**Prior run:** RED (nil watcher panic at workloop.go:672 — fixed by hk-e2kwq / 9dfd7e8)

---

## 1. Setup

### Git verification

```
git log --oneline -5
```

```
0c441f4 chore(beads): close hk-e2kwq (nil-watcher fix landed)
9dfd7e8 fix(daemon): nil-guard watcher at all 5 substrate call sites (hk-e2kwq)
4978bf1 chore(beads): file hk-e2kwq — nil watcher panic in substrate workloop path (hk-kqdpf.5 follow-up)
bbecca9 docs(smoke): dogfood-smoke-run 2026-05-13-bridge-substrate (hk-kqdpf.5)
6dde546 chore(beads): close hk-kqdpf.4 — substrate wired into composition root
```

Required fix commit 9dfd7e8 present. PASS.

### Preconditions

- `claude --version`: `2.1.140 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br`: reachable at `/Users/gb/.local/bin/br` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS

### Build

```
go build -o /tmp/hk ./cmd/harmonik
```

Exit 0. Binary built successfully at `/tmp/hk`.

Note: `harmonik` is NOT installed to any PATH directory. Only `/tmp/hk` exists. This becomes a critical failure point (see §3).

### Scratch directory setup

```
SMOKE_DIR=/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.qckQXrzc0U
```

- `git init`, `user.email smoke@harmonik.local`, `user.name Smoke Runner`
- `echo "# smoke repo" > README.md && touch marker.txt && git add -A && git commit -m "initial"` — PASS
- `br init --prefix smoke` — PASS
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels "workflow:single"` → **smoke-ru6**

---

## 2. Run

### Tmux session

Created detached tmux session `smoke-1778693612`:

```
tmux new-session -d -s smoke-1778693612 -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    2>&1 | tee $SMOKE_DIR/hk.log; sleep 120"
```

---

## 3. Stage-by-stage analysis

### Stage 1: daemon_started — PASS

```json
{"type":"daemon_started","payload":{"pid":15062,"started_at":"2026-05-13T17:33:33Z"}}
```

No panic. No crash. The nil-guard fix (hk-e2kwq / 9dfd7e8) is confirmed working — the daemon no longer crashes at workloop.go:672.

### Stage 2: daemon_orphan_sweep_completed — PASS

```json
{"type":"daemon_orphan_sweep_completed","payload":{"tmux_sessions_killed":0,"tmux_windows_killed":0,...}}
```

Clean sweep. No stale sessions from prior runs.

### Stage 3: run_started — PASS

```json
{"type":"run_started","payload":{"bead_id":"smoke-ru6","run_id":"019e2266-a8d1-728c-ad1e-c49e5ce0f265","workspace_path":".../worktrees/019e2266-a8d1-728c-ad1e-c49e5ce0f265"}}
```

Bead claimed, worktree created, run ID assigned.

### Stage 4: .claude/settings.json materialization — PASS

All five bridge hooks materialized correctly in the worktree:
```json
{
  "hooks": {
    "SessionStart": [{"hooks":[{"command":"harmonik","args":["hook-relay","SessionStart"],...}]}],
    "Stop": [{"hooks":[{"command":"harmonik","args":["hook-relay","Stop"],...}]}],
    "SessionEnd": [{"hooks":[{"command":"harmonik","args":["hook-relay","SessionEnd"],...}]}],
    "StopFailure": [{"hooks":[{"command":"harmonik","args":["hook-relay","StopFailure"],...}]}],
    "Notification": [{"hooks":[{"command":"harmonik","args":["hook-relay","Notification"],...}]}]
  }
}
```

CHB-001..005 materialization correct.

### Stage 5: tmux window spawn — PASS (partial)

A second tmux window was created within `smoke-1778693612`:

```
tmux list-windows -t smoke-1778693612
1: zsh* (1 panes)
2: /var/folders/.../worktrees/019e2266-a8d1-728c-ad1e-c49e5ce0f265 (1 panes)
```

The substrate dispatched correctly and spawned a window for the worktree path. A `claude --session-id 019e2266-a984-7718-a4a2-e61d89e9a8` process (pid 15078) was created.

### Stage 5a: claude trust prompt — MANUAL INTERVENED

Claude displayed an interactive trust-gate:

```
Quick safety check: Is this a project you created or one you trust?
❯ 1. Yes, I trust this folder
  2. No, exit
```

This gate must be dismissed before Claude can start. The smoke agent sent `Enter` to dismiss it. **This is a known gap (interactive trust gate blocks unattended runs)** but is pre-existing and not new to this run.

### Stage 5b: SessionStart hook relay — FAIL (NEW BUG)

Immediately after trust dismissal, Claude's SessionStart hook fired and failed:

```
SessionStart:startup hook error
Failed with non-blocking status code: Error occurred while executing hook command:
  Executable not found in $PATH: "harmonik"
```

The `.claude/settings.json` hook command is `"harmonik"` (bare binary name). In the tmux window environment, `harmonik` is not installed to any PATH directory. Only `/tmp/hk` exists. The hook relay command cannot execute.

**Root cause:** The hook spec hardcodes `"command": "harmonik"` but `harmonik` is not on PATH in the tmux window's shell environment. The binary path used to launch hk (`/tmp/hk`) is not registered as `harmonik`.

**Impact:** SessionStart relay never reaches the daemon socket. The workloop's `waitAgentReady` (HC-056) starts its deadline immediately; no real `agent_ready` event will arrive through the bridge.

### Stage 6: HC-056 agent_ready timeout — FAIL

After the configured deadline, the workloop logged:

```
daemon: workloop: waitAgentReady bead smoke-ru6 run 019e2266-a8d1-728c-ad1e-c49e5ce0f265: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
```

The workloop attempted to kill the session via `sess.Kill()` and then called `ReopenBead`. **However**, the claude process (pid 15078) was NOT actually killed — it continued running, sitting idle at the Claude Code prompt awaiting user input. `sess.Kill()` on a tmux substrate does not terminate the underlying claude process.

### Stage 7: Bead "reopened" but daemon stalled

After HC-056, `ReopenBead` was called. The bead remained `IN_PROGRESS` in `br` (the status flip to `open` either did not execute, or the workloop main loop did not re-examine the bead because it stayed IN_PROGRESS). The daemon process continued running (alive at pid 15062) but emitted no further output and did not retry.

One additional event appeared ~5 min after start — an `agent_heartbeat` from the daemon infrastructure (not from claude executing any task). No retry run_started was observed.

### Stages 8–10: agent_ready (real), outcome_emitted, run_completed — NOT REACHED

Claude sat idle at the prompt. `marker.txt` was never written. No commit was made. No outcome was emitted.

---

## 4. Full hk.log

```
harmonik daemon starting in /var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.qckQXrzc0U
daemon: workloop: waitAgentReady bead smoke-ru6 run 019e2266-a8d1-728c-ad1e-c49e5ce0f265: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
```

(Two lines total. No panic. No crash.)

---

## 5. Full events.jsonl

```jsonl
{"event_id":"019e2266-a861-70ee-ac38-1ae59264dca3","schema_version":1,"type":"daemon_started","timestamp_wall":"2026-05-13T10:33:33.665061-07:00","source_subsystem":"eventbus","payload":{"binary_commit_hash":"unknown","pid":15062,"started_at":"2026-05-13T17:33:33Z"}}
{"event_id":"019e2266-a8b7-739d-9443-e14a04c0515b","schema_version":1,"type":"daemon_orphan_sweep_completed","timestamp_wall":"2026-05-13T10:33:33.751237-07:00","source_subsystem":"eventbus","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-13T17:33:33Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
{"event_id":"019e2266-a980-7bb0-a7a0-342682e0446c","schema_version":1,"type":"run_started","timestamp_wall":"2026-05-13T10:33:33.952766-07:00","source_subsystem":"eventbus","payload":{"bead_id":"smoke-ru6","run_id":"019e2266-a8d1-728c-ad1e-c49e5ce0f265","started_at":"2026-05-13T17:33:33Z","workspace_path":"/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.qckQXrzc0U/.harmonik/worktrees/019e2266-a8d1-728c-ad1e-c49e5ce0f265"}}
{"event_id":"019e2266-a990-7956-b779-c08ebe72bb47","schema_version":1,"type":"handler_capabilities","timestamp_wall":"2026-05-13T10:33:33.968612-07:00","run_id":"019e2266-a8d1-728c-ad1e-c49e5ce0f265","source_subsystem":"eventbus","payload":{"claude_session_id":"019e2266-a984-7718-a4a2-e95b006a4f69","supported_versions":[1],"type":"handler_capabilities"}}
{"event_id":"019e2266-a990-7ad9-a011-dbb05e8a4da1","schema_version":1,"type":"session_log_location","timestamp_wall":"2026-05-13T10:33:33.968711-07:00","run_id":"019e2266-a8d1-728c-ad1e-c49e5ce0f265","source_subsystem":"eventbus","payload":{"agent_type":"claude-code","log_format":"jsonl","log_path":"/Users/gb/.claude/projects/var-folders-s9-kq5h9q_17t571w_xq_1q9f2r0000gp-T-tmp.qckQXrzc0U-.harmonik-worktrees-019e2266-a8d1-728c-ad1e-c49e5ce0f265/019e2266-a984-7718-a4a2-e95b006a4f69.jsonl","node_id":"bead/smoke-ru6","run_id":"019e2266-a8d1-728c-ad1e-c49e5ce0f265","session_id":"019e2266-a990-77c0-839e-b98009500a3b","type":"session_log_location"}}
{"event_id":"019e2266-a990-7b4a-a988-6b053c044171","schema_version":1,"type":"skills_provisioned","timestamp_wall":"2026-05-13T10:33:33.96874-07:00","run_id":"019e2266-a8d1-728c-ad1e-c49e5ce0f265","source_subsystem":"eventbus","payload":{"run_id":"019e2266-a8d1-728c-ad1e-c49e5ce0f265","session_id":"019e2266-a990-77c0-839e-b98009500a3b","skills":[],"type":"skills_provisioned"}}
{"event_id":"019e2266-a990-7ba4-a2ad-80335f78fe0d","schema_version":1,"type":"agent_ready","timestamp_wall":"2026-05-13T10:33:33.968763-07:00","run_id":"019e2266-a8d1-728c-ad1e-c49e5ce0f265","source_subsystem":"eventbus","payload":{"capabilities":[],"session_id":"019e2266-a990-77c0-839e-b98009500a3b","type":"agent_ready"}}
{"event_id":"019e226b-3d85-7914-bcd6-1f698a4295eb","schema_version":1,"type":"agent_heartbeat","timestamp_wall":"2026-05-13T10:38:33.989598-07:00","run_id":"019e2266-a8d1-728c-ad1e-c49e5ce0f265","source_subsystem":"eventbus","payload":{"phase":"reasoning","session_id":"019e2266-a990-77c0-839e-b98009500a3b"}}
```

Note: `agent_ready` at 10:33:33 is the pre-exec synthetic event (pre-Launch artifacts). The `agent_heartbeat` at 10:38:33 (5 min later) is daemon-side periodic infrastructure — not from claude executing a task.

---

## 6. Success criteria checklist

| Criterion | Result |
|-----------|--------|
| 1. No panic / SIGSEGV | PASS — nil-guard fix (hk-e2kwq) confirmed working |
| 2. Tmux window for claude spawned | PASS — window @1880 created |
| 3. marker.txt contains SMOKE-OK | FAIL — file empty |
| 4. Commit with SMOKE-OK on worktree branch | FAIL — no new commit |
| 5. `outcome_emitted` in events.jsonl | FAIL — never reached |
| 6. Real `agent_ready` via Stop hook relay | FAIL — SessionStart hook failed (harmonik not in PATH) |
| 7. `.claude/settings.json` with all five bridge hooks | PASS — materialized correctly |
| 8. Bead closed with reason `done` | FAIL — bead stuck IN_PROGRESS |
| 9. `run_completed` has `success: true` | FAIL — never emitted |
| 10. HC-056 claude subprocess killed on timeout | FAIL — claude pid 15078 survived `sess.Kill()` |

---

## 7. New failure modes identified

### Bug A: hook relay command `harmonik` not in PATH in tmux window (P0, blocks bridge)

**Location:** `.claude/settings.json` materialization path + tmux substrate launch environment.

The hooks use `"command": "harmonik"` (bare binary name). When hk is run from a non-installed location (e.g., `/tmp/hk` as in this smoke run, or any custom install path not on PATH), the tmux window shell cannot find `harmonik`. The SessionStart hook fails immediately, breaking the entire bridge relay chain. The fix must either: (a) materialize hooks with the absolute path to the running binary (`os.Executable()` at launch time), or (b) inject the binary's parent directory into PATH in the tmux window's environment.

### Bug B: `sess.Kill()` on tmux substrate does not terminate the claude process (P1)

**Location:** `internal/daemon/workloop.go` HC-056 path; tmux substrate Kill() implementation.

When HC-056 fires, the workloop calls `sess.Kill(ctx)`. For the tmux substrate, this likely sends SIGTERM/SIGKILL to the tmux window or pane, but the underlying `claude` process (pid 15078) survived and continued running orphaned after the smoke session. This is a resource-leak and correctness issue: HC-056 is supposed to cleanly terminate a stalled agent.

### Gap C: trust gate blocks unattended smoke runs (pre-existing)

Claude's interactive "do you trust this folder?" prompt requires a keypress before any session starts. This is the same gap noted in the v1 run. Not a new bug, but still unaddressed. For unattended dogfood runs, `--dangerously-skip-permissions` or pre-adding the project to Claude's trust list would bypass it.

### Gap D: HC-056 "reopening" leaves bead IN_PROGRESS (P1)

After HC-056, `ReopenBead` is called, which should set the bead back to `open`/`todo`. However, the bead remained `IN_PROGRESS` in `br show`. The workloop did not retry after the HC-056 path returned. This may indicate `ReopenBead` is silently failing (br command error swallowed), or the workloop main loop doesn't re-examine IN_PROGRESS beads on re-entry.

---

## 8. What worked (confirmed by this run)

- **nil-guard fix (hk-e2kwq) is effective.** No SIGSEGV, no panic. The daemon ran the full workloop through to HC-056 without crashing.
- **Daemon startup and orphan sweep** ran cleanly.
- **Bead claim and run_started** emitted correctly.
- **Worktree setup** (git config, marker.txt, README) correct.
- **settings.json materialization** (CHB-001..005) is structurally correct.
- **Tmux window spawn** (substrate dispatch) succeeded: window @1880 created, claude launched.
- **PL-028b ($TMUX guard)** did not false-fire.

---

## 9. Follow-up beads filed

### hk-kqdpf.6 — Hook relay: use absolute binary path instead of bare `harmonik` in settings.json hooks

**Type:** bug, **Priority:** P0, **Parent:** hk-kqdpf

The `.claude/settings.json` hook command hardcodes `"harmonik"` (bare name). When the binary is not on PATH in the tmux window's shell, all hook relays fail. Fix: materialize hooks with the absolute binary path from `os.Executable()` at daemon startup. This is the primary blocker for bridge signal delivery in the substrate path.

### hk-kqdpf.7 — tmux substrate sess.Kill() does not terminate the claude process

**Type:** bug, **Priority:** P1, **Parent:** hk-kqdpf

HC-056 calls `sess.Kill()` on the tmux substrate session, but the underlying `claude` process (pid 15078) survived. Investigate and fix the Kill() implementation in the tmux substrate to ensure the claude child process is terminated when the workloop aborts a run.

### hk-kqdpf.8 — HC-056 "reopening" leaves bead IN_PROGRESS; no retry observed

**Type:** bug, **Priority:** P1, **Parent:** hk-kqdpf

After HC-056 fires and `ReopenBead` is called, the bead remained IN_PROGRESS and the workloop did not retry. Either `ReopenBead` silently failed (br error swallowed), or the workloop main loop doesn't re-examine IN_PROGRESS beads on return. Investigate and ensure the reopen-and-retry loop is correct.

---

## 10. Cleanup

- tmux session `smoke-1778693612` killed via `tmux kill-session`.
- Orphaned claude process (pid 15078) manually killed via `kill -9`.
- Scratch directory `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.qckQXrzc0U` left in place (OS temp, will be cleaned).
- hk process (pid 15062) terminated when tmux session was killed.

---

*Prior RED run (nil-watcher panic): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate.md`*
