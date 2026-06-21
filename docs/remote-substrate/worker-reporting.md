# Worker Reporting — periodic resource + problem telemetry (Phase 1)

Worker reporting is the Phase-1 observability layer over a remote-substrate worker.
On a timer the daemon samples each enabled worker's resources and derives a small set
of advisory problem flags, emitting a typed `worker_report` event per sample. There is
**no time-series store** in Phase 1: each sample is just an event, and the daemon event
log is the history you mine later (e.g. to pick a real "at what point are we maxing out?"
threshold).

It is the resource-snapshot sibling of the boot-time health check (`worker_unhealthy`):
same SSH transport, same emit path, same off-by-default behaviour.

Codename: `worker-report` (beads WR1–WR5). Planning artifacts:
`plans/2026-06-20-remote-node-telemetry-autoscale/`.

---

## What `worker_report` is

A periodic snapshot of one worker, carrying two things:

1. **A resource snapshot** — "at what point are we maxing out?"
2. **Problem flags** — "are there issues?" (an advisory list; empty/absent on a clean
   report).

It is collected by running an inline `sh -c` collector over the worker's SSH transport
(`internal/workers/telemetry.go` → `CollectReport`), exactly the way the boot health
check (`internal/workers/health.go` → `RunHealthCheck`) probes the same worker. A
slow or failing collection is logged to the daemon's stderr and dropped — it never
wedges the work loop and never touches dispatch.

---

## Cadence

The recurring poll (`internal/workers/report_poll.go` → `RunReportLoop`) runs on a
ticker driven by `report_interval_seconds` in `workers.yaml`. **Default: 60 s** when the
field is omitted or set `<= 0`.

The loop is **off-by-default**: if no worker is `enabled` (or the registry is empty) it
returns immediately without arming a ticker, so a deployment with no `workers.yaml`
behaves byte-identically to before this feature landed. Disabled workers — including a
worker the boot health check disabled in the registry after a probe failure — are
skipped on every tick.

---

## Payload fields

The `worker_report` event payload (`WorkerReportPayload`):

| Field             | JSON               | Meaning                                                            |
|-------------------|--------------------|-------------------------------------------------------------------|
| WorkerName        | `worker_name`      | The worker the snapshot describes.                                 |
| SampledAt         | `sampled_at`       | RFC 3339 UTC timestamp at sample time.                            |
| Load1             | `load1`            | 1-minute load average.                                            |
| Load5             | `load5`            | 5-minute load average.                                            |
| NCPU              | `ncpu`             | CPU count, so load is interpretable.                              |
| MemTotalMB        | `mem_total_mb`     | Total physical memory (MB).                                       |
| MemFreeMB         | `mem_free_mb`      | Available memory ≈ (free + inactive pages) (MB).                  |
| SwapUsedMB        | `swap_used_mb`     | Swap in use (MB) — the decisive "really out of headroom" signal. |
| DiskFreeMB        | `disk_free_mb`     | Free disk on the worktree volume (`repo_path`) (MB).             |
| ClaudeProcs       | `claude_procs`     | Count of running `claude --session-id` processes.                |
| WorktreeCount     | `worktree_count`   | `git worktree list` entries on the worker repo (main + linked).  |
| Problems          | `problems`         | Advisory problem flags; omitted entirely on a clean report.      |

### The three problem flags

A flag's **presence** in `problems` means the condition was detected. All three are
**advisory / report-only** in Phase 1 — nothing acts on them automatically.

- **`orphaned_claude`** — `claude_procs > 0` while the registry reports **no** harmonik
  run in flight. This is the "claude process lingers after its run completed" symptom.
  **Known false-positive surface:** the count also catches an operator-run `claude` on
  the box, the health-check permission-probe `claude`, and the brief window before a
  just-finished `claude` exits. So it can fire benignly — it is a signal to look, not a
  fault. (A dwell/grace guard would suppress those transients in a later phase.)
- **`disk_pressure`** — `disk_free_mb` below the disk floor. The floor is
  `disk_floor_mb` from `workers.yaml`, defaulting to **2048 MB (2 GB)** when unset.
- **`worktree_leak`** — `worktree_count` above the worker's worktree **baseline**, where
  `baseline = 1 (the main checkout) + max_slots (one run worktree per concurrent slot)`.
  A fully-loaded healthy worker holds exactly `1 + max_slots` worktrees and must **not**
  flag; a count above that means run worktrees were not cleaned up (the ghost-worktree
  class). When `max_slots` is unset it falls back to 4, and the baseline is floored at 2
  so a single legitimate run worktree never trips the signal.

---

## How an operator observes them

`worker_report` rides the **same surfacing path as `worker_unhealthy`**, automatically.
Both are emitted via the daemon's event bus (`bus.Emit`), and the bus delivers every
event it receives to every subscriber. There is no per-event-type allowlist or "notable
events" gate to add a new type to — the only filter is the caller-supplied `--types`
list, which **defaults to all event types**.

So an operator watches worker reports the same way they watch any other event — the
daemon's first-class subscriber stream:

```bash
# Every event, including worker_report and worker_unhealthy:
harmonik subscribe

# Just the worker telemetry:
harmonik subscribe --types worker_report,worker_unhealthy

# Pull out the problem flags with jq:
harmonik subscribe --types worker_report \
  | jq 'select(.payload.problems != null) | {worker: .payload.worker_name, problems: .payload.problems}'
```

`harmonik subscribe` replaces the brittle "tail `.harmonik/events/events.jsonl`" pattern
with a proper subscriber interface; the events also still land in that NDJSON event log,
which is the history mined later.

---

## Optional `workers.yaml` config knobs

Both knobs sit on the top-level `Config` (alongside `version` and `workers`), and both
are optional — omit them to take the defaults:

```yaml
version: 1
report_interval_seconds: 60   # poll cadence; default 60s when omitted or <= 0
disk_floor_mb: 2048           # disk_pressure floor (MB); default 2048 (2 GB) when omitted or <= 0
workers:
  - name: worker-mac-1
    transport: ssh
    host: worker-mac-1
    os: darwin
    repo_path: ~/harmonik-worker/repo
    max_slots: 4                # also the worktree_leak baseline input (baseline = 1 + max_slots)
    enabled: true
```

`max_slots` does double duty: it is the worker's concurrency cap **and** the input to the
`worktree_leak` baseline.

---

## Phase-1 scope (observability only)

This is **observability only**. Explicitly out of scope for Phase 1:

- **No autoscale.** Nothing in this path touches `SelectWorker`, dispatch, or concurrency.
- **`max_slots` stays hardcoded** per worker in `workers.yaml`; the reports do not adjust
  it.
- **The problem flags are advisory.** `orphaned_claude` in particular is report-only and
  has a known false-positive surface (see above) — no remediation is automated on any
  flag.

The intent is to accumulate real samples in the event log so a later phase can pick true
thresholds and, eventually, act on them.
