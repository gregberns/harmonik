# 07 — Tasks: keeper-test-harden

All beads labeled `codename:keeper-test-harden`, epic `hk-193bo`.

## T1 (parallel now, start here)
- B4 fix + test: restart-now/ping tmux-target resolution seam returns the live pane for a healthy
  watcher; RunOnDemand does not abort no_tmux_target. (corpus #1)
- B3 auto-recover scenario: stale-gauge + alive-pane + mangled-target → gated ForceRestart fires once,
  re-resolves target, no alert storm, no loop. (corpus #4)

## T2 (parallel now)
- sid-rebind twin scenario: same conversation survives /clear→resume; anti-loop gate holds. (corpus #2)
- hold invariants: session-id-keyed death-on-restart + hard-ceiling-overrides-hold + WARN-under-hold +
  older-binary-ignores. (corpus #5)
- binary-upgrade migration: new required key → aggregated refuse-to-start; config --example round-trips
  complete. (corpus #6)

## T3 (parallel now, after T1/T2 land)
- Regression-floor lock: register the acceptance corpus as a named conformance set (band min matrix,
  force-act, hard-ceiling SID-independent, pct-inert-warn, live-watcher flock, operator-attached
  warn-only, hk-vpnp no-truncate-no-loop). (corpus floor + #3)

## T4 (BLOCKED — do not dispatch)
- Crew-restart e2e re-hydration: daemon auto-arms crew keeper → crew /clear→resume same name → named
  queue re-drains. Gated on the dispatch lane's core-loop-proof scratch-daemon substrate.
