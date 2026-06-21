# Phase 2 — Resource-breach detector. Design (transport-agnostic mechanics).

**Codename:** `worker-report` (Phase 2 slice) · **Date:** 2026-06-20
**Scope:** the *detector logic* only — the breach signals + thresholds, the sustained-breach state machine, the `resource_breach` event payload, the config knobs, and the reuse boundary against Phase 1.

**Out of scope (a separate agent owns it):** the transport and the sampler's *deployment location* — whether the sample loop and the state machine run worker-side (inside a per-run sampler) or central-side (a goroutine that polls over the already-open tunnel). This document is written so the same detector code works either way; see **§6 Deployment dependency** for the one place that choice changes the wiring.

This is **deterministic Go**, no LLM — consistent with harmonik's "deterministic skeleton, probabilistic organs" ethos and with Phase 1's pure `parseWorkerReport` / `deriveProblems`.

---

## 1. What "maxing out" means — the signals + default thresholds

A breach is a *sustained* condition where a running job is pushing the box to its limit. We watch three signals, evaluated **independently** (each has its own enter/exit threshold and its own dwell), and a breach episode names which signal(s) tripped. All three are already in `WorkerReportPayload` — Phase 1's collector reads them; Phase 2 reuses that exact collection (§5).

| Signal | Enter (breach) | Exit (clear) | Why this threshold |
|---|---|---|---|
| **CPU saturation** — normalized `load5 / ncpu` | `> 0.85` | `< 0.70` | This is the operator's "80% CPU for X seconds" framing rendered in the *only* CPU proxy the Phase-1 collector exposes. On darwin there is **no instantaneous %CPU** in `WorkerReportPayload` — only `vm.loadavg`. Normalized load (`load / ncpu`) is the standard "1.0 == fully saturated" proxy: `> 0.85` means the run queue is ~85% of core count sustained. `load5` (5-min) not `load1` because the dwell timer (§2) already debounces; using the smoother average avoids double-counting the same spike. |
| **Memory exhaustion** — `mem_free_mb / mem_total_mb` | `< 0.08` (free under 8%) | `> 0.15` | Low free memory is the leading edge of thrashing. Ratio-of-total, not an absolute MB floor, so the same threshold works across a 16 GB and a 64 GB box. |
| **Swap headroom gone** — `swap_used_mb` | `> 256` MB | `< 64` MB | The **decisive** "really out of headroom" signal already called out in Phase 1 (`telemetry.go` field comment). Once macOS is actively swapping, the box is past its working-set limit; an agent that swaps will run minutes-slow. A small non-zero enter floor (256 MB) tolerates the baseline compressed-memory/swap macOS keeps even when healthy. |

**Disk:** deliberately **excluded from the live breach detector.** Disk pressure is slow-moving and not a "right now, this run is maxing out" signal — Phase 1's `disk_pressure` problem flag (slow 60 s poll) already covers it. Adding it here would only flap on a steady-state low-disk box. (If the transport agent later wants a live disk breach it slots in as a 4th independent signal with no state-machine change.)

### CPU recommendation (load-ratio vs a real %CPU source)

**Recommendation: ship with `load5/ncpu` (load-ratio) as the CPU signal; do NOT block Phase 2 on adding a real %CPU source.** Rationale:

- Load-ratio is already collected, parsed, and tested in Phase 1 — zero new collection surface.
- The dwell timer (§2) makes the load-vs-%CPU distinction mostly moot: we are alerting on a *sustained* condition (≥ N seconds), exactly the regime where 5-min load average and true %CPU converge. Load's lag is a feature here, not a bug.
- **The one weakness, stated honestly:** load average counts runnable *and* uninterruptible (I/O-wait) tasks, so a heavy-I/O run can read high-load without high CPU. If field data shows that false-positive, the **clean upgrade** is to have the sampler add `cpu_pct` from `top -l 1 -n 0` (parse the `CPU usage:` line: `% user + % sys`) or `ps -A -o %cpu` summed — a *one-line collector addition* and a new optional payload field. The state machine does not change: `cpu_pct > 80` simply replaces (or ANDs with) the `load5/ncpu > 0.85` enter test. **Defer that to a fast-follow; recommend the sampler owner pre-wire the `top -l 1` line behind a config flag (`cpu_source: load|top`, default `load`) so the upgrade is config, not code.**

---

## 2. The debounce / sustained-breach state machine

One independent state machine **per (worker, signal)** pair. A breach must *persist* before it fires (no alerting on a 1-second spike), fires **once per episode** (not per sample), and emits a **CLEAR** when it recovers — with **hysteresis** (asymmetric enter/exit thresholds from §1) so it does not flap at the boundary.

Four states, two dwell timers:

```
        sample over enter-threshold for >= breach_dwell
  OK ───────────────────────────────────────────────►  BREACHED   (emit resource_breach{kind:"breach"})
   ▲  │                                                   │  ▲
   │  └─ sample over enter, dwell not yet met ─► ARMING ──┘  │  (any sample back under enter resets ARMING → OK, no event)
   │                                                         │
   └──────────  sample under exit-threshold for  ───────────┘
         CLEARING ◄── first sample under exit ──┘  >= clear_dwell  → OK (emit resource_breach{kind:"clear"})
                       (any sample back over exit resets CLEARING → BREACHED, no event)
```

State transitions (deterministic; evaluated once per sample with the sample's timestamp):

1. **OK → ARMING** — sample crosses the **enter** threshold. Record `breach_started_at = sample_time`. No event.
2. **ARMING → BREACHED** — sample still over enter AND `sample_time − breach_started_at ≥ breach_dwell`. **Emit `resource_breach{kind:"breach"}`** (the once-per-episode fire). **ARMING → OK** — any sample drops back under enter before the dwell elapses; discard `breach_started_at`, no event (this is the spike we wanted to swallow).
3. **BREACHED → CLEARING** — sample drops under the **exit** threshold (note: exit < enter, the hysteresis gap). Record `clear_started_at`. No event. **BREACHED → BREACHED** — sample between exit and enter (in the hysteresis band) or back over enter: stay breached, no event, no flap.
4. **CLEARING → OK** — sample still under exit AND `sample_time − clear_started_at ≥ clear_dwell`. **Emit `resource_breach{kind:"clear"}`** carrying the total `breached_for` duration. **CLEARING → BREACHED** — any sample pops back over exit before clear_dwell; discard `clear_started_at`, no event (recovery wasn't real).

Properties this guarantees: exactly **one** `breach` and at most one matching `clear` per episode; no event in the hysteresis band; a transient spike under `breach_dwell` produces nothing; an unclean recovery (bouncing over the exit line) produces nothing until it stays down `clear_dwell`. The state, the two timestamps, and nothing else are the machine's whole footprint — trivially serializable if the deployment owner needs to survive a restart mid-episode.

---

## 3. The `resource_breach` event payload

Small struct, mirroring `WorkerReportPayload` / `WorkerUnhealthyPayload` style (typed, `core.RegisterEventType` in `init()`, RFC 3339 UTC timestamps, JSON tags). Lives in a new `internal/workers/breach.go`.

```go
// ResourceBreachPayload — a sustained resource-breach (or its recovery) for a
// worker during a running job. Emitted once per episode edge by the Phase-2
// breach detector's state machine (NOT once per sample).
// Durability class: O (ordinary — operator observability; the slow worker_report
// poll and the run record reconstruct context if one is lost).
// Event: "resource_breach".
type ResourceBreachPayload struct {
    WorkerName string `json:"worker_name"`

    // Kind is "breach" (entered a sustained breach) or "clear" (recovered).
    Kind string `json:"kind"`

    // Signal is which signal tripped: "cpu", "memory", or "swap".
    Signal string `json:"signal"`

    // Value is the breaching sample's value for Signal; Threshold is the enter
    // threshold it crossed (for "clear", Threshold is the exit threshold).
    // Units match the signal: cpu = load5/ncpu ratio; memory = free/total ratio;
    // swap = swap_used_mb.
    Value     float64 `json:"value"`
    Threshold float64 `json:"threshold"`

    // BreachedForSeconds is how long the breach has lasted so far. On "breach"
    // it is the dwell that armed the fire (>= breach_dwell). On "clear" it is the
    // full episode duration breach_started_at..now.
    BreachedForSeconds int64 `json:"breached_for_seconds"`

    // RunID / BeadID identify the in-flight job, when knowable. May be empty when
    // the detector cannot attribute the breach to a specific run (see §6).
    RunID  string `json:"run_id,omitempty"`
    BeadID string `json:"bead_id,omitempty"`

    // StartedAt is when the breach began (OK→ARMING, RFC3339 UTC); FiredAt is when
    // this event was emitted (the BREACHED or cleared-OK transition).
    StartedAt string `json:"started_at"`
    FiredAt   string `json:"fired_at"`
}

func init() {
    if err := core.RegisterEventType("resource_breach", func() core.EventPayload { return &ResourceBreachPayload{} }); err != nil {
        panic("workers: init: register resource_breach: " + err.Error())
    }
}
```

`core/eventtype.go` gets a matching `EventTypeResourceBreach EventType = "resource_breach"` constant (Durability class **O**) registered under the §8.16 remote-substrate-worker block, right beside `EventTypeWorkerReport`.

---

## 4. Config knobs (all defaulted; nothing hardcoded deep)

Live at the **`workers.yaml` top level**, alongside Phase 1's `report_interval_seconds` / `disk_floor_mb`. Every knob has a package-const default (the §1/§2 numbers), exactly like Phase 1's `DefaultDiskFloorMB` — so `workers.yaml` needs no new required fields, and the deep code (the state machine, the signal evals) never sees a literal.

```yaml
# workers.yaml (top level — Phase 2 additions)
breach_detection_enabled:    true   # master off-switch; false = no in-run sampler, no events
breach_sample_interval_secs: 5      # in-run sample cadence (default 5; faster than Phase-1's 60s)
breach_dwell_secs:           20     # sustained duration before a breach fires ("80% CPU for X seconds" = X)
clear_dwell_secs:            15      # sustained recovery before a clear fires

# per-signal enter/exit thresholds (asymmetric → hysteresis). All optional; defaults shown.
cpu_enter_ratio:    0.85   # load5/ncpu
cpu_exit_ratio:     0.70
mem_free_enter_ratio: 0.08 # mem_free/mem_total below this = breach
mem_free_exit_ratio:  0.15
swap_enter_mb:      256
swap_exit_mb:       64

cpu_source: load            # "load" (default, reuse Phase-1 load5/ncpu) | "top" (sampler runs top -l 1 for true %CPU)
```

Defaults map directly onto the operator's "80% CPU for X seconds": `cpu_enter_ratio: 0.85` ≈ "80%+", `breach_dwell_secs: 20` is the "X seconds". `breach_sample_interval_secs: 5` is the "faster than the 60 s Phase-1 poll" the live channel needs — at 5 s cadence a 20 s dwell is 4 confirming samples. Validation: each `*_exit` must be on the recovered side of its `*_enter` (exit ratio < enter for cpu/swap; exit ratio > enter for mem-free) or config load errors — the hysteresis gap must be real.

---

## 5. Reuse vs new — the minimal-new-surface recommendation

**Reuse (do NOT duplicate):**
- The **collector** — `darwinCollectorScript` already emits `load=`, `ncpu=`, `memtotal=`, the `vmstat<<` block, and `swap=`. Those are exactly the three signals. The in-run sampler runs the **same collector** (just on the faster `breach_sample_interval_secs` cadence).
- The **parser** — `parseWorkerReport` already turns that output into `Load5`, `NCPU`, `MemTotalMB`, `MemFreeMB`, `SwapUsedMB`. The detector consumes a `WorkerReportPayload` and reads those five fields. **No new parser.**
- The **emit pattern** — `EmitFunc` + `core.RegisterEventType` + `emit == nil` is a no-op, same as `emitWorkerReport` / `emitUnhealthyEvent`.

**Genuinely new (the only new surface):**
1. `breach.go` — the `ResourceBreachPayload` struct + registration.
2. The **state machine** — a tiny pure `breachDetector` type holding per-(worker,signal) `{state, breachStartedAt, clearStartedAt}` and an `Observe(rep WorkerReportPayload, now time.Time) []ResourceBreachPayload` method that returns the zero-or-more events to emit for that sample. Pure and unit-testable with canned `WorkerReportPayload` sequences — no SSH, mirroring `parseWorkerReport`'s test style.
3. The thresholds-from-config plumbing (a `BreachConfig` struct of the §4 knobs with the const defaults).

**Recommendation — sample the full `WorkerReportPayload` faster, do not invent a "lighter" struct.** Reusing the existing collector + parser verbatim means Phase 2's *only* new code is the state machine + the event — the smallest possible surface, and it keeps one collection path to maintain. The 3 extra collector lines a full report carries (disk/claude/worktrees) cost nothing at a 5 s cadence and keep the in-run sample and the slow poll byte-identical. A bespoke lighter sampler would be premature optimization the operator dislikes. If profiling ever shows the full collector is too heavy at 5 s, trimming the collector script to the three needed lines is a later, isolated change behind the same parser.

---

## 6. Deployment dependency (the transport agent's call)

The detector code above is deployment-agnostic. **One** thing flips on the sampler-location decision the other agent owns:

- **If the sampler runs central-side** (a goroutine polls the worker over the already-open per-run tunnel every `breach_sample_interval_secs`): the `breachDetector` state lives **on central**, in the daemon, keyed by worker. `RunID`/`BeadID` are **known** (central started the run) → always populated. Emit goes straight to the local `bus.Emit`. This is the lowest-new-infrastructure option and the natural fit, because Phase 1's `CollectReport` is *already* a central-pull over the same runner — Phase 2 central-side is just "call it faster while a run is in flight, and feed each result through the detector." **Recommended default**, pending the transport agent's confirmation.
- **If the sampler runs worker-side** (a short-lived process the run spawns, watching locally and pushing only on a breach edge): the `breachDetector` state lives **in the sampler on the worker**; only the resulting `resource_breach` events cross the tunnel (the hook-relay path). `RunID`/`BeadID` must be **passed into** the sampler at launch (the run knows them) to populate the payload — they are not otherwise visible worker-side. This is more "silent when idle" by construction but needs the sampler-lifecycle + push transport the other agent is designing.

Either way the **state machine, the payload, the thresholds, and the config are identical** — only *where the detector struct is instantiated* and *how `RunID` is obtained* differ. Build the detector as a self-contained pure unit (`Observe` in/out) so it drops into whichever host the transport decision picks.

---

## 7. Beads (codename:worker-report, Phase 2)

1. **P2-1** — `ResourceBreachPayload` + `resource_breach` event registration (`breach.go` + `eventtype.go` constant). Foundation; no SSH. Mirror WR1.
2. **P2-2** — the pure `breachDetector` state machine + `Observe`, with table-driven unit tests over canned `WorkerReportPayload` sequences (spike-swallow, single-fire, hysteresis-no-flap, clear-after-recovery). Depends on P2-1.
3. **P2-3** — `BreachConfig` knobs + const defaults + `workers.yaml` parse + exit-vs-enter validation. Depends on P2-2.
4. **P2-4** — wire the detector to the in-run sampler **at the location the transport agent specifies** (central goroutine over the tunnel, or worker-side sampler) + emit. Depends on P2-3 and on the transport agent's deployment decision (§6).
5. **P2-5** — operator surfacing: confirm `resource_breach` reaches the operator the same path as `worker_unhealthy` / `worker_report`; doc note. Depends on P2-4.

**Acceptance:** with a worker running a job, driving CPU/mem/swap over the enter thresholds for ≥ `breach_dwell_secs` emits exactly one `resource_breach{kind:"breach"}` naming the signal; a transient spike under the dwell emits nothing; dropping back under the exit thresholds for ≥ `clear_dwell_secs` emits exactly one `{kind:"clear"}` with the episode duration — all with `max_slots` and dispatch unchanged.
