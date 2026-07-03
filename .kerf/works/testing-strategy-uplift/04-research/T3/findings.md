# T3 — Integration Tests (composition-root wiring): Research Findings

**Track:** T3 — Integration Tests  
**Date:** 2026-05-20  
**Status:** complete

---

## Research Questions

1. What test seams exist in the composition root for integration testing?
2. What wiring assertions are most valuable (from T3 requirements)?
3. Where do integration tests currently live? What does the stub contain?
4. Can integration tests run without a real claude subprocess?
5. What does `go test -race -tags=integration ./internal/daemon/...` currently produce?

---

## Findings

### Q1: Test seams in the composition root

`daemon.Config` has two `TestOnly*` fields already wired:

1. **`TestOnlyBusObserver func(bus eventbus.EventBus)`** — called immediately after all pre-Seal subscriptions are registered, before `bus.Seal()`. Allows asserting subscription state without changing the EventBus interface. Bead ref: hk-37zy8.

2. **`TestOnlyBrAdapterFactory func(brPath, projectDir string) (*brcli.Adapter, error)`** — replaces `brcli.NewForProject` at all 3 call sites. Allows stubbing the br adapter without a real br binary. Bead ref: hk-th378.

These two hooks are exactly what T3 integration tests need. No new seams required.

The `Substrate` field on `Config` (type `handler.Substrate`) allows injecting a fake substrate for testing tmux-path wiring without real tmux.

### Q2: Most valuable wiring assertions

Based on `02-analysis.md` §7 (scenario gap audit) and T3 requirements:

**Test 1: `TestIntegration_HandlerPausePolicyGoroutine_Subscribed`**
  - Use `TestOnlyBusObserver` to assert that the bus has ≥1 subscriber for the BudgetExhausted event type after `daemon.Start` registration phase
  - Catches: hk-37zy8 class (policy goroutine existed but was never Subscribe()'d)
  - File: `internal/daemon/composition_integration_test.go`

**Test 2: `TestIntegration_SubstrateWiring_NonNil`**
  - Pass a stub `handler.Substrate` in `Config.Substrate`
  - After the buildSpecs phase, assert that both `implSpec.Substrate` and `revSpec.Substrate` are non-nil
  - Catches: hk-2hb2y class (substrate unwired)
  - Challenge: buildSpecs is an internal function; needs an exported test entry point or a higher-level assertion

**Test 3: `TestIntegration_QueueOperatorEventConsumer_Subscribed`**
  - Use `TestOnlyBusObserver` to verify QueueOperatorEventConsumer is subscribed before Seal
  - Future regression guard for any new subscriber that gets added without wiring

### Q3: Integration test current state

`test/integration/integration_stub.go` contains only a package declaration with a comment. No real tests.

The component-requirement in `03-components.md` says integration tests can live in `internal/daemon/` (not necessarily `test/integration/`). Given that `TestOnlyBusObserver` is in daemon.Config, `internal/daemon/composition_integration_test.go` is the natural home. The `test/integration/` stub can remain for future daemon-external integration tests.

### Q4: No real claude subprocess needed

The `TestOnlyBrAdapterFactory` allows replacing the br adapter. The `TestOnlyBusObserver` fires before the work loop starts — we don't need to run the full daemon work loop, just call `daemon.Start` with a context that cancels immediately after the observer fires.

Pattern:
```go
ctx, cancel := context.WithCancel(context.Background())
var observedBus eventbus.EventBus
cfg.TestOnlyBusObserver = func(bus eventbus.EventBus) {
    observedBus = bus
    cancel() // stop Start after observation
}
_ = daemon.Start(ctx, cfg) // returns ctx.Err() = OK
// assert on observedBus
```

### Q5: go test -tags=integration current state

`./internal/daemon/...` with -tags=integration: the daemon package compiles but has a BUILD FAILED for other reasons (queue/cli build error noted in T6 findings). After that's fixed, the daemon package should compile cleanly under integration tag since no integration-tagged files exist there yet.

`test/integration/` with -tags=integration: compiles (just the stub package), 0 failures.

---

## Options and Tradeoffs

**Test location: internal/daemon/ vs test/integration/**

Option A: `internal/daemon/composition_integration_test.go`
- Pros: direct access to Config fields; natural home for composition-root tests
- Cons: test/integration/ remains empty stub

Option B: `test/integration/daemon_wiring_test.go`
- Pros: consistent test layout (all integration tests in test/integration/)
- Cons: requires importing daemon package from test directory; less direct access

Verdict: Option A — the TestOnly seams are in daemon package, tests belong there. The T3 requirement explicitly says "at minimum one integration test in `internal/daemon/`".

---

## Risks and Unknowns

1. The `daemon.Start` call + immediate cancel pattern may trigger a race between the observer callback and the work-loop goroutine startup. Needs -race validation.
2. The `buildSpecs` function (for substrate assertion, Test 2) may not be a public function — needs a different assertion approach (e.g., via a higher-level check that a spawn attempt happens when substrate is provided)
3. hk-37zy8 bus-subscription assertion needs to know exactly which event types `HandlerPausePolicyGoroutine` subscribes to — needs a quick scan of that goroutine's Subscribe call
