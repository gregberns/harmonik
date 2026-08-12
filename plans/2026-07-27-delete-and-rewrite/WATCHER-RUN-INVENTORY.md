# Watcher.Run job inventory

Date: 2026-08-01

This inventory describes the current outer watcher. It does not choose an
extraction order. `internal/keeper/watcher_run_characterization_test.go`
records these joins as direct `Watcher.Run` calls. Existing behavioral tests
remain the behavior evidence for each job.

| Job | Current seam | Existing behavior evidence |
|---|---|---|
| Boot gauge check | `gaugeUnavailable`, `maybeEmitNoGauge` | `TestWatcher_EmitsNoGaugeWhenFileAbsent` |
| Warn text reload | `seedConfigMtime`, `maybeReloadWarnMessages` | `TestMaybeReloadWarnMessages_MtimeGatedAndScoped_223zs` |
| Poll and cancellation | `Clock.NewTicker` and `ctx.Done` | `driveWatcherFakeClockFrom` |
| Cycle suppression | `Cycler.InCycle` | New structural characterization test |
| Decision reaping | `maybeReapOrphanedDecisions` | `TestScenario_DecisionsOrphanReap_S7` |
| Dashboard nag | `maybeNagDashboardStale` | `TestMaybeNagDashboardStale_ApproachingNags` |
| Gauge read and no-gauge state | `ReadCtxFile`, `maybeEmitNoGauge` | `TestWatcher_NoWarnWhenGaugeIsStale` |
| Live-pane heartbeat | `maybeHeartbeat` | `TestHeartbeat_KeepsLiveGaugeFresh` |
| Stale-gauge recovery | `maybeRespawn`, `maybeLivePaneRecover` | `TestWatcher_LivePaneRecover_FiresWhenStalePaneAliveValidSid` |
| Session binding | managed and live SID read and write functions | `TestWatcher_AcceptsManagedSession` |
| Foreign-session backstops | `emitBlind`, `emitHardCeiling` | `TestBlindKeeperAlarm_FiresAfter5Min`, `TestHardCeiling_FiresAbove280K_DespiteForeignSession` |
| Normal restart cycle | `Cycler.MaybeRun` | `TestCycler_HappyPath` |
| Precompact restart cycle | `HasPrecompactTrigger`, `Cycler.RunForPrecompact` | `TestRunForPrecompact_HappyPath` |
| Idle restart cycle | `Cycler.RunForIdle` | `TestCycler_RunForIdle_EmitsEventBelowThreshold` |
| Warn and self hint | `emitWarn`, `SelfHintInjectFn` | `TestWatcher_SelfHint_InjectedOncePerSession` |
| Leader warn delivery | `maybeDeliverLeaderWarn` | `TestMaybeDeliverLeaderWarn_Routing` |

The three cleanest existing method seams are decision reaping, dashboard
nagging, and live-pane recovery. This is only a recommendation for the
operator's open decision. No job moved in this pass.
