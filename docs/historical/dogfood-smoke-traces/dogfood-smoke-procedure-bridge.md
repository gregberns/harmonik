# Dogfood Smoke Procedure — Bridge Integration GREEN Run

**Bead:** hk-gql20.23 (Stream E: re-run smoke against real claude with full bridge wired)
**Baseline:** RED run documented at `docs/dogfood-smoke-run-2026-05-12.md`
**Target verdict:** GREEN

---

## 1. Purpose

This runbook is the GREEN-target re-run of the original RED smoke (hk-1n0cw.2, 2026-05-12). That run established a false-positive baseline: hk launched `claude` with no task context, no hook hooks materialized, and no bridge-protocol messages arrived; the bead closed on clean EOF alone. The integration work under hk-gql20 (Streams A–D) wires the full bridge end-to-end: LaunchSpec delivery on stdin, `.claude/settings.json` materialization (CHB-001..005), env-var injection (CHB-006), the `harmonik hook-relay` subcommand, the `agent_ready` timeout (HC-056), heartbeat ownership (HC-057), the direct-tmux substrate (PL-021b), deterministic window naming (WM-002a), and the `hk tmux-start` / PL-028b guard.

A GREEN result confirms that claude receives its task, performs real work (mutates `marker.txt` and commits), and the daemon closes the bead on a verified `outcome_emitted` event rather than on clean EOF alone.

---

## 2. Preconditions

- `claude` CLI version ≥ 2.1.140 reachable on PATH.
  ```
  claude --version
  ```
- `tmux` ≥ 3.0 installed and on PATH.
  ```
  tmux -V
  ```
- Operator shell is running **inside an active tmux session** (`$TMUX` is set). hk refuses to start when `$TMUX` is unset (PL-028b, exit code 24). If not already inside tmux, bootstrap first:
  ```
  hk tmux-start
  ```
  Then open a new window / pane for the setup commands below.
- `harmonik` binary built from the target commit and reachable as `hk` on PATH (see §3 below).
- `go` toolchain (1.22+) present for the build step.
- `br` (beads-rust CLI) reachable on PATH.

---

## 3. Setup script

Copy and run verbatim in a tmux pane. All paths are captured in `SMOKE_DIR` for use in subsequent sections.

```bash
#!/usr/bin/env bash
set -euo pipefail

# --- 1. Scratch directory ---
SMOKE_DIR=$(mktemp -d)
echo "SMOKE_DIR=${SMOKE_DIR}"

# --- 2. Git repo ---
git -C "$SMOKE_DIR" init -q
git -C "$SMOKE_DIR" config user.email "smoke@harmonik.local"
git -C "$SMOKE_DIR" config user.name  "Smoke Runner"
echo "# smoke repo" > "$SMOKE_DIR/README.md"
touch "$SMOKE_DIR/marker.txt"
git -C "$SMOKE_DIR" add -A
git -C "$SMOKE_DIR" commit -q -m "initial"

# --- 3. Beads init ---
br init --dir "$SMOKE_DIR" --prefix smoke

# --- 4. Disposable bead ---
BEAD_ID=$(br create \
  --dir "$SMOKE_DIR" \
  --title "Add SMOKE-OK marker line to marker.txt and commit" \
  --type task \
  --priority 1 \
  --labels "workflow:single" \
  --format id)
echo "BEAD_ID=${BEAD_ID}"

# --- 5. Build hk ---
REPO_ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
  || git rev-parse --show-toplevel)
go build -o /tmp/hk "$REPO_ROOT/cmd/harmonik"
echo "hk built: $(/tmp/hk --version 2>&1 | head -1)"

echo "---"
echo "Setup complete."
echo "  SMOKE_DIR=${SMOKE_DIR}"
echo "  BEAD_ID=${BEAD_ID}"
```

After running, note the values printed for `SMOKE_DIR` and `BEAD_ID`. Export them in your shell:

```bash
export SMOKE_DIR=<value printed above>
export BEAD_ID=<value printed above>
```

---

## 4. Run procedure

### Step 1 — Verify you are inside tmux

```bash
echo "TMUX=${TMUX}"
```

Must be non-empty. If empty, run `hk tmux-start` first (see §2).

### Step 2 — Start hk

```bash
/tmp/hk --project "$SMOKE_DIR" --max-concurrent 1 \
  > /tmp/hk-stdout.txt 2> /tmp/hk-stderr.txt &
HK_PID=$!
echo "hk PID: ${HK_PID}"
```

### Step 3 — Watch stderr for startup confirmation

```bash
tail -f /tmp/hk-stderr.txt
# Expect: "harmonik daemon starting in <SMOKE_DIR>"
# Then:   "daemon ready" (or ready-state indicator)
# Hit Ctrl-C when you see daemon ready.
```

### Step 4 — Observe the tmux window appear

Within a few seconds of hk picking up the bead, a new tmux window named after the bead ID appears in the current session. The window name is derived by WM-002a: because hk is running in `$TMUX`-reuse mode (`owns_session=false`), the window is prefixed `hk-<hash6>-`, e.g.:

```
hk-a1b2c3-smoke-abc123
```

List windows to confirm:

```bash
tmux list-windows
```

### Step 5 — Optionally watch claude work live

```bash
tmux select-window -t "hk-<hash6>-${BEAD_ID}"
# or
tmux attach-session -t "$(tmux display-message -p '#S')" \; select-window -t "hk-<hash6>-${BEAD_ID}"
```

claude's TUI is live in the pane (HC-054: `Session.Attach()` returns a pty stream). You can detach at any time with `Ctrl-B d` — this does not terminate the session.

### Step 6 — Wait for completion

hk will close the bead when `outcome_emitted` arrives via the Stop hook relay. A normal single-mode run completes in under 60 seconds. Monitor:

```bash
tail -f "$SMOKE_DIR/.harmonik/events.jsonl"
```

The run is complete when you see `run_completed` in the stream.

---

## 5. Observation channels

### hk stdout / stderr

```bash
cat /tmp/hk-stdout.txt
cat /tmp/hk-stderr.txt
```

Stderr should show daemon startup and bead claim lines. Stdout captures any daemon-level structured output.

### Events JSONL

Primary observability channel. All daemon lifecycle and bridge-relay messages land here.

```bash
cat "$SMOKE_DIR/.harmonik/events.jsonl" | jq .
```

Key event types in expected order:
1. `daemon_started`
2. `daemon_orphan_sweep_completed`
3. `run_started`
4. `agent_started` (once LaunchSpec delivered and claude exec'd)
5. `agent_ready` (arrives via SessionStart hook relay)
6. `agent_heartbeat` (daemon-emitted per HC-057, cadence 300 s)
7. `outcome_emitted` (arrives via Stop hook relay when claude finishes)
8. `run_completed`

### Tmux window contents

Live via `tmux select-window` (§4 Step 5). After completion the window is closed by hk per PL-021b cleanup.

### `.claude/settings.json` materialization (CHB-001..005)

Verify bridge hooks are present in the worktree before claude is exec'd. The worktree path appears in the `run_started` event payload as `workspace_path`.

```bash
WORKSPACE_PATH=$(jq -r 'select(.type=="run_started") | .payload.workspace_path' \
  "$SMOKE_DIR/.harmonik/events.jsonl" | head -1)
cat "$WORKSPACE_PATH/.claude/settings.json" | jq .hooks
```

All five hook event types (`SessionStart`, `Stop`, `SessionEnd`, `StopFailure`, `Notification`) must appear with `command: "harmonik"` and `args: ["hook-relay", "<event-kind>"]`.

### Worktree git log

After completion, verify claude committed:

```bash
git -C "$WORKSPACE_PATH" log --oneline -3
```

The commit message should describe the SMOKE-OK task. Also verify the content:

```bash
cat "$WORKSPACE_PATH/marker.txt"
```

Must contain `SMOKE-OK`.

---

## 6. Success criteria checklist

Run these after `run_completed` appears in events.jsonl. All items must pass for a GREEN verdict.

**1. marker.txt mutated to contain `SMOKE-OK`**

```bash
WORKSPACE_PATH=$(jq -r 'select(.type=="run_started") | .payload.workspace_path' \
  "$SMOKE_DIR/.harmonik/events.jsonl" | head -1)
grep -q "SMOKE-OK" "$WORKSPACE_PATH/marker.txt" && echo "PASS" || echo "FAIL"
```

**2. A commit with the SMOKE-OK work exists on the worktree branch**

```bash
git -C "$WORKSPACE_PATH" log --oneline | grep -i "smoke\|marker" \
  && echo "PASS" || echo "FAIL"
```

**3. `outcome_emitted` event arrived at the daemon (visible in events.jsonl)**

```bash
jq 'select(.type == "outcome_emitted")' "$SMOKE_DIR/.harmonik/events.jsonl" \
  && echo "PASS" || echo "FAIL — outcome_emitted not found"
```

**4. `agent_ready` event recorded (Stop hook relay fired)**

```bash
jq 'select(.type == "agent_ready")' "$SMOKE_DIR/.harmonik/events.jsonl" \
  && echo "PASS" || echo "FAIL — agent_ready not found"
```

**5. `.claude/settings.json` materialized with all five bridge hooks (CHB-001..003)**

```bash
WORKSPACE_PATH=$(jq -r 'select(.type=="run_started") | .payload.workspace_path' \
  "$SMOKE_DIR/.harmonik/events.jsonl" | head -1)
for EVT in SessionStart Stop SessionEnd StopFailure Notification; do
  jq -e ".hooks.${EVT}" "$WORKSPACE_PATH/.claude/settings.json" > /dev/null \
    && echo "PASS: ${EVT}" || echo "FAIL: ${EVT} missing"
done
```

**6. Bead closed with reason `done`**

```bash
br show "$BEAD_ID" --dir "$SMOKE_DIR" --format json \
  | jq '{status, close_reason}'
# Expect: {"status": "closed", "close_reason": "done"}
```

**7. `run_completed` has `success: true` (not a false-positive close)**

```bash
jq 'select(.type == "run_completed") | .payload.success' \
  "$SMOKE_DIR/.harmonik/events.jsonl"
# Expect: true
```

---

## 7. Failure modes

### claude exits 0 with no work done (false-positive, same as RED baseline)

Symptom: bead closed, `run_completed.success=true`, but `marker.txt` is empty and no new commit exists.

Checks:
- Did `agent_ready` arrive? If not, HC-056 timeout (30 s default) may have fired — look for `agent_failed{sub_reason=agent_ready_timeout}` in events.jsonl.
- Did `.claude/settings.json` materialize? If the file is missing or empty, CHB-002 materialization did not run before claude was exec'd. Check for the file at the workspace_path and inspect the daemon's stderr for write errors.
- Did claude receive the LaunchSpec on stdin? Confirm `agent_started` appears in events.jsonl. If it does not, the handler process failed before exec.

### tmux window never appeared

Symptom: no new tmux window; claude was never launched.

Checks:
- Is `$TMUX` set in the shell where hk was started? PL-028b: hk exits 24 when `$TMUX` is unset. Check `cat /tmp/hk-stderr.txt` for "tmux-session-unavailable".
- Did the PL-021b probe succeed? Look for the tmux availability check in hk stderr. If tmux is not on PATH at daemon startup, hk exits with code 22 (`ntm-unavailable`).
- Did PL-021c orphan sweep find a stale window with a colliding name from a prior run? Check `tmux list-windows` for a window named `hk-<hash6>-<bead_id>` that is not owned by this run.
- Check `daemon_orphan_sweep_completed` payload in events.jsonl for `tmux_windows_killed > 0` (unexpected if this is a fresh scratch dir).

### Stop hook never arrived at daemon

Symptom: `outcome_emitted` absent from events.jsonl; claude's window closed but the bead is still open or was reopened.

Checks:
- Was `.claude/settings.json` present in the worktree? If missing, Claude never registered hooks and no `harmonik hook-relay Stop` was invoked.
- Check `HARMONIK_DAEMON_SOCKET` was injected into Claude's env (CHB-006). If the socket path is wrong, the relay cannot dial. Look for `bridge_dial_failed` in events.jsonl or relay-side stderr.
- Check that the daemon socket exists: `ls -la "$SMOKE_DIR/.harmonik/daemon.sock"`. If absent, the daemon crashed or never reached ready state.
- HC-056 timeout: if `agent_ready` never arrived within 30 s, the daemon killed claude before the Stop hook could fire. Look for `agent_failed{sub_reason=agent_ready_timeout}`.

### CHB-024 `settings.local.json` guard fired

Symptom: `agent_failed{sub_reason=bridge_settings_shadowed}` in events.jsonl; claude never exec'd.

Cause: A `.claude/settings.local.json` file in the worktree contains `disableAllHooks: true` or a `hooks` block that shadows the bridge-required entries. `settings.local.json` takes precedence over `settings.json` in Claude's settings hierarchy.

Remediation: Remove or relocate the conflicting content from `settings.local.json` in the worktree. The bridge-required hooks live in `settings.json` (CHB-001); user hooks that must survive can be merged into `settings.json` per CHB-004.

### `agent_ready` timeout (HC-056)

Symptom: `agent_failed{sub_reason=agent_ready_timeout}` in events.jsonl; bead reopened.

Cause: claude did not emit `agent_ready` (via the SessionStart hook relay) within `Config.AgentReadyTimeout` (default 30 s).

Checks:
- Did the SessionStart hook fire? Look for any relay-side output in the pane before the window was killed.
- Is `harmonik` on PATH inside the claude subprocess's env? The hook `command: "harmonik"` must be resolvable (HC-042 pre-launch check). If the check failed, `agent_started` will not appear in events.jsonl.
- Is the daemon socket reachable? Relay subprocess connects to `HARMONIK_DAEMON_SOCKET`; if the socket is not yet listening when the relay fires, the relay's 5-second retry window may have exhausted.

---

## 8. Cleanup

### Stop hk (if still running)

```bash
kill "$HK_PID"
# hk drains in-flight runs and exits cleanly on SIGTERM per PL-016.
# Wait a few seconds then confirm:
wait "$HK_PID" 2>/dev/null || true
```

### Remove residual tmux windows

hk closes its handler windows on run completion. If any remain (e.g., hk was killed mid-run):

```bash
tmux list-windows | grep "^hk-" | awk -F: '{print $1}' | xargs -I{} tmux kill-window -t {}
```

### Remove scratch directory

```bash
rm -rf "$SMOKE_DIR"
echo "Cleaned up ${SMOKE_DIR}"
```

This is safe: `SMOKE_DIR` is a `mktemp -d` temporary directory created solely for this smoke run. It contains no operator data.

---

*Mirror of failure baseline: `docs/dogfood-smoke-run-2026-05-12.md`*
