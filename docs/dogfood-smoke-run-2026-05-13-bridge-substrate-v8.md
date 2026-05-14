# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate (v8, post hk-yngq2 pane-ID fix)

**Verdict:** RED — `WriteLastPane` now uses stable pane ID `%NNNN` (hk-yngq2 fix confirmed working); paste-inject delivers the kick-off message to the Claude pane; but trust dialog still blocks because `EnsureWorktreeTrust` pre-seeds under `/var/folders/...` while Claude Code canonicalizes to `/private/var/folders/...` via `realpath`
**Bead:** smoke-97j (scratch repo)
**Follow-up filed:** hk-o5eww (P0, in harmonik repo) — `EnsureWorktreeTrust uses non-canonical path — trust entry written under /var/folders not /private/var/folders, Claude Code uses realpath`
**Date:** 2026-05-14 (UTC-7)
**Runner:** automated smoke agent
**Procedure:** mirrors v7 (`docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v7.md`)

**Prior runs:**
- v1 RED: nil watcher panic → fixed hk-e2kwq (9dfd7e8)
- v2 RED: `harmonik` not on PATH in hook commands → fixed hk-kqdpf.6 (96de004)
- v3 RED: no task delivery — Claude launched idle, no CLAUDE.md, no `--print`
- v4 RED: socket never bound — `RunSocketListener` implemented but never called from composition root (hk-tjl40)
- v5 RED: socket bound, but `sess.Wait()` deadlocks — tmux window-name target resolves to session active pane after `Kill` (hk-smuku)
- v6 RED: `SetAgentReadyCallback` never called in workloop — hook-relay connects and sends `agent_ready` to socket, but callback is nil, `tapCh` never receives the event, HC-056 fires every 30s (hk-lj1p9.4)
- v7 RED: `agent_ready` arrives 0.57s after launch (PRIMARY FIX CONFIRMED); `pasteInjectOnLaunch` fires but `WriteLastPane` errors — tmux can't resolve `session:worktree-path.0` pane target when window name is a full filesystem path containing slashes (hk-yngq2)
- v8 RED (this run): hk-yngq2 fix confirmed — `WriteLastPane` now uses `%NNNN` pane ID; paste-inject succeeds and delivers kick-off message; but trust dialog blocks `SessionStart` hook → `agent_ready` never arrives → HC-056 fires → new gap: `EnsureWorktreeTrust` path mismatch (hk-o5eww)

---

## 1. Setup

### Git verification

```
git log --oneline -6

56f5ce6 chore(beads): sync — close hk-yngq2
ff79246 fix(daemon+tmux): use stable pane ID for WriteLastPane pane target (hk-yngq2)
d968b76 docs(smoke): dogfood-smoke-run v7 2026-05-13 — RED: agent_ready confirmed T+0.57s, paste-inject WriteLastPane fails on slash-path window name (hk-yngq2)
1e5dd0c chore(beads): sync — close hk-lj1p9.4
f79b2a8 fix(daemon): wire SetAgentReadyCallback and pasteInjectOnLaunch in beadRunOne (hk-lj1p9.4)
835e390 docs(smoke): dogfood-smoke-run v6 2026-05-13 — RED: SetAgentReadyCallback never wired (hk-lj1p9.4)
```

HEAD is `56f5ce6`. Both `ff79246` (hk-yngq2 pane-ID fix) and `56f5ce6` (beads sync) present. PASS.

### Preconditions

- `claude --version`: `2.1.141 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br --version`: `br 0.1.45` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS
- `go build -o /tmp/hk ./cmd/harmonik`: exit 0, timestamp `2026-05-14 00:49`, size `8342802` — PASS (rebuilt from HEAD, includes hk-yngq2 pane-ID fix)

### Scratch directory

```
SMOKE_DIR=/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.hWuuy0DwQc
```

- `git init` + `git config user.email smoke@harmonik.local` + `git config user.name "Smoke Runner"` — PASS
- Initial commit: `README.md` + `marker.txt` (`initial\n`) — SHA `9f392d7` — PASS
- `br init --prefix smoke` — PASS
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels "workflow:single"` → **smoke-97j**

---

## 2. Run

### Tmux session launch

```
tmux new-session -d -s smoke-1778744969 -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    2>&1 | tee $SMOKE_DIR/hk.log; sleep 300"
```

Session: `smoke-1778744969`.

---

## 3. Stage-by-stage analysis

### Stage 1: daemon_started — PASS

```json
{"type":"daemon_started","payload":{"binary_commit_hash":"unknown","pid":84284,"started_at":"2026-05-14T07:49:30Z"}}
```

Clean start, no crash.

### Stage 2: daemon_orphan_sweep_completed — PASS

```json
{"type":"daemon_orphan_sweep_completed","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-14T07:49:30Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
```

Clean sweep.

### Stage 3: run_started — PASS

```json
{"type":"run_started","payload":{"bead_id":"smoke-97j","run_id":"019e2576-4bef-7d1b-9d47-340b131be88f","started_at":"2026-05-14T07:49:30Z","workspace_path":".../.harmonik/worktrees/019e2576-4bef-7d1b-9d47-340b131be88f"}}
```

Bead claimed, worktree created.

### Stage 4: handler_capabilities, session_log_location, skills_provisioned — PASS

All three pre-exec messages emitted correctly. Claude session ID `019e2576-4c9c-726d-b96a-7391c235abdb` minted.

### Stage 4a: .claude/settings.json materialization — PASS (inherited from v5/v6)

All five bridge hooks materialized with absolute binary path `/tmp/hk`.

### Stage 4b: agent-task.md materialization — PASS

`$WORKTREE/.harmonik/agent-task.md` written with correct content:

```
bead_id: smoke-97j
title: Add SMOKE-OK marker line to marker.txt and commit
run_id: 019e2576-4bef-7d1b-9d47-340b131be88f
workspace_path: /var/folders/.../.harmonik/worktrees/019e2576-4bef-7d1b-9d47-340b131be88f
```

### Stage 4c: trust pre-seed — PARTIAL FAIL (new gap: hk-o5eww)

`EnsureWorktreeTrust` writes the trust entry to `~/.claude.json["projects"]` under the key `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.hWuuy0DwQc/.harmonik/worktrees/<run_id>` — the path returned by `os.MkdirAll` / the raw `SMOKE_DIR` path (symlink, non-canonical on macOS).

Claude Code on macOS resolves the working directory via `realpath` before keying into `~/.claude.json["projects"]`. The canonical form on macOS is `/private/var/folders/...` (symlink target of `/var/folders`). Claude Code shows the `/private/var/...` form in its trust dialog:

```
Accessing workspace:
/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.hWuuy0DwQc/.harmonik/worktrees/019e2576-4bef-7d1b-9d47-340b131be88f
```

`~/.claude.json["projects"]` is keyed by `/var/folders/...` (no `/private` prefix). Claude Code does not find a trust entry for the `/private/var/...` key and shows the interactive trust dialog.

**Confirmed by inspection of `~/.claude.json`:**

```json
"/var/folders/s9/.../019e2576-4bef-7d1b-9d47-340b131be88f": {
  "hasTrustDialogAccepted": true
}
```

Entry exists but under wrong key. All real (user-accepted) entries in `~/.claude.json` use `/private/var/...` form.

### Stage 5: launch_initiated — PASS

```json
{"type":"launch_initiated","payload":{"claude_session_id":"019e2576-4c9c-726d-b96a-7391c235abdb",...}}
```

Tmux window 2 created; Claude pane visible at worktree path.

### Stage 6: daemon.sock bound — PASS (carried)

Socket file present at `$SMOKE_DIR/.harmonik/daemon.sock`.

### Stage 7: pasteInjectOnLaunch — PASS (hk-yngq2 fix confirmed)

**This is the primary fix under test for v8.** The `hk.log` shows:

```
2026/05/14 00:49:30 INFO daemon_pane_write session_id=019e2576-4c9c-726d-b96a-7391c235abdb pane_target=%1966 buffer_name=harmonik-019e2576-4c9c-726d-b96a-7391c235abdb-task purpose=task payload_bytes=47
```

**Key observation:** `pane_target=%1966` — a `%NNNN` pane ID, NOT the broken `session:path-with-slashes.0` form from v7. No error from `WriteLastPane`. The hk-yngq2 fix stores the pane ID at spawn time and uses it directly.

**tmux pane capture confirms kick-off message delivered:**

```
Please read .harmonik/agent-task.md and begin.

────────────────────────────────────────────────────────────
 Accessing workspace:
 /private/var/.../019e2576-c9a1-781a-ad1f-2c3b1fcbb49c
```

The text "Please read .harmonik/agent-task.md and begin." is the first line of the pane — paste-inject worked. **hk-yngq2 is confirmed fixed.**

### Stage 7a: trust dialog blocks agent_ready — FAIL (hk-o5eww)

The paste-inject message arrived, but immediately below it the trust dialog is showing. Claude Code cannot proceed past the trust prompt without human input. The `SessionStart` hook has not fired because Claude has not completed initialization.

The trust dialog blocks indefinitely (no human at the terminal). After 30s, HC-056 fires:

```
daemon: workloop: waitAgentReady bead smoke-97j run 019e2576-4bef-7d1b-9d47-340b131be88f: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
```

The workloop kills the window, creates a new worktree, and retries. The cycle repeats every ~32s (30s HC-056 + ~2s setup):

```
T+0s    run_started (019e2576-4bef-7d1b-9d47-340b131be88f) + daemon_pane_write pane_target=%1966
T+31s   run_failed (agent_ready_timeout) + run_started (019e2576-c9a1-781a-ad1f-2c3b1fcbb49c) + daemon_pane_write pane_target=%1967
T+63s   run_failed + run_started (019e2577-475e-7762-b6cf-4e16f336f3ec) + pane_target=%1968
T+95s   run_failed + run_started (019e2577-c520-769f-a3e6-37bb24da9dc6) + pane_target=%1969
```

Each retry correctly writes a fresh trust entry via `EnsureWorktreeTrust` for the new worktree path, but the same path-key mismatch applies — `/var/folders` vs `/private/var/folders`.

**Root cause:**

`EnsureWorktreeTrust` calls `os.UserHomeDir()` for the config path but uses the raw worktree path (from `cfg.ProjectDir + "/.harmonik/worktrees/" + runID`) as the key. On macOS, `/var/folders` is a symlink to `/private/var/folders`. When Claude Code opens its working directory it calls `realpath` (or `filepath.EvalSymlinks` equivalent) and stores the resolved path. The symlink form (`/var/folders/...`) never matches the resolved form (`/private/var/folders/...`).

**Fix scope (hk-o5eww):**

`EnsureWorktreeTrust` must resolve `worktreePath` via `filepath.EvalSymlinks` before using it as the key in `~/.claude.json["projects"]`. Claude Code itself does the same resolution, so the keys will match.

```go
// Fix:
resolved, err := filepath.EvalSymlinks(worktreePath)
if err == nil {
    worktreePath = resolved
}
```

### Stage 8: marker.txt, commit, bead close — NOT REACHED

`marker.txt` still contains only `initial\n`. No new git commit. Bead `smoke-97j` manually reset to OPEN after daemon kill.

---

## 4. Full hk.log

```
harmonik daemon starting in /var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.hWuuy0DwQc
2026/05/14 00:49:30 INFO daemon_pane_write session_id=019e2576-4c9c-726d-b96a-7391c235abdb pane_target=%1966 buffer_name=harmonik-019e2576-4c9c-726d-b96a-7391c235abdb-task purpose=task payload_bytes=47
daemon: workloop: waitAgentReady bead smoke-97j run 019e2576-4bef-7d1b-9d47-340b131be88f: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
2026/05/14 00:50:02 INFO daemon_pane_write session_id=019e2576-ca57-796e-a4ee-b816e7bcef19 pane_target=%1967 buffer_name=harmonik-019e2576-ca57-796e-a4ee-b816e7bcef19-task purpose=task payload_bytes=47
daemon: workloop: waitAgentReady bead smoke-97j run 019e2576-c9a1-781a-ad1f-2c3b1fcbb49c: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
2026/05/14 00:50:34 INFO daemon_pane_write session_id=019e2577-4815-744d-932c-e71e55c871f5 pane_target=%1968 buffer_name=harmonik-019e2577-4815-744d-932c-e71e55c871f5-task purpose=task payload_bytes=47
daemon: workloop: waitAgentReady bead smoke-97j run 019e2577-475e-7762-b6cf-4e16f336f3ec: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
2026/05/14 00:51:07 INFO daemon_pane_write session_id=019e2577-c5e1-73eb-b17a-84b521bfaf17 pane_target=%1969 buffer_name=harmonik-019e2577-c5e1-73eb-b17a-84b521bfaf17-task purpose=task payload_bytes=47
```

No paste-inject errors (contrast v7: `paste-buffer exited 1: can't find pane`). No panics. Daemon cycles through HC-056 retries indefinitely.

---

## 5. Event summary (25 total events across 4 retry cycles)

| Event type | Count |
|---|---|
| daemon_started | 1 |
| daemon_orphan_sweep_completed | 1 |
| run_started | 4 |
| handler_capabilities | 4 |
| session_log_location | 4 |
| skills_provisioned | 4 |
| launch_initiated | 4 |
| run_failed | 3 (4th cycle still in progress at kill) |

No `agent_ready` events. Each `run_failed` has `summary: "agent_ready_timeout"`.

---

## 6. Success criteria checklist

| Criterion | Result |
|-----------|--------|
| 1. No panic / SIGSEGV | PASS — no crash |
| 2. Tmux window spawned for Claude | PASS — `launch_initiated` emitted, tmux window visible |
| 3. `harmonik` absolute path in hooks (kqdpf.6) | PASS — `/tmp/hk` in settings.json |
| 4. agent-task.md written before launch (hk-9ow36) | PASS — file present with correct content |
| 5. EnsureWorktreeTrust called (hk-lj1p9.3) | PASS — called; trust entry written |
| 6. Synthetic agent_ready removed from pre-exec (hk-1rocd) | PASS — no pre-exec `agent_ready` event |
| 7. daemon.sock bound for hook-relay (hk-tjl40) | PASS — socket file exists, lsof confirms listener |
| 8. sess.Wait() returns after Kill (hk-smuku) | PASS — not triggered this run; confirmed in v6 |
| 9. workloop dispatches retry after HC-056 (hk-smuku) | PASS — HC-056 correctly triggers reopen + retry |
| 10. hook-relay socket connects to daemon | NOT REACHED — Claude never completes init (trust dialog) |
| 11. agent_ready via SessionStart hook relay | NOT REACHED — trust dialog blocks SessionStart |
| **12. pasteInjectOnLaunch uses stable pane ID (hk-yngq2)** | **PASS** — `pane_target=%1966` confirms `%NNNN` form; no error |
| **13. paste message delivered to Claude pane** | **PASS** — "Please read .harmonik/agent-task.md and begin." in pane |
| 14. Trust dialog bypassed (hk-lj1p9.3) | FAIL — trust entry at wrong path key (`/var/folders` vs `/private/var/folders`) |
| 15. Claude reads .harmonik/agent-task.md | FAIL — not reached (trust dialog blocks) |
| 16. marker.txt contains SMOKE-OK | FAIL — not reached |
| 17. Commit with SMOKE-OK on worktree branch | FAIL — not reached |
| 18. Bead closed with reason `done` | FAIL — not reached |

---

## 7. What the hk-yngq2 fix confirms

**hk-yngq2 is confirmed working.** The fix stores the pane ID (`%NNNN` from `tmux display-message -p -t <handle> '#{pane_id}'`) at spawn time and uses it as the `-t` target in `WriteLastPane`.

- v7 logged: `paste-buffer exited 1: can't find pane: kjv2d1cswF/.harmonik/worktrees/019e2565-8fac-79f2-9a75-7b16ea836170.0`
- v8 logs: `INFO daemon_pane_write ... pane_target=%1966` — no error

The paste-inject message "Please read .harmonik/agent-task.md and begin." is visible as the first line of the Claude pane. Delivery works end-to-end.

All prior umbrella deliverables remain confirmed:
1. **hk-9ow36 (agent-task.md):** Written correctly before tmux window creation.
2. **hk-1rocd (relay-synthesized agent_ready):** No synthetic `agent_ready` in pre-exec events.
3. **CHB-028 (agent-task.md overwrite per launch):** MkdirAll + atomic write confirmed.
4. **hk-tjl40 (daemon.sock bound):** Socket present and listening.
5. **hk-smuku (sess.Wait deadlock):** Not triggered; confirmed in v6. HC-056 retry path works correctly.
6. **hk-lj1p9.4 (SetAgentReadyCallback + pasteInjectOnLaunch wiring):** Confirmed in v7; `%NNNN` pane ID confirms call path reaches WriteLastPane without error.
7. **hk-yngq2 (stable pane ID):** Confirmed in v8 (this run).

---

## 8. Root cause: EnsureWorktreeTrust path not canonicalized via filepath.EvalSymlinks

### Symptom

Trust dialog shows every run. `~/.claude.json["projects"]` has an entry for the worktree path but Claude Code cannot find it because the key uses the symlink form of the path.

### Path mismatch

macOS: `/var/folders` is a symlink → `/private/var/folders`.

- `EnsureWorktreeTrust` receives `worktreePath` from the daemon (constructed via `cfg.ProjectDir + "/.harmonik/worktrees/" + runID`). `cfg.ProjectDir` was passed as `--project /var/folders/...` — the symlink form.
- The function writes the key as-is: `"/var/folders/s9/kq5h9q_.../worktrees/<run_id>"`.
- Claude Code resolves its working directory via `realpath` on startup and stores `"/private/var/folders/s9/kq5h9q_.../worktrees/<run_id>"` in its state.
- Lookup fails: `/var/folders/...` ≠ `/private/var/folders/...`.

### Evidence

`~/.claude.json` inspection confirms:
- Entry written: `"/var/folders/.../019e2576-4bef-7d1b-9d47-340b131be88f": {"hasTrustDialogAccepted": true}`
- Claude pane shows: `"Accessing workspace: /private/var/folders/.../019e2576-4bef-7d1b-9d47-340b131be88f"`
- All real (user-accepted) project entries in `~/.claude.json` use `/private/var/...` form.

### Fix scope (hk-o5eww)

`internal/workspace/claudetrust_wm040b.go` — `ensureWorktreeTrustAt` should resolve `worktreePath` via `filepath.EvalSymlinks` before using it as the `projects` map key:

```go
// At top of ensureWorktreeTrustAt, before the lock:
if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
    worktreePath = resolved
}
```

This is a one-line fix. The `filepath.EvalSymlinks` call is available in stdlib; no imports needed beyond what is already present.

---

## 9. Follow-up bead

### hk-o5eww (filed) — EnsureWorktreeTrust uses non-canonical path — trust entry written under /var/folders not /private/var/folders, Claude Code uses realpath

**Type:** bug, **Priority:** P0, **Labels:** subsystem:workspace, subsystem:bridge
**Relation:** related to hk-lj1p9 (dependency type set to `related`)

`EnsureWorktreeTrust` writes `~/.claude.json["projects"][worktreePath]` where `worktreePath` is the raw path passed by the daemon (which may use a symlink form). On macOS, `/var/folders` is a symlink to `/private/var/folders`. Claude Code uses the resolved path as the key. The mismatch means the pre-seeded trust entry is never found, and the interactive trust dialog blocks indefinitely in the daemon-spawned tmux pane.

Fix: call `filepath.EvalSymlinks(worktreePath)` at the start of `ensureWorktreeTrustAt` and use the resolved form as the key.

---

## 10. Cleanup

- Tmux session `smoke-1778744969` killed.
- Daemon (PID 84284) killed via session kill.
- Scratch directory left in place: `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.hWuuy0DwQc`
- Bead `smoke-97j` manually reset to OPEN after daemon kill.

---

*Prior RED run (WriteLastPane slash-path window name): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v7.md`*
*Prior RED run (SetAgentReadyCallback never wired): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v6.md`*
*Prior RED run (sess.Wait deadlock): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v5.md`*
*Prior RED run (socket never bound): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v4.md`*
*Prior RED run (no task delivery): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v3.md`*
*Prior RED run (harmonik PATH): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v2.md`*
*Prior RED run (nil-watcher panic): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate.md`*
