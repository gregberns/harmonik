# 15 — Keeper Reliability Baseline (pre-`89852bb3`)

**Role:** QA baseline for the keeper's two context-reset cycles.
**Subject:** current `main`, HEAD `1ccc2b90`, `/Users/gb/github/harmonik`.
**Purpose:** establish a re-runnable reliability baseline BEFORE the stranded
−542-line simplification (`89852bb3`) merges. Re-run this exact doc after merge
to measure the delta.
**Run date:** 2026-06-20. **Suite verdict: deterministic suite GREEN.**
**tmux safety:** baseline 9 → integration run leaked 2 `*-flywheel` sessions →
killed → back to 9. **Leak observed and contained (see §5).**

---

## The two cycles under test

- **L (in-place):** handoff → `/clear` → `/session-resume` pasted into the SAME
  tmux pane / SAME Claude process.
  - **L-auto** — the automatic watcher cycle: `watcher.go` tick →
    `Cycler.MaybeRun` → `runCycle` (`cycle.go:706`). Also `RunForPrecompact`.
  - **L-manual** — the operator/captain `harmonik keeper restart-now` →
    marker → watcher consume → `RunOnDemand` → `runOnDemandCycleTail`
    (`cycle.go:1240`). **This is the path `89852bb3` rips out** and replaces with
    a direct synchronous `restartnow.go`.
- **D (kill+respawn):** handoff → kill the Claude process → spawn a fresh
  process that resumes from `HANDOFF.md`. In code: `NewLiveRecoverViaRespawn`
  (`internal/keeper/respawn.go`) + the watcher's `maybeRespawn` /
  `maybeLivePaneRecover` paths, driven by `sh -c <RespawnCmd>`.

---

## 1. Test inventory (`internal/keeper/*_test.go`)

31 test files. Build-tag split: **default** (runs on every `go test`) vs
**`//go:build integration`** (Tier-3 `check-full` only) vs **`//go:build
scenario`**.

### Default-tag (deterministic, in-process, no real tmux)

| File | Covers | Cycle |
|---|---|---|
| `cycle_test.go` (120 KB) | the gate truth-table for `MaybeRun`/`runCycle`; `waitForNewSessionID`; `RunForPrecompact` happy/anti-loop; `ManagedGuardInsideMaybeRun` | **L-auto** (logic only) |
| `cycle_restart_now_test.go` | `RunOnDemand` gate-pass / stale-handoff / nonce-mismatch / SID-mismatch / anti-loop / not-crisp-idle / holding-dispatch; `WriteRestartNowMarker`+`ReadRestartNowMarker` round-trip & atomicity | **L-manual** (logic + marker I/O) |
| `cycle_reactive_harness_test.go`, `cycle_scenario_reactive_test.go`, `cycle_scenario_reactive_wave2_test.go` | reactive harness: proves `/clear`→SID-flip is CAUSED by the inject (not time-faked); injection is a plain Go call, no tmux | **L-auto** (causal, no tmux) |
| `cycle_operator_attached_test.go`, `…_throttle_test.go` | operator-busy suppression / throttle | L-auto gate |
| `precompact_test.go` | `RunForPrecompact` not-managed / empty-sid / holding-dispatch / anti-loop / happy; `HasPrecompactTrigger`; `ClearPrecompactTrigger` idempotent | **L-auto** (precompact variant) |
| `live_recover_action_test.go` | `NewLiveRecoverViaRespawn`: nil-when-no-cmd; runs-on-valid-sid (cmd=`true`); **refuses-on-invalid-sid** (uuidv7/garbage/absent → `ErrLiveRecoverIdentityUntrusted`, sentinel NOT touched) | **D** (identity gate; cmd stubbed) |
| `watcher_test.go` (29 KB) | tick loop; `RespawnFiredWhenGaugeAbsentAndPaneIdle`; `RespawnSkippedWhenPaneNotIdle`; `RespawnCooldownPreventsDoubleSpawn` (all `RespawnCmd:"true"`); `AdoptsSameAgentNewSidAfterExternalClear`; `IgnoresForeignSessionGauge` | **D** (decision; cmd stubbed) + L re-resolve |
| `watcher_live_pane_recover_test.go`, `watcher_decision_exempt_test.go` | live-pane-recover decision; decision-exempt | D decision |
| `injector_test.go` | **only** empty-target guard, `sleepCtx` cancel, 3 timing-constant asserts (`submitSettle==750ms`, `submitRetries==2`, `submitRetryDelay==400ms`) | **the paste mechanism — NOT executed** |
| `heartbeat_test.go`, `sleep_gate_hkl3gs_test.go`, `gates_test.go`, `thresholds_test.go`, `sessionid_test.go`, `statusline_test.go`, `keeper_test.go`, `tmuxresolve_test.go`, `tmuxresolve_operator_test.go`, `export_test.go` | heartbeat; sleep gate; gate helpers; threshold math; SID validation; statusline parse; flock; tmux-target resolution | supporting (pure) |

### Integration-tag (`//go:build integration` — dark by default)

| File | Covers | Real? |
|---|---|---|
| `cycle_twin_e2e_integration_test.go` (45 KB) | `TestIntegration_TwinClearRestartCycle_E2E`, `…_OperatorRealEnv`, `…_DefaultsPin`, `…_GaugeStateTransitions`, `TwinWatcher_ExternalClearReResolve` | **real tmux + real `InjectText` paste**, but session = **cooperative `cmd/harmonik-twin-session` fake**, NOT real Claude |
| `cycle_twin_1m_gauge_integration_test.go` | 1M-window inference via the real `keeper-statusline.sh` | real script, twin |
| `cycle_twin_gauge_liveness_integration_test.go` | `TwinEmitNA_SkipsCtxWrite`, `TwinSuppressStatusline_GoesStale` | real script, twin |
| `cycle_operator_attached_integration_test.go`, `watcher_live_pane_recover_integration_test.go`, `sessionstart_hook_integration_test.go`, `tmuxresolve_integration_test.go` | live tmux variants of the above | real tmux, twin |

### Scenario-tag
`scenario_decisions_orphan_reap_s7_hk061_test.go` (`//go:build scenario`).

### Coverage map by path

| Path / mechanism | Deterministic | Integration-only | NONE |
|---|---|---|---|
| L-auto gate logic (`MaybeRun`/`runCycle` truth-table) | ✅ strong | | |
| L-auto **end-to-end paste delivery** | | ⚠️ twin-only (real tmux, fake REPL) | |
| L-manual `RunOnDemand` gate logic | ✅ | | |
| L-manual marker write→consume **across a live keeper** | | | ❌ marker round-trip ≠ consume-by-live-keeper |
| `InjectText` paste / settle / Enter / retry (`injector.go`) | constants only (10.5% / `sendEnter` 0%) | exercised in twin e2e (cooperative, no ingest-race) | ❌ real-REPL timing race |
| D respawn **decision + identity gate** | ✅ (`RespawnCmd:"true"`) | | |
| D respawn **actual spawn of a fresh Claude** | | | ❌ command stubbed `true`/`touch` |
| bash hooks (`keeper-statusline.sh` jq / `[1m]` window) | | partial via twin (script runs) | ❌ no direct hook unit test |

---

## 2. Suite execution (run ONCE each)

### Deterministic suite — GREEN
```
$ go -C /Users/gb/github/harmonik test ./internal/keeper/... -cover
ok  github.com/gregberns/harmonik/internal/keeper  4.639s  coverage: 74.4% of statements
```
- **Pass/fail:** PASS. **Duration:** ~4.6 s. **Coverage:** 74.4 % of statements.
- Matches the critique's reported figure. No flakes, no real tmux.

### Integration Twin suite — RAN ONCE, 2 expectation failures (NOT mechanism failures)
```
$ go -C /Users/gb/github/harmonik test -tags integration ./internal/keeper/... -run Twin -count=1
FAIL  github.com/gregberns/harmonik/internal/keeper  39.213s
```
Confirmed safe before running: `cmd/harmonik-twin-session` self-documents
*"Real injection, real tmux and real Claude are NOT simulated here."* The test
`go build`s the twin, spawns it in real tmux sessions, and `tmux kill-session`s
them on teardown. No real claude spawn, no looping restart-now.

**PASSED (the cycle mechanics):**
- `TestIntegration_TwinClearRestartCycle_E2E` — the full L in-place cycle against
  real tmux + the cooperative twin. ✅
- `TestIntegration_TwinWatcher_ExternalClearReResolve` — keeper re-resolves
  `.managed` after an external `/clear`. ✅
- `TestIntegration_TwinEmitNA_SkipsCtxWrite`, `…_SuppressStatusline_GoesStale`,
  all `Twin1mGauge*`, `TwinE2E_OperatorRealEnv`. ✅

**FAILED (band-expectation drift, not a cycle defect):**
- `TestIntegration_TwinE2E_DefaultsPin` — asserts `WarnAbsTokens=270000`,
  `ActAbsTokens=300000`, `ForceActAbsTokens=340000`; code is pinned at
  **200000 / 215000 / 240000**.
- `TestIntegration_TwinE2E_GaugeStateTransitions` (`1m-below-act`,
  `1m-below-warn`, `1m-act-but-not-crisp`) — fail for the same reason: the test
  expects a fire boundary at the wider 270k/300k band; the code fires at the
  narrower 200k/215k band.

**Interpretation:** these two failures are the **pct-vs-band conflict** named in
the recovery report (README §2 bug D + the `89852bb3` ⚠️ note). Someone authored
the integration tests expecting the operator-intended **300k cap** band, but
`main`'s code (and `89852bb3`) pin the locked **200k/215k** band. This is a
**test/code expectation mismatch**, not a reset-cycle reliability failure — the
in-place cycle mechanics themselves (`TwinClearRestartCycle_E2E`) pass. Flag for
the post-merge re-run: decide whether the band tests or the band code is wrong.

---

## 3. Per-cycle reliability verdict

### L-auto (automatic in-place `/clear`) — **NOT provably reliable end-to-end**
- **Deterministic coverage:** gate/decision logic only. The truth-table is
  exhaustively tested (`cycle_test.go`), so "should we fire?" is reliable.
- **Where the test mocks the failing thing:** the inject is `cycleSpyInjector`
  (`cycle_test.go:15-33`) — it just appends text to a slice. The real
  `InjectText` paste→750ms-settle→Enter→2×retry sequence (`injector.go`) has
  **10.5 %** coverage; `sendEnter` is **0 %**. Per report 06 Hazards A/B/C, `/clear`
  and `/session-resume` are **fire-and-forget** — `waitForNewSessionID` polls
  3 s (`ClearSettle`) and proceeds regardless; the journal is marked `complete`
  whether or not the keystrokes landed.
- **Gap to provably-reliable:** a default-tag test that drives the real
  `InjectText` sequence AND a closed-loop ACK (read-back the pane for a nonce) so
  success is confirmed not inferred. Today only the twin e2e exercises real
  paste, and its fake REPL cannot reproduce the production ingest-race.

### L-manual (`restart-now`) — **NOT provably reliable; test certifies the no-op**
- **Deterministic coverage:** `RunOnDemand` gate logic is well tested; the marker
  is round-trip + atomicity tested.
- **Where the test mocks the failing thing:** `TestRestartNowMarker_RoundTrip`
  asserts the marker was **written and re-read** — i.e. it certifies the
  production no-op (README bug B: "writes marker, exit 0, cycle never advances").
  There is **NO** test that fails when no live keeper consumes the marker, and
  **NO** end-to-end CLI-write→disk→watcher-consume test (the two halves "meet
  only at the struct shape"; the consume side uses an injected in-memory stub).
- **Gap to provably-reliable:** the ACK handshake (`[KEEPER ACK <nonce>]`
  read-back) — which is exactly what `89852bb3` introduces. **This is the cycle
  the merge most directly improves**, so the post-merge re-run should show this
  verdict change.

### D (kill+respawn) — **decision reliable; the actual respawn is untested**
- **Deterministic coverage:** strongest *decision* coverage of the three —
  fire-when-gauge-absent-and-idle, skip-when-not-idle, cooldown-prevents-double,
  and a genuine **identity-trust gate** (`ErrLiveRecoverIdentityUntrusted` on
  uuidv7/garbage/absent .sid, sentinel proves the cmd did NOT run).
- **Where the test mocks the failing thing:** every respawn test stubs the actual
  spawn with `RespawnCmd:"true"` / `touch sentinel`. The real
  `sh -c <RespawnCmd>` (which kills + spawns a fresh `harmonik keeper` and a new
  Claude that resumes from HANDOFF) is **never executed**. So "does the respawned
  process actually come up and resume intent?" is **0 % covered**.
- **Gap to provably-reliable:** a guarded live smoke that runs a real respawn
  once and asserts the new process is alive and re-hydrated from HANDOFF.md
  (§4 runbook, cycle 2). This is also the experiment the EXEC_SUMMARY calls the
  **highest-value next action** (resolves the L-vs-D fork).

**Bottom line:** all three cycles are *testable-but-untested where they fail*.
The gate logic is reliable; the side-effect surface (real paste for L, real
spawn for D) is mocked. None of the three is provably reliable end-to-end on a
real Claude REPL today.

---

## 4. Post-merge LIVE-SMOKE runbook (DO NOT run before merge)

> ⚠️ **FORK-BOMB SAFETY — read first.** The known leak signature is an *ad-hoc
> LOOPING* keeper restart-now smoke that leaks 1500+ `*-flywheel` tmux sessions
> and drives load → 42. Rules: **run each cycle EXACTLY ONCE; never wrap in a
> loop / `for` / `go test -count=N>1`; record `tmux ls | wc -l` before AND after
> each step; abort + clean if the count climbs.** The integration suite itself
> leaks 2 `*-flywheel` sessions (§5) — always sweep them after.

### 0. Pre-flight
```bash
# Confirm the merge landed and the binary is rebuilt.
git -C /Users/gb/github/harmonik log --oneline -1                 # expect 89852bb3 merged
go -C /Users/gb/github/harmonik install ./cmd/harmonik
# Re-run the deterministic baseline — MUST be green before any live step.
go -C /Users/gb/github/harmonik test ./internal/keeper/... -cover
# tmux guard — record this number.
BEFORE=$(tmux ls 2>/dev/null | wc -l); echo "tmux BEFORE=$BEFORE"
```

### Cycle 1 — one real in-place `/clear` restart (L-manual, the `89852bb3` path)
```bash
# A. Scratch session running a REAL claude (this is the live part — opus/sonnet
#    your choice; a small/cheap model is fine for a liveness smoke).
SMOKE=keeper-smoke-$$            # unique, NOT a *-flywheel or *-default name
tmux new-session -d -s "$SMOKE" 'claude --dangerously-skip-permissions'
sleep 5                          # let the REPL boot
tmux ls 2>/dev/null | wc -l      # expect BEFORE+1

# B. Point a keeper at it (single shot; the watcher is a bounded ticker — fine).
#    Use whatever enable/agent wiring keeper doctor reports for a scratch agent.
harmonik keeper enable --agent "$SMOKE" --project /Users/gb/github/harmonik
harmonik keeper doctor --agent "$SMOKE"     # confirm it sees the pane + gauge

# C. Fire restart-now ONCE. On 89852bb3 this is the DIRECT synchronous path:
#    expect it to print nonce=rn-… and exit NON-ZERO loudly if it fails (no
#    silent exit-0 no-op).  *** RUN THIS LINE EXACTLY ONCE — NO LOOP ***
harmonik keeper restart-now --agent "$SMOKE" --project /Users/gb/github/harmonik

# D. Confirm the ACK + the cycle landed in the pane (the closed loop):
tmux capture-pane -p -t "$SMOKE" | grep -E "KEEPER ACK rn-|session-resume" || \
  echo "FAIL: no ACK / no resume visible — restart-now did not close the loop"
```

### Cycle 2 — one real kill+respawn (D, `respawn.go`)
```bash
# Reuse $SMOKE (or a second scratch session). Capture the pre-kill SID so we can
# prove a NEW process came up.
OLD_SID=$(cat /Users/gb/github/harmonik/.harmonik/keeper/"$SMOKE".sid 2>/dev/null)

# Drive ONE respawn. Either: kill the claude pane and let the watcher's
# maybeRespawn fire on the next idle tick, OR invoke the respawn command path
# directly. *** ONE invocation — NO LOOP. ***
tmux send-keys -t "$SMOKE" C-c            # stop the claude process in the pane
# wait for ONE watcher tick + RespawnGrace (single bounded wait, not a loop):
sleep 15

# Confirm a fresh process resumed from HANDOFF and the SID changed:
NEW_SID=$(cat /Users/gb/github/harmonik/.harmonik/keeper/"$SMOKE".sid 2>/dev/null)
tmux capture-pane -p -t "$SMOKE" | grep -E "session-resume|HANDOFF" \
  && [ "$NEW_SID" != "$OLD_SID" ] \
  && echo "PASS: respawn produced a fresh, re-hydrated session" \
  || echo "FAIL: no fresh respawn / no re-hydration"
```

### Teardown + tmux guard (ALWAYS, even on failure)
```bash
harmonik keeper disable --agent "$SMOKE" 2>/dev/null
tmux kill-session -t "$SMOKE" 2>/dev/null
# Sweep any leaked flywheel/scratch sessions the run produced:
tmux ls 2>/dev/null | grep -E "flywheel|keeper-smoke" | cut -d: -f1 \
  | xargs -r -n1 tmux kill-session -t
AFTER=$(tmux ls 2>/dev/null | wc -l); echo "tmux AFTER=$AFTER (BEFORE=$BEFORE)"
[ "$AFTER" -le "$BEFORE" ] && echo "tmux OK" || echo "LEAK — investigate & kill"
```

> **NEVER** kill a `*-default` session (it is the single fleet-wide spawn
> target) or any `captain` / `crew-*` / live keeper session listed in §5.

---

## 5. tmux leak observed during THIS baseline

| Step | tmux count |
|---|---|
| Baseline (start) | **9** |
| Before integration run | 12 (fleet churn — crews/captain) |
| After `go test -tags integration -run Twin` | 11, **but 2 NEW `*-flywheel` sessions present** |
| `harmonik-07d0c30717aa-flywheel`, `harmonik-bb1d74e5b159-flywheel` (created 08:42:51, during the run) | — |
| After `tmux kill-session` of both | **9** (back to baseline; all remaining are pre-existing captain/crew/keeper) |

**Finding:** the integration Twin suite leaks **2 `*-flywheel` tmux sessions per
run** that its teardown does not reap (it kills its own `tw*` twin sessions but
not these). This is small and bounded for a single run, but it matches the
fork-bomb session-name family — **any looped run would multiply it**. Recommend
filing a cleanup bead so the integration harness reaps `*-flywheel` on teardown,
and ALWAYS sweep after an integration run (the §4 teardown does this).
No loop was ever run; the leak was contained immediately.

---

## 6. Re-run checklist (after `89852bb3` merges)

1. Re-run §2 deterministic suite — confirm still GREEN; note new coverage %
   (`89852bb3` deletes `cycle_restart_now_test.go` and adds `restartnow_test.go`).
2. Re-run §2 integration Twin suite ONCE — check whether the band-expectation
   failures (`DefaultsPin`, `GaugeStateTransitions`) are resolved or still
   present (depends on the pct-vs-band decision made at merge).
3. Re-evaluate §3 verdicts: **L-manual** should improve most (direct synchronous
   path + ACK read-back); **L-auto** and **D** likely unchanged (the merge does
   NOT touch the automatic ACT-loop or the respawn spawn).
4. Run §4 live smoke ONCE per cycle, with the tmux guard, and record BEFORE/AFTER.
