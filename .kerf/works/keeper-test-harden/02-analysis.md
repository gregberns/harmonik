# 02 — Analysis: keeper-test-harden

Full failure-class analysis lives in `plans/2026-07-06-quality-system/11-keeper-test-design.md` §1
(G1–G6). Summary retained here for jig completeness:

- **G1 — band/threshold math.** `min(absTokens, pctCeil·windowSize)`. Mostly covered; add a 1M-vs-200k
  matrix guard. Layer: L-unit.
- **G2 — restart-now tmux-target resolution + sid rebind (B4, live pain).** `restartnow.go:83-86` aborts
  on empty `cfg.TmuxTarget`; the resolution seam one layer up (`tmuxresolve.go`) produced empty despite a
  live bound pane. Layer: L-fake-tmux (resolution) + L-twin (rebind drive).
- **G3 — nonce-confirmed-handoff gate (hk-vpnp).** Already green (`act_loop_hkvpnp_test.go`); lock as
  floor. Layer: L-fake-tmux reactive.
- **G4 — hold/release.** Session-id-keyed marker, 45m TTL, hard-ceiling override, WARN-under-hold,
  older-binary-ignores. Mostly covered; add hard-ceiling-overrides-hold explicitly. Layer: L-unit +
  L-fake-tmux.
- **G5 — doctor/live-watcher + binary-upgrade migration.** Gauge freshness ≠ liveness (needs
  `LiveKeeperPresent` flock probe); binary-upgrade required-keys landmine needs an aggregated
  refuse-to-start test + `config --example` round-trip. Layer: L-unit (cmd-level).
- **G6 — live-pane auto-recover (B3, live pain).** `ForceRestart` via `--respawn-cmd` exists but is not
  proven wired for the `watch` session's re-stall class. Primary new build. Layer: L-fake-tmux gate +
  L-twin loop.

No class requires the shared Phase-2 daemon substrate except crew-restart e2e re-hydration (T4,
deliberately deferred — see 01-problem-space.md Non-goals).

Three test layers already exist and are sufficient; no fourth layer needed:
- **L-unit** — pure Go table tests, no tmux/daemon.
- **L-fake-tmux** — `tmuxRunFn` seam + injectable `CyclerConfig` fns + in-process `reactiveSession` fake.
- **L-twin** — real-tmux `cmd/harmonik-twin-session`, real statusline/injection, faked wall-clock + LLM.
