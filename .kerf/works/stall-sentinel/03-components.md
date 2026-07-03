# Stall-Sentinel — Components

> Formalizes brief §4 (home/architecture) and §7 (build order) into named components.
> Authoritative source = `plans/2026-07-02-stall-sentinel/DESIGN.md`. Do not re-derive.

Seven components, in dependency order. The signal library is the shared deterministic primitive; the
detectors and escalation compose over it; the acceptance suite and consumer are downstream.

## C1 — Signal library (deterministic core)

Reads `.harmonik/events/events.jsonl` + the run registry and computes, per active run,
**last-event-age** + **phase state**; plus lane-level rollups (last forward-progress per lane,
live-crew set, queue/assignment state) for Layer B's expectation-of-progress predicate. Pure Go,
no side effects, unit-tested against recorded / synthetic event streams. **Gates C2 and C3.**

## C2 — Layer A detectors (per-run stall)

Thin decision logic over C1: **heartbeat-gap** (`run_silence_stall`), **review-stall**
(`reviewer_verdict` fired, no terminal within `review_finalize_stall`), **run-age**
(`run_max_age` backstop). Emits `stall_detected`. Depends on C1.

## C3 — Layer B detector (global lane no-forward-progress)

Thin decision logic over C1: lane has an **expectation of progress** (non-empty queue OR
open-assigned bead OR mid-flight run) AND a live crew, but zero forward-progress events for
> `lane_noprogress_stall` → fire. Includes the **false-positive guard** and a **NEGATIVE idle test**
(correctly-idle crew must NOT trip it). Depends on C1.

## C4 — Config block (thresholds, fail-loud)

X/Y/Z escalation thresholds + `run_silence_stall` / `review_finalize_stall` / `run_max_age` /
`lane_noprogress_stall`. Config-driven, fail-loud on missing required keys; mirror the keeper/watch
`ResolveKeeperConfig` pattern (names missing keys, points at a `--example`; off/0 is valid explicit).
Independent of C1–C3 (a parallel wiring track), but consumed by C2/C3/C5.

## C5 — Tiered escalation

Deterministic `harmonik comms send` per tier (crew X → captain Y → operator-mailbox Z), plus the
**pane-keystroke path** for a wedged crew (reuse keeper `send-keys`). Consumes the `stall_detected`
firings + C4 thresholds. Depends on C2 + C3 (needs something to escalate) and C4 (needs thresholds).

## C6 — Acceptance suite

Replays the 3 real stall classes against recorded / synthetic event streams (deadlock → Layer B;
silent hang → Layer A heartbeat-gap; review-wedge → Layer A review-stall) PLUS the negative idle test.
Depends on C2 + C3 (the detectors under test).

## C7 — Watch / ops-monitor consumption (display only)

Surface `stall_detected` in the watch feed / ops-monitor `latest.json`. Display only — does NOT
detect. Depends on the `stall_detected` event contract stabilizing (C2/C3 emit it).

## Dependency shape

```
C1 (signal library, GATE)
   ├──▶ C2 (Layer A) ─┐
   └──▶ C3 (Layer B) ─┤
C4 (config) ──────────┼──▶ C5 (escalation)
                      └──▶ C6 (acceptance suite)
C2/C3 (stall_detected contract) ──▶ C7 (watch/ops-monitor consume)
```
