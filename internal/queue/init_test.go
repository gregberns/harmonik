package queue_test

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestQueueSubsystemRegistered verifies that importing internal/queue causes
// the package init() to register "github.com/gregberns/harmonik/internal/queue"
// in the core subsystem registry per EV-034a.
//
// Strategy: the queue package is imported by this test binary (via the blank
// import side-effect in queue_test). If init() ran, attempting to register the
// same identifier again MUST return ErrDuplicateSourceSubsystem.
//
// Spec ref: event-model.md §4.9 EV-034a.
func TestQueueSubsystemRegistered(t *testing.T) {
	t.Parallel()

	const queueSubsystemID = "github.com/gregberns/harmonik/internal/queue"

	err := core.RegisterSourceSubsystem(queueSubsystemID)
	if err == nil {
		t.Fatal("expected ErrDuplicateSourceSubsystem (init() should have already registered), got nil")
	}
	if !errors.Is(err, core.ErrDuplicateSourceSubsystem) {
		t.Errorf("got %v, want errors.Is(ErrDuplicateSourceSubsystem)", err)
	}
}
