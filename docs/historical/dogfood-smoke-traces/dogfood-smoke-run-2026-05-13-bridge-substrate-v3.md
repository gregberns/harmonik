# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate (v3, post wave 5 fixes)

**Verdict:** RED (new failure mode — different from v1 and v2)
**Bead:** hk-kqdpf.5 (3rd run)
**Date:** 2026-05-13
**Runner:** automated smoke agent
**Procedure:** same shape as v2 run (`docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v2.md`)
**Prior runs:**
- v1 RED: nil watcher panic → fixed by hk-e2kwq (9dfd7e8)
- v2 RED: hook command `harmonik` not on PATH → fixed by hk-kqdpf.6 (96de004)
- Also fixed in parallel: tmux Kill() (kqdpf.7 / e867b79) + HC-056 reopen-log (kqdpf.8 / 917a052)

---

## 1. Setup

### Git verification

```
git log --oneline -6
```

```
58c6c96 chore(beads): close hk-kqdpf.6 (absolute binary path in hook settings)
96de004 fix(bridge): materialize absolute binary path in settings.json hooks (hk-kqdpf.6)
917a052 fix(daemon): log ReopenBead failure in HC-056 path; add reopen+repickup test (hk-kqdpf.8)
e867b79 fix(daemon): Kill() sends SIGTERM/SIGKILL to pane PID before KillWindow (hk-kqdpf.7)
929c1b4 chore(beads): file hk-kqdpf.{6,7,8} — v2 smoke follow-ups (harmonik PATH, Kill(), reopen-retry)
f8e512e docs(smoke): dogfood-smoke-run v2 2026-05-13 after hk-e2kwq fix (hk-kqdpf.5)
```

All 4 required commits present (58c6c96, 96de004, 917a052, e867b79). PASS.

### Preconditions

- `claude --version`: `2.1.140 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br`: `br 0.1.45` at `/Users/gb/.local/bin/br` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS
- `go build -o /tmp/hk ./cmd/harmonik`: exit 0 — PASS

### Scratch directory setup

```
SMOKE_DIR=/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.ZaPeuAJVmY
```

- `git init`, `user.email smoke@harmonik.local`, `user.name Smoke Runner` — PASS
- `echo "# smoke repo" > README.md && touch marker.txt && git add -A && git commit -m "initial"` — PASS
- `br init --prefix smoke` — PASS
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels "workflow:single"` → **smoke-nj9**

---

## 2. Run

### Tmux session

```
tmux new-session -d -s smoke-1778695748 -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    2>&1 | tee $SMOKE_DIR/hk.log; sleep 120"
```

---

## 3. Stage-by-stage analysis

### Stage 1: daemon_started — PASS

```json
{"type":"daemon_started","payload":{"pid":45718,"started_at":"2026-05-13T18:09:09Z"}}
```

No panic. No crash. Clean start.

### Stage 2: daemon_orphan_sweep_completed — PASS

```json
{"type":"daemon_orphan_sweep_completed","payload":{"br_subprocesses_killed":0,"locks_cleared":0,...,"tmux_sessions_killed":0,"tmux_windows_killed":0}}
```

Clean sweep.

### Stage 3: run_started — PASS

```json
{"type":"run_started","payload":{"bead_id":"smoke-nj9","run_id":"019e2287-409f-7908-b666-e13ba8ad8f4e","started_at":"2026-05-13T18:09:09Z","workspace_path":".../.harmonik/worktrees/019e2287-409f-7908-b666-e13ba8ad8f4e"}}
```

Bead claimed, worktree created, run ID assigned.

### Stage 4: .claude/settings.json materialization — PASS (kqdpf.6 fix confirmed)

All five bridge hooks materialized with **absolute binary path** `/tmp/hk`:

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

**hk-kqdpf.6 fix is confirmed working.** The `harmonik not on PATH` failure from v2 is resolved. Hook commands now use the absolute binary path from `os.Executable()`.

### Stage 5: tmux window spawn — PASS

```
tmux list-windows -t smoke-1778695748
1: zsh* (1 panes) [220x50]
2: /var/folders/.../worktrees/019e2287-409f-7908-b666-e13ba8ad8f4e (1 panes) [175x66]
```

Second window created for the worktree. Claude spawned in window 2.

### Stage 5a: trust gate — MANUAL INTERVENED (pre-existing)

Interactive trust prompt required Enter to dismiss. Same as v1 and v2. Pre-existing gap.

### Stage 5b: Claude at idle prompt — STRUCTURAL GAP EXPOSED

After trust dismissal, Claude loaded and displayed the welcome screen at an idle interactive prompt. No task was sent to Claude. The worktree had no `CLAUDE.md`, no `--print` flag, no `--message`, no injected task. The daemon spawns claude with only `--session-id <uuid>`.

**Root cause:** The daemon does not inject a task prompt into Claude. `buildClaudeLaunchSpec` constructs args as `["--session-id", "<uuid>"]` only. The bead title is never passed to Claude as a task. Claude sits idle at the interactive prompt waiting for user input that will never arrive.

### Stage 6: HC-056 agent_ready timeout — FAIL (expected, root cause is Stage 5b)

The `waitAgentReady` function (daemon step 6) watches a per-run event tap for an `agent_ready` event. This tap is created **after** the pre-exec messages (including the synthetic `agent_ready`) are emitted to the bus. The tap therefore never sees the pre-exec `agent_ready` — it would only see a hook-relay `agent_ready` delivered when Claude's `Stop` hook fires after completing a task.

Since Claude received no task, it never completed a turn, the `Stop` hook never fired, and no `agent_ready` reached the bus via the tap. After 30 seconds (defaultAgentReadyTimeout), HC-056 timed out:

```
daemon: workloop: waitAgentReady bead smoke-nj9 run 019e2287-409f-7908-b666-e13ba8ad8f4e: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
```

**Note on tap ordering:** There is a secondary issue: even if the Stop hook had fired, the pre-exec `agent_ready` was emitted before the tap was created, so the tap would only catch post-Launch events. This is correct by design — the pre-exec `agent_ready` is a CHB-018 progress stream event for the handler subprocess, not for the tap's HC-056 readiness gate.

### Stage 7: Kill() — PASS (kqdpf.7 fix confirmed)

After HC-056, `sess.Kill(ctx)` was called. The specific smoke claude process (`--session-id 019e2287-414e-7b71-b271-888399550885`) was **not found** in `ps` after HC-056 fired. The kqdpf.7 Kill() fix (SIGTERM/SIGKILL to pane PID before KillWindow) is confirmed working. The claude process was successfully terminated.

### Stage 8: ReopenBead — PARTIAL (no FAIL log, but bead stuck IN_PROGRESS)

`ReopenBead` was called after HC-056. The kqdpf.8 fix added logging for `ReopenBead` failures. No "ReopenBead FAILED" line appeared in hk.log — suggesting `br update smoke-nj9 --status open` returned exit 0. However, `br show smoke-nj9` still shows `IN_PROGRESS` at session end. This needs investigation. Either: (a) br returned exit 0 but didn't persist the change (JSONL sync delay?), or (b) the daemon's poll loop doesn't re-examine the bead after the workloop returns.

### Stage 9–10: outcome_emitted, run_completed — NOT REACHED

Claude never executed any task. `marker.txt` was not written. No git commit was made. No `outcome_emitted` event was emitted. The scratch git log has only `37dba29 initial`.

---

## 4. Full hk.log

```
harmonik daemon starting in /var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.ZaPeuAJVmY
daemon: workloop: waitAgentReady bead smoke-nj9 run 019e2287-409f-7908-b666-e13ba8ad8f4e: agent_ready timeout: no agent_ready event within deadline (HC-056) (reopening)
```

(Two lines. No panic. No crash. Same as v2 in structure but different root cause.)

---

## 5. Full events.jsonl

```jsonl
{"event_id":"019e2287-402e-74d6-a040-2c111396b344","schema_version":1,"type":"daemon_started","timestamp_wall":"2026-05-13T11:09:09.678317-07:00","source_subsystem":"eventbus","payload":{"binary_commit_hash":"unknown","pid":45718,"started_at":"2026-05-13T18:09:09Z"}}
{"event_id":"019e2287-4086-7b0c-8a8c-e0d8f6d3fa0a","schema_version":1,"type":"daemon_orphan_sweep_completed","timestamp_wall":"2026-05-13T11:09:09.766724-07:00","source_subsystem":"eventbus","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-13T18:09:09Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
{"event_id":"019e2287-414a-71b9-ba8c-b64caa7aafd4","schema_version":1,"type":"run_started","timestamp_wall":"2026-05-13T11:09:09.962113-07:00","source_subsystem":"eventbus","payload":{"bead_id":"smoke-nj9","run_id":"019e2287-409f-7908-b666-e13ba8ad8f4e","started_at":"2026-05-13T18:09:09Z","workspace_path":"/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.ZaPeuAJVmY/.harmonik/worktrees/019e2287-409f-7908-b666-e13ba8ad8f4e"}}
{"event_id":"019e2287-415a-7d1b-860d-c694c9963c3b","schema_version":1,"type":"handler_capabilities","timestamp_wall":"2026-05-13T11:09:09.978859-07:00","run_id":"019e2287-409f-7908-b666-e13ba8ad8f4e","source_subsystem":"eventbus","payload":{"claude_session_id":"019e2287-414e-7b71-b271-888399550885","supported_versions":[1],"type":"handler_capabilities"}}
{"event_id":"019e2287-415a-7e48-b8ef-c7d197cfb13f","schema_version":1,"type":"session_log_location","timestamp_wall":"2026-05-13T11:09:09.978936-07:00","run_id":"019e2287-409f-7908-b666-e13ba8ad8f4e","source_subsystem":"eventbus","payload":{"agent_type":"claude-code","log_format":"jsonl","log_path":"/Users/gb/.claude/projects/var-folders-s9-kq5h9q_17t571w_xq_1q9f2r0000gp-T-tmp.ZaPeuAJVmY-.harmonik-worktrees-019e2287-409f-7908-b666-e13ba8ad8f4e/019e2287-414e-7b71-b271-888399550885.jsonl","node_id":"bead/smoke-nj9","run_id":"019e2287-409f-7908-b666-e13ba8ad8f4e","session_id":"019e2287-415a-7b90-9944-e39a66269c80","type":"session_log_location"}}
{"event_id":"019e2287-415a-7eb5-aab6-a17df44aed6a","schema_version":1,"type":"skills_provisioned","timestamp_wall":"2026-05-13T11:09:09.978964-07:00","run_id":"019e2287-409f-7908-b666-e13ba8ad8f4e","source_subsystem":"eventbus","payload":{"run_id":"019e2287-409f-7908-b666-e13ba8ad8f4e","session_id":"019e2287-415a-7b90-9944-e39a66269c80","skills":[],"type":"skills_provisioned"}}
{"event_id":"019e2287-415a-7f0b-a646-5ecb65732d8d","schema_version":1,"type":"agent_ready","timestamp_wall":"2026-05-13T11:09:09.978986-07:00","run_id":"019e2287-409f-7908-b666-e13ba8ad8f4e","source_subsystem":"eventbus","payload":{"capabilities":[],"session_id":"019e2287-415a-7b90-9944-e39a66269c80","type":"agent_ready"}}
```

(7 events total. All pre-exec. No hook-relay events. No `agent_heartbeat` — daemon had already returned from workloop by the 5-minute mark.)

---

## 6. Success criteria checklist

| Criterion | Result |
|-----------|--------|
| 1. No panic / SIGSEGV | PASS — no crash, no nil-guard issues |
| 2. Tmux window for claude spawned | PASS — window @1882 created, Claude launched |
| 3. `harmonik` bare name resolved (kqdpf.6) | PASS — hooks use `/tmp/hk` absolute path |
| 4. Claude subprocess killed on HC-056 timeout (kqdpf.7) | PASS — claude PID not found after HC-056 |
| 5. marker.txt contains SMOKE-OK | FAIL — file empty (no task sent to Claude) |
| 6. Commit with SMOKE-OK on worktree branch | FAIL — no new commit |
| 7. `outcome_emitted` in events.jsonl | FAIL — never reached |
| 8. Real `agent_ready` via Stop hook relay | FAIL — Claude received no task, never completed a turn |
| 9. `.claude/settings.json` with all five bridge hooks | PASS — materialized correctly with absolute path |
| 10. Bead closed with reason `done` | FAIL — bead stuck IN_PROGRESS |
| 11. `run_completed` has `success: true` | FAIL — never emitted |
| 12. HC-056 reopen-log works (kqdpf.8) | PARTIAL — no "FAILED" log, but bead still IN_PROGRESS |

---

## 7. What worked (wave 5 fixes confirmed)

- **hk-kqdpf.6 (absolute binary path):** hooks.json uses `/tmp/hk` — confirmed. The "harmonik not on PATH" failure from v2 is resolved.
- **hk-kqdpf.7 (Kill() terminates claude):** claude process (session-id 019e2287-414e) not found in ps after HC-056. SIGTERM/SIGKILL fix is effective.
- **hk-kqdpf.8 (ReopenBead failure logging):** No "ReopenBead FAILED" log line appeared — the happy path did not fail. The stuck-bead symptom may be a br sync issue or poll-loop re-entry issue, not a ReopenBead invocation failure.
- **Nil-guard (hk-e2kwq):** No panic at all. Substrate path is stable.
- **Tmux substrate dispatch:** Window created, Claude launched, trust gate dismissable.

---

## 8. New failure modes identified

### Bug A: No task injection — daemon spawns Claude without a prompt (P0, BLOCKS GREEN)

**Location:** `internal/daemon/claudelaunchspec.go` `buildClaudeLaunchSpec` / `internal/daemon/workloop.go`

The daemon launches Claude with only `--session-id <uuid>`. The bead title (the task) is never passed to Claude. `buildClaudeLaunchSpec` constructs `args = ["--session-id", mintRes.ClaudeSessionID]` and there is no mechanism to pass `--print "<task>"` or write a `CLAUDE.md` with the task text. Claude waits at an interactive idle prompt indefinitely.

Until task injection is implemented, the HC-056 timeout will fire on every single-mode run: Claude has nothing to do, never completes a turn, the Stop hook never fires, and `agent_ready` never reaches the tap.

**Fix options:**
1. Pass `--print "<bead.title>"` in args (headless print mode — simplest for smoke, but may not be MVH approach)
2. Write a `CLAUDE.md` in the worktree with the task before launch, then rely on Claude's SessionStart hook to acknowledge
3. Use `sess.SendInput` after launch to send the task as a chat message (requires Stop hook round-trip first)

Option 1 (`--print`) is simplest for smoke validation. Option 3 matches the spec's CHB-023 session context delivery pattern.

### Gap B: Trust gate blocks unattended smoke runs (pre-existing, P2)

Same as v1 and v2. Not addressed by wave 5. `--dangerously-skip-permissions` would bypass it for unattended runs.

### Gap C: Bead stuck IN_PROGRESS after HC-056 ReopenBead (P1, partially addressed)

After HC-056, `br show smoke-nj9` still shows IN_PROGRESS at session end. No "ReopenBead FAILED" log line, so the br invocation didn't error. Root cause unknown — either br returned exit 0 but the JSONL wasn't flushed, or the daemon poll loop doesn't re-examine IN_PROGRESS beads after workloop returns. Needs targeted investigation.

---

## 9. What the smoke sequence proves is plumbing-ready

The following substrate + bridge subsystems are confirmed structurally correct end-to-end through this run:

1. Daemon startup + orphan sweep
2. Bead dispatch (br polling → run_started)
3. Worktree creation with git config
4. `settings.json` materialization (CHB-001..005, absolute path fix)
5. Tmux substrate window spawn (Gas Town pattern entry)
6. Claude process launch (PL-028b $TMUX guard not tripping)
7. HC-056 timeout detection and workloop control flow
8. Kill() subprocess termination (kqdpf.7)
9. ReopenBead invocation (kqdpf.8 path hit without error)

The missing piece is task injection — Claude needs to receive the task before the bridge can demonstrate real work.

---

## 10. Follow-up beads to file

### hk-kqdpf.9 — No task injection: daemon spawns Claude without a prompt

**Type:** bug, **Priority:** P0, **Parent:** hk-kqdpf

`buildClaudeLaunchSpec` builds args as `["--session-id", <uuid>]` only. The bead title is never passed to Claude. Claude sits idle; HC-056 always fires. The bridge and substrate cannot be validated end-to-end without task delivery. Fix: implement task injection (simplest path: `--print "<bead.title>"` or CLAUDE.md in worktree).

### hk-kqdpf.10 — Bead stuck IN_PROGRESS after HC-056 ReopenBead (no error log)

**Type:** bug, **Priority:** P1, **Parent:** hk-kqdpf

After HC-056 fires and `ReopenBead` returns without error, `br show` still shows IN_PROGRESS. Either br doesn't persist the status change synchronously, or the daemon poll loop doesn't re-examine beads after workloop returns. Investigate the ReopenBead → poll re-entry cycle.

---

## 11. Cleanup

- tmux session `smoke-1778695748` killed via `tmux kill-session`.
- Orphaned claude process was not found (kqdpf.7 Kill() worked).
- hk process (pid 45718) terminated when tmux session was killed.
- Scratch directory `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.ZaPeuAJVmY` left in place (OS temp).

---

*Prior RED run (harmonik PATH): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate-v2.md`*
*Prior RED run (nil-watcher panic): `docs/dogfood-smoke-run-2026-05-13-bridge-substrate.md`*
