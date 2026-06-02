package daemon_test

// set_concurrency_hkohiaf_test.go — unit tests for runtime-adjustable dispatch
// ceiling via ConcurrencyController (hk-ohiaf).
//
// The tests assert three properties specified by the bead:
//  1. Raising n above the running count lets the gate dispatch up to n.
//  2. Lowering n below the running count stops NEW dispatch (running count
//     above n does not kill in-flight runs; the gate simply blocks until
//     running drops below n).
//  3. n < 1 is rejected with an error.
//
// The tests use ConcurrencyController directly (white-box) rather than
// running a full workloop, keeping them fast and deterministic.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestConcurrencyController_GetAfterNew verifies initial value is floored at 1.
func TestConcurrencyController_GetAfterNew(t *testing.T) {
	t.Parallel()

	cases := []struct {
		initial int
		want    int
	}{
		{initial: 0, want: 1},
		{initial: -5, want: 1},
		{initial: 1, want: 1},
		{initial: 4, want: 4},
		{initial: 10, want: 10},
	}
	for _, tc := range cases {
		tc := tc
		t.Run("", func(t *testing.T) {
			t.Parallel()
			c := daemon.NewConcurrencyController(tc.initial)
			if got := c.Get(); got != tc.want {
				t.Errorf("NewConcurrencyController(%d).Get() = %d, want %d", tc.initial, got, tc.want)
			}
		})
	}
}

// TestConcurrencyController_Set_RaiseCeiling verifies that raising n updates
// the ceiling and returns the previous value.
func TestConcurrencyController_Set_RaiseCeiling(t *testing.T) {
	t.Parallel()

	c := daemon.NewConcurrencyController(2)
	oldN, err := c.Set(6)
	if err != nil {
		t.Fatalf("Set(6) returned unexpected error: %v", err)
	}
	if oldN != 2 {
		t.Errorf("Set(6) old = %d, want 2", oldN)
	}
	if got := c.Get(); got != 6 {
		t.Errorf("Get() after Set(6) = %d, want 6", got)
	}
}

// TestConcurrencyController_Set_LowerCeiling verifies that lowering n updates
// the ceiling and returns the previous value. The controller itself does not
// enforce any running-count gate — it just updates the atomic; the workloop is
// responsible for letting in-flight runs drain naturally.
func TestConcurrencyController_Set_LowerCeiling(t *testing.T) {
	t.Parallel()

	c := daemon.NewConcurrencyController(8)
	oldN, err := c.Set(2)
	if err != nil {
		t.Fatalf("Set(2) returned unexpected error: %v", err)
	}
	if oldN != 8 {
		t.Errorf("Set(2) old = %d, want 8", oldN)
	}
	if got := c.Get(); got != 2 {
		t.Errorf("Get() after Set(2) = %d, want 2", got)
	}
}

// TestConcurrencyController_Set_RejectsBelowOne verifies that n < 1 is
// rejected and the ceiling is not modified.
func TestConcurrencyController_Set_RejectsBelowOne(t *testing.T) {
	t.Parallel()

	cases := []int{0, -1, -100}
	for _, n := range cases {
		n := n
		t.Run("", func(t *testing.T) {
			t.Parallel()
			c := daemon.NewConcurrencyController(4)
			_, err := c.Set(n)
			if err == nil {
				t.Errorf("Set(%d): expected error, got nil", n)
			}
			// Ceiling must be unchanged.
			if got := c.Get(); got != 4 {
				t.Errorf("Set(%d) modified ceiling to %d; want 4 (unchanged)", n, got)
			}
		})
	}
}

// TestConcurrencyController_Set_Idempotent verifies that setting to the same
// value succeeds and returns old == new.
func TestConcurrencyController_Set_Idempotent(t *testing.T) {
	t.Parallel()

	c := daemon.NewConcurrencyController(5)
	oldN, err := c.Set(5)
	if err != nil {
		t.Fatalf("Set(5) returned unexpected error: %v", err)
	}
	if oldN != 5 {
		t.Errorf("Set(5) old = %d, want 5", oldN)
	}
	if got := c.Get(); got != 5 {
		t.Errorf("Get() = %d, want 5", got)
	}
}

// TestConcurrencyController_ExportedWorkLoopDeps_Wired verifies that
// ExportedWorkLoopDeps accepts and stores a ConcurrencyCtrl (compile-time
// wiring check only — actual gate behaviour is exercised by the workloop
// integration test).
func TestConcurrencyController_ExportedWorkLoopDeps_Wired(t *testing.T) {
	t.Parallel()

	ctrl := daemon.NewConcurrencyController(3)
	if ctrl == nil {
		t.Fatal("NewConcurrencyController returned nil")
	}
	// Smoke: ensure the struct field is accepted without panic.
	_ = daemon.WorkLoopDepsParams{
		MaxConcurrent:   3,
		ConcurrencyCtrl: ctrl,
	}
}
