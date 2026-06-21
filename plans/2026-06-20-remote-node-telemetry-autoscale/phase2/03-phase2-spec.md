# Phase 2 — Live resource-breach alerts. Concrete spec.

**Codename:** `worker-breach` · **Date:** 2026-06-20 · **Builds on:** Phase 1 (`codename:worker-report`, landed).
**Backing:** `phase2/01-transport-lifecycle-grounding.md`, `phase2/02-breach-detector-design.md`.

Phase 2 answers the operator's live-monitoring need: *"when a job is running, tell me the moment the box is maxing out (e.g. >80% CPU for X seconds) — and stay silent when idle."*

## The decided shape (why it's small)

Two grounding findings collapse Phase 2 to an almost entirely **central-side, in-package** change:

1. **Sampling is central-side, reusing Phase 1.** A box-pressure breach is a property of the BOX, not a single run. So we extend Phase 1's `RunReportLoop` (`internal/workers/report_poll.go`) — already box-scoped, central, reusing `darwinCollectorScript` + `parseWorkerReport` — rather than hook chani's per-run 500ms liveness ticker (which would sample redundantly per concurrent run, on the hot path, too fast for `vm_stat`).
2. **No worker/relay/teardown changes.** Because the detector runs on central and emits `resource_breach` straight to the event bus (exactly like `worker_report`), the event never traverses the worker tunnel. No worker resident process, no relay accept-switch change, no new teardown. This is the payoff of Phase 1's "keep the worker dumb" stance.

**Net new surface:** one event type, one pure state machine, an adaptive-cadence tweak to the existing poll loop, config knobs, a doc note. That's it.

## Adaptive cadence (the one change to RunReportLoop)

Today `RunReportLoop` ticks at `report_interval_seconds` (60s) and emits one `worker_report`. Phase 2 makes the cadence adaptive per worker:

- **Idle** (`reg.InFlight() == 0` for that worker): tick slow (60s), emit `worker_report` only. Breach detectors are held in/reset to `OK` — **silent when idle**, as the operator wants.
- **Run in flight** (`InFlight() > 0`): tick fast (`breach_sample_interval_secs`, default 5s). Each tick samples the box once (the Phase-1 collector), feeds the sample to the per-signal breach detectors, AND still emits a `worker_report` at the slow interval (every Nth fast tick) so the baseline history is unbroken.
- **Transition to idle:** reset all of that worker's detectors to `OK` (emit a `clear` if mid-breach), so a breach can't dangle across runs.

Box-scoped sampling means N concurrent runs still sample the box once per tick, not N times.

## The breach detector (pure, heavily tested — `breachDetector`)

One state machine per (worker × signal). Signals + default thresholds (hysteresis enter/exit), each evaluated independently:

| Signal | Enter | Exit | Source |
|---|---|---|---|
| cpu | `load5/ncpu > 0.85` | `< 0.70` | Phase-1 collector (load ratio; no instantaneous %CPU) |
| memory | `mem_free/mem_total < 0.08` | `> 0.15` | Phase-1 collector |
| swap | `swap_used_mb > 256` | `< 64` | Phase-1 collector (decisive headroom signal) |

Disk excluded (slow-moving; Phase-1's `disk_pressure` already covers it).

**States (4) + two dwell timers:**
1. `OK → ARMING` on first over-enter sample (record start). Drops back under before `breach_dwell` → `OK`, no event.
2. `ARMING → BREACHED` once over-enter sustained ≥ `breach_dwell_secs` (default **20s** — the operator's "X seconds") → emit ONE `breach`.
3. `BREACHED → CLEARING` on first under-exit sample. Samples in the hysteresis band stay `BREACHED` (no flap).
4. `CLEARING → OK` once under-exit sustained ≥ `clear_dwell_secs` (default 15s) → emit ONE `clear` with episode duration. Pops back over exit before then → `BREACHED`, no event.

Deterministic Go, no LLM. Detector is a pure function of (sample sequence, clock) → events — fully unit-testable without SSH.

**CPU source:** ship load-ratio (zero new collection; the dwell makes load≈%CPU in the sustained regime). Pre-wire a `cpu_source: load|top` config knob so a true-%CPU upgrade (`top -l 1`) is later config, not a state-machine change. Don't block Phase 2 on it.

## `resource_breach` event payload (class O)

Mirrors `WorkerReportPayload` style. Emitted directly to the bus; rides the same uniform `harmonik subscribe` path as `worker_report`/`worker_unhealthy` (verified in WR5 — no event-type gate).

```go
type ResourceBreachPayload struct {
    WorkerName        string  `json:"worker_name"`
    Kind              string  `json:"kind"`                 // "breach" | "clear"
    Signal            string  `json:"signal"`               // "cpu" | "memory" | "swap"
    Value             float64 `json:"value"`                // the breaching value
    Threshold         float64 `json:"threshold"`            // enter (breach) or exit (clear)
    BreachedForSeconds int    `json:"breached_for_seconds"` // episode duration (on clear; dwell so far on breach)
    InFlight          int     `json:"in_flight"`            // runs on the box at fire time (box-pressure context)
    StartedAt         string  `json:"started_at"`           // RFC3339 UTC, episode start
    FiredAt           string  `json:"fired_at"`             // RFC3339 UTC, this event
}
```
`run_id`/`bead_id` deliberately omitted: a box-pressure breach is not attributable to one run; `InFlight` gives the box-load context instead.

## Config knobs (workers.yaml top-level, all defaulted — no new required fields)

```yaml
breach_detection_enabled: true       # master switch
breach_sample_interval_secs: 5       # fast cadence while a run is in flight
breach_dwell_secs: 20                # "X seconds" sustained before firing
clear_dwell_secs: 15
cpu_source: load                     # load | top  (top = future true-%CPU)
# per-signal enter/exit (all defaulted to the table above):
cpu_enter: 0.85 ; cpu_exit: 0.70
mem_free_enter: 0.08 ; mem_free_exit: 0.15
swap_enter_mb: 256 ; swap_exit_mb: 64
```

## Beads (codename:worker-breach)

1. **PB1 (`hk-necs`)** — `ResourceBreachPayload` struct + `resource_breach` event registration (`core.EventType`), mirroring WR1. Foundation; no SSH. Unit test the round-trip.
2. **PB2 (`hk-462t`)** — the pure `breachDetector` state machine (per worker×signal, hysteresis, two dwell timers, breach/clear emission) + exhaustive table-driven tests over sample-sequences with an injected clock (spike-below-dwell = no event; sustained = one breach; flap-in-band = no re-fire; recovery = one clear; episode-duration correctness). The crux bead — test hard. Depends on PB1. *(Built with PB1 on one branch.)*
3. **PB3 (`hk-kaf0`)** — wire detectors into `RunReportLoop`: adaptive cadence (fast while `InFlight>0`, slow/silent when idle, reset-on-idle), feed samples, emit. Config knobs with defaults on `workers.Config`. Preserve Phase-1 `worker_report` cadence + off-by-default (no workers / `breach_detection_enabled:false` ⇒ byte-identical to Phase 1). Depends on PB1+PB2.
4. **PB4 (`hk-xldo`)** — surfacing + doc: confirm `resource_breach` reaches `harmonik subscribe` (like WR5); extend `docs/remote-substrate/worker-reporting.md` with the Phase-2 section (signals, state machine, config, the "silent when idle" guarantee). Depends on PB3.

Acceptance: with a worker enabled and a run in flight, a sustained CPU/mem/swap breach emits exactly one `resource_breach{kind:breach}` after the dwell and one `{kind:clear}` on recovery; an idle box emits none; a sub-dwell spike emits none; `breach_detection_enabled:false` and no-workers are byte-identical to Phase 1.

## Explicitly NOT in Phase 2
- No worker-side sampler / resident process / relay change (central-side by construction).
- No autoscale / action on breaches — still observability only (Phase 3).
- No true instantaneous %CPU (load-ratio proxy; `cpu_source:top` pre-wired for later).
- Disk breaches (Phase-1 `disk_pressure` covers it).
