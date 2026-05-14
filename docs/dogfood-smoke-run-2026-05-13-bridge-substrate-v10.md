# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate (v10, post hk-zchbu paste-inject reorder)

**Verdict:** RED — hk-zchbu ordering fix confirmed partially: `pasteInjectOnLaunch` now fires AFTER `agent_ready` (code ordering correct), but `SessionStart` fires while the welcome splash is still rendering; the trailing `\n` is still consumed by the splash's input handler before the REPL input state is active. Message sits unsubmitted in `❯` bar. Additionally, `dangerouslySkipPermissions` is absent from worktree settings.json — Claude prompts for every file edit and shell command (would block unattended operation even if paste-inject were fixed). Two new beads filed.
**Bead:** smoke-dq6 (scratch repo)
**Follow-up filed:**
- hk-rf4ux (P0) — `pasteInjectOnLaunch \n still swallowed — agent_ready fires during splash, not after REPL ready` (partial fix gap)
- hk-53y35 (P1) — `worktree settings.json missing dangerouslySkipPermissions — Claude prompts for every edit and shell command`
**Date:** 2026-05-14 (UTC-7)
**Runner:** automated smoke agent
**Procedure:** mirrors v9 (`docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v9.md`)

**Prior runs:**
- v1 RED: nil watcher panic → fixed hk-e2kwq (9dfd7e8)
- v2 RED: `harmonik` not on PATH in hook commands → fixed hk-kqdpf.6 (96de004)
- v3 RED: no task delivery — Claude launched idle, no CLAUDE.md, no `--print`
- v4 RED: socket never bound — `RunSocketListener` implemented but never called from composition root (hk-tjl40)
- v5 RED: socket bound, but `sess.Wait()` deadlocks — tmux window-name target resolves to session active pane after `Kill` (hk-smuku)
- v6 RED: `SetAgentReadyCallback` never called in workloop — hook-relay connects and sends `agent_ready` to socket, but callback is nil, `tapCh` never receives the event, HC-056 fires every 30s (hk-lj1p9.4)
- v7 RED: `agent_ready` arrives 0.57s after launch (PRIMARY FIX CONFIRMED); `pasteInjectOnLaunch` fires but `WriteLastPane` errors — tmux can't resolve `session:worktree-path.0` pane target when window name is a full filesystem path containing slashes (hk-yngq2)
- v8 RED: hk-yngq2 fix confirmed — `WriteLastPane` now uses `%NNNN` pane ID; paste-inject succeeds and delivers kick-off message to input bar; but trust dialog blocks because `EnsureWorktreeTrust` uses non-canonical path `/var/folders` vs. Claude's `realpath`-resolved `/private/var/folders` (hk-o5eww)
- v9 RED: hk-o5eww fix confirmed — trust entry written under `/private/var/folders/...` key; no trust dialog; `agent_ready` T+0.80s; but paste `\n` swallowed by welcome splash before REPL ready — message sits in input bar unsubmitted (hk-zchbu)
- v10 RED (this run): hk-zchbu ordering fix confirmed at code level — paste-inject now fires AFTER `waitAgentReady` returns; but `SessionStart` fires during splash initialization, not after REPL is ready to accept submitted input; `\n` still consumed by splash (hk-rf4ux)

---

## 1. Setup

### Git verification

```
git log --oneline -6

e37d0ca chore(beads): sync — close hk-zchbu
ff2bc6f fix(daemon): paste-inject AFTER agent_ready, not before (hk-zchbu)
daf9ab0 docs(smoke): dogfood-smoke-run v9 2026-05-13 — RED: hk-o5eww trust-path fix confirmed, paste \n swallowed by welcome splash (hk-zchbu)
988dabb chore(beads): sync — close hk-o5eww
e8fd1df fix(workspace): EvalSymlinks worktreePath before trust key (hk-o5eww)
1b81e5e docs(smoke): dogfood-smoke-run v8 2026-05-13 — RED: hk-yngq2 pane-ID fix confirmed, trust path canonicalization missing (hk-o5eww)
```

HEAD is `e37d0ca`. Both `ff2bc6f` (hk-zchbu paste-inject reorder) and `e37d0ca` (beads sync) present. PASS.

### Preconditions

- `claude --version`: `2.1.141 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br --version`: `br 0.1.45` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS
- `go build -o /tmp/hk ./cmd/harmonik`: exit 0, rebuilt from HEAD, includes hk-zchbu paste-inject reorder — PASS

### Scratch directory

```
SMOKE_DIR=/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.cyQYq3idv7
```

- `git init` + `git config user.email smoke@harmonik.local` + `git config user.name "Smoke Runner"` — PASS
- Initial commit: `README.md` + `marker.txt` (`initial\n`) — SHA `0b2485d` — PASS
- `br init --prefix smoke` — PASS
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels "workflow:single"` → **smoke-dq6**

---

## 2. Run

### Tmux session launch

```
tmux new-session -d -s smoke-1778745939 -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    2>&1 | tee $SMOKE_DIR/hk.log; sleep 300"
```

Session: `smoke-1778745939`.

---

## 3. Stage-by-stage analysis

### Stage 1: daemon_started — PASS

```json
{"type":"daemon_started","payload":{"binary_commit_hash":"unknown","pid":89886,"started_at":"2026-05-14T08:05:40Z"}}
```

Clean start, no crash.

### Stage 2: daemon_orphan_sweep_completed — PASS

```json
{"type":"daemon_orphan_sweep_completed","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-14T08:05:40Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
```

Clean sweep.

### Stage 3: run_started — PASS

```json
{"type":"run_started","payload":{"bead_id":"smoke-dq6","run_id":"019e2585-1a77-7595-8e25-f4c4bfb6aa8f","started_at":"2026-05-14T08:05:40Z","workspace_path":".../.harmonik/worktrees/019e2585-1a77-7595-8e25-f4c4bfb6aa8f"}}
```

Bead claimed, worktree created.

### Stage 4: handler_capabilities, session_log_location, skills_provisioned — PASS

All three pre-exec messages emitted correctly. Claude session ID `019e2585-1b28-77ef-8876-3a5ba48a0144` minted.

### Stage 4a: .claude/settings.json materialization — PARTIAL PASS / GAP NOTED

All five bridge hooks materialized with absolute binary path `/tmp/hk`. However, `dangerouslySkipPermissions` is absent from the generated settings.json (see Stage 9 below). Only hooks; no permissions section.

### Stage 4b: agent-task.md materialization — PASS

`$WORKTREE/.harmonik/agent-task.md` written with correct content:

```
bead_id: smoke-dq6
title: Add SMOKE-OK marker line to marker.txt and commit
run_id: 019e2585-1a77-7595-8e25-f4c4bfb6aa8f
workspace_path: /var/folders/.../.harmonik/worktrees/019e2585-1a77-7595-8e25-f4c4bfb6aa8f
```

### Stage 4c: trust pre-seed — PASS (hk-o5eww fix still confirmed)

`~/.claude.json["projects"]` key written under `/private/var/folders/...` canonical form. No trust dialog shown. Consistent with v9.

### Stage 5: launch_initiated — PASS

```json
{"type":"launch_initiated","payload":{"claude_session_id":"019e2585-1b28-77ef-8876-3a5ba48a0144",...}}
```

Tmux window 2 created; Claude pane visible at worktree path.

### Stage 6: daemon.sock bound — PASS

Socket file present at `$SMOKE_DIR/.harmonik/daemon.sock`.

### Stage 7: agent_ready — PASS (T+1.133s)

```json
{"type":"agent_ready","timestamp_wall":"2026-05-14T01:05:41.598786-07:00","payload":null}
```

`agent_ready` at 01:05:41.598786, daemon started at 01:05:40.465. Delta: **+1.133s** — sub-second window (HC-056 30s timeout). No HC-056 fired. `SessionStart` hook relay works correctly.

### Stage 8: pasteInjectOnLaunch AFTER agent_ready — PARTIAL PASS / \n still swallowed (hk-rf4ux)

**Code ordering is now correct (hk-zchbu fix confirmed at code level).** The `go pasteInjectOnLaunch` call is at step 6a in `workloop.go`, which is textually and execution-order after `waitAgentReady` returns. The `daemon_pane_write` log entry confirms the goroutine fired at `01:05:41` (same second as `agent_ready` at .598, so at ≥T+1.133s).

**However, the same symptom persists:** pane capture shows `❯ Please read .harmonik/agent-task.md and begin.` in the input bar — text present, not submitted.

```
╭─── Claude Code v2.1.141 ────────────────────────────────╮
│           Welcome back Greg!       │  Tips for getting started  │
│                  ▐▛███▜▌                                        │
│                 ▝▜█████▛▘                                       │
╰─────────────────────────────────────────────────────────────────╯
❯ Please read .harmonik/agent-task.md and begin.
```

**Root cause refinement (hk-rf4ux):** `SessionStart` fires when Claude's session initialises — this is during TUI render, not after the welcome splash is dismissed and the REPL is in ready-to-submit state. The splash appears to be an overlay that occupies the pane terminal input stream during its lifetime. Even after `agent_ready`, the splash is still displayed and still consuming keypresses. The paste fires after `agent_ready` (correct ordering), but the splash is still up at that moment, so `\n` is consumed by the splash's keypress handler rather than the REPL.

**Evidence:** The splash remained on screen 15+ seconds after `agent_ready`. When a manual Enter was sent, Claude immediately began processing — confirming the REPL was ready, the splash just needed dismissal. The paste-inject `\n` was either consumed by the splash before the REPL state was active, or not delivered in a way that the splash treated as a dismiss+submit sequence.

**Fix scope (hk-rf4ux):** A delay of 500ms–1s after `agent_ready` before paste fires may allow the splash to clear, OR a `PreToolUse` hook event (emitted when Claude actually begins a tool call) could serve as a more reliable "REPL ready" signal, OR adding `--dangerously-skip-permissions` to the `claude` launch command bypasses the welcome splash entirely.

### Stage 9: dangerouslySkipPermissions absent — NEW GAP (hk-53y35)

With manual Enter delivered to unstick the run, Claude read `agent-task.md` and attempted to edit `marker.txt`. The worktree `settings.json` has no `permissions` section and no `dangerouslySkipPermissions: true`. Claude Code presented interactive confirmation dialogs for:
1. **File edit** — `Do you want to make this edit to marker.txt?`
2. **Shell command** — `Do you want to proceed? git add marker.txt && git commit -m "..."`

Both required human interaction to proceed. In an unattended smoke run (no human at the terminal), the daemon would wait indefinitely at these prompts. This is a second blocking gap for fully autonomous operation.

**Fix scope (hk-53y35):** Add `"dangerouslySkipPermissions": true` to the worktree `settings.json` generated by `WriteSettingsJSON`, or pass `--dangerously-skip-permissions` to the `claude` invocation.

### Stage 10: task execution (with manual assistance) — CONDITIONAL PASS

After manual Enter (to unblock paste-inject) and manual dialog accepts (to unblock permissions), Claude completed the task correctly:

```
⏺ Update(marker.txt)
  ⎿  Added 1 line
      1  initial
      2 +SMOKE-OK

⏺ Bash(git add marker.txt && git commit -m "Add SMOKE-OK marker line to marker.txt")
  ⎿  [run/019e2585-1a77-7595-8e25-f4c4bfb6aa8f 7e27f7d] Add SMOKE-OK marker line to marker.txt
      1 file changed, 1 insertion(+)
```

- `grep SMOKE-OK $WORKTREE/marker.txt` → **SMOKE-OK** — PASS (conditional)
- `git -C $WORKTREE log --oneline` → `7e27f7d Add SMOKE-OK marker line to marker.txt` — PASS (conditional)
- Claude commit SHA: `7e27f7d`

### Stage 11: bead_closed — NOT REACHED

After task completion Claude sat at the idle REPL (`❯`). The `Stop` hook never fired because Claude did not exit. The daemon was waiting via `waitWithSocketGrace`. No `run_completed` or `bead_closed` events emitted. Session killed manually.

Note: the smoke task specification does not include a `claude --print` (one-shot) flag or an explicit "then exit" instruction. A third gap: the smoke task description would need to include `/exit` or the daemon needs to invoke `claude` with `--print` for single-shot execution.

---

## 4. Full hk.log

```
harmonik daemon starting in /var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.cyQYq3idv7
2026/05/14 01:05:41 INFO daemon_pane_write session_id=019e2585-1b28-77ef-8876-3a5ba48a0144 pane_target=%1973 buffer_name=harmonik-019e2585-1b28-77ef-8876-3a5ba48a0144-task purpose=task payload_bytes=47
```

No paste-inject errors. No HC-056 timeouts. No panics. Single run. Daemon alive (waiting for bead close) until session killed.

---

## 5. Event timeline (12 total events, single run)

| # | Event type | Timestamp (wall, local) |
|---|---|---|
| 1 | daemon_started | 01:05:40.465 |
| 2 | daemon_orphan_sweep_completed | 01:05:40.569 |
| 3 | run_started | 01:05:40.771 |
| 4 | handler_capabilities | 01:05:40.818 |
| 5 | session_log_location | 01:05:40.818 |
| 6 | skills_provisioned | 01:05:40.818 |
| 7 | launch_initiated | 01:05:40.818 |
| 8 | agent_ready | 01:05:41.598 — **T+1.133s** |
| 9 | agent_heartbeat | 01:10:40.842 — (5m after agent_ready; Claude working via manual trigger) |
| 10 | agent_heartbeat | 01:15:40.843 |
| 11 | agent_heartbeat | 01:20:40.843 |
| 12 | agent_heartbeat | 01:25:40.844 |

No `run_completed`. No `bead_closed`. No `HC-056`. No panics.

---

## 6. Success criteria checklist

| Criterion | Result |
|-----------|--------|
| 1. No panic / SIGSEGV | PASS — no crash |
| 2. Tmux window spawned for Claude | PASS — `launch_initiated` emitted, tmux window visible |
| 3. `harmonik` absolute path in hooks (kqdpf.6) | PASS — `/tmp/hk` in settings.json |
| 4. agent-task.md written before launch (hk-9ow36) | PASS — file present with correct content |
| 5. EnsureWorktreeTrust called (hk-lj1p9.3) | PASS — called; trust entry written |
| 6. Trust entry uses canonical path (hk-o5eww) | PASS — `~/.claude.json` key is `/private/var/folders/...` |
| 7. No trust dialog (hk-o5eww) | PASS — no trust prompt; Claude REPL welcome splash shown correctly |
| 8. Synthetic agent_ready removed from pre-exec (hk-1rocd) | PASS — no pre-exec `agent_ready` event |
| 9. daemon.sock bound for hook-relay (hk-tjl40) | PASS — socket file exists, listener confirmed |
| 10. sess.Wait() returns after Kill (hk-smuku) | NOT TRIGGERED — session killed manually |
| 11. hook-relay socket connects to daemon | PASS — SessionStart hook fired; `agent_ready` received |
| **12. agent_ready via SessionStart hook relay** | **PASS** — T+1.133s, under HC-056 30s deadline |
| 13. No HC-056 retry cycle | PASS — single run, no timeouts, no restarts |
| 14. pasteInjectOnLaunch uses stable pane ID (hk-yngq2) | PASS — `pane_target=%1973` (`%NNNN` form) |
| **15. pasteInjectOnLaunch fires AFTER agent_ready (hk-zchbu code fix)** | **PASS — code ordering correct; goroutine starts after waitAgentReady returns** |
| **16. Paste message submitted (Enter triggers execution)** | **FAIL** — `\n` still swallowed; splash consuming it even though paste fires post-agent_ready (hk-rf4ux) |
| **17. Autonomous operation (no permission dialogs)** | **FAIL** — no `dangerouslySkipPermissions`; Claude prompts for edit + commit (hk-53y35) |
| 18. Claude reads .harmonik/agent-task.md | CONDITIONAL PASS — reached only via manual Enter |
| 19. marker.txt contains SMOKE-OK | CONDITIONAL PASS — present after manual assists; `7e27f7d` |
| 20. Commit with SMOKE-OK on worktree branch | CONDITIONAL PASS — `7e27f7d` present; not fully autonomous |
| 21. Bead closed with reason `done` | FAIL — Stop hook never fired (Claude stayed alive); daemon never received close signal |

---

## 7. What hk-zchbu confirms and what it doesn't

**hk-zchbu is confirmed at code level:** `go pasteInjectOnLaunch` now executes at step 6a in `workloop.go`, after `waitAgentReady` returns nil. The goroutine cannot start before `agent_ready` is received. The `daemon_pane_write` log entry at `01:05:41` is consistent with firing after `agent_ready` at `01:05:41.598`.

**hk-zchbu does NOT solve the submission gap:** The symptom from v9 (text in `❯` bar, not submitted) persists in v10. The root cause is that `SessionStart` fires during TUI initialization (the welcome splash phase), not when the REPL input state is fully active. The splash still consumes the `\n` as a keypress for its own interaction model.

**Root cause refined:** Moving paste after `agent_ready` was necessary but not sufficient. The `agent_ready` signal is "session has started" (SessionStart), not "REPL is ready to submit input." A delay or a different signal is needed.

All prior umbrella deliverables remain confirmed:
1. **hk-9ow36 (agent-task.md):** Written correctly before tmux window creation.
2. **hk-1rocd (relay-synthesized agent_ready):** No synthetic `agent_ready` in pre-exec events.
3. **CHB-028 (agent-task.md overwrite per launch):** MkdirAll + atomic write confirmed.
4. **hk-tjl40 (daemon.sock bound):** Socket present and listening.
5. **hk-smuku (sess.Wait deadlock):** Not triggered; confirmed in v6.
6. **hk-lj1p9.4 (SetAgentReadyCallback + pasteInjectOnLaunch wiring):** `agent_ready` T+1.133s confirms full relay path working.
7. **hk-yngq2 (stable pane ID):** `pane_target=%1973` confirms `%NNNN` form.
8. **hk-o5eww (trust path canonicalization):** No trust dialog. Confirmed v9; still holds v10.
9. **hk-zchbu (paste-inject ordering):** Code ordering correct; paste fires after agent_ready. **Confirmed at code level in v10.** Symptom persists due to refined root cause.

---

## 8. Root cause analysis: two blocking gaps

### Gap 1 (hk-rf4ux, P0): `\n` consumed by welcome splash even post-agent_ready

**Symptom:** `\n` delivered via paste-inject after `agent_ready` still gets consumed by Claude Code's welcome splash instead of the REPL input handler.

**Root cause:** `SessionStart` hook fires during TUI initialization while the welcome splash is rendering. The REPL input state becomes active only after the splash is dismissed. Between `agent_ready` and the REPL-ready state there is a gap (likely 100–300ms) during which the splash's own input handler owns the terminal. The paste `\n` arrives during this gap.

**Observed evidence:**
1. Pane shows splash + text-in-`❯`-bar 15+ seconds after `agent_ready`.
2. Manual `Enter` sent after 15s immediately triggered Claude to begin processing — the REPL was ready, just waiting for Enter.
3. The paste's `\n` must have been consumed by the splash before the REPL's Enter-to-submit binding was active.

**Fix options (hk-rf4ux):**
- **Option A:** Add a fixed delay (500ms–1s) between `agent_ready` and paste-inject. Simple but fragile on slow machines.
- **Option B:** Use `--dangerously-skip-permissions` flag on `claude` invocation — this bypasses the splash entirely (no welcome overlay on headless launch). Eliminates the race permanently.
- **Option C:** Use `PreToolUse` hook event as the "REPL ready" signal instead of `SessionStart`. `PreToolUse` fires only when Claude is actively processing a tool call, but this requires a two-phase approach (send a no-op to trigger PreToolUse, then inject the real message).

### Gap 2 (hk-53y35, P1): Missing `dangerouslySkipPermissions` in worktree settings.json

**Symptom:** Claude Code presents interactive confirmation dialogs for file edits and shell commands. These dialogs block unattended operation indefinitely.

**Root cause:** `WriteSettingsJSON` generates only the bridge hooks section. It does not set `"dangerouslySkipPermissions": true` or a `permissions` allowlist.

**Fix:** Add `"dangerouslySkipPermissions": true` to the generated settings.json, or pass `--dangerously-skip-permissions` as a CLI flag to the `claude` invocation. This is mandatory for autonomous operation. Note: Option B from gap 1 (`--dangerously-skip-permissions` flag) would address both gaps simultaneously.

---

## 9. Follow-up beads

### hk-rf4ux (filed) — pasteInjectOnLaunch \n still swallowed — agent_ready fires during splash, not after REPL ready

**Type:** bug, **Priority:** P0, **Labels:** subsystem:daemon, subsystem:bridge
**Relation:** related to hk-lj1p9

`pasteInjectOnLaunch` now correctly fires after `waitAgentReady` returns (hk-zchbu code ordering fix confirmed). But `SessionStart` fires during TUI initialization while the welcome splash is still rendering. The splash's input handler consumes the trailing `\n` before the REPL input state becomes active. Text sits in `❯` bar unsubmitted.

Fix: add `--dangerously-skip-permissions` to the `claude` invocation (bypasses splash entirely) or add a fixed post-`agent_ready` delay (fragile). The `--dangerously-skip-permissions` option is preferred as it also resolves hk-53y35.

### hk-53y35 (filed) — worktree settings.json missing dangerouslySkipPermissions — Claude prompts for every edit and shell command

**Type:** bug, **Priority:** P1, **Labels:** subsystem:daemon, subsystem:workspace
**Relation:** related to hk-lj1p9

`WriteSettingsJSON` generates only bridge hooks. No `dangerouslySkipPermissions` or `permissions` allowlist. Claude presents confirmation dialogs for file edits and shell commands. Fully autonomous smoke (no human at terminal) is blocked.

Fix: set `"dangerouslySkipPermissions": true` in generated settings.json or pass `--dangerously-skip-permissions` flag to `claude` invocation.

---

## 10. Cleanup

- Tmux session `smoke-1778745939` killed.
- Daemon (PID 89886) killed via session kill.
- Scratch directory left in place: `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.cyQYq3idv7`
- Bead `smoke-dq6` remains in_progress (not reached completion via daemon path).

---

*Prior RED run (paste ordering fix — splash still swallows \n): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v9.md`*
*Prior RED run (trust path canonicalization): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v8.md`*
*Prior RED run (WriteLastPane slash-path window name): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v7.md`*
*Prior RED run (SetAgentReadyCallback never wired): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v6.md`*
*Prior RED run (sess.Wait deadlock): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v5.md`*
*Prior RED run (socket never bound): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v4.md`*
*Prior RED run (no task delivery): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v3.md`*
*Prior RED run (harmonik PATH): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v2.md`*
*Prior RED run (nil-watcher panic): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate.md`*
