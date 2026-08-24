---
id: spendmeter-extract
title: Extract the spend meters — two files, one dependency, and it disappears on its own
type: task
priority: 1
labels: [daemon, extraction, clear-the-ground]
depends_on: [runregistry-extract, lint-rekey-exclusion-list]
blocks: []
workstream: W2
batch: 3
---

## Problem

The spend meters — daily budget accounting and per-queue budget pausing — are two files in
`internal/daemon` totalling **466 production lines**. (The review says 638; that predates the comment
cut.)

This is the smallest and cleanest of the four extractions. Four symbols cross the boundary, **all
already exported**, and only two production files call them.

**Its one dependency on daemon disappears by itself.** `perqueuespendmeter_tigaf11.go` takes
`reg *RunRegistry` at lines 41 and 60, and calls only `reg.Get(runID)`. After `runregistry-extract`
lands, that type lives elsewhere and the dependency is gone.

## Scope

Move to a new package `internal/spend`:

- `internal/daemon/spendmeter_hkk3f8g.go` (222 lines)
- `internal/daemon/perqueuespendmeter_tigaf11.go` (244 lines)
- `internal/daemon/perqueuespendmeter_tigaf11_test.go` (already `package daemon_test`)

Inbound symbols: `DaemonSpendMeter`, `NewDaemonSpendMeter`, `PerQueueSpendMeter`,
`NewPerQueueSpendMeter`. Consumers: `bootstate.go:137,148` and `daemon.go:613`.

**The spend half of `export_meters_pause_test.go` moves with it** — `ExportedNewDaemonSpendMeter`,
`ExportedSpendMeterHandleRunStarted`, `ExportedSpendMeterHandleBudgetAccrual`,
`ExportedSpendMeterSetMaxRunsPerDay`, `ExportedSpendMeterSetDailyCapBytes`,
`ExportedNewPerQueueSpendMeter`, `ExportedPerQueueSpendMeterHandleBudgetAccrual`,
`ExportedPerQueueSpendMeterSetDayKey`, `ExportedPerQueueSpendMeterSetGlobalCapUSD`. They poke
unexported methods and fields, so they become an `export_test.go` in the new package.

**`scripts/queuewiring-freeze-gate.sh` lines 23–26 instruct this task directly** — it says the
`*spendmeter*` pattern is not yet fenced because the files are gated on the run-registry closure, and
to add the pattern when that lands. Add it.

## Done when

1. `internal/spend` exists with a `depguard` rule denying `internal/daemon`.
2. `bootstate.go` and `daemon.go` compile against it.
3. `scripts/queuewiring-freeze-gate.sh` includes `*spendmeter*` in its forbidden-file find.
4. `tools/lintreport/allow.txt` gains nothing. Three existing entries travel — all three are on
   production files, so all three move.

## Limits

- **A claim in the 2026-08-22 review is wrong and following it will waste time.** It says
  `perqueuespendmeter_tigaf11.go` references handler-pause and so must land before the handler-pause
  extraction. Checked against current source: its pause vocabulary is `queue.QueueStatusPausedByBudget`
  from `internal/queue`, not `HandlerPauseController`. **There is no spend-to-pause symbol edge.**
  This task and `handlerpause-extract` are serialized only because both edit `bootstate.go`,
  `daemon.go` and `export_meters_pause_test.go` — a file conflict, not a design one.
- `shutdownbudget_test.go`, `budget_landing_verdict_helpers_test.go`,
  `commitbudget_heartbeat_progress_test.go` and `queue_perqueue_pause_globalflag_tigaf6_test.go` are
  budget-named but touch no spend-meter symbol. Leave them.
- Do not start before `runregistry-extract`. Doing so means inverting the `RunRegistry` dependency
  behind an interface that will be deleted a week later.
