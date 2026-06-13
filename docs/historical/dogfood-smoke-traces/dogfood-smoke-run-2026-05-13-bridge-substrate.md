# Dogfood Smoke Run — 2026-05-13 — Bridge + Substrate

**Verdict:** RED  
**Bead:** hk-kqdpf.5  
**Date:** 2026-05-13  
**Runner:** automated smoke agent  
**Procedure:** `docs/dogfood-smoke-procedure-bridge.md`

---

## 1. Setup

### Git verification

```
git log --oneline -8
```

```
6dde546 chore(beads): close hk-kqdpf.4 — substrate wired into composition root
df31747 feat(daemon): wire tmuxSubstrate into composition root — set workLoopDeps.substrate (hk-kqdpf.4)
5385c70 chore(beads): close hk-kqdpf.1 — hookRelayEnabled dual path collapsed
2eee881 feat(daemon): collapse hookRelayEnabled dual path — bridge is the only path (hk-kqdpf.1)
fc58bbe feat(tmux): add SweepOrphanTmuxSessions to kill dead harmonik sessions (hk-kqdpf.3)
0bedede chore(beads): close hk-kqdpf.2 (reviewer waitAgentReady)
0f004ba feat(daemon): wire waitAgentReady into reviewloop reviewer phase (hk-kqdpf.2)
c584dd8 chore(beads): hk-kqdpf epic + .1..5 child beads for bridge follow-up
```

Required commits present: 6dde546 (substrate-wiring), 5385c70 (dual-path-close), fc58bbe (orphansession). PASS.

### Preconditions

- `claude --version`: `2.1.140 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br`: reachable at `/Users/gb/.local/bin/br` — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS

### Build

```
go build -o /tmp/hk ./cmd/harmonik
```

Exit 0. Binary built successfully.

### Scratch directory setup

```
SMOKE_DIR=/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.Fqx5CwD9Vx
```

- `git init`, `user.email smoke@harmonik.local`, `user.name Smoke Runner`
- `echo "# smoke repo" > README.md && touch marker.txt && git add -A && git commit -m "initial"`
- `br init --prefix smoke` — PASS
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels "workflow:single"` → **smoke-40x**

---

## 2. Run

### Tmux session

Created detached tmux session `smoke-1778692917`:

```
tmux new-session -d -s smoke-1778692917 -x 220 -y 50 \
  "cd $SMOKE_DIR && /tmp/hk --project $SMOKE_DIR --max-concurrent 1 \
    > $SMOKE_DIR/hk-stdout.txt 2> $SMOKE_DIR/hk-stderr.txt; sleep 120"
```

hk was launched inside a tmux session (so `$TMUX` was set). The daemon therefore did NOT panic on PL-028b ($TMUX check).

### hk startup (hk-stderr.txt)

```
harmonik daemon starting in /var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.Fqx5CwD9Vx
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x2 addr=0x10 pc=0x1044b0408]

goroutine 81 [running]:
github.com/gregberns/harmonik/internal/handlercontract.(*Watcher).Done(...)
        /Users/gb/github/harmonik/internal/handlercontract/watcher_hc011.go:233
github.com/gregberns/harmonik/internal/daemon.beadRunOne.func1()
        /Users/gb/github/harmonik/internal/daemon/workloop.go:672 +0x28
created by github.com/gregberns/harmonik/internal/daemon.beadRunOne in goroutine 6
        /Users/gb/github/harmonik/internal/daemon/workloop.go:670 +0x15a0
```

The daemon panicked with a SIGSEGV immediately after picking up the bead.

---

## 3. Stage-by-stage analysis

### Stage 1: daemon_started — PASS

Event recorded. Daemon started, pid 9282.

### Stage 2: daemon_orphan_sweep_completed — PASS

Event recorded. No stale sessions/windows.

### Stage 3: run_started — PASS

Event recorded. Workspace path:
```
/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.Fqx5CwD9Vx/.harmonik/worktrees/019e225c-0c86-711d-8881-2dd682b8f728
```

### Stage 4: .claude/settings.json materialization — PASS

All five bridge hooks materialized correctly in the workspace before the crash:
- `SessionStart` → `harmonik hook-relay SessionStart`
- `Stop` → `harmonik hook-relay Stop`
- `SessionEnd` → `harmonik hook-relay SessionEnd`
- `StopFailure` → `harmonik hook-relay StopFailure`
- `Notification` → `harmonik hook-relay Notification`

CHB-001..005 materialization worked correctly.

### Stage 5: agent_started — NOT REACHED

handler.Launch was called with spec.Substrate != nil (substrate wired by hk-kqdpf.4). This routed to `launchViaSubstrate`. The tmux window spawn appears to have succeeded (bead status changed to IN_PROGRESS and `run_started` was emitted), but the daemon panicked before claude could execute.

**Root cause:** `launchViaSubstrate` returns `watcher = nil` when `SubstrateSession.Stdout()` is nil (the tmux path). The comment in `handler/handler.go:263-265` documents this: "for tmux-hosted sessions the bridge wire is the daemon Unix socket, so Stdout() returns nil and the watcher is nil (the caller uses HookSessionStore.WaitForOutcome for completion detection instead)."

The workloop at `daemon/workloop.go:670` launches a goroutine:

```go
go func() {
    select {
    case <-watcher.Done():   // LINE 672 — panics: watcher is nil
        readyCancel()
    case <-readyCtx.Done():
    }
}()
```

`watcher.Done()` is called on a nil pointer, causing the SIGSEGV.

### Additional nil dereference sites (not yet hit but latent)

The same nil watcher would also panic at:
- `workloop.go:687`: `<-watcher.Done()` (HC-056 timeout path)
- `workloop.go:727`: `watcher.Err()` (terminal event mapping)
- `waitsocketgrace.go:74`: `<-watcher.Done()` (race in waitWithSocketGrace)
- `waitsocketgrace.go:80`: `<-watcher.Done()` (drain after ctx cancel)

### Stages 6-8: agent_ready, outcome_emitted, run_completed — NOT REACHED

The last events in events.jsonl are `agent_ready` and `skills_provisioned` from the LaunchSpec/pre-exec message path — these are pre-exec synthetic events from `buildClaudeLaunchSpec` artifacts, emitted before Launch returns. Claude never actually executed.

### Bead state

`smoke-40x` remained `IN_PROGRESS` (daemon claimed it before crashing; never got to close).

### marker.txt

Empty. No SMOKE-OK written, no commit made.

---

## 4. Full events.jsonl

```jsonl
{"event_id":"019e225c-0bfa-786c-a559-3e29bf3591c4","schema_version":1,"type":"daemon_started","timestamp_wall":"2026-05-13T10:21:58.266552-07:00","source_subsystem":"eventbus","payload":{"binary_commit_hash":"unknown","pid":9282,"started_at":"2026-05-13T17:21:58Z"}}
{"event_id":"019e225c-0c59-7e6f-9bd5-76adb20589da","schema_version":1,"type":"daemon_orphan_sweep_completed","timestamp_wall":"2026-05-13T10:21:58.361946-07:00","source_subsystem":"eventbus","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-13T17:21:58Z","tmux_sessions_killed":0,"tmux_windows_killed":0}}
{"event_id":"019e225c-0d24-7e11-a275-ca7f7e89895f","schema_version":1,"type":"run_started","timestamp_wall":"2026-05-13T10:21:58.564922-07:00","source_subsystem":"eventbus","payload":{"bead_id":"smoke-40x","run_id":"019e225c-0c86-711d-8881-2dd682b8f728","started_at":"2026-05-13T17:21:58Z","workspace_path":"/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.Fqx5CwD9Vx/.harmonik/worktrees/019e225c-0c86-711d-8881-2dd682b8f728"}}
{"event_id":"019e225c-0d36-75ff-a411-38c9f8561416","schema_version":1,"type":"handler_capabilities","timestamp_wall":"2026-05-13T10:21:58.582394-07:00","run_id":"019e225c-0c86-711d-8881-2dd682b8f728","source_subsystem":"eventbus","payload":{"claude_session_id":"019e225c-0d29-7497-8db5-0e061d89e9a8","supported_versions":[1],"type":"handler_capabilities"}}
{"event_id":"019e225c-0d36-7728-9705-891cd1e80253","schema_version":1,"type":"session_log_location","timestamp_wall":"2026-05-13T10:21:58.582469-07:00","run_id":"019e225c-0c86-711d-8881-2dd682b8f728","source_subsystem":"eventbus","payload":{"agent_type":"claude-code","log_format":"jsonl","log_path":"/Users/gb/.claude/projects/var-folders-s9-kq5h9q_17t571w_xq_1q9f2r0000gp-T-tmp.Fqx5CwD9Vx-.harmonik-worktrees-019e225c-0c86-711d-8881-2dd682b8f728/019e225c-0d29-7497-8db5-0e061d89e9a8.jsonl","node_id":"bead/smoke-40x","run_id":"019e225c-0c86-711d-8881-2dd682b8f728","session_id":"019e225c-0d36-74c6-808b-422af8630d28","type":"session_log_location"}}
{"event_id":"019e225c-0d36-779d-8c31-cd7579354c38","schema_version":1,"type":"skills_provisioned","timestamp_wall":"2026-05-13T10:21:58.582499-07:00","run_id":"019e225c-0c86-711d-8881-2dd682b8f728","source_subsystem":"eventbus","payload":{"run_id":"019e225c-0c86-711d-8881-2dd682b8f728","session_id":"019e225c-0d36-74c6-808b-422af8630d28","skills":[],"type":"skills_provisioned"}}
{"event_id":"019e225c-0d36-77fa-a5ef-ee9864ab7bfc","schema_version":1,"type":"agent_ready","timestamp_wall":"2026-05-13T10:21:58.582523-07:00","run_id":"019e225c-0c86-711d-8881-2dd682b8f728","source_subsystem":"eventbus","payload":{"capabilities":[],"session_id":"019e225c-0d36-74c6-808b-422af8630d28","type":"agent_ready"}}
```

Note: the `agent_ready` event here is the pre-exec synthetic event from the LaunchSpec artifacts (emitted at Step 3 of the workloop before Launch is called). It does NOT represent a real SessionStart hook relay from claude.

---

## 5. Success criteria checklist

| Criterion | Result |
|-----------|--------|
| 1. marker.txt contains SMOKE-OK | FAIL — file empty |
| 2. Commit with SMOKE-OK work on worktree branch | FAIL — no new commit |
| 3. `outcome_emitted` in events.jsonl | FAIL — never reached |
| 4. `agent_ready` via Stop hook relay | FAIL — synthetic pre-exec event only; no real relay |
| 5. `.claude/settings.json` with all five bridge hooks | PASS — materialized correctly |
| 6. Bead closed with reason `done` | FAIL — bead stuck IN_PROGRESS |
| 7. `run_completed` has `success: true` | FAIL — never emitted |

---

## 6. Root cause

**Bug:** Nil pointer dereference in `beadRunOne` (workloop.go:672) when substrate path is active.

When `deps.substrate != nil` (wired by hk-kqdpf.4 on main), `handler.Launch` routes to `launchViaSubstrate` which returns `watcher = nil` for tmux-hosted sessions. The workloop does not check for nil before using the watcher:

```go
// workloop.go:670-676
go func() {
    select {
    case <-watcher.Done():  // PANIC: watcher is nil
        readyCancel()
    case <-readyCtx.Done():
    }
}()
```

The `handler.go` comment at line 263 documents the intended contract ("callers use HookSessionStore.WaitForOutcome for completion detection instead") but the workloop does not implement the substrate-specific completion path. Five call sites need nil-guard or a substrate-aware branch:

1. `workloop.go:672` — goroutine watcher-done cancel
2. `workloop.go:687` — HC-056 drain after kill
3. `workloop.go:727` — `watcher.Err()`
4. `waitsocketgrace.go:74` — race select watcher.Done()
5. `waitsocketgrace.go:80` — drain after ctx cancel

---

## 7. What worked

- Daemon started, orphan sweep ran cleanly
- Bead was claimed and run_started emitted
- Workspace worktree was set up with correct git config
- `.claude/settings.json` materialized with all five hook event types (CHB-001..005 PASS)
- Pre-exec messages (handler_capabilities, session_log_location, skills_provisioned, agent_ready) emitted correctly
- PL-028b ($TMUX guard) did not false-fire — hk detected the tmux environment correctly
- Substrate was non-nil, so launchViaSubstrate was invoked — the dispatch routing is correct

---

## 8. Follow-up beads filed

### hk-e2kwq (P0 bug, blocks hk-kqdpf)

**Title:** Fix nil watcher dereference in workloop when substrate path returns nil watcher

Five sites in `workloop.go` and `waitsocketgrace.go` call `watcher.Done()` or `watcher.Err()` without nil-guarding. When the substrate (tmux) path is active, `launchViaSubstrate` returns `watcher=nil`. The fix must: (1) nil-guard the watcher-done goroutine at line 670, skipping it when watcher is nil; (2) nil-guard or branch the HC-056 drain at line 687; (3) nil-guard `watcher.Err()` at line 727; (4) nil-guard both `watcher.Done()` calls in `waitWithSocketGrace` (waitsocketgrace.go:74,80), using the socket-outcome path exclusively when watcher is nil.

---

## 9. Cleanup

- Tmux session `smoke-1778692917` killed via `tmux kill-session`.
- Scratch directory `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.Fqx5CwD9Vx` left in place (temp dir, will be cleaned by OS).
- hk process (pid 9282) already dead (panicked).

---

*Baseline RED run: `docs/dogfood-smoke-run-2026-05-12.md`*
