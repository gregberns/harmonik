# Spec — keeper-test-harden

## B4 fix: restart-now tmux-target resolution

- `ResolveTmuxTarget` (or the caller in `cmd/harmonik/keeper_restart_now_*.go`) must correctly resolve
  the pane for a healthy, pane-bound watcher against real live-session naming conventions, including the
  `:agent` suffix form and the `:a`-style mangled-target class (hk-5266t).
- `RunOnDemand` (`restartnow.go:83-86`) must not abort with `no_tmux_target` when a pane is genuinely
  live and bound — either by re-attempting resolution before the abort check, or by widening the
  resolution logic so it no longer produces a false-empty for this class.
- Regression test: a live-named-tmux (or `tmuxRunFn`-faked) scenario proves resolution succeeds and
  `RunOnDemand` proceeds through ACK→`/clear`→resume.

## B3 fix: watch auto-recover

- Wire the existing gated `ForceRestart` (`respawn.go`, `--respawn-cmd`) into the `watch` session's
  re-stall path: stale gauge + alive pane + mangled/unresolved target → re-resolve target, fire
  `ForceRestart` exactly once (cooldown + valid-sid gates honored), no loop, no alert storm.
- `SuppressNoGauge` flood suppression (hk-F21) must remain intact under the new path.
- Regression test: L-fake-tmux gate scenario + L-twin end-to-end scenario (twin scripted to wedge on
  cue) proving the watch self-heals without a human restart.

## Acceptance-corpus lock

Register the 6 scenarios (see `plans/2026-07-06-quality-system/11-keeper-test-design.md` §3) as a named,
CI-runnable conformance set:
1. restart-now resolves + does not abort `no_tmux_target` for a healthy watcher (B4)
2. session_id survives `/clear`, resume rebinds same conversation
3. unconfirmed handoff not truncated + no loop (hk-vpnp) — already green, lock as floor
4. watch re-stall auto-heals once, no alert storm (B3)
5. hold dies on restart + hard-ceiling overrides it
6. binary-upgrade refuse-to-start (aggregated) + `config --example` restores

No new harness layer. No band/threshold value changes (locked decision).
