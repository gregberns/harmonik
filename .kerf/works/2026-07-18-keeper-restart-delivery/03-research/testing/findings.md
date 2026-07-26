# R-C — TESTING: integration / end-to-end / twin-scenario coverage for the keeper-restart redesign

Cluster R-C (K6, G6/SC-7). Read-only code grounding. Every claim cited to `file:line`.

## Headline finding (read first)

**There are two "twins", and the keeper's failures live in the one the parity audit does NOT cover.**

- `cmd/harmonik-twin-claude` — the **wire-protocol scenario-harness twin** the parity audit
  (`docs/twin-parity-audit-2026-05-14.md`) and `specs/scenario-harness.md` are about. **No tmux
  pane**; NDJSON to a UDS/stdout (`cmd/harmonik-twin-claude/main.go:215-243`). Cannot observe or
  reproduce anything at the pane/keystroke/statusline layer.
- `cmd/harmonik-twin-session` — the **session twin** the keeper's own integration tests build and
  run **in a real tmux pane** (`cmd/harmonik-twin-session/main.go`;
  `internal/keeper/cycle_twin_e2e_integration_test.go:120-127` builds it, `:267` `tmux new-session`,
  `:410` drives it with REAL `keeper.InjectText`). Emits statusLine JSON through the real
  `scripts/keeper-statusline.sh` pipeline, touches the real `<agent>.idle` marker, writes a real
  `HANDOFF-<agent>.md` (`cycle_twin_e2e_integration_test.go:16-31, :433`).

Four of five R-C target failures are pane/timing/handoff/operator-typing failures. Their vehicle is
**`harmonik-twin-session` + real tmux (integration-tagged Go tests)**, NOT `scenarios/*.yaml` +
`harmonik-twin-claude`. The problem-space "the twin is the scenario-test vehicle"
(`01-problem-space.md:118-121`) points at `harmonik-twin-claude`; that is correct only for
*wire-observable* behavior (the comms-unreachable fallback, if the keeper emits an event). The rest
ride the session-twin integration tier. Load-bearing design implication for the whole cluster.

---

## Q1 — The twin (`cmd/harmonik-twin-claude`)

### Finding
**What/launch.** Stand-in for a Claude Code session, subprocess-launched by the daemon in place of
real Claude (`main.go:1-3`); no cognition, deterministic (`scenarios.go:29-31`). Flags:
`--socket-path` (UDS dial-back, `main.go:97`), `--script-path` (YAML, `:112`), `--scenario`
(canned, `:120`), `--worktree-path` (reads `.claude/settings.json`, `:129, :202-210`),
`--replay-path` (`:137`), `--launch-spec`, `--preserve-timing`, `--version`. Priority replay >
scenario > script (`:167-194`).

**Script driving.** YAML → `ScriptFile` (`scriptdriver.go:182-208`): `heartbeat_mode`,
`startup_delay_ms`, `messages`, `exit_with_error`; each `ScriptMessage` (`:145-176`) has `type`,
`payload`, `relative_timestamp_ms`, `delay_ms`. External via `--script-path` (`loadScriptFile`
`:214-242`; convention `<fixture-root>/<scenario>/twin-scripts/<role>.yaml` `:17-19`) or embedded
`scenarios.go:45-72`. Built-ins: `single-happy-path`, `review-loop-3iter`, `rate-limit`,
`dial-failed`, `daemon-not-ready-retry`, `commit-on-cue-startup-delay`, `budget-exhausted`,
`handler-fatal`, `silent-hang`, `partial-pre-exec`, `heartbeat-then-hold` (`:46-68`).

**Wire events.** NDJSON to `--socket-path` else stdout (`main.go:215-243`; framing `wire.go:22-26`).
Typed emitters (`wire.go`): `handler_capabilities` (`:95-107`, first), `session_log_location`
(`:118-142`), `skills_provisioned` (`:160-173`), `agent_ready` (`:183-196`), `agent_started`
(`:206-223`), `agent_heartbeat` (`:262-273`), `agent_output_chunk` (`:285-302`),
`agent_rate_limited` (`:315-332`), `agent_rate_limit_cleared` (`:344-357`), `outcome_emitted`
(`:369-384`), `agent_completed` (`:395-412`), `agent_failed` (`:423-446`). Twin extensions:
`twin_settings_loaded` (`:464-482`), `twin_hook_called` (`:498-511`), `twin_committed` (`:530-545`),
`twin_error` (`:557-566`) — documented in `scenario-harness.md` §6.4. Script mode emits verbatim
from YAML `type` (`emitScriptMessage` `scriptdriver.go:652-668`). **No `checkpoint`, no
`agent_message`/comms event.** Reads control msgs (`version_selected`/`cancel`/`shutdown`/
`rotate_account`) via `wireReader` (`wire.go:582-620`).

**Can / cannot.** CANNOT: own a tmux pane; receive tmux `send-keys` (input is UDS control only,
`wire.go:568-620`); parity Fix 5/Fix 8 are real-claude-only (`docs/twin-parity-audit-2026-05-14.md:75-77,§5`).
CAN write a real commit (`runCommitOnCue`, `scriptdriver.go:474-558`); read settings.json + call the
Stop hook (`callStopHook` `:439-458`).

**Slow-handoff?** Timing knobs — `startup_delay_ms` (`scriptdriver.go:196`, `main.go:269-280`),
per-msg `relative_timestamp_ms` (`:347-353`), `delay_ms` both modes (`:368-374`), `hold` step
(`runHold` `:325,625-645`). But these gate *wire-event emission*, not writing a handoff *file*; the
only file it writes is the commit-on-cue sentinel (`:493`). So it can model "slow to emit
`outcome_emitted`" but not natively "slow to write `HANDOFF.md`". **Type into own pane?** No pane —
`agent_output_chunk` is a wire event, not keystrokes. **Go silent?** Yes: `silent-hang`
(`scenarios.go:826-873`), `partial-pre-exec` (`:900-938`), `heartbeat-then-hold` (`:1041-1108`).

### Implication
`harmonik-twin-claude` fits only a wire-observable keeper failure — the comms-unreachable fallback
(c) IF K1 emits a bus event. For (a) collision, (b) handoff-file timing, (d) operator-present, (e)
FORCE-ACT cut it is the wrong tool (needs real pane/handoff-file/statusline). Those map to
`harmonik-twin-session`. Do not scope twin-claude YAML scenarios for them.

---

## Q2 — Existing keeper tests (inventory)

### Finding
**Unit (no tag, offline):** thresholds `thresholds_test.go`; cycle call-count fakes `cycle_test.go`
(121KB, spy InjectFn records `/clear`, flips gauge SID on fixed call-count —
`cycle_twin_e2e_integration_test.go:11-13`), `cycle_idle_test.go`, `cycle_convo_aware_test.go`,
`precompact_test.go`, `step_test.go`, `keeper_hold_test.go`. **Reactive harness**
`cycle_reactive_harness_test.go`: `reactiveSession` fake (`:46`, `newReactiveSession` `:101`) mutates
gauge+handoff in reaction to the inject; entry points `newReactiveCycler` (`:287`),
`newReactiveCyclerWithBackstop` (`:305`), `rs.withClearDelay` (`:269`), fn-seams
inject/readGauge/readHandoff/handoffModTime/truncate. **No FakeClock** — shrinks real durations
(`PollInterval 5ms` `:323`, tiny handoffTimeout `:321-322`, slow-`/clear` via real `time.Sleep`
`:147`). Built on by `cycle_scenario_reactive_test.go`, `cycle_scenario_reactive_wave2_test.go`
(header `:1-24` — `ClearSettleUnconfirmed`, `ForcedClearAboveHardThreshold`, `AntiLoopReArm`).
Operator: `tmuxresolve_operator_test.go` (pure `TestOperatorActiveSince` table),
`cycle_operator_attached_test.go` (fake `OperatorAttachedFn`). Backstops: `backstop_test.go`.
Injector: `injector_test.go`, `injector_sequence_hkzole_test.go` (swap `tmuxRunFn` seam
`injector.go:106`). Restart: `restartnow_test.go`.

**Integration (`//go:build integration`, some `&& darwin`, real tmux):**
`cycle_twin_e2e_integration_test.go` (TRUE send-keys E2E: builds `harmonik-twin-session` `:120-127`,
real pane `:267`, real statusline, real `keeper.InjectText` `:410`, asserts nonce in
`HANDOFF-<agent>.md` `:431-434`, handoff<clear<brief order `:739-758`; only wall-clock+LLM faked
`:31`); `cycle_twin_1m_gauge_integration_test.go`; `cycle_twin_sid_rebind_hk5wadr_integration_test.go`;
`cycle_twin_gauge_liveness_integration_test.go`; `cycle_operator_attached_integration_test.go`
(`integration && darwin` — real tmux server, real client via allocated pty `:1-22`, drives real
`OperatorAttached`); `tmuxresolve_integration_test.go`; `restartnow_smoke_integration_test.go`;
`actionable_warn_self_service_zj1y_integration_test.go` (LOW warn from config.yaml, real
`Watcher.Run`, asserts verbatim `harmonik keeper restart-now --agent <name>` + one `/clear`);
`conformance_keeper_integration_test.go` (`make test-keeper-conformance-full`);
`watcher_b3_restall_hk9cqtm_integration_test.go`, `watcher_real_env_integration_test.go`,
`watcher_live_pane_recover_integration_test.go`. Only `//go:build scenario` keeper file is
`scenario_decisions_orphan_reap_s7_hk061_test.go` (orphan-reap, not restart-timing).

### Implication
Keeper already has a mature real-tmux integration tier + causal offline reactive harness. K6 should
extend these, not invent a harness. `//go:build scenario` in this repo = daemon/orchestrator harness
(`test/scenario`, `internal/daemon`), which the keeper does not join for restart timing.

---

## Q3 — Scripting the five target failures

### (a) Operator-typing collision
**Finding.** Surface = `injectTextClocked` (`injector.go:144-184`): `load-buffer`→`paste-buffer`
(`:154-158`)→**750ms settle** (`submitSettle` `:84`)→`Enter` (`sendEnter` `:169-193`)→2× retry-Enter
(`submitRetries=2` `:90`; `submitRetryDelay=400ms` `:95`). The operator-attached guard suppresses the
actionable restart text but NOT the lighter warn injection, so the warn paste+Enter still lands on
the operator's line (`01-problem-space.md:14-21`).
**Level: integration (real tmux) + unit adjunct.** Pane-collision is irreducibly pane-level (wire
twin blind, parity §5). Copy the pty-attach harness from `cycle_operator_attached_integration_test.go`;
put partial input on the pane (`tmux send-keys` no Enter), trigger a warn-inject, assert the
operator line isn't submitted/corrupted (fail-before). Unit adjunct: swap `tmuxRunFn`
(`injector.go:106`, like `injector_sequence_hkzole_test.go`) to assert zero paste/Enter to the pane
when operator-present and comms taken instead.

### (b) Late-handoff-after-300s
**Finding.** `DefaultHandoffTimeout = 300*time.Second` (`thresholds.go:157`), field
`CyclerConfig.HandoffTimeout` (`cycle.go:68`, default `:340-342`). Armed after `/session-handoff`
via `ActArmTimer{TimerHandoffTimeout}` (`step.go:838-843`) → deadline `executeArmTimer`
(`shell.go:161`, `c.cfg.Clock.Now()`). Watch loop `Cycler.drive` (`shell.go:225-271`) →
`pollAwaitingHandoff` (`shell.go:310-323`): timeout deadline first (`:313-317`), else nonce marker
(`:319-322`). Timeout → `stepAbort`, emits `cycle_aborted` reason `"handoff_timeout"`
(`step.go:329-349`, abort ~`:858`). Injectable clock `CyclerConfig.Clock substrate.ClockPort`
(`cycle.go:97`, default `SystemClock{}` `:358-360`); `substrate.FakeClock`
(`internal/substrate/fakeclock.go:18`) advances via `Advance(d)` (`:114`). Reactive harness does NOT
wire FakeClock (shrinks durations).
**Level: twin/harness unit (abort) + integration (T+301 restart, SC-4).** (1) fail-before: wire
`Clock=substrate.NewFakeClock(t0)`, `HandoffTimeout=300s`, drive to `PhaseAwaitingHandoff` with
reactive `writeNonce=false` (`cycle_reactive_harness_test.go:62,128`), `Advance(300s+)`, assert
`cycle_aborted{reason=handoff_timeout}` — recommended pattern (cleaner than harness's shrink-time).
(2) pass-after (SC-4, handoff at T+301 still restarts): copy `restartnow_test.go` /
`restartnow_smoke_integration_test.go` (`restartnow.go`, hk-5da7, `01-problem-space.md:114-117`) —
nonce-carrying self-restart completes a clean clear after the watch window closed.

### (c) Comms-unreachable fallback
**Finding.** Keeper touches NO comms code today — only comments in `tmuxresolve.go:44,115`,
`awaitack.go:28-29`; confirmed `02-components.md:40-47` ("new *caller*"). Reachability signal =
`harmonik comms who` (`cmd/harmonik/comms.go:1059-1189`, reads presence registry of online agents).
Terminal fallback = `commsInjectTmuxPane` (`comms.go:451`, bracketed-paste into a pane). Reusable
test substrate = `cmd/harmonik/comms_recv_follow_hk5xuvc_test.go`: stands up daemon+UDS in-process,
seeds `agent_message` into `events.jsonl` (`followTestSeedMessage` `:33-60`), runs
`runCommsRecvFollowIO` on the socket (`:160,307,395`).
**Level: integration (comms/daemon).** Seed presence registry so target is absent (or `comms who`
empty), invoke K1 reachability check, assert delivery resolves to **terminal-fallback, never a
silent no-op** (SC-2); positive counterpart seeds present → comms delivery. K1 is net-new, so the
test targets the new function + fallback branch; `comms_recv_follow` harness + `comms who` are the
substrate.

### (d) Operator-present misread
**Finding.** `OperatorAttached(target)` (`tmuxresolve.go:214-227`) shells `tmux list-clients -F
'#{client_activity}'` (`:220`) → pure `operatorActiveSince(out,now,window)` (`:233-248`);
`operatorActiveWindow = 5*time.Minute` (`:186`). Misread documented in code: passive remote/iOS
`claude --remote-control` attach keeps the client attached but keystrokes bypass tmux so
`client_activity` freezes → 5-min window reads absent (`:199-205`, hk-0t5s, ~2265 false
suppressions). No injectable tmux-client interface; unit seam is the pure `operatorActiveSince`
(tests fake the list-clients string). `TestOperatorActiveSince` (`tmuxresolve_operator_test.go:16-95`)
already tables it — "single idle attached client" (`:36-38`, 16d stale, `want:false`) IS the
misread.
**Level: unit (primary) + integration adjunct.** Feed a fake `client_activity` stale-but-present,
assert current 5-min window returns absent (fail-before) and the augmented G5 signal returns present
(pass-after); pure-function unit copied from `tmuxresolve_operator_test.go`. Integration adjunct:
real remote-style attach via `cycle_operator_attached_integration_test.go` /
`tmuxresolve_integration_test.go`.

### (e) FORCE-ACT still cuts a never-idle session
**Finding.** Two backstops. Cycle-side FORCE-ACT `aboveForceThreshold` (`cycle.go:53-60,319-327,
478-486`) bypasses CrispIdle, fires unconditionally — already tested by
`ForcedClearAboveHardThreshold` (`cycle_scenario_reactive_wave2_test.go` header `:1-24`: CrispIdle
false, cycle fires, Escape before `/session-handoff`, `/clear` still nonce-gated). Watcher
hard-ceiling `HardCeilingAbsTokens = 280_000` (`thresholds.go:95,98`) SID-independent failsafe in
`backstop_test.go`: `TestHardCeiling_FiresAbove280K_DespiteForeignSession` (`:330`) — gauge above
ceiling via `foreignSessionConfig(t,dir,agent,290_000)` (`:342`), `HardCeilingMode=Restart` (`:343`),
`restartSpy` (`:344,308-324`), `runWatcherFor` (`:347`), assert `spy.count()>=1` (`:350-352`) +
`session_keeper_hard_ceiling` event (`:355-378`); negative control `does_not_fire_at_270K` (`:381`).
**Level: existing-level (offline reactive + watcher backstop).** Extend
`ForcedClearAboveHardThreshold` to assert the new K2 deferral does NOT weaken the backstop — a
never-idle session carrying the defer framing still gets cut at the force ceiling.
`backstop_test.go` hard-ceiling = assertion template; wave2 force scenario = cycle-side. No new
infra.

---

## Q4 — The scenario-harness spec

### Finding
Normatively: scenarios are YAML under `scenarios/` (SH-001/002 `:152-160`); four assertion kinds
`event_present`/`event_absent`/`workspace_state`/`exit_code` (SH-021 `:311-321`); twin substitution
is a handler-config override, never a runtime branch (SH-008 `:202-207`; SH-INV-001 `:465-478`);
harness drives the production daemon entry-point (SH-017 `:282-287`); three-scenario conformance
floor §10.1 (`:893-899`: `smoke/twin-launch-and-ready.yaml`, `smoke/checkpoint-and-merge.yaml`,
`regression/twin-failure-classification.yaml`). Twin surface = `harmonik-twin-claude` (§6.4 wire msgs
`:649-711`). On-disk `scenarios/` = `_workflows/`, `core-loop-proof/`, `regression/`, `smoke/`.
Adding to §10.1 = foundation amendment (`:899`; §9.1 → `architecture.md §4.6` `:848`). Test-surface
obligations named in prose §10.2 (`:903-918`).

**Where K6 tests go.** Wrong home for four of five failures — SH's loop is
daemon/orchestrator-against-wire-twin (SH-017/008) with no tmux-pane/statusline/handoff-file surface,
exactly the keeper's layer (`harmonik-twin-claude` has no pane, Q1). `scenario-harness.md`'s K6
change (`02-components.md:49-57`) should: (i) add at most ONE wire-observable keeper scenario IF the
comms-unreachable fallback (c) emits an assertable bus event (candidate `scenarios/regression/*.yaml`);
(ii) record normatively that pane/timing/handoff/operator-typing coverage (a,b,d,e) is delivered via
the keeper's **session-twin integration tier** (`harmonik-twin-session` + real tmux; the
`cycle_twin_*_integration` + `backstop_test.go` + reactive-harness patterns), outside SH's contract
— mirroring the spec's own real-tmux carve-out; (iii) any §10.1 floor addition is a foundation
amendment.

### Implication
The operator's "twin-scenario" mandate (`01-problem-space.md:63-64`) is met by the session-twin
integration harness, not the SH YAML harness, for most failures. SC-7 map: (a) integration/real-tmux
(session twin + pty-attach); (b) twin/harness unit (FakeClock) + integration for T+301 restart; (c)
integration (comms/daemon presence registry) + optionally one SH wire scenario; (d) unit
(`operatorActiveSince` table) + integration adjunct; (e) existing-level offline reactive + watcher
hard-ceiling. `scenario-harness.md`'s edit is a thin K6 pointer + optionally one wire scenario, not
the primary carrier.
