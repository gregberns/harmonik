# RT11 — coverage floor record (run-state-machine)

**Purpose:** the RT11 acceptance record (07-tasks.md §RT11; liveness-parity-design §6
item (4); RSM-030 "the state-machine path MUST meet the measured coverage floor from
the coverage audit"). Consumed by RT12's evidence bundle. Floor = the M1-5 audit
(`M1-5-coverage-baseline.md`, captured 2026-07-14 at `32791808`).

## How it was produced

Captured **2026-07-14** on branch `rt11-fault-matrix` at `f9e3f0d4` (parent
`53c524b4` on `phase1-session-restart-substrate`).

```bash
# Reactor + merge-queue packages (incl. the RT11 runexectest tiers):
go test ./internal/runexec/... ./internal/mergeq/... ./internal/runexectest/... \
  -count=1 -coverprofile=rsm-cov.out -coverpkg=./internal/runexec/...,./internal/mergeq/...

# Shell (runshell.go) via the full daemon suite:
go test ./internal/daemon/ -coverprofile=daemon-cov.out \
  -coverpkg=./internal/daemon/... -count=1 -timeout=900s
go tool cover -func=daemon-cov.out
```

The daemon run reported FAIL on **4 known-flaky environmental E2Es only**
(SSHLocalhost, StopHookE2E ×2, Hk6ynv4_SubscribeStream — all on the pinned
known-flaky list); coverage still computes, same treatment as the M1-5 capture.

## Measured vs floor

| Surface | Measured | M1-5 floor | Verdict |
|---|---|---|---|
| `internal/runexec` (package) | **95.4%** | (new package — path floor 73.5%) | PASS |
| `internal/mergeq` (package) | **87.0%** | 87.0 (coverage.baseline) | PASS (met) |
| runexec+mergeq combined profile | **95.0%** | 73.5% path floor | PASS |
| `internal/daemon/...` overall | **74.3%** (`-func` total; 74.2% statements) | 73.5% | PASS |
| `beadRunOne` (workloop.go) | **63.7%** | 60.3% | PASS |
| `runWorkLoop` (workloop.go) | **71.9%** | 71.8% | PASS |

## runshell.go per-func (the extracted shell)

```
newRunShell 100.0  normalize 87.5  execute 100.0  executeAgentAction 80.0
executeRunAction 77.8  executeEmitOrTimer 100.0  feed 100.0  drive 83.3
drainPending 100.0  driveOnce 58.8  nearestDeadline 90.9  fireElapsedTimers 83.3
fireOnCancel 0.0  RunDispatch 87.5  dispatchSegmentActive 100.0
driveDispatchOnce 88.2  driveRun 100.0
```

Note: `fireOnCancel` (the shutdown-cancel timer flush) is the one uncovered
runshell func — a candidate for an RT12/follow-on shell test; it does not gate
the M1-5 floor (which is the daemon-path aggregate + the two anchors).

## RT11 companion evidence (same capture)

- Fault matrix: `go test ./internal/runexectest/` — **102/102 cells pass**
  (6 strata × {drop_after, stall, truncate, dup} × every stimulus position
  + 1 clean cell per stratum), 100% terminal-never-silence; headline
  `TestRunexecFaultMatrix_StallAfterResume` green; `-race` clean.
- N=10 relaunch oracle: `TestRunexecOracle_CleanRelaunchN10` green
  (10 consecutive clean resumed relaunches, one FakeClock, zero wall-clock).
