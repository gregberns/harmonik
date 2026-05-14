package core

import (
	"errors"
	"sync"
	"testing"
)

// TestSubsystemRegistry groups all source_subsystem identifier registry tests
// sequentially under one driver so that tests that mutate the shared
// package-level registry don't interfere with each other.
//
// Spec ref: event-model.md §4.9 EV-034a — "each subsystem MUST register its
// source_subsystem identifier at daemon init; duplicates MUST fail startup with
// a typed error."
//
// Subtests are NOT run with t.Parallel() because they share global state.
// Only the concurrent-registration subtest spawns goroutines internally.
func TestSubsystemRegistry(t *testing.T) {
	t.Run("RegisterSourceSubsystem_Succeeds", func(t *testing.T) {
		t.Cleanup(subsystemRegistryReset)

		const id = "github.com/harmonik/internal/orchestrator"
		err := RegisterSourceSubsystem(id)
		if err != nil {
			t.Fatalf("RegisterSourceSubsystem returned unexpected error: %v", err)
		}
	})

	t.Run("RegisterSourceSubsystem_DuplicateReturnsErrDuplicate", func(t *testing.T) {
		t.Cleanup(subsystemRegistryReset)

		const id = "github.com/harmonik/internal/daemon"
		if err := RegisterSourceSubsystem(id); err != nil {
			t.Fatalf("first registration failed unexpectedly: %v", err)
		}
		err := RegisterSourceSubsystem(id)
		if err == nil {
			t.Fatal("second registration: expected ErrDuplicateSourceSubsystem, got nil")
		}
		if !errors.Is(err, ErrDuplicateSourceSubsystem) {
			t.Errorf("second registration: got %v, want errors.Is(ErrDuplicateSourceSubsystem)", err)
		}
	})

	t.Run("RegisterSourceSubsystem_EmptyIDReturnsError", func(t *testing.T) {
		t.Cleanup(subsystemRegistryReset)

		err := RegisterSourceSubsystem("")
		if err == nil {
			t.Fatal("expected error for empty id, got nil")
		}
	})

	t.Run("RegisterSourceSubsystem_MultipleDistinctSucceed", func(t *testing.T) {
		t.Cleanup(subsystemRegistryReset)

		ids := []string{
			"github.com/harmonik/internal/orchestrator",
			"github.com/harmonik/internal/daemon",
			"github.com/harmonik/internal/reconciler",
		}
		for _, id := range ids {
			if err := RegisterSourceSubsystem(id); err != nil {
				t.Fatalf("RegisterSourceSubsystem(%q) returned unexpected error: %v", id, err)
			}
		}
	})

	t.Run("ConcurrentRegistration_DistinctIDsSucceed", func(t *testing.T) {
		t.Cleanup(subsystemRegistryReset)

		const idA = "github.com/harmonik/internal/concurrent.alpha"
		const idB = "github.com/harmonik/internal/concurrent.beta"

		var wg sync.WaitGroup
		errA := make(chan error, 1)
		errB := make(chan error, 1)

		wg.Add(2)
		go func() {
			defer wg.Done()
			errA <- RegisterSourceSubsystem(idA)
		}()
		go func() {
			defer wg.Done()
			errB <- RegisterSourceSubsystem(idB)
		}()
		wg.Wait()

		if err := <-errA; err != nil {
			t.Errorf("idA registration failed: %v", err)
		}
		if err := <-errB; err != nil {
			t.Errorf("idB registration failed: %v", err)
		}
	})

	t.Run("ConcurrentRegistration_DuplicateReturnsError", func(t *testing.T) {
		t.Cleanup(subsystemRegistryReset)

		const id = "github.com/harmonik/internal/concurrent.dup"

		errCh := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			errCh <- RegisterSourceSubsystem(id)
		}()
		go func() {
			defer wg.Done()
			errCh <- RegisterSourceSubsystem(id)
		}()
		wg.Wait()
		close(errCh)

		var successes, duplicates int
		for err := range errCh {
			if err == nil {
				successes++
			} else if errors.Is(err, ErrDuplicateSourceSubsystem) {
				duplicates++
			} else {
				t.Errorf("unexpected error type: %v", err)
			}
		}
		if successes != 1 {
			t.Errorf("expected exactly 1 success, got %d", successes)
		}
		if duplicates != 1 {
			t.Errorf("expected exactly 1 ErrDuplicateSourceSubsystem, got %d", duplicates)
		}
	})
}
