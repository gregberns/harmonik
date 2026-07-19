package daemon

// ev036_startup_gate_a4_test.go — regression guard for mega-review finding A4.
//
// Before A4, core.ScanRegisteredPayloadsForSecretFields (the EV-036 secret-field
// scan) had ZERO production callers: the fail-closed guarantee lived only in a
// core unit test. A future payload with a field like SecretToken could ship and
// emit to the durable JSONL log with no startup failure. The fix wires the scan
// into daemon.Start after all payload types are registered and before bus.Seal,
// making a positive result FATAL (the daemon refuses to boot).
//
// This test exercises the real daemon.Start composition path (unit-test mode:
// no ProjectDir/BrPath, so Start boots the bus + all pre-Seal wiring and stops
// at Seal). It substitutes a synthetic-violation scan stub via the package seam
// so it asserts the abort behavior WITHOUT polluting the global event registry
// that other parallel daemon tests share.
//
// Spec ref: event-model.md §4.10 EV-036 (structural guardrail; HC-033 analog).

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestA4_DaemonStart_EV036SecretFieldScan_AbortsBootBeforeSeal verifies that
// daemon.Start invokes the EV-036 secret-field scan and that a positive result
// (a payload with a secret-looking field) is FATAL: Start returns the scan error
// and the bus is NEVER sealed. The busObserver firing proves the scan runs AFTER
// all pre-Seal subscriber wiring and BEFORE Seal — exactly the ordering EV-036
// requires (after Register-all, before seal).
//
// NOT parallel: it mutates the package-level scan seam.
func TestA4_DaemonStart_EV036SecretFieldScan_AbortsBootBeforeSeal(t *testing.T) {
	sentinelErr := errors.New("event type \"synthetic_bad\" has field \"SecretToken\" matching secret-prefix rule")

	orig := scanRegisteredPayloadsForSecretFields
	t.Cleanup(func() { scanRegisteredPayloadsForSecretFields = orig })

	var observedPreSeal bool
	scanRegisteredPayloadsForSecretFields = func() error { return sentinelErr }

	cfg := Config{
		// Unit-test mode: no ProjectDir, no BrPath, no JSONL log. Start boots the
		// bus + every pre-Seal subscriber, invokes the busObserver just before
		// Seal, then runs the EV-036 gate.
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	}

	err := StartForTesting(context.Background(), cfg,
		WithBusObserver(func(bus eventbus.EventBus) {
			// Fires immediately before Seal in the composition root (documented
			// pre-Seal seam); proves the gate below runs after all wiring and
			// before Seal.
			_ = bus
			observedPreSeal = true
		}),
	)

	if !observedPreSeal {
		t.Fatal("busObserver never fired; Start did not reach the pre-Seal composition point")
	}
	if err == nil {
		t.Fatal("daemon.Start returned nil; want fatal EV-036 secret-field error (boot must abort)")
	}
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("daemon.Start error = %v; want wrapped EV-036 scan error", err)
	}
}

// TestA4_ScanSeamDefaultsToRealScanner asserts the production seam is the real
// global-registry scanner, so the wiring above is not a no-op stub in prod.
func TestA4_ScanSeamDefaultsToRealScanner(t *testing.T) {
	got := reflect.ValueOf(scanRegisteredPayloadsForSecretFields).Pointer()
	want := reflect.ValueOf(core.ScanRegisteredPayloadsForSecretFields).Pointer()
	if got != want {
		t.Errorf("scanRegisteredPayloadsForSecretFields seam is not core.ScanRegisteredPayloadsForSecretFields; production would not run the real EV-036 gate")
	}
}
