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

---

# Resource breach — threshold-crossing alerts (Phase 2)

Phase 2 (codename `worker-breach`, beads PB1–PB4) adds a second event type on top of
the same poll loop: `resource_breach`. Where `worker_report` is a *periodic snapshot*
("here is the box right now, every minute"), `resource_breach` is an *event on a
threshold crossing* — it fires only when a watched signal has been over its limit long
enough to matter, and again when it recovers. It is still **observability only**:
nothing acts on a breach, nothing autoscales, nothing touches dispatch.

Planning artifacts: `plans/2026-06-20-remote-node-telemetry-autoscale/phase2/`.

## What `resource_breach` is, vs `worker_report`

| | `worker_report` (Phase 1) | `resource_breach` (Phase 2) |
|---|---|---|
| Shape | Periodic snapshot of the box. | Event on a sustained threshold crossing. |
| When it fires | Every `report_interval_seconds` (default 60 s), always. | Only when a signal enters/leaves breach, after a sustain window. |
| While idle | Still emitted (it is a heartbeat-ish snapshot). | **Silent.** A breach episode is scoped to a run in flight. |

A `resource_breach` event fires **only while a run is in flight** on the worker. When the
worker goes idle the detector is **Reset** — any open breach is closed with a final
`clear`, and no breach can dangle across runs. So a quiet worker emits no breach events
at all.

## Adaptive cadence (the same loop, two speeds)

`RunReportLoop` (`internal/workers/report_poll.go`) drives both event types off one
ticker that runs at two speeds:

- **Slow (60 s baseline) while every worker is idle.** Same as Phase 1 — one
  `worker_report` per worker per minute, no breach sampling.
- **Fast (`breach_sample_interval_secs`, default 5 s) while any enabled worker has a run
  in flight** *and* breach detection is enabled. Each fast tick samples the box and feeds
  the breach detector, but the `worker_report` emit is still **throttled to ~the slow
  interval**, so you do not get a `worker_report` every 5 s — the baseline history stays
  one-per-minute while the breach detector watches at 5 s resolution underneath.

When breach detection is off (`breach_detection_enabled: false`, or no workers) the loop
collapses to the fixed slow ticker and only emits `worker_report` — byte-identical to
Phase 1.

## The three signals, thresholds, and hysteresis

Three signals are watched, each with an **enter** threshold (cross this and a breach
*arms*) and a lower/looser **exit** threshold (cross back and a clear *arms*). The gap
between enter and exit is a hysteresis dead-band: a value bouncing inside the band while
breached neither re-fires nor clears, so a flapping signal makes no noise.

| Signal | Normalized value | Enter (default) | Exit (default) | Direction |
|---|---|---|---|---|
| `cpu` | `load5 / ncpu` | **0.85** | **0.70** | higher is worse |
| `memory` | `mem_free / mem_total` (free fraction) | **0.08** | **0.15** | **lower** is worse |
| `swap` | `swap_used_mb` | **256** | **64** | higher is worse |

Note `memory` is inverted: it is the **free** fraction, so a *lower* number is the
breach direction (enter when free drops below 0.08, recover when free rises above 0.15).

Crossing a threshold is not enough — the value must stay over (or under) it for a
**sustained dwell** before the event fires:

- `breach_dwell_secs` (default **20 s**) — how long a signal must stay over its enter
  threshold before a `breach` fires. This is the operator's "alert me only after X
  seconds of pressure" knob.
- `clear_dwell_secs` (default **15 s**) — how long it must stay under its exit threshold
  before the matching `clear` fires.

A signal that pops over enter and back under before 20 s elapses disarms silently — no
event.

## The breach/clear episode model

Each signal runs a small state machine — `OK → ARMING → BREACHED → CLEARING → OK`. One
**episode** produces exactly two events:

- one `breach` event when the enter threshold has been held for `breach_dwell_secs`, and
- one `clear` event when the exit threshold has later been held for `clear_dwell_secs`
  (or when the worker goes idle mid-episode, which forces a `clear` via Reset).

No event is emitted per sample; a sustained breach is one `breach` and (eventually) one
`clear`, never a storm.

### `resource_breach` payload fields

The payload (`ResourceBreachPayload`):

| Field               | JSON                    | Meaning                                                                  |
|---------------------|-------------------------|--------------------------------------------------------------------------|
| WorkerName          | `worker_name`           | The worker the breach describes.                                         |
| Kind                | `kind`                  | `"breach"` (onset) or `"clear"` (recovery).                             |
| Signal              | `signal`                | `"cpu"`, `"memory"`, or `"swap"`.                                       |
| Value               | `value`                 | Normalized signal value at the moment the transition fired.             |
| Threshold           | `threshold`             | The enter (on a breach) or exit (on a clear) threshold compared against. |
| BreachedForSeconds  | `breached_for_seconds`  | Episode duration in seconds — `0` on a breach, breach→clear elapsed on a clear. |
| InFlight            | `in_flight`             | The worker's in-flight slot count when the event fired.                 |
| StartedAt           | `started_at`            | RFC 3339 UTC timestamp the breach episode started.                      |
| FiredAt             | `fired_at`              | RFC 3339 UTC timestamp this transition fired.                           |

## How an operator observes it

`resource_breach` rides the **exact same surfacing path as `worker_report` and
`worker_unhealthy`**, automatically. It is emitted via the daemon's event bus
(`emitResourceBreach` → `bus.Emit`, wired in `daemon.go` from the same `reportEmit`
closure that carries `worker_report`) — this is the **central-side** path, *not* the
worker hook-relay tunnel. The subscriber stream (`SubscribeHub`) registers a **wildcard
observer** on the bus and delivers every event; the only filter is the caller-supplied
`--types` list, which **defaults to all event types**. There is no per-event-type
allowlist or "notable events" gate that a new type has to be added to — so a new event
type is operator-visible the moment it is emitted, with no extra wiring.

```bash
# Just the breach alerts:
harmonik subscribe --types resource_breach

# Pull out kind/signal/value/worker with jq:
harmonik subscribe --types resource_breach \
  | jq '{kind: .payload.kind, signal: .payload.signal, value: .payload.value, worker: .payload.worker_name}'
```

(The envelope is the standard `core.Event`: `.type` is `resource_breach`, the typed
fields live under `.payload`.)

## Optional `workers.yaml` config knobs

All Phase-2 knobs sit on the top-level `Config` (alongside `version`, `workers`, and the
Phase-1 `report_interval_seconds` / `disk_floor_mb`). Every one is optional — omit it to
take the default. Breach detection is **on by default** for a configured worker; an
operator opts *out* with `breach_detection_enabled: false`.

```yaml
version: 1
report_interval_seconds: 60
disk_floor_mb: 2048

# Phase-2 resource-breach detection (all optional):
breach_detection_enabled: true   # master switch; default TRUE (set false for Phase-1 behaviour)
breach_sample_interval_secs: 5   # fast cadence while a run is in flight; default 5s
breach_dwell_secs: 20            # sustain over enter before a breach fires; default 20s
clear_dwell_secs: 15             # sustain under exit before a clear fires; default 15s
cpu_source: load                 # load|top; default "load" ("top" pre-wired, behaves as load)
# per-signal hysteresis thresholds (all default from the table above):
cpu_enter: 0.85
cpu_exit: 0.70
mem_free_enter: 0.08             # LOWER free is worse — enter when free fraction drops below this
mem_free_exit: 0.15
swap_enter_mb: 256
swap_exit_mb: 64
workers:
  - name: worker-mac-1
    transport: ssh
    host: worker-mac-1
    os: darwin
    repo_path: ~/harmonik-worker/repo
    max_slots: 4
    enabled: true
```

## Phase-2 scope (observability only)

Like Phase 1, this is **observability only**:

- **No autoscale, no action.** A `resource_breach` is an alert in the event stream;
  nothing in this path touches `SelectWorker`, `max_slots`, dispatch, or concurrency.
- **`cpu_source: top` is pre-wired but behaves as `load`.** The knob is accepted so the
  config surface is stable for a later true-%CPU upgrade; in Phase 2 it computes the same
  `load5 / ncpu` proxy as `load`.

The intent, as in Phase 1, is to accumulate real breach episodes in the event log so a
later phase can tune thresholds and, eventually, act on a breach.
