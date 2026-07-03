# Research/Design — Harmonik supervisor CLI surface

> Component: `harmonik-supervisor-surface`. Round-3 (component build-out). Source: sub-agent (sonnet) over `/Users/gb/github/harmonik` specs + cmd/, 2026-05-30. **[SOURCED]** = existing harmonik pattern; **[DESIGN]** = proposal.

## TL;DR
- **`harmonik supervise`** = a new top-level subcommand group (parallel to `harmonik queue`) owning flywheel lifecycle: `start | stop | status | attach | restart | logs`. Does NOT embed cognition — writes a config file, shells out to launch the Pi extension in a tmux pane, tracks pid+heartbeat. [SOURCED tmux-start + queue CLI-dispatch pattern in `cmd/harmonik/main.go`]
- **`harmonik digest`** = separate read-only subcommand (no daemon required for snapshot mode). [DESIGN — no current analog]
- **Restart-on-crash:** a `--watch-restart` wrapper shim (~30-50 lines Go) holds the lock + respawns the Pi process on non-zero exit. No external supervisor (no systemd/launchd) needed.

## CLI shapes

**`harmonik digest [--project DIR] [--watch] [--json] [--since <event_id>]`**
Snapshot reads `.harmonik/events/events.jsonl` + `queue.json` + `daemon.pid` directly — no daemon for static mode. `--watch` = live TUI loop (polls/tails JSONL; subscribe socket needed when hk-6ynv4 lands). Exit 17 = daemon not running (when `--watch` needs socket). [SOURCED `harmonik handler status` no-daemon read pattern]

**`harmonik supervise start [flags]`** — `--model`, `--token-cap`, `--max-concurrent`, `--budget-cap` (USD/day), `--instructions <path>`, `--priority-source kerf|beads|file:<path>`, `--areas`, `--epic <work-codename>`, `--detach`. Sequence:
1. Check daemon liveness (ECONNREFUSED on `.harmonik/daemon.sock` → refuse with "daemon not running; start with: harmonik tmux-start").
2. Acquire `.harmonik/cognition/supervisor.lock` via `flock(LOCK_EX|LOCK_NB)` [SOURCED PL-002a daemon.pid pattern].
3. Atomic-write `.harmonik/cognition/config.json` (temp+rename) [SOURCED WM-026].
4. tmux session name `harmonik-<project_hash>-flywheel` [SOURCED PL-006a scoping].
5. `tmux new-session -d -s <name> -c <project_dir>` then `tmux send-keys` to start the Pi binary; `--detach` returns to shell.
6. Write `.harmonik/cognition/supervisor.pid` with the Pi process pid (inner process, not tmux session).
**`set-option remain-on-exit on`** at creation so a crash doesn't destroy the pane before the operator reads it.

**`harmonik supervise stop [--timeout N=30s]`** — read supervisor.pid → SIGTERM → wait → SIGKILL → release lock → optionally `tmux kill-session`. [SOURCED PL-011 SIGTERM+bounded-wait+SIGKILL]

**`harmonik supervise status [--json]`** — reads supervisor.pid + config.json + `.harmonik/cognition/heartbeat.json` (written periodically by the Pi extension): pid, running (via `kill(pid,0)`), uptime, heartbeat_age_s, context_fullness_pct, active/pending task counts. File-surface only, no socket. [SOURCED `harmonik handler status`]

**`harmonik supervise attach`** — `execve tmux attach-session -t <name>` [SOURCED PL-028 §iv]. Does NOT kill the loop.

**`harmonik supervise restart`** — `stop` then `start` reading existing `config.json` (parameters preserved in the file, not in process state).

**`harmonik supervise logs [--lines N] [--follow]`** — `tmux capture-pane -p -S -<n>` for snapshot; `--follow` wraps `tmux pipe-pane` to a temp file + tail. Pane IS the log at v1.

## tmux convention + a SPEC EXTENSION required
Session = `harmonik-<project_hash>-flywheel`. **Problem:** the orphan sweep PL-006 will kill any matching session — would inadvertently kill the flywheel pane if the daemon restarts. **Fix (pick one):** (a) add a flywheel-session exclusion clause to PL-006; OR (b) move flywheel naming OUTSIDE the prefix scope (e.g. `harmonik-flywheel-<project_hash>`). Either way, `process-lifecycle.md §4.2 PL-006` needs an update.

## Lifecycle / lock / restart
File: `.harmonik/cognition/supervisor.lock`, same `flock(LOCK_EX|LOCK_NB)` as PL-002a. With `--detach` the lock fd must be handed to a background goroutine — simpler: a wrapper shim that holds the lock + watches the Pi pid + respawns on crash if `--watch-restart`. ~30-50 LOC, no external supervisor. Without `--watch-restart`, `remain-on-exit` leaves the pane up and the operator runs `supervise restart` manually.

## Parameter propagation: config file (recommended)
**Recommend `.harmonik/cognition/config.json`** (atomic write per WM-026). Pi extension reads on startup (no hot-reload at v1; `restart` is the parameter-change operator action). Rationale: revisable, auditable, allows `restart` w/o re-supplying flags, avoids env-var pollution across the pane environment.

Example:
```json
{
  "schema_version": 1,
  "model": "claude-sonnet-4-6",
  "token_cap": 200000,
  "max_concurrent": 3,
  "budget_cap_usd_per_day": 20.00,
  "instructions_path": ".claude/flywheel-instructions.md",
  "priority_source": "kerf",
  "areas": ["foundation", "daemon"],
  "epic": "extqueue",
  "debounce_ms": 5000,
  "watchdog_ms": 300000,
  "warn_threshold": 0.80,
  "force_threshold": 0.95,
  "started_at": "2026-05-30T12:00:00Z",
  "daemon_instance_id": "01915abc-..."
}
```
`schema_version` follows N-1-readable convention (queue-model §3); `daemon_instance_id` lets the extension validate it's talking to the same daemon it started against.

## Priority source contract
| Value | Agent behavior |
|---|---|
| `kerf` | `kerf next --format=json` each cycle; default. |
| `beads` | `br ready --format json` (optionally filtered by areas/epic from config). |
| `file:<path>` | Static NDJSON of bead IDs; re-read each cycle (editable live). |
Owned by the Pi extension; `supervise status` reports active source.

## Integration with daemon
**Separated lifecycle (recommended):** `supervise start` REFUSES if daemon down (clear message + command to start it). No auto-start. Entangling lifecycles creates ambiguous responsibility for orphan sweep + pidfile lock.

## Spec extensions required
- `process-lifecycle.md §4.2 PL-006` — flywheel-session exclusion (or rename outside the scope).
- `process-lifecycle.md §4.10 PL-028` — add `harmonik supervise` and `harmonik digest` to the command-surface enumeration.
- `operator-nfr.md §4.3` — record the "daemon must be healthy before flywheel start" + separated-lifecycle invariant.
- `control-points.md` — add `priority_source` as an operator-controlled dispatch-shaping input.
- `operator-nfr.md §8` (exit-code taxonomy) — new exit code for `supervisor-already-running`.

## Files (harmonik)
specs/process-lifecycle.md PL-006a/PL-006/PL-011/PL-028; specs/workspace-model.md WM-026; cmd/harmonik/main.go (tmux-start + queue subcommand patterns); `.harmonik/handler-state.json` (file-surface read pattern).
