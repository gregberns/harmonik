# Phase 1 — Worker reporting (slow poll). Concrete spec.

**Codename:** `worker-report` · **Date:** 2026-06-20 · **Scope:** observability only — `max_slots` stays hardcoded, dispatch behavior unchanged.

Phase 1 answers the operator's two questions and nothing else:
- **"At what point are we maxing out?"** → a periodic resource snapshot, logged over time so a future max can be chosen from real data.
- **"Are there issues?"** → problem flags the box knows but never says today (orphaned claude, worktree leak, disk pressure).

It is pure addition to `internal/workers/`. It does **not** touch chani's substrate files (SSHRunner / tmuxsubstrate / reviewloop), so it can land in parallel with that lane.

---

## Why this fits the existing code with almost no new surface

`internal/workers/health.go` already does exactly the shape we need:
- runs commands on the worker via `tmux.CommandRunner` (the same `SSHRunner` that's everywhere else): `runner.Command(ctx, argv...)`, capture stdout;
- emits a typed event (`worker_unhealthy`) via an `EmitFunc` that matches `bus.Emit`, registered through `core.RegisterEventType`.

Phase 1 is a **sibling** of `RunHealthCheck` — `CollectReport` — using the same runner, the same emit pattern, the same registry. No new transport, no remote process, no new subsystem boundary.

---

## Data model — one struct

```go
// WorkerReportPayload — periodic worker resource + problem snapshot.
// Durability class: O (ordinary — operator observability). Event: "worker_report".
type WorkerReportPayload struct {
    WorkerName string `json:"worker_name"`
    SampledAt  string `json:"sampled_at"` // RFC3339 UTC

    // "At what point are we maxing out?" — resource snapshot.
    Load1        float64 `json:"load1"`
    Load5        float64 `json:"load5"`
    NCPU         int     `json:"ncpu"`            // so load is interpretable
    MemTotalMB   int64   `json:"mem_total_mb"`
    MemFreeMB    int64   `json:"mem_free_mb"`
    SwapUsedMB   int64   `json:"swap_used_mb"`    // the decisive "really out of headroom" signal
    DiskFreeMB   int64   `json:"disk_free_mb"`    // on the worktree volume (repo_path)
    ClaudeProcs  int     `json:"claude_procs"`    // running `claude --session-id` count

    // "Are there issues?" — problem flags (true = problem detected).
    Problems []string `json:"problems,omitempty"` // e.g. "orphaned_claude","worktree_leak","disk_pressure"
}
```

No time-series store in Phase 1. Each sample is emitted as an event; the event log *is* the history we mine later to pick a max.

---

## The collector — one `sh -c` on the worker (no script file to install)

Mirror the probe pattern: one inline command returning parseable lines. Darwin sources:

```sh
# emitted as a single runner.Command(ctx, "sh", "-c", <below>)
echo "load=$(sysctl -n vm.loadavg | tr -d '{}')"           # {1.20 1.10 0.95}
echo "ncpu=$(sysctl -n hw.ncpu)"
echo "memtotal=$(sysctl -n hw.memsize)"
echo "vmstat<<$(vm_stat)"                                   # free/inactive pages → MemFreeMB
echo "swap=$(sysctl -n vm.swapusage)"                       # "... used = N.NM ..."
echo "disk=$(df -m <repo_path> | tail -1)"                  # available MB column
echo "claude=$(pgrep -f 'claude --session-id' | wc -l | tr -d ' ')"
```

Parser lives in Go (`telemetry.go`), one small function with a unit test fed canned worker output (no SSH needed to test parsing — same style as the existing probe tests).

## Problem detection (the "are there issues?" flags)

Cheap, derived from the same poll plus one or two extra worker reads. Each is a boolean → string in `Problems`:
- **`orphaned_claude`** — `claude_procs > 0` while the daemon has **no in-flight run** assigned to this worker (central knows its own `Registry.inFlight`). This is the exact "claude exits 0 in 9s but a process lingers for minutes" symptom from the chani handoff — surfaced automatically instead of found by SSHing in.
- **`worktree_leak`** — count of `git -C <repo_path> worktree list` entries beyond a small expected baseline (or stale `*/worktrees-*` dirs). The ghost `/Users/gb/harmonik-worker/` class.
- **`disk_pressure`** — `DiskFreeMB` below a configured floor.

Each problem also emits/raises through the existing operator-surfacing path (same `EmitFunc`), so they show up without the operator polling.

---

## Wiring

- New `internal/workers/telemetry.go`: `CollectReport(ctx, runner, w, reg, emit) (WorkerReportPayload, error)` + the parser. Register `worker_report` (and reuse for problem surfacing) via `core.RegisterEventType`, same `init()` pattern as `health.go`.
- Driven on a **timer**, alongside/just after the existing health probe cadence (the periodic loop that already calls `RunHealthCheck`). Find that caller and add the report poll there. One config knob `report_interval` (default e.g. 60s); thresholds (`disk_floor_mb`, etc.) configurable, **none hardcoded** — but with defaults, so workers.yaml needs no new required fields.
- **Off-by-default / no-op:** empty registry → nothing runs (same as today). `emit == nil` → silent. Disabled worker → skipped, exactly like `RunHealthCheck`.

`max_slots` and `SelectWorker` are **untouched** in Phase 1.

---

## Beads (codename:worker-report)

1. **WR1 (`hk-9wbl`)** — `WorkerReportPayload` struct + `worker_report` event registration + parser, with a parser unit test fed canned darwin output. (foundation; no SSH)
2. **WR2 (`hk-ec9v`)** — `CollectReport` using the SSH `CommandRunner` (the inline `sh -c` collector), with a test using a fake runner returning canned output (mirror `health_test.go`). Depends on WR1.
3. **WR3 (`hk-jn3u`)** — wire `CollectReport` into the periodic health/poll loop on a `report_interval` timer; config knobs with defaults; off-by-default behavior + test. Depends on WR2.
4. **WR4 (`hk-b2f9`)** — problem detection: `orphaned_claude` (cross-check vs `Registry.inFlight`), `worktree_leak`, `disk_pressure`; flags into `Problems`; surface via emit. Tests per flag. Depends on WR2.
5. **WR5 (`hk-p86m`)** — operator surfacing: confirm `worker_report` + problem events reach the operator (comms/log/status line) the same way `worker_unhealthy` does; a short doc note in `docs/remote-substrate/`. Depends on WR3+WR4.

Acceptance: with a worker enabled, central logs a resource snapshot every interval, and an injected orphan/leak/low-disk condition raises the matching problem to the operator — all with `max_slots` unchanged.

---

## Explicitly NOT in Phase 1
- No dynamic concurrency / AIMD controller (Phase 3, after we have history).
- No live in-run breach alerts (Phase 2).
- No resident worker process (Phase 3).
- No `SelectWorker` / dispatch changes.
