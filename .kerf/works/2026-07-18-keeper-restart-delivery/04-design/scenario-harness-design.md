# Design — scenario-harness.md change (K6 testing)

Codename: `2026-07-18-keeper-restart-delivery` · pass 4
Grounded by `03-research/testing/findings.md` (all citations there).

## The load-bearing correction: two twins, and the keeper's failures live in the OTHER one

`cmd/harmonik-twin-claude` (the parity-audit / scenario-harness twin) has **no tmux pane** —
it speaks NDJSON wire events only (`twin-claude/main.go:215-243`). Four of the five target
failures are pane/timing/handoff/operator-typing failures, invisible to it. The right vehicle
is **`cmd/harmonik-twin-session`**, which the keeper's own integration tests build and run in
a **real tmux pane** with the **real `keeper.InjectText`** and a real `HANDOFF-<agent>.md`
(`cycle_twin_e2e_integration_test.go:120-127,267,410,431-434`). The problem-space's "the twin
is the scenario-test vehicle" is therefore only correct for wire-observable behavior; the rest
ride the keeper's session-twin **integration tier**.

## Design: extend the mature keeper integration tier; do NOT invent a harness

The keeper already has (a) a real-tmux integration tier (`cycle_twin_*_integration_test.go`,
`cycle_operator_attached_integration_test.go`) and (b) an offline causal reactive harness
(`cycle_reactive_harness_test.go`) plus a `substrate.FakeClock`. K6 extends these.

**SC-7 failure → test mapping (each fails before, passes after):**

| # | Failure | Level | Pattern to copy |
|---|---|---|---|
| a | operator-typing collision | integration (real tmux) + unit adjunct | pty-attach harness `cycle_operator_attached_integration_test.go`; put partial input on the pane (no Enter), trigger warn, assert the operator line is not submitted. Unit: swap `tmuxRunFn` (`injector.go:106`) to assert **zero** pane write when operator-present and comms is taken |
| b | late-handoff after 300s | harness unit (abort) + integration (T+301 restart) | wire `substrate.FakeClock` into `CyclerConfig.Clock` (`cycle.go:97`), drive to AwaitingHandoff with `writeNonce=false`, `Advance(300s+)`, assert `cycle_aborted{reason=handoff_timeout}`; then `restartnow_smoke_integration_test.go` for the T+301 self-restart completing a clean clear (SC-4) |
| c | comms-unreachable fallback | integration (comms/daemon) | `comms_recv_follow_hk5xuvc_test.go` in-process daemon+UDS + the `comms who` presence registry; seed target absent → assert delivery resolves to terminal-fallback, **never a silent no-op** (SC-2); positive control seeds present → comms |
| d | operator-present misread | unit (primary) + integration adjunct | pure `operatorActiveSince` table `tmuxresolve_operator_test.go`; feed a stale-but-present client_activity, assert current 5-min window says absent (fail-before) and the augmented signal says present (pass-after) |
| e | FORCE-ACT still cuts a never-idle session | existing-level | extend `ForcedClearAboveHardThreshold` (`cycle_scenario_reactive_wave2_test.go`) + `backstop_test.go` hard-ceiling — assert the K2 deferral does NOT weaken the backstop |

## What changes in scenario-harness.md (thin)

- **(i)** Add at most **one** wire-observable scenario IF the comms-unreachable fallback (c)
  emits an assertable bus event — candidate `scenarios/regression/*.yaml`. Only if K1 emits an
  event the wire twin can see.
- **(ii)** Record **normatively** that pane/timing/handoff/operator-typing coverage (a,b,d,e)
  is delivered by the keeper's **session-twin integration tier** (`harmonik-twin-session` +
  real tmux), **outside** the SH YAML contract — mirroring the spec's own real-tmux carve-out
  (SH-INV-001, real-tmux is not the SH harness's job).
- **(iii)** Any addition to the §10.1 three-scenario conformance floor is a foundation
  amendment (`scenario-harness.md:899`) — avoid unless the one wire scenario in (i) is
  promoted to the floor (it should not be for v1).

Net: scenario-harness.md gets a thin K6 pointer + optionally one wire scenario; it is **not**
the primary carrier of this work's tests.
