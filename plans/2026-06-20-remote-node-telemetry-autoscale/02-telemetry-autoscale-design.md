# Remote-node telemetry + dynamic-concurrency control loop

Design — 2026-06-20. Codename: `node-autoscale` (proposed).

Status: DESIGN. Grounds in the live worker model: `internal/workers/{workers,registry,health,offline}.go`,
`internal/daemon/workloop.go` (the remote-dispatch block ~L2440), `.harmonik/workers.yaml`.

---

## 0. The problem in one paragraph

`max_slots` is a static hand-set number (gb-mbp = 4). It is enforced in exactly one place:
`Registry.SelectWorker()` returns `nil` when `inFlight >= MaxSlots`, which makes the workloop fall
back to local for that bead. The number is a guess: too high and the worker thrashes (RAM pressure,
swap, claude OOM, runaway load), too low and capacity is wasted. The operator wants the **remote box to
report its own resource pressure** and the **central daemon to deterministically derive an effective
concurrency** that floats at or below the static ceiling. No LLM in the loop — this is a Go controller.

### Grounding facts that constrain the design

- **Single-worker V1.** `workers.Load` rejects >1 worker (`ErrTooManyWorkers`). The `Registry` holds
  exactly one `Worker` + one `inFlight` int. The design must not assume a fleet; it must *generalize
  cleanly* to N workers later but ship for one.
- **Slot gate is the only lever.** Concurrency is controlled solely by the `inFlight >= MaxSlots`
  comparison in `SelectWorker()`. To make concurrency dynamic, we change *what number that comparison
  uses* — nothing else in the dispatch path needs to move.
- **`SetEnabled(false)` already exists** as the hard kill-switch (health check, offline detection). The
  autoscaler is the *soft* analogue: it never disables, it only lowers the effective ceiling.
- **SSH is already the transport for everything.** Each remote run opens a long-lived `ssh -N -R`
  reverse tunnel and several short-lived `ssh box '<cmd>'` calls (`ensureWorkerHarmonikDir`,
  `waitWorkerSocketLive`, liveness `pgrep`). There is already an `SSHRunner{Host}` (`tmuxpkg`) that the
  daemon uses to run arbitrary argv on the worker. **Telemetry has a ready-made transport: one more
  short-lived `ssh box '<probe>'`.**
- **Typed-event + `EmitFunc` pattern is established** (`worker_unhealthy`, `worker_offline`,
  `worker_tunnel_failed`) — register in `init()`, emit via `deps.bus.Emit`. New telemetry/scale events
  follow the same mold for operator observability and replay.

---

## 1. Telemetry channel (remote → central)

### 1.1 What to report

A **single small snapshot struct**, sampled on the worker, returned as one JSON line. We want the
minimum set that lets the controller answer "is this box under pressure, and is the pressure
*agent-attributable*":

| Field | Source (macOS / darwin) | Why it's in the payload |
|---|---|---|
| `load1`, `load5` | `sysctl -n vm.loadavg` | Primary saturation signal; load5 damps load1's noise. Normalize by `ncpu`. |
| `ncpu` | `sysctl -n hw.ncpu` | So central can compute `load1/ncpu` without hardcoding per-box core counts. |
| `mem_total_bytes`, `mem_free_bytes` | `vm_stat` page counts × `hw.pagesize` (free = free+inactive+speculative) | RAM headroom — the hard limiter for claude instances. |
| `swap_used_bytes` | `sysctl -n vm.swapusage` | **The decisive backoff signal.** Any non-trivial swap = over-committed; claude is heavy and paging it is the failure mode we're avoiding. |
| `claude_count` | `pgrep -c -f 'claude'` (or `-x claude`) | Cross-check: does observed claude process count match `inFlight` the daemon thinks it dispatched? Detects orphans / runaways. |
| `top_rss_bytes` | `ps -axo rss -c claude \| sort -rn \| head -1` × 1024 | Largest single-agent footprint — catches one runaway agent eating the box. |
| `disk_free_bytes` | `df -k <repo_path> \| tail -1` | A full disk silently wedges worktree-create (we've hit ENOSPC before — see memory). |
| `sampled_at` | worker `date -u +%s` | Staleness detection on central. |

This is intentionally a **flat, ~8-field snapshot**, not a metrics time-series. The controller keeps the
last sample (plus a short EWMA) per box; it does not store history. No Prometheus, no abstraction layer.

### 1.2 Payload struct

```go
// internal/workers/telemetry.go  (new file, same package)

// NodeTelemetry is one resource snapshot sampled on a worker and parsed on
// central. All byte fields are absolute bytes; the controller derives ratios.
// Durability class: O (ordinary — operator observability), same as worker_unhealthy.
type NodeTelemetry struct {
    WorkerName     string  `json:"worker_name"`     // filled in on central (probe knows which box)
    Load1          float64 `json:"load1"`
    Load5          float64 `json:"load5"`
    NCPU           int     `json:"ncpu"`
    MemTotalBytes  uint64  `json:"mem_total_bytes"`
    MemFreeBytes   uint64  `json:"mem_free_bytes"`
    SwapUsedBytes  uint64  `json:"swap_used_bytes"`
    ClaudeCount    int     `json:"claude_count"`
    TopRSSBytes    uint64  `json:"top_rss_bytes"`
    DiskFreeBytes  uint64  `json:"disk_free_bytes"`
    SampledAtUnix  int64   `json:"sampled_at"`      // worker wall-clock, seconds
}
```

### 1.3 How it gets there — three candidate transports

**(A) Central-side `ssh box '<probe-script>'` poll.** The daemon, on a ticker, runs one short-lived
`ssh box 'sh -c "<one-shot stat collector>"'` via the existing `SSHRunner`, parses the JSON on stdout.

- ➕ **Zero new moving parts on the worker** — the worker already only needs sshd + claude + harmonik.
  No daemon to install, no version skew, nothing to supervise. Reuses `SSHRunner` verbatim.
- ➕ **Central owns the cadence and timeout** — trivially fail-safe: a hung/over-loaded box just makes the
  ssh call time out, which *is itself the signal* ("box is saturated → back off").
- ➕ **Stateless on the worker** — matches the current model exactly (the worker is a dumb execution
  substrate; all intelligence is central).
- ➖ One extra ssh/connect per interval (cheap at a 15–30 s cadence; amortized against the per-run
  tunnels already open).
- ➖ Sampling is pull, so the granularity is the poll interval (fine — control decisions are slow).

**(B) Lightweight remote push agent (heartbeat).** A small `harmonik node-report` long-running process on
the worker samples locally and pushes snapshots to central (over the same reverse-tunnel substrate, or a
dedicated one).

- ➕ Push = lower latency, finer granularity, survives central restart cleanly.
- ➖ **A new supervised process on the worker** — must be installed, version-matched to the daemon,
  kept alive (who restarts it?), and it's a second thing that can silently die. This is exactly the
  "unrequested abstraction layer" the operator dislikes, *for V1*.
- ➖ Needs its own transport/auth back to central. The per-run tunnel is per-run; a standing telemetry
  channel is a new long-lived connection to design.

**(C) Piggyback on the existing hook relay.** Fold a telemetry field into the hook NDJSON the
worker-side agent already ships on `SessionStart`/`Stop`.

- ➕ Reuses the live tunnel — no new connection.
- ➖ **Telemetry only flows when an agent is running and only at hook moments.** We need pressure data
  *to decide whether to dispatch the next agent* — precisely when there may be no agent hook firing.
  Coupling the control signal to agent lifecycle is backwards. Reject.

### 1.4 PICK: (A) central-side ssh poll, with (B) as the documented evolution

(A) is the only option consistent with harmonik's "worker is a dumb substrate; central is the brain"
model and the "no unrequested abstraction" rule. It reuses `SSHRunner`, adds zero worker-side
processes, and its dominant failure mode (ssh times out) is *also the correct control input*. (B) is the
right eventual model **only** when (i) we go multi-worker and per-interval N-fan-out ssh becomes wasteful,
or (ii) we want sub-poll-interval reaction. Until then it's strictly more machinery for no V1 benefit.

The probe is one `sh -c` collector script (embedded as a Go const string, shipped as the ssh argv — same
way `runProbes` ships `sh -c 'test -z "$ANTHROPIC_API_KEY"'` today). It emits the JSON above on stdout.
Keep it to portable `sysctl`/`vm_stat`/`pgrep`/`ps`/`df` (darwin is the only supported worker OS today;
gate on `worker.OS == "darwin"` and skip telemetry — fail-safe — on any other OS until a Linux collector
is added).

---

## 2. The control loop on central

A new `internal/workers.Autoscaler` owned by the daemon. It runs one goroutine on a ticker, holds the
last telemetry sample + an EWMA per worker, computes an **effective slot count**, and pushes that number
into the `Registry`. **The Registry's `SelectWorker` gate changes from `inFlight >= MaxSlots` to
`inFlight >= effectiveSlots`** — that one-line change is the entire mechanical coupling.

### 2.1 Inputs and pressure classification

Per tick, after a fresh sample, compute three normalized pressures and reduce to a single **state**:

```
load_ratio   = load5 / ncpu                  // 1.0 == fully busy
mem_used_frac = 1 - mem_free/mem_total
swap_pressure = swap_used_bytes > SWAP_BYTES_LIMIT   // boolean; default 512 MiB

state =
  CRITICAL  if swap_pressure
            OR mem_used_frac >= MEM_HIGH (0.90)
            OR load_ratio   >= LOAD_HIGH (2.0)
            OR disk_free_bytes < DISK_MIN (2 GiB)
  RELIEVED  if  load_ratio  <= LOAD_LOW (0.7)
            AND mem_used_frac <= MEM_LOW (0.70)
            AND !swap_pressure
  STEADY    otherwise   (the hysteresis dead-band)
```

All thresholds are **config fields with defaults** (per the keeper-config-redesign ethos: zero
hardcoded numbers in the hot path; defaults live in one struct). They land in `workers.yaml` under an
`autoscale:` block per worker.

### 2.2 The decision: additive-increase / multiplicative-decrease (AIMD)

Classic AIMD, because the cost asymmetry is real: **over-provisioning hurts immediately and sharply**
(swap → every agent on the box slows, claude can OOM), **under-provisioning only wastes a slot**. So we
react to pressure fast and recover slowly:

```
on CRITICAL:  effective = max(FLOOR, effective / 2)     // multiplicative decrease, react hard
on RELIEVED:  if (now - lastChange) >= INCREASE_COOLDOWN:
                  effective = min(CEILING, effective + 1) // additive increase, recover gently
on STEADY:    no change                                  // dead-band → no oscillation
```

- `CEILING = worker.MaxSlots` — **the static number becomes the hard ceiling, never exceeded.** This is
  how dynamic reconciles with static: `effective ∈ [FLOOR, MaxSlots]`, `effective` starts at `MaxSlots`
  (optimistic) or at a configurable `autoscale.start` (conservative). Operator intent in `max_slots` is
  preserved as the upper bound.
- `FLOOR = autoscale.floor` (default 1) — never scale to zero from pressure; zero is reserved for the
  hard kill-switch (`SetEnabled(false)`), which is a *different* signal (box unhealthy/offline). A box
  under memory pressure should still take *one* agent; if even one agent can't fit, that's a health
  problem, not a scaling problem.

### 2.3 Hysteresis / damping (so it does not oscillate)

Three independent damping mechanisms, all necessary:

1. **The STEADY dead-band** between `*_LOW` and `*_HIGH`. A sample in the band changes nothing. This is
   the primary anti-flap: a box hovering at 80% RAM neither scales up nor down.
2. **EWMA smoothing on the inputs.** `load5` and `mem` are smoothed `s = α·sample + (1-α)·s`
   (α default 0.4) before classification, so one transient spike (a compile, a `go test`) doesn't trip
   CRITICAL. Swap and disk are used raw (they're already slow-moving and decisive).
3. **Asymmetric cadence.** Decrease may fire every tick (react fast). Increase is gated by
   `INCREASE_COOLDOWN` (default 90 s ≈ several poll intervals) so we don't ramp back up into a box that's
   still settling from the work we just stopped sending it. This is the AIMD "slow start of recovery."

### 2.4 In-flight protection (never kill to scale down)

**The autoscaler never touches a running agent.** Scaling down only lowers `effectiveSlots`; the gate
`inFlight >= effectiveSlots` then *stops dispatching new* runs. Already-reserved slots drain naturally
as their runs complete (`ReleaseSlot`). Concretely: if `inFlight == 4` and a CRITICAL sample drops
`effective` to 2, no agent is killed — the next two `SelectWorker()` calls return `nil` (local fallback)
until `inFlight` drains to < 2. This is the single most important invariant and it falls out for free
from only ever changing the comparison number. (Killing a mid-flight claude would orphan a worktree and
strand a bead `in_progress` — exactly the failure class we've fought repeatedly.)

`effective` is allowed to be **below** current `inFlight` transiently; the gate handles it (`>=` is
correct, so `inFlight=4, effective=2` simply blocks new dispatch). No clamping needed.

### 2.5 Registry changes (minimal)

```go
// Registry gains one field + setter; SelectWorker's comparison changes.
type Registry struct {
    mu        sync.Mutex
    worker    Worker
    hasWorker bool
    inFlight  int
    effective int   // dynamic ceiling; 0 == "uninitialized, fall back to MaxSlots"
}

func (r *Registry) SetEffectiveSlots(n int) { … clamp [0, MaxSlots]; store … }

// in SelectWorker, replace the gate:
ceiling := r.worker.MaxSlots
if r.effective > 0 && r.effective < ceiling { ceiling = r.effective }
if ceiling > 0 && r.inFlight >= ceiling { return nil }
```

`effective == 0` means "autoscaler hasn't spoken / disabled" → behaviour is **byte-identical to today**
(`MaxSlots` gate). This makes the whole feature opt-in and a no-op when off — important for safe rollout.

### 2.6 Observability

Emit a typed `worker_scaled` event on every *change* of `effective` (not every tick), payload
`{worker_name, from, to, reason, state, load_ratio, mem_used_frac, swap_used_bytes}`. Operator can watch
scaling decisions on the bus exactly like `worker_unhealthy`. Telemetry samples themselves are NOT
evented per-tick (noise); optionally a debug `worker_telemetry` behind a flag.

---

## 3. Failure modes

| Failure | Detection | Response (fail-safe direction = **fewer** agents) |
|---|---|---|
| **Stale telemetry** (`now - sampled_at > STALE_TTL`, default 3× interval) | controller checks age each tick | Treat as CRITICAL-lite: **freeze `effective` and decay it toward FLOOR** one step per stale tick. We do not trust an old sample to *raise* capacity. |
| **Box goes silent** (ssh poll times out / errors) | the `ssh` call returns non-nil or exceeds `PROBE_TIMEOUT` (default 8 s) | Same decay-to-FLOOR. After `N_SILENT` consecutive failures (default 3) escalate to the existing hard path: `SetEnabled(false)` + emit `worker_offline{phase:"telemetry"}` — reusing the offline machinery, not inventing a new one. The timeout *is* a saturation signal, so backing off is correct even before the hard cutoff. |
| **Runaway agent eats all RAM** | `top_rss_bytes` huge / `mem_free` collapses / `swap_pressure` | CRITICAL → multiplicative decrease stops *new* dispatch immediately. The runaway itself is not killed by the autoscaler (out of scope — that's liveness/orphan-sweep's job), but we stop pouring fuel on. `claude_count > inFlight` (orphan detected) is surfaced in the `worker_scaled` reason for operator triage. |
| **Telemetry lag vs spawn latency** | inherent: a sample is ~interval-old; a claude spawn takes seconds to show in RAM | Mitigated by (a) AIMD's *additive* increase — we add **one** slot at a time with a cooldown, so we never fan out into stale-green data; (b) reacting on `load5`+EWMA which lead RAM; (c) the FLOOR/CEILING bracket bounds worst-case error to ±1 dispatch. The asymmetry (slow up, fast down) is exactly the hedge against lag. |
| **Disk full** | `disk_free_bytes < DISK_MIN` | CRITICAL (worktree-create wedges on ENOSPC — known failure). Stop dispatch; operator alarm via `worker_scaled` reason `disk_low`. |
| **Controller crash / disabled** | `effective` stops updating | `effective` is just a number in the Registry; if the goroutine dies, the last value sticks. Safe-ish, but a watchdog should reset `effective = 0` (→ static `MaxSlots`) if no tick in `WATCHDOG_TTL`, so a dead controller fails *open to the operator's hand-set ceiling*, never to a stale-low or stale-high autoscaled number. |

**Fail-safe principle:** every uncertainty (stale, silent, parse error, unknown OS) resolves toward
**lower effective concurrency**, except total controller death which resolves to the **operator's static
ceiling**. We never let a missing/old signal *raise* capacity.

---

## 4. MVP vs full

### 4.1 MVP (ship this)

- **Transport:** central-side `ssh box '<collector.sh>'` on a 20 s ticker, via the existing `SSHRunner`.
  Darwin-only collector; skip (no-op, static `MaxSlots`) on other OS.
- **Signal set:** `load5/ncpu`, `mem_free/mem_total`, `swap_used`, `disk_free` — the four that gate the
  state machine. (`claude_count`/`top_rss` shipped in the payload but only surfaced for observability,
  not yet acted on.)
- **Controller:** the AIMD state machine of §2 — multiplicative-decrease on CRITICAL, additive-increase
  on RELIEVED-with-cooldown, STEADY dead-band, EWMA on load+mem, `effective ∈ [1, MaxSlots]`.
- **Coupling:** one new `Registry.effective` field + the gate swap in `SelectWorker`. `effective == 0`
  ⇒ identical to today (feature off by default until `autoscale.enabled: true` in `workers.yaml`).
- **Failure handling:** stale/silent → decay to FLOOR; `N_SILENT` consecutive → existing
  `SetEnabled(false)`+`worker_offline`; watchdog resets to static ceiling on controller death.
- **Observability:** `worker_scaled` event on each change.
- **Config:** an `autoscale:` block per worker in `workers.yaml`, all thresholds defaulted in one
  `AutoscaleConfig` struct (no hardcoded constants in the loop).

This is genuinely small: ~one collector string, one `telemetry.go` (struct + parse), one
`autoscale.go` (the goroutine + state machine), and a ~6-line `Registry` change. No worker-side install,
no new long-lived connection, no new subsystem package boundary to negotiate in `.golangci.yml`.

### 4.2 Full (evolution, only when justified)

1. **Multi-worker.** Lift the V1 single-worker cap (`ErrTooManyWorkers`), make `Registry` hold a map of
   `name → {worker, inFlight, effective}`, run one autoscaler that fans the ssh probe across boxes.
   This is the trigger that makes per-interval ssh-poll cost real and motivates transport (B).
2. **Push-model remote agent (transport B).** `harmonik node-report` on each worker pushes snapshots over
   a standing channel — only worth it at multi-worker scale or when sub-20 s reaction matters.
3. **Act on `claude_count`/`top_rss`.** Orphan-kill integration: when `claude_count > inFlight` persists,
   hand off to orphan-sweep to reap the stray; when one RSS dominates, weight it in the decrease.
4. **Predictive headroom.** Reserve expected per-agent RSS (learned from `top_rss` history) so increase
   is gated on *projected* free RAM, not just current — removes the spawn-latency lag entirely. Only if
   ±1-slot AIMD error proves too coarse in practice.
5. **Linux collector** + OS-dispatched probe strings, when a non-darwin worker is added.

---

## 5. Design-ethos check

- **Deterministic skeleton, probabilistic organs:** the controller is pure deterministic Go — a ticker, a
  parse, a state machine, an int. No LLM, no cognition. ✔
- **No unrequested abstraction:** reuses `SSHRunner`, the `EmitFunc`/typed-event pattern, and the single
  existing slot-gate. Adds one field to `Registry` and two small files. No metrics framework, no
  worker-side daemon, no new package boundary in MVP. ✔
- **Static intent preserved:** `max_slots` becomes the hard ceiling; the operator's hand-set number is
  never exceeded and is the failure-open default. ✔
- **Generalizes but doesn't pre-build:** the map-per-worker and push-agent shapes are named in §4.2 but
  not built — the single-worker MVP is honest to the V1 registry. ✔
