# Dogfood Smoke Run — 2026-05-14 — OPERATIONAL GREEN

## Verdict

**OPERATIONAL GREEN** — harmonik dispatches a bead to claude, claude executes it end-to-end without human intervention, the daemon detects completion and closes the bead, `sess.Wait` unblocks, and the workloop capacity gate is released.

This is the Phase 1 milestone: a real Claude Code session, inside a daemon-managed tmux substrate, picks a bead, does the work, commits it, types `/quit`, and the daemon closes the loop — all automatically.

**Date:** 2026-05-14  
**HEAD:** `4e4376e` (chore(beads): sync — close hk-gql20.15)  
**hk-cmybm fix commit:** `cbc4725` (fix(daemon+workspace): daemon-side quit injection unblocks workloop)  
**Smoke bead:** `smoke-qiw`  
**Smoke dir:** `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.MMRjbc59ho`  
**Claude's commit:** `99c195c` ("Add SMOKE-OK marker line to marker.txt")  
**Wall-clock, run_started → run_completed:** 24.1 s  

---

## Convergence Story — 11 Umbrella Fixes

The umbrella work `hk-lj1p9` (bridge substrate) required 11 targeted fixes across 13 smoke runs (v1–v13, spanning 2026-05-12 to 2026-05-14) before reaching OPERATIONAL GREEN. Each fix was driven by a failing smoke and filed as a child bead.

| # | Bead | Fix | Commit |
|---|------|-----|--------|
| 1 | hk-lj1p9.3 | Trust env-override + flock — isolate EnsureWorktreeTrust from real ~/.claude.json | `f6cd256` |
| 2 | hk-tjl40 | Socket bind — RunSocketListener was never called in daemon.Start | `2104168` |
| 3 | hk-smuku | Wait uses stored PID — decouple Wait liveness from tmux name resolution | `e430807` |
| 4 | hk-lj1p9.4 | SetAgentReadyCallback + pasteInjectOnLaunch wired in beadRunOne | `f79b2a8` |
| 5 | hk-yngq2 | Pane ID stable target — use pane %NNNN not slash-path window name | `ff79246` |
| 6 | hk-o5eww | EvalSymlinks trust key — canonicalize worktreePath before trust lookup | `e8fd1df` |
| 7 | hk-zchbu | Paste-inject ordering — inject AFTER waitAgentReady, not before | `ff2bc6f` |
| 8 | hk-rf4ux | Splash dismiss — SendEnterToLastPane + 750ms splashDismissDelay | `cec27e6` |
| 9 | hk-53y35 | permissions.allow — write dangerouslyAllowedPermissions into materialized settings.json | `cec27e6` |
| 10 | hk-cmybm (layer 1) | Session completion instruction — agent-task.md ## Session Completion section tells claude to type /quit after committing | `cbc4725` |
| 11 | hk-cmybm (layer 2) | Daemon-side quit injection — pasteInjectQuitOnCommit goroutine polls worktree HEAD every 500ms and sends /quit Enter when a new commit appears | `cbc4725` |

In addition to the 11 fixes, the spec corpus for `specs/claude-hook-bridge.md` was built across the umbrella (`hk-gql20` sub-beads .14 and .15 wired the single-mode workloop and review-loop to the bridge respectively, committed at `36ac566` and `22d8459`).

---

## The Mechanism — 7 Steps

The operational loop, now confirmed end-to-end:

1. **Daemon spawns claude** — `beadRunOne` forks `claude --dangerously-skip-permissions` inside a tmux window. The PID is stored for `sess.Wait`.
2. **Writes agent-task.md** — `buildAgentTaskContent` materializes the bead title + description + a `## Session Completion` section instructing claude to type `/quit` after committing. File is written to `$worktree/.harmonik/agent-task.md` before launch.
3. **Splash dismiss** — `SendEnterToLastPane` fires 750ms after session start, clearing the Claude Code welcome splash so the REPL accepts input.
4. **Paste-inject** — Once `agent_ready` fires (relay-synthesized from the Stop hook initial signal), `pasteInjectOnLaunch` submits "Please read .harmonik/agent-task.md and begin." into the REPL using tmux bracketed-paste to the stable `%NNNN` pane ID.
5. **Claude works** — Claude reads agent-task.md, edits marker.txt via the Edit tool, commits via Bash. No permission dialogs (permissions.allow covers Edit + Bash + Write). No trust dialog (EvalSymlinks canonicalization + trust env-override).
6. **Daemon /quit on commit** — `pasteInjectQuitOnCommit` goroutine detects HEAD changed (new commit). It calls `SendKeysQuit` → `tmux send-keys -t <pane> /quit Enter`, submitting the typed `/quit`. The Stop hook fires.
7. **bead_closed + workloop unblocks** — Stop hook triggers outcome envelope delivery. `sess.Wait` returns. Daemon closes the bead (`smoke-qiw` → `closed`). `run_completed` is emitted. Capacity gate is released.

---

## Run Evidence

### Event stream (full)

```
2026-05-14T02:50:22 daemon_started           pid=38583
2026-05-14T02:50:22 daemon_orphan_sweep_completed  (nothing swept)
2026-05-14T02:50:22 run_started              bead_id=smoke-qiw  run_id=019e25e4-f5b1-7a68-a178-96d127e03da3
2026-05-14T02:50:22 handler_capabilities     claude_session_id=019e25e4-f65a-71c1-887c-6f4cd65b1eb2
2026-05-14T02:50:22 session_log_location     node_id=bead/smoke-qiw
2026-05-14T02:50:22 skills_provisioned       skills=[]
2026-05-14T02:50:22 launch_initiated
2026-05-14T02:50:23 agent_ready              (T+0.49s from run_started)
2026-05-14T02:50:46 run_completed            success=true  summary="auto-close: exit=0"
```

**run_started → run_completed: 24.1 s**  
**agent_ready → run_completed: 23.6 s**

### Claude session log excerpt (kick-off)

From `~/.claude/projects/…/019e25e4-f65a-71c1-887c-6f4cd65b1eb2.jsonl` (23 events):

```
[user]      "Please read .harmonik/agent-task.md and begin."
[assistant] (read + edit marker.txt + git commit)
[last-prompt] "Please read .harmonik/agent-task.md and begin."
```

### claude's commit

```
commit 99c195c81624d9cd7a52c5b0d5f7e5b9156e9261
Author: Smoke Test <smoke@harmonik.test>
Date:   Thu May 14 02:50:43 2026 -0700

    Add SMOKE-OK marker line to marker.txt

diff --git a/marker.txt b/marker.txt
index 48cdce8..d8a0d0f 100644
--- a/marker.txt
+++ b/marker.txt
@@ -1 +1,2 @@
 placeholder
+SMOKE-OK
```

### Bead state after run

```
br show smoke-qiw
✓ smoke-qiw · Add SMOKE-OK marker line to marker.txt and commit   [● P1 · CLOSED]
Owner: gb · Type: task
Created: 2026-05-14 · Updated: 2026-05-14
Closed: 2026-05-14 (done)
```

### Clean kill

```
tmux kill-session -t smoke-1778752221 → SESSION GONE - CLEAN KILL CONFIRMED
```

---

## Green Checklist

| Check | Result |
|-------|--------|
| (a) marker.txt contains SMOKE-OK line | PASS — `git show 99c195c:marker.txt` → `placeholder\nSMOKE-OK` |
| (b) Worktree has claude's commit | PASS — commit `99c195c` on the run branch |
| (c) Smoke bead `smoke-qiw` is `closed` in `.beads/` | PASS — `issues.jsonl` status=closed, close_reason=done |
| (d) `run_completed` (success=true) in events.jsonl | PASS — T+24.1s, summary="auto-close: exit=0" |
| (e) Smoke session killed cleanly | PASS — tmux kill-session confirmed |

---

## Open Caveats

1. **61 commits ahead of origin** — the harmonik repo has not been pushed since the smoke work began. No remote validation of the fix set.

2. **noopRequestHandler stub** — `internal/daemon/` still uses the noop request handler for the Claude Hook Bridge socket (filed in commit `2104168` with ref hk-tjl40). The socket binds and accepts connections, but agent-to-daemon RPC (outcome envelope push from agent side) is not yet implemented. The operational close path currently relies entirely on the daemon-side quit injection rather than agent-initiated outcome emission.

3. **reviewloop paste-inject ordering** — `reviewloop.go` received the `pasteInjectQuitOnCommit` goroutine stub in `cbc4725`, but the paste-inject ordering fix (hk-zchbu) has not been independently validated in review-mode. Single-mode is the confirmed path.

4. **Multi-bead concurrent re-validation pending** — `--max-concurrent 1` was used throughout all smoke runs. The capacity gate release is confirmed for a single bead. Concurrent dispatch (multiple worktrees, multiple claude sessions) has not been smoke-tested.

5. **run_completed event, not bead_closed** — The terminal event in the stream is `run_completed` (success=true, summary="auto-close: exit=0"). A dedicated `bead_closed` event type is not yet emitted. The daemon infers close from process exit + git HEAD change.

---

## Phase 2 Unblock

OPERATIONAL GREEN unlocks the first meaningful Phase 2 capability: **the orchestrator can now dispatch beads via harmonik rather than via the Agent tool**.

Prior to this milestone, the only way to run a sub-agent task was to call `Agent(prompt=...)` directly from the orchestrator's context thread — burning orchestrator context and limiting parallelism to what the Agent tool supports. With harmonik operational, the orchestrator can instead:

1. Create a bead (`br create`) for each unit of work
2. Start harmonik against the project (`hk --project $REPO --max-concurrent N`)
3. Watch `events.jsonl` for `run_completed` signals

This gives true process-level isolation, persistent worktrees, and clean capacity management outside the orchestrator's context window — which is the core thesis of harmonik's "deterministic skeleton, probabilistic organs" architecture.

The first post-MVH unlock (concurrent runs) requires validating `--max-concurrent > 1` in smoke — that is the natural next step.
