# Architectural Review of Recent Daemon Commits — 2026-05-20

Source: architectural review of commits a92090b, a61823d, 6442e4b, 5e651c2, 2e48555, 5e8f868.

## Unifying Anti-Pattern

**State whose correct lifetime is one bead run was stored on an object whose lifetime is the daemon.**

- `tmuxSubstrate.lastHandle / lastPaneID` (hk-wx8z8, hk-012af) — written per `SpawnWindow`, consumed by per-run paste-inject goroutine; run N+1 overwrote run N's pane target.
- `processDead(err != nil)` (hk-88nno) — misclassified EPERM as dead, propagating to long-lived queue state.
- `runWait ctx.Done → exitCode=-1` (hk-cj0gm) — inherited daemon-context cancellation, wrong exit classification.
- `drainCancelledQueue` missing (hk-ppt32) — per-run loop exit paths left queue state for next daemon.
- `--sort priority` missing (hk-rp48p) — implicit ordering leaked into claim path.

All five: state with narrower valid lifetime than its holder, not enforced by types/init discipline.

## Other Latent Sites

- **tmuxSubstrate.WriteLastPane / SendEnterToLastPane / SendQuitToLastPane (lines 401-453)** — still read shared `lastHandle/lastPaneID`. Unreachable in production today but re-activates the hk-012af class if `newPerRunSubstrate` ever returns nil. → `hk-jfh59`.
- **tmuxSubstrateSession.outcome lazy init (lines 521-522, 610-612)** — `waitDone` initialized inside `waitOnce.Do`. If `Outcome()` called before `Wait()`, returns zero struct silently. Mixed eager/lazy init.
- **workloop.go nil-guards on adapterRegistry (1159, 1090-1092)** — `_ = tapCh` at 1228 is the tell. → `hk-d8u1y`.
- **reviewloop.go dual substrate variables (211-217, 459-464)** — two pointers for what should be one run-scoped handle; future refactor could wire one and not the other.

## Highest-Leverage Structural Change

**Promote `perRunSubstrate` to a first-class per-run context object — a `runCtx` struct — that is the exclusive carrier of all run-scoped state.**

Currently scattered: `runSubstrate`, `runPasteTarget`, `artifacts.claudeSessionID`, `runID`, `wtPath`, `headSHA`, plus the dual substrate wiring in reviewloop. Threaded as separate args.

A `runCtx` constructed at goroutine birth, threaded as one argument:
- Lifetime explicit: construction = goroutine birth; GC = exit.
- Eliminates dual-variable patterns.
- Turns nil-guard anti-pattern into compile-time check.
- Makes hk-012af class structurally impossible.

Targets:
- workloop.go:1065-1070
- reviewloop.go:211-217 and 459-464
- pasteinject.go:178-205 (signature shrinks from 6 args to `(ctx, runCtx)`)

## Patterns To Adopt

- **R1**: No shared state on long-lived objects for values that vary per run. Delete lastHandle/lastPaneID once hk-jfh59 lands.
- **R2**: Initialize channels at construction, not inside `sync.Once`. `waitDone: make(chan struct{})` in `SpawnWindow` (tmuxsubstrate.go:196-202).
- **R3**: No nil-guard in production code for test-only divergence. (hk-d8u1y)
- **R4**: Name exit classifications explicitly. Constants `exitCodeClean = 0`, `exitCodeUnknown = -1` (tmuxsubstrate.go:665, 682, 695, 708).
- **R5**: Process-boundary seams only in tests; no seam at loop-internal function level. Shrink `export_test.go` toward zero; migrate to `daemon.Start` + twin + fake adapter. (hk-p3diy / hk-jf2tb roadmap)

Full review: agent a2bfd99e65716f5f8 output.
