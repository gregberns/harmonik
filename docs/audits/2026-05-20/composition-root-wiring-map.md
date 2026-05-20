# Composition-Root Wiring Map — 2026-05-20

Source: investigation under user directive 2026-05-20. Read-only audit.

## Summary

29 major wiring points in `internal/daemon/daemon.go::Start()`. Of these, **6 CRITICAL wirings** have composition-test-only coverage and match the bug pattern of hk-37zy8 / hk-yjduq / hk-2hb2y / hk-012af.

## Critical Tier

| # | Symbol | Call Site | Wires | E2E Coverage | Risk if Dropped |
|---|---|---|---|---|---|
| 1 | `HandlerPausePolicyGoroutine.Subscribe` | daemon.go:510 | agent_rate_limit_status + budget_exhausted consumers | composition only (hk-37zy8) | HIGH — pause feature silently broken |
| 2 | `QueueOperatorEventConsumer.Subscribe` | daemon.go:529 | operator_pause_status + operator_resuming | composition only | HIGH — queue pause/resume stuck |
| 9 | `NewHandlerPauseController()` | daemon.go:492 | pause state machine, pre-Seal | composition only | HIGH — state machine never instantiated |
| 10 | `NewRunRegistry()` (shared) | daemon.go:493 | in-flight bead registry read by pause policy | composition only | HIGH — empty snapshot, concurrent runs invisible |
| 13 | `handlerPauseCtrl.SetPersistFn()` | daemon.go:743 | patch persistence post-Seal | composition only | HIGH — pause state lost on restart |
| 23 | `deps.queueStore` injection | daemon.go:816 | singleton QueueStore for pull-dispatch | composition + exploratory fallback | HIGH — queue dispatch unavailable |
| 27 | `deps.handlerPauseController` 2nd inject | daemon.go:835 | shared RunRegistry reference | composition only | CRITICAL — loop/policy desync |
| 28 | `deps.runRegistry` injection | daemon.go:844 | wire SHARED RunRegistry from L493 into workloop | composition only | CRITICAL — loop/policy use different registries |

## Registry-Sharing Footgun (Lines 391 vs 493)

Two separate `NewRunRegistry()` calls exist:
- **daemon.go:493** — `sharedRunRegistry := NewRunRegistry()` injected into hk-37zy8 policy goroutine.
- **workloop.go:391** — fresh `NewRunRegistry()` inside `newWorkLoopDeps`, NOT the same instance.

If line 844's injection were dropped, workloop would use the local L391 registry, policy would snapshot the shared L493 registry, and **concurrent pause would silently fail**. This is exactly the hk-012af pattern.

## Recommendations

1. Convert the 6 composition-only tests to synthetic E2E tests with minimal dispatch.
2. Rename L391 to `newLocalRunRegistry()` with a comment forbidding shared-instance use.
3. Add a daemon-startup wiring log (debug mode) listing all 29 wiring points.
4. Mandate deps-field verification tests: every field injected at L816..L844 must have an exploratory test that reads it.

Full investigation transcript: agent a44816d... output.
