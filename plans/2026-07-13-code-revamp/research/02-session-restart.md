# Dossier — Session-Restart (Keeper) Anatomy

Factual anatomy of the keeper session-restart vertical, for the event/port-substrate
rebuild plan. Documents what IS. No fixes recommended. Every claim carries a
`file:line`. All paths absolute-rooted at the repo (`internal/keeper/...`).

## Orientation — the moving parts

- **Gauge (token source):** `.harmonik/keeper/<agent>.ctx` — JSON written by the Claude
  Code statusLine hook `scripts/keeper-statusline.sh`; parsed by `ReadCtxFile`
  (`internal/keeper/gauge.go:34`). Struct `CtxFile` = `{pct, tokens, window_size,
  session_id, ts}` (`gauge.go:18-24`).
- **Cycle journal (transient):** `.harmonik/keeper/<agent>.cycle` — per-agent, overwritten
  each cycle. Path: `Cycler.journalPath()` (`cycle.go:638-640`). Struct `CycleJournal`
  (`cycle.go:22-27`+). Written by `writeJournalFile` (`cycle.go:471`), read by
  `defaultReadJournal` (`cycle.go:487`).
- **Watcher loop:** `Watcher.Run` polls the gauge every `PollInterval` (default 5s,
  `thresholds.go:127`) via a `time.NewTicker` at `watcher.go:1003` and calls
  `Cycler.MaybeRun` each tick.
- **Cycler:** `MaybeRun` (`cycle.go:656`) gates, then `runCycle` (`cycle.go:935`) →
  `completeCycleTail` (`cycle.go:1101`) execute the 7 steps.
- **Injector (tmux port):** `InjectText` (`injector.go:131`) — the single seam through
  which `/session-handoff`, `/clear`, and the brief are delivered.
- **Emitter (bus port):** standalone `harmonik keeper` process uses `FileEmitter`
  (`watcher.go:39-100`) which appends to `.harmonik/events/events.jsonl`.
- **Hooks (Claude-side ports):** `keeper-statusline.sh` (writes `.ctx`),
  `keeper-stop-hook.sh` (writes `.idle`), `keeper-sessionstart-hook.sh` (writes `.sid`),
  `keeper-precompact-hook.sh` (writes `.precompact`).

Construction/wiring is in `cmd/harmonik/keeper_cmd.go:457-491`
(`NewFileEmitter` → `NewCycler` → `RecoverFromCrash` → `NewWatcher` → `w.Run`).

---

## 1. The 7 operator steps → actual code

The operator's 7-step model maps onto the auto-cycle path
`MaybeRun → runCycle → completeCycleTail`. Mapping:

| # | Operator step | Code | Where |
|---|---|---|---|
| 1 | Token crosses threshold → handoff needed | Watcher tick reads gauge; `MaybeRun` Gate 3 `belowActThreshold` | `cycle.go:747`, threshold math `cycle.go:397-402` + `thresholds.go:214` |
| 2 | Send the handoff message | `runCycle` injects `/session-handoff <path>` + nonce directive | `cycle.go:989-997` |
| 3 | Detect handoff-file write done | `pollForNonce` — polls HANDOFF file for the nonce string | `cycle.go:1003` → `cycle.go:1511-1529` |
| 4 | Wait for final response (model done) | **NOT a discrete interior step** — see below | — |
| 5 | Send `/clear` | `completeCycleTail` injects `/clear` | `cycle.go:1111` |
| 6 | Handler picks up new session | `waitForNewSessionIDWithBackstop` polls gauge for a changed `session_id` | `cycle.go:1128` → `cycle.go:1540-1576` |
| 7 | Submit the brief command | `completeCycleTail` injects `briefRestartCmd` | `cycle.go:1147` |

Step 2 verbatim (`cycle.go:989-992`):
```go
handoffCmd := fmt.Sprintf(
    "/session-handoff %s\n\nIMPORTANT: include exactly this line verbatim in the handoff file: %s",
    handoffPath, nonceMarker(cycleID),
)
```
Step 7 command (`cycle.go:20`): `const briefRestartCmd = "harmonik agent brief --wake keeper-restart"`.

### Steps that are NOT cleanly represented

- **Step 1 is split.** "Threshold crossed" is not a single event; it is re-evaluated on
  every 5s watcher tick as Gate 3 of a 7-gate ladder (`MaybeRun`, `cycle.go:656-865`).
  Gates that can silently defer the cycle even when tokens are over the line: `.managed`
  opt-in (`:658`), empty session_id (`:662`), boot-grace (`:728`), CrispIdle
  (`:754`), HoldingDispatch (`:762`), sleeping (`:769`), operator hold (`:777`),
  auto-hold on recent operator turn (`:792`), post-answer grace (`:809`), anti-loop
  suppression (`:825`), operator-attached (`:860`). So "threshold crossed → handoff
  needed" is really "threshold crossed AND 10 other gates pass."

- **Step 4 has no interior implementation.** There is no wait-for-model-done step between
  confirm (step 3) and `/clear` (step 5). After `pollForNonce` returns true,
  `runCycle` sets `j.Phase="confirmed"` (`cycle.go:1081`) and calls `completeCycleTail`,
  which injects `/clear` immediately (`cycle.go:1109-1111`) with no wait. The only
  approximations of "model done working":
  - **Pre-cycle:** `CrispIdle` (Gate 4, `cycle.go:754` → `gates.go:77-93`) requires the
    `.idle` marker (written by `keeper-stop-hook.sh` when the model stops) to be newer
    than `.ctx` (within a 10s tolerance). This gates the *whole* cycle, not step 4.
  - **In-step-3:** the nonce landing in the handoff file is itself the proxy for "model
    finished writing the handoff." There is no separate signal that the model's turn
    ended.

- **Step 6 detection is indirect.** "Handler picks up new session" is not observed
  directly; it is inferred by polling the gauge until `cf.SessionID != prevSID`
  (`cycle.go:1571`). The new session_id is re-minted at `/clear` and written to `.sid`
  by `keeper-sessionstart-hook.sh`; the keeper never observes the Claude process
  lifecycle.

### Alternate path (self-service / operator restart-now)

`RestartNow` (`restartnow.go:67-149`) implements steps 2b–7 synchronously in-process for
the `harmonik keeper restart-now` command: verify sid (`:98-102`), ONE handoff-freshness
check (`:109-121`), inject ACK line (`:129`), `/clear` (`:136`), brief (`:143`). It does
**NOT** poll for a new session_id or emit `clear_unconfirmed` — it fires `/clear`+brief
blind after the freshness gate.

---

## 2. Events — durable bus vs transient journal

**Durability:** in the standalone `harmonik keeper` process the emitter is `FileEmitter`
(`watcher.go:39`), which appends an EV-001 envelope line to
`.harmonik/events/events.jsonl` (`watcher.go:88-99`). That file IS the durable bus
substrate. So every `emit*` call listed below is durable; every `j.Phase=...;
WriteJournalFn(...)` write goes ONLY to the transient, overwritten-each-cycle `.cycle`
journal.

### The 7 steps: durable event vs journal-only

| Step | Journal write (transient) | Durable bus event |
|---|---|---|
| 1 (gate pass) | — | none (gating is silent) |
| 2 handoff sent | `Phase="opened"` `:955`, then `Phase="handoff_injected"` `:998-1000` | **`session_keeper_handoff_started`** emitted at `cycle.go:960` (site `:1586`) — fired at cycle *open*, before the inject |
| 3 handoff confirmed | `Phase="confirmed"` `:1081-1083` | **NONE** — nonce-confirm writes journal only |
| 4 model done | — | **NONE** (no step) |
| 5 `/clear` sent | `Phase="cleared"` `:1113-1115` | **NONE on success** — only the *failure* emits `clear_unconfirmed` (see §5) |
| 6 new session up | (no journal write for the wait itself) | **NONE on success** — success is silent; failure → `clear_unconfirmed` |
| 7 brief submitted | `Phase="resumed"` `:1149-1151`, then `Phase="complete"` `:1154-1157` | **`session_keeper_cycle_complete`** at `cycle.go:1158` (site `:1598`) |

This confirms the synthesis claim: **boundary events exist
(`handoff_started`, `cycle_complete`, `cycle_aborted`, `clear_unconfirmed`), but interior
steps 3, 4, 5-success, and 6-success are NOT on the bus** — they are recorded only as
`.cycle` phase transitions, which are overwritten on the next cycle.

### Every event the keeper package emits (grep of `EmitWithRunID` call sites)

Cycle core (`cycle.go`):
- `session_keeper_idle_crew` — `cycle.go:1421`
- `session_keeper_precompact_blocked` — `cycle.go:1484`
- `session_keeper_handoff_started` — `cycle.go:1586`
- `session_keeper_cycle_complete` — `cycle.go:1598`
- `session_keeper_cycle_aborted` — `cycle.go:1610`
- `session_keeper_clear_unconfirmed` — `cycle.go:1621`
- `session_keeper_cycle_recovered` — `cycle.go:1632`
- `session_keeper_operator_attached` — **defined but NO-OP**: `emitOperatorAttached`
  is an empty function (`cycle.go:1507`); no longer persisted (logmine TA3/F55).

Ack handshake (`awaitack.go`):
- `session_keeper_ack_timeout` — `awaitack.go:204`

Watcher (`watcher.go`):
- `session_keeper_no_gauge` — `watcher.go:1497`
- `session_keeper_warn` — `watcher.go:1515`
- `session_keeper_respawn_attempted` — `watcher.go:1824`
- `session_keeper_live_pane_recover` — `watcher.go:1713`
- `session_keeper_blind` — `watcher.go:1790`
- `session_keeper_hard_ceiling` — `watcher.go:1807`

Defined event types NOT emitted from the keeper package:
- `session_keeper_watcher_dead` — emitted by the daemon, `internal/daemon/crewstart.go:688`.
- `session_keeper_restart_now_blocked` — defined `core/eventtype.go:1022`; emitted from
  `cmd/harmonik` (the CLI), not from `internal/keeper`.

Full type registry: `internal/core/eventtype.go:942-1081`.

---

## 3. ClockPort gap — every direct clock call in `internal/keeper/` (non-test)

No `ClockPort` abstraction exists in the cycle core. `cycle.go` and `watcher.go` call
`time.Now()` / `time.Since()` directly on the process wall clock. Two peripheral entry
points (`restartnow.go`, `awaitack.go`) DO thread an injectable `now func() time.Time`
seam (`cfg.Now`) — but the auto-cycle does not.

### `injector.go` — real wall-clock sleeps (confirmed)

No `time.Sleep` anywhere in the package. The injector sleeps via a real `time.NewTimer`:
```go
// injector.go:197-206
func sleepCtx(ctx context.Context, d time.Duration) bool {
    t := time.NewTimer(d)
    defer t.Stop()
    select {
    case <-ctx.Done(): return false
    case <-t.C:        return true
    }
}
```
Called twice per `InjectText`: `submitSettle` = 750ms (`injector.go:82`, used `:148`) and
`submitRetryDelay` = 400ms × `submitRetries`=2 (`injector.go:88,93`, used `:161`). So
every `/session-handoff`, `/clear`, and brief injection blocks the cycle goroutine on
~750ms + up to 800ms of real wall-clock. Verified: synthesis claim is TRUE (via
`time.NewTimer`, not `time.Sleep`).

### `cycle.go` — direct clock calls (32 sites; synthesis said "8+")

`time.Now()`:
`:435` (cycle-id prefix), `:707` (grace arm), `:937` (journal open), `:945`
(lastForcedAttemptAt), `:984` (handoffInjectedAt), `:999`, `:1020`, `:1031`, `:1082`,
`:1114`, `:1150`, `:1155` (journal `UpdatedAt` stamps), `:1219`, `:1227`, `:1235`
(RecoverFromCrash stamps), `:1468` (lastIdleRestartAt), `:1494`
(maybeEmitOperatorAttached), `:1541` (clear-backstop deadline), `:1546` (deadline check).

`time.Since()`:
`:714`, `:730`, `:735`, `:739` (boot-grace math), `:794`, `:799` (auto-hold),
`:811`, `:813` (post-answer grace), `:832`, `:844` (force-retry interval), `:1304`,
`:1307` (precompact boot-grace), `:1450` (idle-restart cooldown).

`time.NewTicker()`: `:1515` (`pollForNonce`), `:1562` (`waitForNewSessionID`) — both use
`c.cfg.PollInterval` and select on `ticker.C` (real time).

Verified: far more than 8 — 20 `time.Now()`/`time.Now().UTC()`, ~11 `time.Since`, 2
`time.NewTicker`. All are the process clock; no seam.

### Other keeper files (non-test)

- `injector.go:198` — `time.NewTimer` (above).
- `gates.go:126`, `:185` — `time.Now().UTC()` (marker content stamps); `:256`
  `time.Since(parsed)` (hold TTL); `:88` `idleMtime.After(ctxMtime)` (CrispIdle compare).
- `heartbeat.go:287` — `time.Now()`.
- `tmuxresolve.go:225` — `time.Now()` (operator-active window compare).
- `watcher.go` — 28 clock call sites incl. `time.NewTicker` at `:1003` (main poll loop),
  and `time.Now()` at `:68, :1025, :1036, :1048, :1073, :1134, :1181, :1195, :1315,
  :1402, :1581, :1687`.
- **Injectable seams (the exception):** `restartnow.go:70` (`now = time.Now` from
  `cfg.Now`) and `awaitack.go:97` (`now = time.Now` from `cfg.Now`) — these two paths
  can be driven by a fake clock; the auto-cycle cannot.

---

## 4. The IO ports — concrete mechanisms

All production defaults wired in `CyclerConfig.applyDefaults` (`cycle.go:330-376`).

**(a) Detect token count** — file poll of the gauge.
`ReadGaugeFn` defaults to `ReadCtxFile` (`cycle.go:334-335`), which `os.ReadFile`s
`.harmonik/keeper/<agent>.ctx` (`gauge.go:34-62`). The file is produced by the Claude
Code statusLine hook `scripts/keeper-statusline.sh`, which reads
`.context_window.total_input_tokens` / `.context_window_size` / `.session_id` from the
hook's stdin JSON (via `jq`) and atomically writes the `.ctx` gauge. The watcher polls it
every `PollInterval` (5s). Threshold decision: `belowActThreshold` (`cycle.go:397`) using
`min(ActAbsTokens, ActPctCeil*window)` (`thresholds.go:214`, band warn=200K/act=215K/
force=240K per `thresholds.go:35-39`).

**(b) Send a message to the session** — tmux bracketed-paste + Enter.
`InjectFn` defaults to `InjectText` (`cycle.go:331-332`). Mechanism
(`injector.go:131-168`): `tmux load-buffer -b hk-keeper-inject -` (stdin=text) →
`tmux paste-buffer -b … -t <target> -d` → settle 750ms → `send-keys -t <target> Enter`
→ 2 retry Enters. Shell-out via `tmuxRunFn`=`runTmuxCombined` (`injector.go:104-114`,
`exec.CommandContext("tmux", …)`). A pre-inject `SendEscapeKey` (`injector.go:184`,
`tmux send-keys Escape`) preempts busy-pane input (`cycle.go:986-988`).

**(c) Detect handoff-file write done** — nonce file poll.
`pollForNonce` (`cycle.go:1511-1529`): `time.NewTicker(PollInterval=200ms
CyclerPollInterval)`, each tick `ReadHandoff(handoffPath)` (`os.ReadFile` of
`HANDOFF-<agent>.md`, `cycle.go:447-454`) and `strings.Contains(content, nonce)`. Timeout
`HandoffTimeout` = 300s (`thresholds.go:157`). The nonce is `nonceMarker(cycleID)` the
agent was told to echo verbatim.
```go
// cycle.go:1522-1526
case <-ticker.C:
    content, err := c.cfg.ReadHandoff(handoffPath)
    if err == nil && strings.Contains(content, nonce) {
        return true
    }
```
Fallback on nonce timeout: `handoffWrittenAndFresh` (`cycle.go:918-928`) — file exists,
non-empty, mtime ≥ `handoffInjectedAt` (via `HandoffModTimeFn`=`defaultHandoffModTime`,
`os.Stat`, `cycle.go:458-464`).

**(d) Detect model-done / final-response** — NO dedicated poll. Approximated only by the
`CrispIdle` pre-gate: `CrispIdleFn`=`CrispIdle` (`cycle.go:337-338`) stat-compares the
`.idle` marker mtime (written by `keeper-stop-hook.sh` on model stop) against `.ctx`
mtime (`gates.go:77-93`). No signal is read between confirm and `/clear`.

**(e) Send `/clear`** — same tmux `InjectFn`. `completeCycleTail` calls
`c.cfg.InjectFn(ctx, TmuxTarget, "/clear")` (`cycle.go:1111`); defensively re-injected on
each backstop retry (`cycle.go:1551`).

**(f) Detect new-session-up** — gauge poll for session_id change.
`waitForNewSessionIDWithBackstop` (`cycle.go:1540-1554`) wraps `waitForNewSessionID`
(`cycle.go:1558-1576`): `time.NewTicker(PollInterval)`, each tick `ReadGaugeFn` and return
when `cf.SessionID != "" && cf.SessionID != prevSID`. The authoritative id comes from the
`.sid` channel written by `keeper-sessionstart-hook.sh` (overlaid in `ReadCtxFile`,
`gauge.go:59-61`). Inner timeout `ClearSettle`=10s (`thresholds.go:162`); outer backstop
`ClearConfirmBackstop`=150s / `ClearConfirmRetries`=20 (`thresholds.go:174-178`). Before
each retry it re-injects `/clear` (`cycle.go:1550-1552`).

**(g) Submit brief** — same tmux `InjectFn`: `c.cfg.InjectFn(ctx, TmuxTarget,
briefRestartCmd)` (`cycle.go:1147`), command `harmonik agent brief --wake keeper-restart`
(`cycle.go:20`). Also HARMONIK_AGENT is set in the tmux session env first via
`SetTmuxEnvFn`=`SetTmuxEnv` (`tmux setenv`, `cycle.go:1106`, `injector.go:224-233`).

Related tmux port: `OperatorAttached` (`tmuxresolve.go:213`) via
`tmux list-clients -t <target> -F '#{client_activity}'` (Gate 7). `capture-pane` is used
by the ACK handshake (`awaitack.go:208-223`, `tmux capture-pane -p -t <target> -S -N`),
not by the auto-cycle.

---

## 5. The `clear_unconfirmed` failure surface

Emitter: `emitClearUnconfirmed` (`cycle.go:1613-1622`), event
`session_keeper_clear_unconfirmed`. Single call site:
```go
// cycle.go:1128-1131 (completeCycleTail)
newSID := c.waitForNewSessionIDWithBackstop(ctx, cf.SessionID)
if newSID == "" {
    c.emitClearUnconfirmed(ctx, cycleID, cf.SessionID)
}
```
**Trigger condition:** after `/clear` is injected, the keeper polls the gauge for a
session_id that differs from the pre-clear one. `waitForNewSessionIDWithBackstop` returns
`""` only when the ENTIRE backstop is exhausted without ever seeing a new session_id —
i.e. `ClearConfirmRetries` (20) attempts OR the `ClearConfirmBackstop` (150s) wall-clock
deadline elapsed, whichever first (`cycle.go:1546`), with each inner attempt itself
bounded by `ClearSettle` (10s) (`cycle.go:1559`). In plain terms: **`/clear` was sent but
no new session ever showed up in the gauge within ~150s.** The cycle then proceeds to fire
the brief anyway as a last resort (`cycle.go:1146-1147`) and still records
`cycle_complete` — so `clear_unconfirmed` is a *warning that the brief may have been
submitted into a still-uncleared context*, not an abort. `.managed` is set to `""` in this
case (`cycle.go:1138`, `newSID==""`), clearing the stale binding. The 347 baseline
occurrences all originate from this one site; root causes noted in-code are slow
statusline repaint on loaded machines and slow/busy-pane handoff writes
(`thresholds.go:158-173`, hk-4xni9 K3 / hk-vdqe2).

Distinct from `cycle_aborted` (`cycle.go:1034`), which fires earlier when the *handoff
nonce* never confirmed AND no fresh handoff was written — that path never issues `/clear`.

---

## 6. Current tests — coverage and real-vs-fake

`internal/keeper/` has ~55 test files (`*_test.go`), split into unit tests (fakes) and
`*_integration_test.go` (real tmux / real filesystem).

**Predominantly fakes / injectable seams.** The cycle core is built to be driven without
tmux or a real clock: `CyclerConfig` exposes function fields
(`InjectFn, ReadGaugeFn, CrispIdleFn, HoldingDispatchFn, WriteJournalFn, ReadJournalFn,
SetManagedSessionFn, SetTmuxEnvFn, OperatorAttachedFn, SleepingCheckFn, HandoffModTimeFn,
ReadHandoff, TruncateHandoffFn, ForceRestartFn, CycleIDGen, SleepingCheckFn`, defaults at
`cycle.go:330-376`). 28 test files set `InjectFn` to a fake recorder rather than shelling
to tmux. Events are asserted via `RecordingEmitter` (`watcher.go:102-134`), which captures
`EmitWithRunID` calls in memory instead of writing `events.jsonl`. The injector's timing
constants are `var`s (`submitSettle`, `submitRetryDelay`, `injector.go:82,93`) so tests
zero them to skip real sleeps; `tmuxRunFn` is a swappable package var (`injector.go:104`).

**What exec's real code:**
- Journal read/write (`writeJournalFile`/`defaultReadJournal`) run against real temp dirs
  in most cycle tests (only some override `WriteJournalFn`).
- Gate helpers `CrispIdle`, `HoldingDispatch`, `IsSleeping`, `IsHeld`, `HasPrecompactTrigger`
  are tested against real marker files on a temp `.harmonik/keeper/` (`gates_test.go`).
- Threshold math (`thresholds_test.go`) pins the band defaults and exercises
  `minAbsOrPctCeil`/`EffectiveBandTokens` directly (no fakes).
- Gauge parse (`ReadCtxFile`) tested against real files.
- **Real tmux** in `*_integration_test.go` (gated, e.g. `cycle_twin_e2e_integration_test.go`,
  `injector_sequence_hkzole_test.go`, `cycle_operator_attached_integration_test.go`,
  `restartnow_smoke_integration_test.go`, `actionable_warn_self_service_zj1y_integration_test.go`):
  these spin a real tmux session and drive `InjectText`/`send-keys` for real.
- Conformance corpus: `conformance_keeper_test.go:32` (`TestKeeperConformance`) and
  `conformance_keeper_integration_test.go:37` (`TestKeeperConformanceCorpus_Integration`).
- Reactive scenario harness: `cycle_reactive_harness_test.go`,
  `cycle_scenario_reactive_test.go`, `cycle_scenario_reactive_wave2_test.go` — script a
  sequence of gauge states and assert the resulting inject/emit sequence against a fake
  pane.

**Not exercised end-to-end without real tmux:** the true `/session-handoff → model writes
file → /clear → new Claude process → brief` loop across a real Claude Code process. The
"model writes the handoff" is always simulated (a test writes the nonce into the HANDOFF
file). The nonce-confirm timing, `/clear` re-mint of the session_id, and new-session
pickup are all faked by the harness writing the gauge/`.sid`; no test observes a real
Claude process lifecycle. Both entry points that DO have a `now` clock seam
(`restartnow.go`, `awaitack.go`) have fake-clock tests; the auto-cycle's direct
`time.Now()`/`NewTicker` calls (§3) are exercised only with real (shortened) timeouts.
