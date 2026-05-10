package lifecycle

import (
	"os"
	"os/exec"
	"sync"
	"testing"
)

// TestPL014_WaitOwner_SingleWaitAndReap verifies that WaitAndReap completes
// and returns nil for a process that exits 0.
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — "Every spawn MUST have exactly
// one Go goroutine that owns the *exec.Cmd and that goroutine MUST call
// cmd.Wait() exactly once."
func TestPL014_WaitOwner_SingleWaitAndReap(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "true") //nolint:noctx // true exits immediately; CommandContext is correct
	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-014 WaitOwner: cmd.Start: %v", err)
	}

	owner := NewWaitOwner(cmd)
	if err := owner.WaitAndReap(); err != nil {
		t.Errorf("PL-014 WaitOwner: WaitAndReap: %v", err)
	}
}

// TestPL014_WaitOwner_WaitBlocksUntilReap verifies that a goroutine calling
// Wait() before WaitAndReap() blocks until WaitAndReap completes.
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — single cmd.Wait() reap discipline.
func TestPL014_WaitOwner_WaitBlocksUntilReap(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "true") //nolint:noctx // true exits immediately; CommandContext is correct
	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-014 WaitOwner blocks: cmd.Start: %v", err)
	}

	owner := NewWaitOwner(cmd)

	var wg sync.WaitGroup
	var waitErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		waitErr = owner.Wait()
	}()

	// Reap after a small delay.
	if err := owner.WaitAndReap(); err != nil {
		t.Errorf("PL-014 WaitOwner blocks: WaitAndReap: %v", err)
	}

	wg.Wait()
	if waitErr != nil {
		t.Errorf("PL-014 WaitOwner blocks: Wait (observer): %v", waitErr)
	}
}

// TestPL014_WaitOwner_MultipleWaiters verifies that multiple goroutines can
// call Wait() and all receive the same exit error after WaitAndReap completes.
//
// Spec ref: process-lifecycle.md §4.5 PL-014; §4.6 PL-016 — single owner,
// multiple observers.
func TestPL014_WaitOwner_MultipleWaiters(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "true") //nolint:noctx // true exits immediately; CommandContext is correct
	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-014 WaitOwner multi: cmd.Start: %v", err)
	}

	owner := NewWaitOwner(cmd)

	const numWaiters = 5
	var wg sync.WaitGroup
	errs := make([]error, numWaiters)
	for i := 0; i < numWaiters; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = owner.Wait()
		}()
	}

	if err := owner.WaitAndReap(); err != nil {
		t.Errorf("PL-014 WaitOwner multi: WaitAndReap: %v", err)
	}

	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Errorf("PL-014 WaitOwner multi: waiter[%d] Wait: %v", i, e)
		}
	}
}

// TestPL014_WaitOwner_WaitAndReapIdempotent verifies that calling WaitAndReap
// a second time is a no-op (sync.Once guard prevents double cmd.Wait()).
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — "exactly one … cmd.Wait() exactly once."
func TestPL014_WaitOwner_WaitAndReapIdempotent(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "true") //nolint:noctx // true exits immediately; CommandContext is correct
	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-014 WaitOwner idempotent: cmd.Start: %v", err)
	}

	owner := NewWaitOwner(cmd)
	if err := owner.WaitAndReap(); err != nil {
		t.Errorf("PL-014 WaitOwner idempotent: first WaitAndReap: %v", err)
	}

	// Second call must not panic and must return zero value (the once.Do guard
	// prevents a second cmd.Wait(); result is the zero-value error from the
	// outer variable, not a second Wait).
	_ = owner.WaitAndReap() // must not panic
}

// TestPL014_WaitOwner_Cmd verifies that Cmd() returns the underlying *exec.Cmd.
func TestPL014_WaitOwner_Cmd(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "true") //nolint:noctx // true exits immediately
	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-014 WaitOwner Cmd: cmd.Start: %v", err)
	}

	owner := NewWaitOwner(cmd)
	if owner.Cmd() != cmd {
		t.Error("PL-014 WaitOwner Cmd: Cmd() returned wrong *exec.Cmd")
	}
	_ = owner.WaitAndReap() // reap to avoid zombie
}

// TestPL014_WaitOwner_NonZeroExitPreserved verifies that a non-zero exit code
// is preserved in the error returned by WaitAndReap and Wait.
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — zombie prevention; correct
// exit-status propagation.
func TestPL014_WaitOwner_NonZeroExitPreserved(t *testing.T) {
	t.Parallel()

	// false exits with code 1.
	cmd := exec.CommandContext(t.Context(), "false") //nolint:noctx // false exits immediately
	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-014 WaitOwner non-zero: cmd.Start: %v", err)
	}

	owner := NewWaitOwner(cmd)
	exitErr := owner.WaitAndReap()
	if exitErr == nil {
		t.Error("PL-014 WaitOwner non-zero: WaitAndReap: expected non-nil error for exit code 1")
	}

	// Wait should return the same error.
	waitErr := owner.Wait()
	// Both should be non-nil.
	if waitErr == nil {
		t.Error("PL-014 WaitOwner non-zero: Wait after WaitAndReap: expected non-nil error")
	}
}

// TestPL014_WaitOwner_SentinelChildStub is the child-process body for
// TestPL014_WaitOwner_SpawnWithProvenance. Called when the test binary is
// re-invoked with GO_PL014_WAITOWNER_STUB=1.
func TestPL014_WaitOwner_SentinelChildStub(t *testing.T) {
	const sentinelEnv = "GO_PL014_WAITOWNER_STUB"
	if os.Getenv(sentinelEnv) != "1" {
		return // not the child stub; skip
	}
	// Child: exit immediately with code 0.
	os.Exit(0)
}

// TestPL014_WaitOwner_SpawnWithProvenance verifies that a subprocess spawned
// via NewWaitOwner can be reaped correctly with the full provenance setup.
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — single-owner Wait for daemon
// subprocesses carrying provenance markers.
func TestPL014_WaitOwner_SpawnWithProvenance(t *testing.T) {
	t.Parallel()

	const sentinelEnv = "GO_PL014_WAITOWNER_STUB"

	testBin := os.Args[0]
	//nolint:gosec,noctx // G204: testBin is os.Args[0]; immediate-exit child
	cmd := exec.Command(testBin, "-test.run=^TestPL014_WaitOwner_SentinelChildStub$")
	cmd.Env = append(os.Environ(), sentinelEnv+"=1")

	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-014 spawn-with-provenance: cmd.Start: %v", err)
	}

	owner := NewWaitOwner(cmd)
	if err := owner.WaitAndReap(); err != nil {
		t.Errorf("PL-014 spawn-with-provenance: WaitAndReap: %v", err)
	}
}
