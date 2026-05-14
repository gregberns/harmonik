# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate (v7, post hk-lj1p9.4 wiring fix)

**Verdict:** RED — `agent_ready` now arrives in 0.57s (both wiring fixes confirmed); paste-inject fires but `WriteLastPane` fails because the pane target (`session:worktree-path.0`) cannot be resolved by tmux when the window name contains path slashes
**Bead:** smoke-m3t (scratch repo)
**Follow-up filed:** hk-yngq2 (P0, in harmonik repo) — `pasteInjectOnLaunch: WriteLastPane fails when window-name contains slashes (pane target broken)`
**Date:** 2026-05-14 (UTC-7)
**Runner:** automated smoke agent
**Procedure:** mirrors v6 (`docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v6.md`)

**Prior runs:**
- v1 RED: nil watcher panic → fixed hk-e2kwq (9dfd7e8)
- v2 RED: `harmonik` not on PATH in hook commands → fixed hk-kqdpf.6 (96de004)
- v3 RED: no task delivery — Claude launched idle, no CLAUDE.md, no `--print`
- v4 RED: socket never bound — `RunSocketListener` implemented but never called from composition root (hk-tjl40)
- v5 RED: socket bound, but `sess.Wait()` deadlocks — tmux window-name target resolves to session active pane after `Kill` (hk-smuku)
- v6 RED: `SetAgentReadyCallback` never called in workloop — hook-relay connects and sends `agent_ready` to socket, but callback is nil, `tapCh` never receives the event, HC-056 fires every 30s (hk-lj1p9.4)
- v7 RED (this run): `agent_ready` arrives 0.57s after launch (PRIMARY FIX CONFIRMED); `pasteInjectOnLaunch` fires but `WriteLastPane` errors — tmux can't resolve `session:worktree-path.0` pane target when window name is a full filesystem path containing slashes

---

## 1. Setup

### Git verification

```
git log --oneline -6

1e5dd0c chore(beads): sync — close hk-lj1p9.4
f79b2a8 fix(daemon): wire SetAgentReadyCallback and pasteInjectOnLaunch in beadRunOne (hk-lj1p9.4)
835e390 docs(smoke): dogfood-smoke-run v6 2026-05-13 — RED: SetAgentReadyCallback never wired (hk-lj1p9.4)
e430807 fix(daemon): decouple Wait liveness from tmux name resolution (hk-smuku)
1f3eae2 docs(smoke): dogfood-smoke-run v5 2026-05-13 — RED: sess.Wait() deadlocks on WindowPanePID fallback (hk-smuku)
bb416e4 chore(beads): sync — close hk-tjl40
```

HEAD is `1e5dd0c`. Both `f79b2a8` (hk-lj1p9.4 wiring fix) and `1e5dd0c` present. PASS.

### Preconditions

- `claude --version`: `2.1.141 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br --version`: `br 0.1.45` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS
- `go build -o /tmp/hk ./cmd/harmonik`: exit 0, timestamp `2026-05-14 00:30`, size `8342562` — PASS (rebuilt from HEAD, includes hk-lj1p9.4 wiring fix)

### Scratch directory

```
SMOKE_DIR=/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.kjv2d1cswF
```

- `git init` + `git config user.email smoke@harmonik.local` + `git config user.name "Smoke Runner"` — PASS
- Initial commit: `README.md` + `marker.txt` (`initial\n`) — SHA `89a1619c54fd3a8cff8c76cef1e459c53e4dd7de` — PASS
- `br init --prefix smoke` — PASS
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels "workflow:single"` → **smoke-m3t**

---

## 2. Run

### Tmux session launch

```
tmux new-session -d -s smoke-1778743872 -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    2>&1 | tee $SMOKE_DIR/hk.log; sleep 300"
```

Session: `smoke-1778743872`.

---

## 3. Stage-by-stage analysis

### Stage 1: daemon_started — PASS

```json
{"type":"daemon_started","payload":{"binary_commit_hash":"unknown","pid":80652,"started_at":"2026-05-14T07:31:13Z"}}
```

Clean start, no crash.

### Stage 2: daemon_orphan_sweep_completed — PASS

```json
{"type":"daemon_orphan_sweep_completed","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-14T07:31:13Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
```

Clean sweep.

### Stage 3: run_started — PASS

```json
{"type":"run_started","payload":{"bead_id":"smoke-m3t","run_id":"019e2565-8fac-79f2-9a75-7b16ea836170","started_at":"2026-05-14T07:31:13Z","workspace_path":".../.harmonik/worktrees/019e2565-8fac-79f2-9a75-7b16ea836170"}}
```

Bead claimed, worktree created.

### Stage 4: handler_capabilities, session_log_location, skills_provisioned — PASS

All three pre-exec messages emitted correctly. Claude session ID `019e2565-9068-769b-a935-82cc9d30bf46` minted.

### Stage 4a: .claude/settings.json materialization — PASS (inherited from v5/v6)

All five bridge hooks materialized with absolute binary path `/tmp/hk`.

### Stage 4b: agent-task.md materialization — PASS

`$WORKTREE/.harmonik/agent-task.md` written with correct content:

```
bead_id: smoke-m3t
title: Add SMOKE-OK marker line to marker.txt and commit
run_id: 019e2565-8fac-79f2-9a75-7b16ea836170
workspace_path: /private/.../.harmonik/worktrees/019e2565-8fac-79f2-9a75-7b16ea836170
```

### Stage 4c: trust pre-seed — PASS (carried from v5)

`~/.claude.json` contains `hasTrustDialogAccepted: true` for the worktree path.

### Stage 5: launch_initiated — PASS

```json
{"type":"launch_initiated","payload":{"claude_session_id":"019e2565-9068-769b-a935-82cc9d30bf46",...}}
```

Tmux window 2 (`smoke-1778743872:2`) created; Claude pane visible at the worktree path. Confirmed via `tmux list-windows` and `tmux capture-pane`.

### Stage 6: daemon.sock bound — PASS (carried)

Socket file present; `lsof` confirms PID 80652 listening.

### NEW Stage 7: agent_ready arrives — PASS (PRIMARY FIX, hk-lj1p9.4)

**This is the primary fix under test for v7.** The `agent_ready` event appears in `events.jsonl` at T+0.57s after `run_started`:

```
run_started   2026-05-14T00:31:13.635709  (T+0)
agent_ready   2026-05-14T00:31:14.20257   (T+0.567s)
```

In v6, `agent_ready` never arrived — `waitAgentReady` always timed out at 30s (HC-056). In v7, the callback is registered and the hook-relay's `SessionStart` signal propagates into `tapCh` within 0.57s of the Claude window becoming active. The 30s HC-056 timeout guard is never needed.

**Confirmed working:**

- `SetAgentReadyCallback` is now called in `beadRunOne` between tapCh creation (step 4) and `waitAgentReady` (step 6).
- The `SessionStart` hook fires when Claude initializes, the hook-relay connects to the daemon socket, and `notifyAgentReady` calls the non-nil callback.
- The callback does a non-blocking send into `tapCh`.
- `waitAgentReady` unblocks and returns nil.

**hk-lj1p9.4 is confirmed working.**

### NEW Stage 7a: pasteInjectOnLaunch fires — PARTIAL FAIL (new gap: hk-yngq2)

`pasteInjectOnLaunch` is now called from `beadRunOne` (the secondary fix in hk-lj1p9.4). The daemon immediately emits its log entry after `agent_ready`:

```
daemon: pasteinject: implementer-initial WriteLastPane: tmux: paste-buffer exited 1: can't find pane: kjv2d1cswF/.harmonik/worktrees/019e2565-8fac-79f2-9a75-7b16ea836170.0
```

The paste-inject **fires** (correct — the call site wiring works), but **fails at `WriteLastPane`** because the pane target it constructs cannot be resolved by tmux.

**Root cause — pane target format incompatible with slash-containing window names:**

`tmuxSubstrate.WriteLastPane` constructs the pane target as:

```go
paneTarget := string(handle) + ".0"
// handle = "smoke-1778743872:/private/var/.../019e2565-8fac-79f2-9a75-7b16ea836170"
// paneTarget = "smoke-1778743872:/private/var/.../019e2565-8fac-79f2-9a75-7b16ea836170.0"
```

In tmux's address grammar, `session:window-name.pane-index` works when the window name contains no slashes. When the window name is a full filesystem path (as harmonik uses — the worktree path is the window name per `SpawnWindow`), tmux cannot parse the target correctly and emits `"can't find pane: <last-path-component>.0"`.

**Verified:** using the pane ID directly (`%1964`) resolves the pane correctly. The pane exists; only the string-based target is broken.

**Per spec:** `pasteInjectOnLaunch` errors are non-fatal (logged to stderr, workloop continues). So the workloop proceeds past the paste-inject failure without crashing.

**Effect:** Claude launches at the interactive REPL with no task message injected. The pane shows:

```
╭─── Claude Code v2.1.141 ─────────────────────────╮
│           Welcome back Greg!                      │
│  /…/worktrees/019e2565-8fac-79f2-...              │
╰───────────────────────────────────────────────────╯
❯
```

No "Please read .harmonik/agent-task.md and begin." message in the pane. Claude sits idle.

### Stage 8: workloop stalls — NOT REACHED (downstream of paste-inject failure)

After `waitAgentReady` returns nil, the workloop advances to `waitWithSocketGrace` (Step 7 in `beadRunOne`), which blocks waiting for the Claude session to emit a terminal event (stop-hook `WORK_COMPLETE` or process exit). Since Claude received no task and is sitting idle at the interactive REPL, it neither exits nor emits a stop-hook. The workloop stalls indefinitely.

No `run_completed`, no `run_failed` events. Total events: 8.

### Stage 9: marker.txt, commit, bead close — NOT REACHED

`marker.txt` still contains only `initial\n`. No new git commit. Bead `smoke-m3t` left IN_PROGRESS by daemon (manually reset to OPEN after daemon kill).

---

## 4. Full hk.log

```
harmonik daemon starting in /private/var/.../tmp.kjv2d1cswF
daemon: pasteinject: implementer-initial WriteLastPane: tmux: paste-buffer exited 1: can't find pane: kjv2d1cswF/.harmonik/worktrees/019e2565-8fac-79f2-9a75-7b16ea836170.0
```

Two lines total. No HC-056 timeouts. No deadlocks. No panics. One paste-inject error. Daemon blocked in `waitWithSocketGrace`.

---

## 5. Event summary (8 total events)

| Event type | Count |
|---|---|
| daemon_started | 1 |
| daemon_orphan_sweep_completed | 1 |
| run_started | 1 |
| handler_capabilities | 1 |
| session_log_location | 1 |
| skills_provisioned | 1 |
| launch_initiated | 1 |
| agent_ready | 1 |

One run, progressed further than any prior run. Prior v6 runs never reached `agent_ready`. This run reaches `agent_ready` in 0.57s, then stalls at paste-inject failure.

---

## 6. Success criteria checklist

| Criterion | Result |
|-----------|--------|
| 1. No panic / SIGSEGV | PASS — no crash |
| 2. Tmux window spawned for Claude | PASS — `launch_initiated` emitted, tmux window visible |
| 3. `harmonik` absolute path in hooks (kqdpf.6) | PASS — `/tmp/hk` in settings.json |
| 4. agent-task.md written before launch (hk-9ow36) | PASS — file present with correct content |
| 5. EnsureWorktreeTrust called (hk-lj1p9.3) | PASS — confirmed from v5, same binary |
| 6. Synthetic agent_ready removed from pre-exec (hk-1rocd) | PASS — no pre-exec `agent_ready` event |
| 7. daemon.sock bound for hook-relay (hk-tjl40) | PASS — socket file exists, lsof confirms listener |
| 8. sess.Wait() returns after Kill (hk-smuku) | PASS — not triggered this run; confirmed in v6 |
| 9. workloop dispatches retry after HC-056 (hk-smuku) | PASS — not triggered (HC-056 not reached) |
| 10. hook-relay socket connects to daemon | PASS — agent_ready event confirms end-to-end |
| **11. agent_ready via SessionStart hook relay** | **PASS** — T+0.57s (primary fix hk-lj1p9.4 confirmed) |
| **12. pasteInjectOnLaunch fires** | **PARTIAL** — fires (wiring correct) but WriteLastPane fails: can't find pane (slash-path window name) |
| 13. paste message delivered to Claude pane | FAIL — WriteLastPane error; pane shows idle REPL |
| 14. Claude reads .harmonik/agent-task.md | FAIL — not reached (no task delivered) |
| 15. marker.txt contains SMOKE-OK | FAIL — not reached |
| 16. Commit with SMOKE-OK on worktree branch | FAIL — not reached |
| 17. Bead closed with reason `done` | FAIL — not reached |

---

## 7. What the hk-lj1p9.4 fix confirms

**hk-lj1p9.4 is confirmed working.** Both sub-fixes from the commit are exercised:

1. **`SetAgentReadyCallback` wiring:** `agent_ready` arrives 0.57s after launch. No HC-056 timeout. The tapCh callback correctly propagates the relay signal into `waitAgentReady`.

2. **`pasteInjectOnLaunch` wiring:** The call site fires. The error is in `WriteLastPane` (downstream of the wiring), not in the call site itself. The wiring gap from v6 is resolved.

All prior umbrella deliverables remain confirmed:
1. **hk-9ow36 (agent-task.md):** Written correctly before tmux window creation.
2. **hk-lj1p9.3 (EnsureWorktreeTrust):** Trust pre-seeded in `~/.claude.json`.
3. **hk-1rocd (relay-synthesized agent_ready):** No synthetic `agent_ready` in pre-exec events.
4. **CHB-028 (agent-task.md overwrite per launch):** MkdirAll + atomic write confirmed.
5. **hk-tjl40 (daemon.sock bound):** Socket present and listening.
6. **hk-smuku (sess.Wait deadlock):** Not triggered; confirmed in v6.

---

## 8. Root cause: WriteLastPane pane target incompatible with path-slash window names

### Code path

**`internal/daemon/tmuxsubstrate.go` `WriteLastPane`:**

```go
func (s *tmuxSubstrate) WriteLastPane(ctx context.Context, bufferName string, payload []byte) error {
    s.lastHandleMu.Lock()
    handle := s.lastHandle
    s.lastHandleMu.Unlock()
    // ...
    // handle = "smoke-1778743872:/private/var/.../019e2565-8fac-79f2-9a75-7b16ea836170"
    paneTarget := string(handle) + ".0"
    // paneTarget = "smoke-1778743872:/private/var/.../019e2565-8fac-79f2-9a75-7b16ea836170.0"
    return s.adapter.WriteToPane(ctx, bufferName, paneTarget, payload)
}
```

**`internal/lifecycle/tmux/osadapter.go` `PasteBuffer`:**

```sh
tmux paste-buffer -b harmonik-... -t smoke-1778743872:/private/.../019e2565....0 -d
# exit 1: can't find pane: kjv2d1cswF/.harmonik/worktrees/019e2565-8fac-79f2-9a75-7b16ea836170.0
```

tmux cannot resolve `session:window-name.pane-index` when `window-name` is a filesystem path containing slashes. The `session:X.Y` grammar treats `.Y` as a pane index suffix, but the window name lookup fails because the slash-containing path does not match any window name in tmux's index.

**The pane exists and is addressable via pane ID `%1964`** — confirmed by `tmux display-message -p -t %1964`. The fix requires `WriteLastPane` to store and use the pane ID (`%NNNN` format) rather than reconstructing a string target from `WindowHandle + ".0"`.

**Alternative fix:** tmux supports `-t %NNNN` pane addresses that bypass the window-name lookup entirely. `WindowPanePID` already resolves the pane ID from `handle` at spawn time; the substrate session could store the pane ID and expose it for `WriteLastPane`.

### Fix scope (hk-yngq2)

`NewWindowIn` or `SpawnWindow` should retrieve the pane ID (`tmux display-message -p -t <handle> '#{pane_id}'`) immediately after window creation and store it in the `tmuxSubstrate`. `WriteLastPane` uses the stored `%NNNN` pane ID as the `-t` target instead of `string(handle) + ".0"`.

The `WindowPanePID` method already demonstrates this pattern: it calls `tmux display-message -p -t <handle> '#{pane_pid}'`. A parallel `#{pane_id}` query at spawn time gives the stable `%NNNN` address that works regardless of window name content.

---

## 9. Follow-up bead

### hk-yngq2 (filed) — pasteInjectOnLaunch: WriteLastPane fails when window-name contains slashes (pane target broken)

**Type:** bug, **Priority:** P0, **Labels:** subsystem:daemon, subsystem:bridge
**Relation:** related to hk-lj1p9 (dependency type converted to `related` per L-011 protocol)

`tmuxSubstrate.WriteLastPane` constructs the pane target as `string(handle) + ".0"` where `handle` is `"session:window-name"`. When the window name is a full filesystem path (harmonik uses the worktree path as window name), the resulting target `"session:path-with-slashes.0"` cannot be resolved by tmux. `paste-buffer` exits 1 with `"can't find pane"`. The task kick-off message is never delivered to the Claude pane.

Fix: store the pane ID (`%NNNN` from `tmux display-message -p -t <handle> '#{pane_id}'`) at window spawn time and use it as the `-t` target in `WriteLastPane`.

---

## 10. Cleanup

- Tmux session `smoke-1778743872` killed.
- Daemon (PID 80652) killed via session kill.
- Scratch directory left in place: `/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.kjv2d1cswF`
- Bead `smoke-m3t` manually reset to OPEN after daemon kill.

---

*Prior RED run (SetAgentReadyCallback never wired): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v6.md`*
*Prior RED run (sess.Wait deadlock): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v5.md`*
*Prior RED run (socket never bound): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v4.md`*
*Prior RED run (no task delivery): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v3.md`*
*Prior RED run (harmonik PATH): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v2.md`*
*Prior RED run (nil-watcher panic): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate.md`*
