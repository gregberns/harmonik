# 03 — Components: keeper-test-harden

Components touched, all under `internal/keeper/` + `cmd/harmonik/keeper_*` + `cmd/harmonik-twin-session`:

1. **`restartnow.go` / `tmuxresolve.go`** (B4) — tmux-target resolution seam + `RunOnDemand` abort logic.
2. **`respawn.go` / `watcher.go`** (B3) — gated `ForceRestart` via `--respawn-cmd`, live-pane detection,
   cooldown/valid-sid gating, `SuppressNoGauge` anti-flood.
3. **`cycle.go` / `sessionid_test.go` / `export_test.go`** — sid-rebind + `lastFiredSID` anti-loop gate.
4. **`hold.go` / `keeper_hold_test.go`** — session-id-keyed hold marker, TTL, hard-ceiling override.
5. **`resolve_keeper_config.go` / `keeper_config_example.go`** — required-key aggregation, `--example`
   round-trip, binary-upgrade migration.
6. **`live_keeper_present_hkx7s_test.go`** — flock-based live-watcher probe.
7. **Acceptance-corpus registration** — a new named conformance set (test suite or tag) that asserts all
   6 scenarios green in one command, for CI/regression-floor use.

No new component/package is introduced. All work is fixes + tests inside existing files/seams.
