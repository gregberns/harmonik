# Keeper substrate touchpoint inventory

Every place the keeper (and its immediate launch/respawn surface) touches tmux, what it runs,
why it runs it, and the herdr method that replaces it. Cite symbols, not line numbers.

Legend for "why": **inject** (write into the agent pane), **probe** (read pane / process
state), **resolve** (find the pane), **operator** (operator-attach gate), **respawn**
(kill + relaunch), **launch** (create the keeper's own window — outside the keeper process).

## Inside `internal/keeper` (the keeper process itself)

| # | Symbol | File | tmux command | Why | herdr mapping |
|---|--------|------|--------------|-----|---------------|
| 1 | `tmuxSessionLive` | `tmuxresolve.go` | `tmux has-session -t =<name>` | resolve — does the conventional session exist | `pane.list` / `session.snapshot` (scan for a matching pane) |
| 2 | `ResolveTmuxTarget`, `HarmonikSessionName`, `HarmonikCrewSessionName`, `SplitTmuxTarget` | `tmuxresolve.go` | none (derives `harmonik-<hash12>-[crew-]<agent>:agent` and probes via #1) | resolve — canonical target from projectDir+agent; explicit `--tmux` passes through | no name convention exists; discover by pane metadata (`pane.report_metadata` written at spawn) or by a recorded pane-id file. See README §4.4 |
| 3 | `OperatorAttached`, `operatorActiveSince` | `tmuxresolve.go` | `tmux list-clients -t <t> -F '#{client_activity}'` | operator — suppress the reset cycle while a human is typing (hk-0t5s: keystroke recency, not bare attach) | **no confirmed equivalent** — OPEN QUESTION (README §5.5) |
| 4 | `InjectText` / `injectTextClocked` / `tmuxRunFn` / `sendEnter` | `injector.go` | `tmux load-buffer` → `paste-buffer -d` → settle → `send-keys Enter` (+2 retries) | inject — handoff / `/clear` / resume / warn texts / ACK lines, bracketed paste + submit-race fix (hk-89g) | `pane.send_text`, then `pane.send_keys` Enter (keep the settle + bounded Enter retry) |
| 5 | `SendEscapeKey` | `injector.go` | `tmux send-keys Escape` | inject — clear partial input before forced handoff (hk-qoz) | `pane.send_keys` Escape |
| 6 | `SetTmuxEnv` | `injector.go` | `tmux setenv -t <t> KEY VALUE` | inject — session env (`HARMONIK_AGENT`) inherited by post-`/clear` processes; advisory (`shell.go` treats failure as non-fatal) | unknown whether herdr has a session env table — OPEN QUESTION; fallback: no-op + env set at `agent.start` by the respawn command (README §5.6) |
| 7 | `CaptureTmuxPane` | `awaitack.go` | `tmux capture-pane -p -S -200` | probe — poll pane scrollback for `AckMatchToken` (restart-now / ping handshake) | `pane.read` (recent); later `pane.wait_for_output` replaces the poll loop entirely |
| 8 | `IsPaneIdle` | `respawn.go` | `tmux display-message -p '#{pane_current_command}'` vs `shellCmds` | probe — agent exited (shell at prompt); gates `maybeRespawn` and the heartbeat (`heartbeat.go`) | needs pane foreground-process info; agent status alone is NOT it (`unknown` ≠ exited) — OPEN QUESTION (README §5.4) |
| 9 | `IsPaneAlive` | `respawn.go` | same probe, inverted | probe — agent hung mid-turn (live-pane recovery gate, hk-75mr); fail-closed on error | same as #8 |
| 10 | `NewLiveRecoverViaRespawn` | `respawn.go` | `sh -c <respawnCmd>` (opaque) | respawn — force-restart escalation; the command itself is tmux-shaped but supplied by the launcher | opaque seam survives unchanged; the launcher supplies a herdr-aware command (README §4.5) |
| 11 | `maybeRespawn` (`WatcherConfig.RespawnCmd`) | `watcher.go` | `sh -c <RespawnCmd>` (opaque) | respawn — dead-pane self-heal after `RespawnGrace` + pane-idle | same as #10 |
| 12 | `WatcherConfig.ResolveTmuxTargetFn` default | `watcher.go` | re-runs `ResolveTmuxTarget` | resolve — rebind the target when the stored one fails the pane-alive probe (hk-9cqtm) | re-run herdr discovery (README §4.4); load-bearing because respawn mints a NEW pane id |
| 13 | `WatcherConfig` inject seams: `InjectFn`, `SelfHintInjectFn`, `MessageInjectFn`, `DashboardNagInjectFn` (defaults → `InjectText`) | `watcher.go`, `dashboardnag.go` | via #4 | inject — warn / hint / nag delivery | via the substrate port; the `*Fn` test seams stay |
| 14 | `PaneWriter` default `configPaneWriter` | `cycle_config_adapters.go` | via #4/#6 | inject — the cycle's write port (already an interface; `SendEscape` is layered on in `cmd/harmonik/keeper_cmd.go` `keeperPaneWithEscape`) | same port, satisfied by the substrate |
| 15 | `OperatorPresenceProbe` default (`CycleDepsFromConfig` wires `OperatorAttached`) | `cycle_deps.go` | via #3 | operator — `GateSnapshot.OperatorAttached` | via the substrate port |

## NOT tmux — the context-fill signal and turn gates (unchanged under herdr)

| Symbol | File | Source | Why it matters here |
|--------|------|--------|---------------------|
| `ReadCtxFile` / `CtxFile` | `gauge.go` | `.harmonik/keeper/<agent>.ctx`, written by the Claude Code statusLine hook (`scripts/keeper-statusline.sh`) | The ACTUAL trigger. herdr has no context-fill state; this file path is substrate-independent and stays exactly as-is |
| `recentTranscriptTurn`, `isRealTranscriptTurn` | `tmuxresolve.go` | transcript `.jsonl` tail read | Gate 5d/5e operator-turn freshness; substrate-independent; the fallback when operator-attach has no herdr equivalent |
| `delivery_decision_0nlqs.go` `commsSendArgs` | — | execs the `harmonik` binary (comms), not tmux | out of scope |
| presence reaper (`ReapOrphanedDecisions` on the watch tick) | `watcher.go` → `internal/presence` | file-backed | out of scope |

## Outside `internal/keeper` (launch + respawn surface the plan must touch)

| Symbol | File | tmux command | Why | herdr change |
|--------|------|--------------|-----|--------------|
| `buildCaptainRespawnWindowCmd` | `cmd/harmonik/captain_respawn.go` | `tmux respawn-window -k -e HARMONIK_AGENT=… claude --resume <sid>` | respawn — what `--respawn-cmd` actually runs for the captain | herdr sibling path: `pane.split` new shell → `agent.start` with `claude --resume` → `pane.close` old (new-before-old; README §5.1) |
| `buildCaptainPanePIDCmd` / `refreshCaptainPID` | `cmd/harmonik/captain_respawn.go` | `tmux display-message -p '#{pane_pid}'` | respawn — refresh `captain.pid` so the orphan sweep does not reap | `pane.list` / pane info must expose the pane PID — verify in the schema |
| `captainRespawnCmdString` | `cmd/harmonik/captain_respawn.go` | builds the `--respawn-cmd` string | respawn | emit `--substrate herdr --pane <id>` form |
| `KeeperWindowArgv` / `KeeperWindowOpts` | `internal/agentlaunch/keeperargv.go` | builds `harmonik keeper --tmux <session>:agent …` | launch — the argv every keeper window runs | grows a substrate field; emits `--substrate herdr --pane <id>` |
| `SpawnKeeperWindow` / `WindowSpawner` | `internal/agentlaunch/keeperwindow.go` | `ltmux.NewWindowIn` (a `tmux new-window`) | launch — create the sibling keeper window | needs a herdr `WindowSpawner` implementation (`pane.split` running the keeper argv); the seam is already an interface but its param types are `ltmux` — see README §9 |
| `tmuxPaneExists` (doctor/enable) | `cmd/harmonik/keeper_enable_doctor_cmd.go` | pane-existence probe | doctor — `keeper enable` / `keeper doctor` verify the target | route through the same substrate port |
| `keeper.ResolveTmuxTarget` call in `runKeeperSubcommand` | `cmd/harmonik/keeper_cmd.go` | via #1/#2 | resolve at boot | substrate-selected resolution |

## Test files that encode the tmux contract (must be parameterized or given herdr twins)

- `cycle_twin_e2e_integration_test.go` — real tmux session, real paste path, `has-session`/`kill-session` setup. The template for a herdr twin.
- `tmuxresolve_integration_test.go` — pins the `has-session` choice (documents that `display-message` exits 0 on a missing session — a real footgun the conformance suite must carry forward).
- `cycle_operator_attached_integration_test.go`, `warn_operator_attached_hk1ryc_test.go` — operator-attach contract.
- `scenario_operator_collision_integration_qji8g_test.go` — inject vs. operator-typing collision (`capture-pane`, partial `send-keys`).
- `restartnow_smoke_integration_test.go`, `scenario_restartnow_integration_qji8g_test.go` — restart-now inject + ACK.
- `actionable_warn_self_service_zj1y_integration_test.go` — warn-text inject.
- `watcher_b3_restall_hk9cqtm_*`, `cycle_twin_sid_rebind_hk5wadr_integration_test.go` — target rebind contract (directly relevant to herdr pane-id churn).
- `internal/keepertest/` (`scenario.go` `RecordingPorts`, L0–L3 suites) — already substrate-free; unchanged.
