# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate (v9, post hk-o5eww trust-path fix)

**Verdict:** RED — hk-o5eww trust-path fix confirmed (trust dialog no longer blocks); `agent_ready` arrives T+0.80s (sub-second, no HC-056); but paste-inject message sits unsubmitted in Claude's input bar — the `\n` in the paste payload was swallowed by the welcome splash before the REPL became interactive (new gap: hk-zchbu)
**Bead:** smoke-w3e (scratch repo)
**Follow-up filed:** hk-zchbu (P0) — `pasteInjectOnLaunch fires before TUI ready — paste message not submitted (\n swallowed by welcome splash)`
**Date:** 2026-05-14 (UTC-7)
**Runner:** automated smoke agent
**Procedure:** mirrors v8 (`docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v8.md`)

**Prior runs:**
- v1 RED: nil watcher panic → fixed hk-e2kwq (9dfd7e8)
- v2 RED: `harmonik` not on PATH in hook commands → fixed hk-kqdpf.6 (96de004)
- v3 RED: no task delivery — Claude launched idle, no CLAUDE.md, no `--print`
- v4 RED: socket never bound — `RunSocketListener` implemented but never called from composition root (hk-tjl40)
- v5 RED: socket bound, but `sess.Wait()` deadlocks — tmux window-name target resolves to session active pane after `Kill` (hk-smuku)
- v6 RED: `SetAgentReadyCallback` never called in workloop — hook-relay connects and sends `agent_ready` to socket, but callback is nil, `tapCh` never receives the event, HC-056 fires every 30s (hk-lj1p9.4)
- v7 RED: `agent_ready` arrives 0.57s after launch (PRIMARY FIX CONFIRMED); `pasteInjectOnLaunch` fires but `WriteLastPane` errors — tmux can't resolve `session:worktree-path.0` pane target when window name is a full filesystem path containing slashes (hk-yngq2)
- v8 RED: hk-yngq2 fix confirmed — `WriteLastPane` now uses `%NNNN` pane ID; paste-inject succeeds and delivers kick-off message to input bar; but trust dialog blocks because `EnsureWorktreeTrust` uses non-canonical path `/var/folders` vs. Claude's `realpath`-resolved `/private/var/folders` (hk-o5eww)
- v9 RED (this run): hk-o5eww fix confirmed — trust entry written under `/private/var/folders/...` key; no trust dialog; `agent_ready` T+0.80s; but paste `\n` swallowed by welcome splash before REPL ready — message sits in input bar unsubmitted (hk-zchbu)

---

## 1. Setup

### Git verification

```
git log --oneline -6

988dabb chore(beads): sync — close hk-o5eww
e8fd1df fix(workspace): EvalSymlinks worktreePath before trust key (hk-o5eww)
1b81e5e docs(smoke): dogfood-smoke-run v8 2026-05-13 — RED: hk-yngq2 pane-ID fix confirmed, trust path canonicalization missing (hk-o5eww)
56f5ce6 chore(beads): sync — close hk-yngq2
ff79246 fix(daemon+tmux): use stable pane ID for WriteLastPane pane target (hk-yngq2)
d968b76 docs(smoke): dogfood-smoke-run v7 2026-05-13 — RED: agent_ready confirmed T+0.57s, paste-inject WriteLastPane fails on slash-path window name (hk-yngq2)
```

HEAD is `988dabb`. Both `e8fd1df` (hk-o5eww EvalSymlinks fix) and `988dabb` (beads sync) present. PASS.

### Preconditions

- `claude --version`: `2.1.141 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br --version`: `br 0.1.45` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS
- `go build -o /tmp/hk ./cmd/harmonik`: exit 0, rebuilt from HEAD, includes hk-o5eww EvalSymlinks fix — PASS

### Scratch directory

```
SMOKE_DIR=/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.9iLKLku3N9
```

- `git init` + `git config user.email smoke@harmonik.local` + `git config user.name "Smoke Runner"` — PASS
- Initial commit: `README.md` + `marker.txt` (`initial\n`) — SHA `b0a55b1` — PASS
- `br init --prefix smoke` — PASS
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels "workflow:single"` → **smoke-w3e**

---

## 2. Run

### Tmux session launch

```
tmux new-session -d -s smoke-1778745322 -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    2>&1 | tee $SMOKE_DIR/hk.log; sleep 300"
```

Session: `smoke-1778745322`.

---

## 3. Stage-by-stage analysis

### Stage 1: daemon_started — PASS

```json
{"type":"daemon_started","payload":{"binary_commit_hash":"unknown","pid":85212,"started_at":"2026-05-14T07:55:23Z"}}
```

Clean start, no crash.

### Stage 2: daemon_orphan_sweep_completed — PASS

```json
{"type":"daemon_orphan_sweep_completed","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-14T07:55:23Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
```

Clean sweep.

### Stage 3: run_started — PASS

```json
{"type":"run_started","payload":{"bead_id":"smoke-w3e","run_id":"019e257b-af70-7461-b047-829233679e12","started_at":"2026-05-14T07:55:23Z","workspace_path":".../.harmonik/worktrees/019e257b-af70-7461-b047-829233679e12"}}
```

Bead claimed, worktree created.

### Stage 4: handler_capabilities, session_log_location, skills_provisioned — PASS

All three pre-exec messages emitted correctly. Claude session ID `019e257b-b029-7710-a376-d0e99634cc19` minted.

### Stage 4a: .claude/settings.json materialization — PASS

All five bridge hooks materialized with absolute binary path `/tmp/hk`.

### Stage 4b: agent-task.md materialization — PASS

`$WORKTREE/.harmonik/agent-task.md` written with correct content:

```
bead_id: smoke-w3e
title: Add SMOKE-OK marker line to marker.txt and commit
run_id: 019e257b-af70-7461-b047-829233679e12
workspace_path: /var/folders/.../.harmonik/worktrees/019e257b-af70-7461-b047-829233679e12
```

### Stage 4c: trust pre-seed — PASS (hk-o5eww fix confirmed)

**This is the primary fix under test for v9.** `EnsureWorktreeTrust` now calls `filepath.EvalSymlinks` on the worktree path before writing the `~/.claude.json["projects"]` key. The canonical form (`/private/var/folders/...`) matches what Claude Code uses after its internal `realpath` resolution.

**Confirmed by inspection of `~/.claude.json`:**

```json
"/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.9iLKLku3N9/.harmonik/worktrees/019e257b-af70-7461-b047-829233679e12": {
  "hasTrustDialogAccepted": true
}
```

Entry is present under `/private/var/...` key (canonicalized form). Claude Code finds the entry and skips the trust dialog.

**Contrast with v8:** in v8 the entry was keyed under `/var/folders/...` (non-canonical) and Claude showed the trust dialog every retry. In v9 the entry is under `/private/var/folders/...` and no trust dialog appears. **hk-o5eww is confirmed fixed.**

### Stage 5: launch_initiated — PASS

```json
{"type":"launch_initiated","payload":{"claude_session_id":"019e257b-b029-7710-a376-d0e99634cc19",...}}
```

Tmux window 2 created; Claude pane visible at worktree path.

### Stage 6: daemon.sock bound — PASS

Socket file present at `$SMOKE_DIR/.harmonik/daemon.sock`.

### Stage 7: pasteInjectOnLaunch — PASS (text delivered); submit FAIL (new gap: hk-zchbu)

The `hk.log` shows:

```
2026/05/14 00:55:23 INFO daemon_pane_write session_id=019e257b-b029-7710-a376-d0e99634cc19 pane_target=%1971 buffer_name=harmonik-019e257b-b029-7710-a376-d0e99634cc19-task purpose=task payload_bytes=47
```

No error from `WriteLastPane`. `pane_target=%1971` — stable `%NNNN` form (hk-yngq2 fix still working).

**tmux pane capture shows:**

```
Please read .harmonik/agent-task.md and begin.
╭─── Claude Code v2.1.141 ────────────────────────────────────────────────────────────────────╮
│                 Welcome back Greg!                 │ Tips for getting started                │
│  ...welcome splash...                                                                        │
╰──────────────────────────────────────────────────────────────────────────────────────────────╯
─────────────────────────────────────────────────────────────────────────────────────────────────
❯ Please read .harmonik/agent-task.md and begin.
─────────────────────────────────────────────────────────────────────────────────────────────────
```

Two observations:
1. `Please read .harmonik/agent-task.md and begin.` appears as a bare line ABOVE the splash — this is the text that arrived at the pane terminal before the TUI finished rendering (pre-splash raw terminal output).
2. The same text appears in the `❯` input bar BELOW the splash — the TUI captured the pasted text as input buffer content.
3. **The message is NOT submitted.** The cursor block (`^[[7m ^[[0m`) is after the text, indicating the REPL is waiting for Enter.

**Root cause:** `pasteInjectOnLaunch` fires in workloop step 4b, immediately after spawn (goroutine), before `waitAgentReady` (step 6). The paste payload includes a trailing `\n`. The `\n` arrived while the welcome splash was still being rendered. The Claude Code TUI's welcome splash consumes key events (including `\n`) for its own interaction before switching to the REPL input state. When the TUI transitions to the REPL state it shows the buffered text (without `\n`) in the input bar, but the `\n` was already consumed. The REPL waits for another Enter to submit.

**Distinction from v8:** In v8 the paste delivered the text visibly (confirmed by `%NNNN` pane target working), but the trust dialog was covering the TUI. In v9 the trust dialog is gone — the TUI initializes correctly — but now the timing gap is exposed: paste fires at spawn time, `\n` is consumed by the welcome splash, text sits in input bar unsubmitted.

### Stage 8: agent_ready — PASS (T+0.80s)

```json
{"type":"agent_ready","timestamp_wall":"2026-05-14T00:55:24.042746-07:00","payload":null}
```

`agent_ready` at 00:55:24, daemon started and pane spawned at 00:55:23. Delta: **+0.804s** — sub-second, well within HC-056 30s timeout. **SessionStart hook relay works correctly.** No HC-056 fired during the entire observation window.

### Stage 9: workloop hangs waiting for bead close — FAIL

After `agent_ready`, the daemon considers the run active. The Claude pane sits at the REPL with the unsubmitted message in the input bar. No new events are emitted. The daemon waits indefinitely for the bead to be closed (no timeout for the post-agent_ready execution phase — only HC-056 guards the agent_ready window, not the full execution). Session killed manually after ~4 minutes observation.

### Stage 10: marker.txt, commit, bead close — NOT REACHED

`marker.txt` still contains only `initial\n`. No new git commit in worktree. Bead `smoke-w3e` remains open.

---

## 4. Full hk.log

```
harmonik daemon starting in /var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.9iLKLku3N9
2026/05/14 00:55:23 INFO daemon_pane_write session_id=019e257b-b029-7710-a376-d0e99634cc19 pane_target=%1971 buffer_name=harmonik-019e257b-b029-7710-a376-d0e99634cc19-task purpose=task payload_bytes=47
```

No paste-inject errors. No HC-056 timeouts. No panics. Single run (no retries). Daemon sat live waiting for bead close.

---

## 5. Event timeline (8 total events, single run)

| # | Event type | Timestamp (wall, local) |
|---|---|---|
| 1 | daemon_started | 00:55:23.238 |
| 2 | daemon_orphan_sweep_completed | 00:55:23.347 |
| 3 | run_started | 00:55:23.557 |
| 4 | handler_capabilities | 00:55:23.605 |
| 5 | session_log_location | 00:55:23.605 |
| 6 | skills_provisioned | 00:55:23.605 |
| 7 | launch_initiated | 00:55:23.605 |
| 8 | agent_ready | 00:55:24.043 — **T+0.804s** |

No `run_failed`. No `HC-056`. No `bead_closed`. Single clean run, no retries.

---

## 6. Success criteria checklist

| Criterion | Result |
|-----------|--------|
| 1. No panic / SIGSEGV | PASS — no crash |
| 2. Tmux window spawned for Claude | PASS — `launch_initiated` emitted, tmux window visible |
| 3. `harmonik` absolute path in hooks (kqdpf.6) | PASS — `/tmp/hk` in settings.json |
| 4. agent-task.md written before launch (hk-9ow36) | PASS — file present with correct content |
| 5. EnsureWorktreeTrust called (hk-lj1p9.3) | PASS — called; trust entry written |
| **6. Trust entry uses canonical path (hk-o5eww)** | **PASS** — `~/.claude.json` key is `/private/var/folders/...` (canonicalized via `filepath.EvalSymlinks`) |
| **7. No trust dialog (hk-o5eww)** | **PASS** — pane shows Claude REPL welcome splash, no trust prompt; Claude Code found the entry |
| 8. Synthetic agent_ready removed from pre-exec (hk-1rocd) | PASS — no pre-exec `agent_ready` event |
| 9. daemon.sock bound for hook-relay (hk-tjl40) | PASS — socket file exists, lsof confirms listener |
| 10. sess.Wait() returns after Kill (hk-smuku) | NOT TRIGGERED — session killed manually |
| 11. hook-relay socket connects to daemon | PASS — SessionStart hook fired; `agent_ready` received |
| **12. agent_ready via SessionStart hook relay** | **PASS** — T+0.804s, well under HC-056 30s deadline |
| 13. No HC-056 retry cycle | PASS — single run, no timeouts, no restarts |
| 14. pasteInjectOnLaunch uses stable pane ID (hk-yngq2) | PASS — `pane_target=%1971` (`%NNNN` form) |
| 15. Paste message text delivered to Claude pane | PASS — text visible in `❯` input bar |
| **16. Paste message submitted (Enter triggers execution)** | **FAIL** — `\n` swallowed by welcome splash; message sits unsubmitted in REPL input bar (hk-zchbu) |
| 17. Claude reads .harmonik/agent-task.md | FAIL — not reached (unsubmitted input) |
| 18. marker.txt contains SMOKE-OK | FAIL — not reached |
| 19. Commit with SMOKE-OK on worktree branch | FAIL — not reached |
| 20. Bead closed with reason `done` | FAIL — not reached |

---

## 7. What the hk-o5eww fix confirms

**hk-o5eww is confirmed fixed.** `EnsureWorktreeTrust` now canonicalizes the worktree path via `filepath.EvalSymlinks` before writing the `~/.claude.json["projects"]` map key. The canonical key (`/private/var/folders/...`) matches Claude Code's internal resolution and the trust entry is found on startup.

Evidence:
- v8: `~/.claude.json` had `/var/folders/...` key → trust dialog shown every run
- v9: `~/.claude.json` has `/private/var/folders/...` key → no trust dialog; Claude REPL welcome splash shown correctly
- `agent_ready` fires at T+0.804s (not blocked by trust dialog prompt)

All prior umbrella deliverables remain confirmed:
1. **hk-9ow36 (agent-task.md):** Written correctly before tmux window creation.
2. **hk-1rocd (relay-synthesized agent_ready):** No synthetic `agent_ready` in pre-exec events.
3. **CHB-028 (agent-task.md overwrite per launch):** MkdirAll + atomic write confirmed.
4. **hk-tjl40 (daemon.sock bound):** Socket present and listening.
5. **hk-smuku (sess.Wait deadlock):** Not triggered; confirmed in v6.
6. **hk-lj1p9.4 (SetAgentReadyCallback + pasteInjectOnLaunch wiring):** `agent_ready` T+0.804s confirms full relay path working.
7. **hk-yngq2 (stable pane ID):** `pane_target=%1971` confirms `%NNNN` form; no error.
8. **hk-o5eww (trust path canonicalization):** Canonical `/private/var/...` key in `~/.claude.json`; no trust dialog. **Confirmed in v9 (this run).**

---

## 8. Root cause: paste-inject fires before Claude TUI is ready to accept submitted input

### Symptom

Kick-off message appears in the `❯` input bar but is not submitted. Claude never reads `agent-task.md` or performs any work.

### Timing sequence

| Time (approx) | Event |
|---|---|
| T+0.000s | Daemon calls `SpawnWindow` → tmux window created |
| T+0.000s | `pasteInjectOnLaunch` goroutine starts (step 4b) |
| T~0.001s | `tmux load-buffer` + `paste-buffer` executes → payload (text + `\n`) injected into pane |
| T~0.001s | Claude Code TUI is mid-initialization (welcome splash rendering) |
| T~0.001s | `\n` consumed by splash input handler (dismissed as keypress before REPL active) |
| T~0.100s | Welcome splash fully rendered; REPL input state active |
| T~0.100s | Buffered text (without `\n`) appears in `❯` input bar |
| T+0.804s | `SessionStart` hook fires → `agent_ready` received by daemon |
| T+∞ | REPL waits for user Enter; no further progress |

### Evidence

1. Pane captures show text in `❯` bar with cursor block at end (awaiting Enter).
2. The same text appears as a raw terminal line ABOVE the splash (pre-TUI render, before Claude took over the terminal).
3. No Claude session log directory created in `~/.claude/projects/` — Claude never started a session or processed any message.
4. Single `daemon_pane_write` event in events.jsonl; no subsequent hook events (no `PostToolUse`, no `Stop`).

### Fix scope (hk-zchbu)

The paste-inject must fire AFTER the Claude TUI is ready to accept submitted input. The reliable signal for this is `agent_ready` (emitted by the `SessionStart` hook, which fires when Claude's session has initialized and the REPL is ready). 

Current ordering (workloop):
- Step 4b: `go pasteInjectOnLaunch(...)` — paste fires at spawn
- Step 6: `waitAgentReady(...)` — daemon waits for `agent_ready`

Required ordering:
- Step 6: `waitAgentReady(...)` — daemon waits for `agent_ready`
- Step 6a: `pasteInjectOnLaunch(...)` — paste fires AFTER `agent_ready` confirmed

The paste should move to after `waitAgentReady` returns nil. A small delay (e.g. 200ms) may additionally be needed after `agent_ready` to ensure the TUI has fully transitioned to REPL input state, but the primary fix is the ordering.

---

## 9. Follow-up bead

### hk-zchbu (filed) — pasteInjectOnLaunch fires before TUI ready — paste message not submitted (\n swallowed by welcome splash)

**Type:** bug, **Priority:** P0, **Labels:** subsystem:daemon, subsystem:bridge
**Relation:** related to hk-lj1p9

`pasteInjectOnLaunch` fires in workloop step 4b (goroutine, immediately after spawn), before `waitAgentReady` (step 6). The paste payload includes a trailing `\n` to trigger submission. The `\n` arrives while the Claude Code welcome splash is still rendering; the splash input handler consumes it as a keypress before the REPL input state is active. When the REPL becomes active it shows the buffered text in the `❯` input bar without the `\n`, waiting for another Enter that never comes.

Fix: move `pasteInjectOnLaunch` to after `waitAgentReady` returns nil (step 6 → step 6a). Optionally add a short delay (100–200ms) to let the TUI fully transition to REPL state after SessionStart.

---

## 10. Cleanup

- Tmux session `smoke-1778745322` killed.
- Daemon (PID 85212) killed via session kill.
- Scratch directory left in place: `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.9iLKLku3N9`
- Bead `smoke-w3e` remains open (not reached completion).

---

*Prior RED run (trust path canonicalization): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v8.md`*
*Prior RED run (WriteLastPane slash-path window name): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v7.md`*
*Prior RED run (SetAgentReadyCallback never wired): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v6.md`*
*Prior RED run (sess.Wait deadlock): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v5.md`*
*Prior RED run (socket never bound): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v4.md`*
*Prior RED run (no task delivery): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v3.md`*
*Prior RED run (harmonik PATH): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v2.md`*
*Prior RED run (nil-watcher panic): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate.md`*
