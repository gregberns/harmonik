# Dogfood Smoke Run — 2026-05-12

**Bead:** hk-1n0cw.2 (Smoke-2: run hk against a disposable bead in an isolated test environment)
**Date:** 2026-05-12 (wall clock UTC)
**Implementer:** sub-agent worktree-agent-smoke2
**Verdict:** RED — bead closed as "success" but claude did no work; protocol mismatch is total

---

## 1. Environment

| Item | Value |
|------|-------|
| hk binary | `/tmp/hk` built from HEAD `1bdcf80895c46bff79b2fac8d556bd503f9f0213` |
| claude CLI | `/Users/gb/.local/bin/claude`, version `2.1.140 (Claude Code)` |
| scratch dir | `/private/var/folders/.../smoke-env` (mktemp -d, git init, br init) |
| smoke bead ID | `smoke-env-98q` (`workflow:single` label, P1) |
| hk flags | `--project <scratch-dir> --max-concurrent 1` |

---

## 2. Exact Command Invoked

```
/tmp/hk --project /private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.gkshAHKVh3/smoke-env --max-concurrent 1
```

Run redirected: `stdout → /tmp/hk-stdout.txt`, `stderr → /tmp/hk-stderr.txt`.

---

## 3. Setup

```
scratch_dir: /private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.gkshAHKVh3/smoke-env
git init + initial commit (README.md + marker.txt)
br init  → .beads/ (prefix: smoke-env)
br create "Add SMOKE-OK marker line to marker.txt and commit" \
    --type task --priority 1 --labels "workflow:single"
→ smoke-env-98q created, status=open
```

---

## 4. hk stdout / stderr

**hk stdout:** 0 bytes (empty).

**hk stderr:**
```
harmonik daemon starting in /private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.gkshAHKVh3/smoke-env
```

No additional lines. No launch errors, no crash, no stack trace.

---

## 5. Events JSONL Trail (verbatim)

```jsonl
{"event_id":"019e1ea3-9a11-7aec-aa13-04e3d76c4132","schema_version":1,"type":"daemon_started","timestamp_wall":"2026-05-12T17:01:38.833716-07:00","source_subsystem":"eventbus","payload":{"binary_commit_hash":"unknown","pid":76638,"started_at":"2026-05-13T00:01:38Z"}}
{"event_id":"019e1ea3-9a83-7395-853a-9748a35dc6b0","schema_version":1,"type":"daemon_orphan_sweep_completed","timestamp_wall":"2026-05-12T17:01:38.947235-07:00","source_subsystem":"eventbus","payload":{"br_subprocesses_killed":0,"locks_cleared":0,"reconciliation_locks_removed":0,"stale_intents_observed":0,"subprocesses_killed":0,"swept_at":"2026-05-13T00:01:38Z","tmux_sessions_killed":0}}
{"event_id":"019e1ea3-9b82-73e0-bce1-2fbec1c874f3","schema_version":1,"type":"run_started","timestamp_wall":"2026-05-12T17:01:39.202254-07:00","source_subsystem":"eventbus","payload":{"bead_id":"smoke-env-98q","run_id":"019e1ea3-9aa2-7831-98f7-6304791d8f78","started_at":"2026-05-13T00:01:39Z","workspace_path":"/private/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.gkshAHKVh3/smoke-env/.harmonik/worktrees/019e1ea3-9aa2-7831-98f7-6304791d8f78"}}
{"event_id":"019e1ea3-a9f1-7c6b-b724-e92e16aa76fd","schema_version":1,"type":"run_completed","timestamp_wall":"2026-05-12T17:01:42.897814-07:00","source_subsystem":"eventbus","payload":{"ended_at":"2026-05-13T00:01:42Z","run_id":"019e1ea3-9aa2-7831-98f7-6304791d8f78","success":true,"summary":"auto-close: exit=0"}}
```

**Sequence:**
- T+0.0s: `daemon_started`
- T+0.1s: `daemon_orphan_sweep_completed`
- T+0.4s: `run_started` (bead smoke-env-98q, worktree path created)
- T+4.1s: `run_completed` (success=true, "auto-close: exit=0")

**Missing events:** No `agent_ready`, `agent_failed`, or any handler-contract progress-stream events. This is expected because the real claude CLI does NOT emit `agent_ready` NDJSON — see §8 below.

---

## 6. Verdict File

**Expected path:** `<workspace_path>/.harmonik/review.json`
**Mode:** `workflow:single` → no reviewer phase → no verdict file expected. Confirmed: no verdict file was created or looked for.

---

## 7. Final Bead State

```json
{
  "id": "smoke-env-98q",
  "status": "closed",
  "close_reason": "done",
  "updated_at": "2026-05-13T00:01:42.850247Z",
  "closed_at": "2026-05-13T00:01:42.849971Z"
}
```

Bead was closed. **This is a FALSE POSITIVE.** The bead was closed because claude exited 0 and the watcher saw clean EOF (nil error). claude did not perform the actual work (marker.txt is still empty, no new commit was made).

---

## 8. Worktree State

The git worktree at `<scratch-dir>/.harmonik/worktrees/<run-id>` was:
- **Created:** `run_started` event confirms creation succeeded.
- **CWD for claude:** the worktree path (worktree branch of scratch repo at HEAD).
- **Cleaned up:** `defer removeWorktree` in `workloop.go:454` removed it after the run. The `.harmonik/worktrees/` dir is empty post-run.

**marker.txt:** empty. No lines added. No new commit in the scratch repo.

---

## 9. What claude actually did (observed)

When `claude` is invoked:
1. **No `-p`/`--print` flag** → launches in interactive mode.
2. **Stdout is a pipe** (not a TTY) → per claude's help text, it notes "workspace trust dialog is skipped when Claude is run in non-interactive mode (via -p, or when stdout is not a TTY)".
3. **stdin:** hk wrote the LaunchSpec JSON, then left stdin open. But hk's `handler.Launch` (line 122–143) does NOT currently write a LaunchSpec to stdin — it just starts the process. The LaunchSpec struct in `handler.LaunchSpec` (the workloop's `spec`) contains Binary/Args/Env/WorkDir/Role only; the JSON-encoded LaunchSpec described in `docs/dogfood-smoke-trace.md §2` is NOT actually written to stdin at MVH.
4. claude received **empty stdin** (hk wrote nothing to its stdin pipe).
5. claude exited in ~3.7 seconds with **exit code 0** and **0 bytes on stdout**.
6. Watcher saw **EOF on stdout → clean exit (nil error)**.
7. workloop: `exit=0 && !watcherFailed` → `CloseBead` → bead closed as done.

**Root cause of false-positive close:** The watcher's clean-EOF = nil-error semantics (correct per protocol) allow any subprocess that exits 0 to close its bead, regardless of whether it performed the actual work. The real claude CLI is not a harmonik handler — it does not emit `agent_ready` nor any NDJSON progress events.

---

## 10. Key Surprises / Gaps

### Gap A — claude receives no instruction: no LaunchSpec on stdin, no prompt, no bead context (CRITICAL)

**Expected (per dogfood-smoke-trace.md §2):** Daemon writes LaunchSpec JSON to subprocess stdin.
**Observed:** `handler.Launch` (`internal/handler/handler.go:122–143`) starts the subprocess but writes NOTHING to stdin. The LaunchSpec struct passed from workloop is Binary/Args/Env/WorkDir/Role only — not the full JSON schema from the spec. The JSON-encoded LaunchSpec injection is not yet wired at MVH.

**Consequence:** claude has no task description, no bead_id, no workspace_path, no workflow_mode. It can't do the work because it doesn't know what the work is.

**Cite:** `internal/handler/handler.go:122–143`; `docs/dogfood-smoke-trace.md §2`.

### Gap B — claude CLI does not speak the harmonik NDJSON progress-stream protocol

**Expected (per dogfood-smoke-trace.md §6):** Subprocess emits `agent_ready` NDJSON on stdout within timeout.
**Observed:** claude emits nothing (0 bytes on stdout when run non-interactively with empty stdin). The watcher sees EOF → nil error. No `agent_ready` event is ever detected. DetectReady never fires.

**Consequence:** The daemon never receives an `agent_ready` signal, but this doesn't currently block the run — the daemon waits for `<-watcher.Done()` and `sess.Wait()`, and the watcher finishes cleanly on EOF. There is no timeout enforced on receiving `agent_ready`. The work-loop succeeds without the subprocess ever signaling readiness.

**This means:** Any subprocess that exits 0 — including a blank shell script — will close a bead as "done" regardless of whether it did the work. The `agent_ready` handshake is completely bypassed at MVH.

**Cite:** `internal/handler/adapter_claudecode.go:88–95`; `internal/handlercontract/watcher_hc011.go:378–381` (EOF → nil error).

### Gap C — No timeout on `agent_ready` reception

**Expected (per dogfood-smoke-trace.md §6):** `agent_ready` MUST arrive within `spec.timeout` seconds.
**Observed:** No timeout is enforced. The daemon blocks on `<-watcher.Done()` indefinitely until the subprocess exits. If claude launched an interactive TUI session (if stdout were a TTY), hk would block forever.

**Cite:** `internal/daemon/workloop.go:504` (`<-watcher.Done()` with no select/timeout).

### Gap D — `binary_commit_hash` is always "unknown"

**Expected (per event-model.md):** The commit hash of the binary should be injected at build time.
**Observed:** `daemon_started` event shows `"binary_commit_hash":"unknown"`.

**Cite:** `cmd/harmonik/main.go:249` (TODO: inject from ldflags).

### Gap E — HandlerEnv=nil means claude inherits parent env; HARMONIK_PROJECT_HASH not injected

**Expected (per dogfood-smoke-trace.md §4):** Daemon MUST inject `HARMONIK_PROJECT_HASH`. If `spec.Env=nil`, subprocess inherits no environment.
**Observed:** `HandlerEnv` is nil (not set in main.go). `cmd.Env=nil` → subprocess inherits full parent env (correct behavior per Go stdlib). But `HARMONIK_PROJECT_HASH` is never injected — `internal/lifecycle/provenance.go` exists but is not called from the workloop env construction path.

**Consequence:** claude has PATH/HOME/etc. (so it can run), but the provenance env var contract is violated.

**Cite:** `internal/daemon/workloop.go:86–89`; `cmd/harmonik/main.go` (HandlerEnv not populated).

---

## 11. What worked correctly

- hk started cleanly, created the .harmonik directory structure.
- `br ready` correctly found the one open bead (smoke-env-98q).
- `git worktree add -b` succeeded on the scratch repo.
- brcli integration: `ClaimBead` → `in_progress`, `CloseBead` → `closed` all worked.
- JSONL events written correctly: daemon_started, orphan_sweep, run_started, run_completed.
- Worktree cleanup (`removeWorktree`) succeeded — no stale worktrees left.
- hk shutdown cleanly on SIGTERM (exit 0).
- workflow:single mode resolved correctly (no review-loop attempted).

---

## 12. Summary

The daemon mechanics (bead lifecycle, JSONL events, worktree create/remove, br integration) work correctly. The integration surface with the claude subprocess is entirely missing: claude receives no task description, emits no NDJSON progress events, and exits 0 immediately — triggering a false-positive bead close. The "loop" runs but does nothing.

**Follow-up beads filed:** hk-1n0cw.3 through hk-1n0cw.7 (see below).

---

## 13. Follow-up Beads Filed

| ID | Priority | Title |
|----|----------|-------|
| hk-1n0cw.3 | P0 | Wire LaunchSpec JSON delivery to subprocess stdin (handler.Launch writes nothing to stdin at MVH) |
| hk-1n0cw.4 | P0 | claude receives no task context: bead body, workspace_path, and workflow instruction not delivered |
| hk-1n0cw.5 | P1 | Enforce agent_ready timeout: watcher.Done() blocks indefinitely with no deadline |
| hk-1n0cw.6 | P1 | HARMONIK_PROJECT_HASH not injected into handler subprocess env |
| hk-1n0cw.7 | P2 | binary_commit_hash always "unknown": ldflags injection not wired in build |
