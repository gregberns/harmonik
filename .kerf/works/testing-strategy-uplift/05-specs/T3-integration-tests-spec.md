# Change Spec: T3 — Integration Tests (composition-root wiring)

**Component:** T3  
**Date:** 2026-05-20  
**Research:** 04-research/T3/findings.md

---

## Requirements (from 03-components.md)

1. ≥1 integration test in `internal/daemon/` with `//go:build integration` asserting subscription wiring (HandlerPausePolicyGoroutine subscribed before bus.Seal()).
2. ≥1 integration test asserting substrate wiring (implSpec.Substrate + revSpec.Substrate non-nil when deps.substrate != nil).
3. Tests use `internal/testhelpers` for tmpdir and clock. No real claude subprocess.
4. `go test -race -tags=integration ./internal/daemon/...` exits 0.
5. Each test file cites the bead it would have caught.

---

## Research Summary

- `daemon.Config.TestOnlyBusObserver` hook exists and fires after all pre-Seal subscriptions, before Seal(). This is the exact seam needed for Test 1.
- `daemon.Config.Substrate` field exists for injecting a fake substrate (Test 2).
- `daemon.Config.TestOnlyBrAdapterFactory` exists for stubbing br adapter (no real br binary needed).
- Pattern: Start daemon with cancellable context, observe bus in TestOnlyBusObserver, cancel context after observation.
- buildSpecs is internal — Test 2 may need a different approach (higher-level assertion or a new TestOnly hook).

---

## Approach

**Test 1: TestIntegration_HandlerPausePolicyGoroutine_Subscribed**

File: `internal/daemon/composition_integration_test.go`
Build tag: `//go:build integration`

Use `TestOnlyBusObserver` to capture the bus after registration. Assert that the bus has at least one subscriber for BudgetExhausted events (or the specific event type that HandlerPausePolicyGoroutine subscribes to — verify in daemon.go).

```go
//go:build integration

func TestIntegration_HandlerPausePolicyGoroutine_Subscribed(t *testing.T) {
    // catches hk-37zy8 class: policy goroutine existed + unit-tested but never Subscribe()'d
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    var capturedBus eventbus.EventBus
    cfg := daemon.Config{
        // minimal config
        TestOnlyBrAdapterFactory: stubBrAdapter,
        TestOnlyBusObserver: func(bus eventbus.EventBus) {
            capturedBus = bus
            cancel() // stop daemon after observation
        },
    }
    _ = daemon.Start(ctx, cfg) // returns ctx.Err() — OK

    require.NotNil(t, capturedBus)
    // Assert subscription exists for the BudgetExhausted event type
    // Implementation: inspect bus subscriber count for the relevant event type
    // Exact assertion depends on EventBus API for introspection
}
```

Note: if EventBus doesn't expose subscriber introspection, the test can assert indirectly by injecting the event and observing a side effect (handler pause state changes).

**Test 2: TestIntegration_SubstrateWiring_NonNil**

The `buildSpecs` function is internal. Two options:
  a. Add a `TestOnlyBuildSpecsObserver` hook to Config (parallel to TestOnlyBusObserver)
  b. Assert indirectly: if substrate is wired, the handler's first spawn call will attempt `SpawnWindow`; inject a stub substrate that records whether SpawnWindow was called

Option b is safer (no new production hook needed). Use a spy substrate that records calls.

```go
//go:build integration

func TestIntegration_SubstrateWiring_Spy(t *testing.T) {
    // catches hk-2hb2y class: implSpec/revSpec.Substrate were unwired
    spy := &substrateCallSpy{}
    cfg := daemon.Config{
        Substrate: spy,
        TestOnlyBrAdapterFactory: stubBrAdapterWithOneReadyBead,
        // ... minimal config for one bead run
    }
    // run daemon for one bead; assert spy.SpawnWindowCalled == true
}
```

This requires a spy that implements `handler.Substrate`. Can be in `internal/daemon/` test helpers or `internal/testhelpers/`.

**Config minimal setup:**
Both tests need a minimal daemon.Config. Use `internal/testhelpers.TempDir(t)` for ProjectDir, a stub br adapter that returns a single "in_progress" bead record, and a sufficiently short timeout.

---

## Files & Changes

- `internal/daemon/composition_integration_test.go` — new file:
  - `//go:build integration`
  - TestIntegration_HandlerPausePolicyGoroutine_Subscribed
  - TestIntegration_SubstrateWiring_Spy
  - stubBrAdapter (local helper)
  - substrateCallSpy struct (implements handler.Substrate)

- No changes to production code. No new TestOnly hooks needed unless EventBus introspection is absent.

---

## Acceptance Criteria

1. `go test -race -tags=integration ./internal/daemon/...` exits 0.
2. TestIntegration_HandlerPausePolicyGoroutine_Subscribed passes — and if the HandlerPausePolicyGoroutine.Subscribe call is removed from daemon.go, the test FAILS.
3. TestIntegration_SubstrateWiring_Spy passes — and if the Substrate assignment is removed from the handler launch path, the test FAILS.
4. Each test file contains a comment citing the bead class it catches (hk-37zy8, hk-2hb2y).
5. No real `claude` subprocess is spawned during the test run.
6. `go test` (default tags, no integration tag) does NOT compile or run these tests.

---

## Verification

```bash
go test -race -tags=integration -v ./internal/daemon/... -run TestIntegration_  # must pass
go test -v ./internal/daemon/... -run TestIntegration_  # must produce 0 tests (build tag gates them)
```

---

## Error Handling

- `daemon.Start` returns an error when context is cancelled — this is expected; the test must not treat ctx.Err() as a failure.
- The stub br adapter must return clean responses (no unexpected errors that would cause daemon.Start to exit before the TestOnlyBusObserver fires).

---

## Bead Candidates

- T3a: `T3: TestIntegration_HandlerPausePolicyGoroutine_Subscribed` (type: task, labels: test-infra, codename:testing-strategy-uplift)
- T3b: `T3: TestIntegration_SubstrateWiring_Spy` (type: task)

## Validation Beads
- Scenario-test bead: hk-lz485 — scenario: T3 integration — HandlerPausePolicyGoroutine wired before bus.Seal
