# 04-Design / C1 — ClockPort in the daemon

> Pass 4 design. Elaborates D2 (00-decisions). Research: `03-research/c1-clockport/findings.md`.
> Target spec: `specs/run-state-machine.md` §Determinism (RSM); no existing spec touched.

## Current state
No `specs/` file owns the daemon run lifecycle or its time-source. The daemon reads the
wall clock directly at 38 sites in `workloop.go` + 8 in `reviewloop.go`, and blocks on wall
timers in `agentready.go:194`, `postreadyhang.go:59`, and the `pasteinject.go` watchdog.
Four disjoint test-time mechanisms exist (hook struct with no clock; mutable package vars;
three `Now func()` config fields; duration configs) — none unified. `substrate.ClockPort`
(clock.go:10) + `FakeClock` are landed and green on codex + keeper; the daemon does not use
them.

## Target state
- **RSM-CLK-1.** The run lifecycle reads time ONLY through `substrate.ClockPort`. A `Clock`
  dependency is threaded through the run-path ports and the reactor shell, defaulting to
  `SystemClock{}` when nil; tests inject `FakeClock`.
- **RSM-CLK-2.** ClockPort scope is the five run-path files (workloop, reviewloop,
  agentready, postreadyhang, pasteinject-watchdog). Every run-path `time.Now`/`Since`
  becomes `Clock.Now()`/`Clock.Since()` (shell, stamped onto events); every
  `time.After`/`NewTimer` select-deadline and the run-path `context.WithTimeout` timeouts
  become reactor timer events (D6). No run-path blocking wait reads the wall clock.
- **RSM-CLK-3.** ClockPort is THE single time seam for the run path: the mutable timeout
  vars (`agentReadyKillReapTimeout`, the pasteinject `commit*` vars), the three `Now func()`
  fields (incl. `StaleWatcher.Now`), and the duration configs reconcile onto it. `StaleWatcher`
  `ScanInterval` migrates to a ClockPort ticker.
- **RSM-CLK-4.** `FakeClock.Advance` + `BlockUntil` make the C5 bounded-liveness property
  test deterministic (virtual time; no real sleeps).

## Rationale
A deterministic clock is the prerequisite for C5's liveness property test (findings §Patterns,
Risk 1). Reconciling to one seam prevents the "codebase ends with five seams" outcome
(findings Risk 3). The keeper D4/D11 template (findings Q4) is reused verbatim; zero new
abstraction.

## Requirements traceability
02-components C1 → RSM-CLK-1..4. Goal "reuse the proven substrate seam" (01-problem-space
§2) → RSM-CLK-1/3. Enables success-criterion 3 (liveness property test) via RSM-CLK-4.

## PLANNER-RECONCILE
Scope is five files, not the 26-site workloop-only estimate in 02-components (D2, item 1).
