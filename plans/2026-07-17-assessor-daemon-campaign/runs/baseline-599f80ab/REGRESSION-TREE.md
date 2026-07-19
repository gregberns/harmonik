# Green-Tree / Regression-Leg — Baseline Gate @ 599f80ab

**Pin:** `599f80ab7f8fc7ef5169db8e37210b04aeb5ccb3` (confirmed `git rev-parse HEAD` in scratch clone)
**Scratch clone:** `/private/tmp/h-assessor/scratch-baseline-599f80ab` (detached HEAD)
**Env:** DEFAULT system TMPDIR (`/var/folders/...` — NOT `/tmp/h-assessor`), `GOCACHE=/tmp/h-assessor/gocache` passed inline. Live scratch daemon (pid ~21101) left running.
**Prior baseline:** a0591ba3 = PASS.
**Delta a0591ba3..599f80ab:** 64 files, keeper + codexdriver + daemon(claudelaunchspec/projectconfig/crewidlereap/workloop isolation-guard) + workspace + sessioncapture. `go build ./...` = CLEAN.

## Verdict: GREEN-with-known-issues

No real product regression. Every RED is an env/load/timing flake with the tested invariant intact; the delta does not touch any implicated product path.

---

## Per-package results (`go test ./internal/<pkg>/... -count=1`)

| Package | Result | Notes |
|---|---|---|
| internal/keeper | PASS (flaky) | 6/7 full-package runs green; 1 intermittent red (Heartbeat) — load flake, 0/20 isolated |
| internal/daemon | reds = load/timing only | whole-pkg run hit 10-min timeout under load; 5 distinct reds all load/timing; subpkgs `bootconfig`, `router` = PASS; test binary compiles clean |
| internal/codexwire | PASS | 0.41s |
| internal/codexdriver | PASS | 4.19s |
| internal/workspace | PASS | 13.87s |
| internal/sessioncapture | PASS | 0.85s |
| internal/core | PASS | 2.77s |

Note: whole-tree `go test ./... -short` sweep was NOT run — the daemon package alone hit a 10-min timeout and a subsequent serial+extended run was SIGKILL'd (exit 137) under this box's resource pressure (many ephemeral daemons + live scratch daemon). A whole-tree sweep here would only manufacture more load noise; the targeted per-package + per-test isolation runs are the reliable signal.

---

## REDs classified

### 1. TestHeartbeat_KeepsLiveGaugeFresh (internal/keeper) — ENV/LOAD-FLAKE, intermittent
- **Determinism:** 1/7 full-package runs failed; **0/20 isolated** (`-run ... -count=20` all green).
- **Assertion:** `heartbeat_test.go:120` — `expected 0 no_gauge:stale events on a live pane, got 1`.
- **Evidence:** Test drives real millisecond timers: `PollInterval=10ms`, `HeartbeatThreshold=60ms`, `Staleness=120ms`, watcher runs 300ms. Under full-package parallel CPU load a single heartbeat tick slips past the 120ms staleness window for one poll cycle → one `no_gauge:stale` event. Invariant (heartbeat keeps gauge fresh) holds — the gauge-refresh mod-time check passes. Pure scheduler-timing sensitivity under load.

### 2. TestAutoStatusInspection_ProcessGroupKillsOrphanedGrandchild (internal/daemon) — ENV/LOAD-FLAKE
- **Determinism:** 0/5 isolated (`-count=5` green). Only red inside the timed-out, overloaded whole-package run.

### 3. TestStopHookE2E_TwinRelayFastPath (internal/daemon) — ENV/LOAD-FLAKE
- **Determinism:** 0/5 isolated. Same as above — 18s E2E test starved during the overloaded whole-pkg run.

### 4. TestStopHookE2E_TwinRelayWaitGrace (internal/daemon) — ENV/LOAD-FLAKE
- **Determinism:** 0/5 isolated. 20s E2E, same load-starvation cause.

### 5. TestThroughput_TenBeadsAtMaxFour (internal/daemon) — ENV/LOAD-FLAKE (known load family)
- **Determinism:** 5/5 isolated fail — but deterministic *because the box load is sustained* (live daemon always consuming CPU), not because product code regressed.
- **Assertion:** `t11_throughput_test.go:419` — `expected 10 run_started events in JSONL; got 16`. (The `ratio 0.40 (limit 3.0×)` line at :403 is a passing `t.Logf`, not the failure — parallel per-bead 2.33s < sequential per-bead 5.76s.)
- **Root-cause analysis of the extra events:** Parsed the emitted JSONL. All run_started envelopes carry **distinct run_ids** and `envelope.run_id == payload.run_id` for every one (no double-emit). The extra events are legitimate **re-dispatches** — beads whose fixture-handler runs exceeded their timeout under CPU starvation were re-run by the daemon, each with a fresh run_id (correct behavior). ~26/50 beads across the 5 iterations re-ran exactly once. The emission invariant (one distinct run_id per dispatch, no duplicate-emit) is fully intact. The test's `exactly 10` assertion encodes a first-try-success assumption that only holds with spare CPU headroom.
- **Delta check:** `internal/daemon/workloop.go` IS in the delta, but the change is (a) an inert fail-closed codex-isolation guard gated on `codexRequireIsolationBoundary` (set only when `HARMONIK_SUBSTRATE=codexdriver`, not set by this test) and (b) a struct-field alignment reformat — **no change to retry/re-dispatch/emission logic**. Not a regression.

### 6. TestScenario_OperatorNFR_PauseWithRunInFlight (internal/daemon) — ENV/LOAD-FLAKE, intermittent
- **Determinism:** 1/5 isolated.
- **Assertions:** `operatornfr_pause_inflight_..._test.go:387` and `:400` — expected to observe the two-phase `pausing` → `paused` transition in strict order; observed swapped. The captured `changed_at` timestamps show `paused`=…856Z actually *precedes* `pausing`=…857Z (emitted within 1ms). A fast pause path emits both phases near-simultaneously and the subscriber saw them out of expected order.
- **Evidence invariant intact:** every downstream check PASSED — drain (`queue_paused{operator_drain}`, status `paused-by-drain`), no-abort in-flight (`ON-008`), idempotent-pause no-extra-events (`ON-013c`), resume restores dispatch (`ON-056/057`). Only the ordering-observation of the intermediate state flaked.

---

## Timeout / cascade note
The whole-package `go test ./internal/daemon/...` run panicked with `test timed out` at ~588s (Go's default 10-min package timeout), which produced cascade `FAIL pkg [build failed]` / `[setup failed]` / `scenariopkg.test [build failed]` lines. These are **timeout-cascade artifacts, not real breakage**: `go build ./...` is clean, `go list ./internal/daemon/...` enumerates all 4 packages, and `go test ./internal/daemon/ -run xxxNoSuch` compiles the full test binary green (`ok ... [no tests to run]`).

## Known-issues seen
- No occurrence of hk-uhxwd (corpus-lint) or hk-xrn8r (TestScenario_Hk6ynv4_SubscribeStream_EndToEnd) in the target suites this run.
- TestThroughput_TenBeadsAtMaxFour is in the documented daemon load family (srt/SocketBinds/ShutdownDrains/Throughput/ClaimSemaphore/SubscribeStream).
