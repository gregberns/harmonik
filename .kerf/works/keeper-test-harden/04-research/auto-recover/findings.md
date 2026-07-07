# Research — live-pane auto-recover (B3)

`watch` re-stalls every ~5–25 min with no self-healing path. Root cause is two-fold: (1) the tmux-target
mangling class from G2/hk-5266t, and (2) no auto-recover hook wired for the `watch` session specifically.

The gated live-pane `ForceRestart` via `--respawn-cmd` already exists (`respawn.go`,
`watcher_live_pane_recover_test.go`, hk-75mr): stale-gauge + pane-alive + not-operator-attached + cooldown
+ valid-sid → fire the respawn command. What's unproven is that this path is wired for the `watch`
session and actually ends the re-stall class end-to-end.

The dominant failure mass historically was `no_gauge:stale` flood (~2699 events) — already fixed by the
heartbeat/suppression seam (`watcher.go:SuppressNoGauge`, hk-F21); a regression here would re-drown real
alerts, so the new B3 test must assert the flood-suppression still holds alongside the new auto-recover
firing exactly once.

Test seam: L-fake-tmux for the gate logic (`IsPaneAlive`/`IsPaneIdle` are injectable fns); L-twin for the
full "watch re-stalls, keeper auto-heals without a human" loop, with a twin scripted to wedge (stop
emitting gauge) on cue.
