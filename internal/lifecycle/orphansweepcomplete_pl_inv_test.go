package lifecycle

import (
	"sync"
	"testing"
	"time"
)

// startupSweepFixtureSweepState models the daemon's in-memory state tracking
// for the orphan-sweep-complete invariant (PL-INV-003). The orphan_sweep_complete_at
// field is set to a non-zero time when the sweep finishes. Every reconciliation
// detector dispatch path MUST assert this flag is non-nil before invocation.
type startupSweepFixtureSweepState struct {
	mu                    sync.Mutex
	orphanSweepCompleteAt *time.Time
	detectorInvocations   []string // names of detectors that were dispatched
}

// startupSweepFixtureMarkSweepComplete records the sweep completion timestamp.
// This corresponds to the daemon setting orphan_sweep_complete_at in memory
// on PL-006 completion.
func (s *startupSweepFixtureSweepState) startupSweepFixtureMarkSweepComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.orphanSweepCompleteAt = &now
}

// startupSweepFixtureDispatchDetector models PL-005 step 8: a reconciliation
// detector dispatch. Per PL-INV-003 the detector MUST only be dispatched when
// orphan_sweep_complete_at is non-nil. Returns error string if the invariant
// is violated; returns "" on success.
func (s *startupSweepFixtureSweepState) startupSweepFixtureDispatchDetector(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.orphanSweepCompleteAt == nil {
		return "PL-INV-003 violation: orphan_sweep_complete_at is nil when dispatching detector " + name
	}
	s.detectorInvocations = append(s.detectorInvocations, name)
	return ""
}

// startupSweepFixtureInvokedDetectors returns the list of detector names that
// have been successfully dispatched.
func (s *startupSweepFixtureSweepState) startupSweepFixtureInvokedDetectors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.detectorInvocations))
	copy(out, s.detectorInvocations)
	return out
}

// TestPL_INV003_OrphanSweepCompletesBeforeReconciliationClassification verifies
// that the in-memory orphan_sweep_complete_at flag is set before any
// reconciliation detector is invoked. A detector dispatch attempted before the
// flag is set constitutes a PL-INV-003 violation.
//
// The fixture models the daemon's state machine without a real daemon binary:
// the startupSweepFixtureSweepState type captures the invariant logic and
// asserts that every dispatch path checks the flag.
//
// Spec ref: process-lifecycle.md §5 PL-INV-003 — "§PL-006's orphan sweep MUST
// complete before any reconciliation detector (per [reconciliation/spec.md
// §4.3]) runs. Sensor: the daemon maintains an in-memory flag
// orphan_sweep_complete_at: Timestamp; every §PL-005 step 8
// reconciliation-dispatch path MUST assert the flag is non-nil before invoking
// any detector. An assertion failure is a panic per PL-018a."
func TestPL_INV003_OrphanSweepCompletesBeforeReconciliationClassification(t *testing.T) {
	t.Parallel()

	t.Run("detector-blocked-before-sweep-complete", func(t *testing.T) {
		t.Parallel()

		state := &startupSweepFixtureSweepState{}

		// orphan_sweep_complete_at is nil at this point.
		// Attempting to dispatch a detector MUST fail the invariant.
		violationMsg := state.startupSweepFixtureDispatchDetector("cat3a-intent-detector")
		if violationMsg == "" {
			t.Error("PL-INV-003: detector dispatched before sweep complete; invariant violation not detected")
		}

		// No detectors must have been recorded.
		if invoked := state.startupSweepFixtureInvokedDetectors(); len(invoked) != 0 {
			t.Errorf("PL-INV-003: %d detectors invoked before sweep; want 0", len(invoked))
		}
	})

	t.Run("detector-allowed-after-sweep-complete", func(t *testing.T) {
		t.Parallel()

		state := &startupSweepFixtureSweepState{}

		// Mark sweep complete first.
		state.startupSweepFixtureMarkSweepComplete()

		// Now dispatch a detector: must succeed (no violation).
		violationMsg := state.startupSweepFixtureDispatchDetector("cat3b-recon-lock-detector")
		if violationMsg != "" {
			t.Errorf("PL-INV-003: detector blocked after sweep complete: %s", violationMsg)
		}

		invoked := state.startupSweepFixtureInvokedDetectors()
		if len(invoked) != 1 || invoked[0] != "cat3b-recon-lock-detector" {
			t.Errorf("PL-INV-003: invoked detectors = %v, want [cat3b-recon-lock-detector]", invoked)
		}
	})

	t.Run("multiple-detectors-after-sweep-complete", func(t *testing.T) {
		t.Parallel()

		state := &startupSweepFixtureSweepState{}
		state.startupSweepFixtureMarkSweepComplete()

		detectors := []string{"cat1-auto-resolve", "cat3a-intent", "cat3b-recon-lock", "cat4-checkpoint"}
		for _, d := range detectors {
			msg := state.startupSweepFixtureDispatchDetector(d)
			if msg != "" {
				t.Errorf("PL-INV-003: detector %q blocked after sweep complete: %s", d, msg)
			}
		}

		invoked := state.startupSweepFixtureInvokedDetectors()
		if len(invoked) != len(detectors) {
			t.Errorf("PL-INV-003: invoked %d detectors, want %d", len(invoked), len(detectors))
		}
	})

	t.Run("sweep-complete-at-timestamp-is-non-zero", func(t *testing.T) {
		t.Parallel()

		state := &startupSweepFixtureSweepState{}

		beforeMark := time.Now()
		state.startupSweepFixtureMarkSweepComplete()
		afterMark := time.Now()

		state.mu.Lock()
		ts := state.orphanSweepCompleteAt
		state.mu.Unlock()

		if ts == nil {
			t.Fatal("PL-INV-003: orphanSweepCompleteAt is nil after markSweepComplete")
		}
		if ts.Before(beforeMark) || ts.After(afterMark) {
			t.Errorf("PL-INV-003: orphanSweepCompleteAt = %v, want in [%v, %v]", *ts, beforeMark, afterMark)
		}
	})

	t.Run("sweep-complete-before-step8-invariant-ordering", func(t *testing.T) {
		t.Parallel()

		// This subtest models the exact PL-005 ordering:
		// step 3 = orphan sweep → sets flag
		// step 8 = reconciliation dispatch → checks flag
		//
		// Both steps run in the fixture sequentially (no concurrency needed
		// because the spec says each step completes before the next begins).
		state := &startupSweepFixtureSweepState{}

		// Step 3: orphan sweep.
		state.startupSweepFixtureMarkSweepComplete()

		// Step 8: dispatch reconciliation — flag must be set.
		msg := state.startupSweepFixtureDispatchDetector("startup-recon-dispatch")
		if msg != "" {
			t.Errorf("PL-INV-003 step ordering: reconciliation dispatch failed: %s", msg)
		}

		invoked := state.startupSweepFixtureInvokedDetectors()
		if len(invoked) != 1 {
			t.Errorf("PL-INV-003 step ordering: invoked %d detectors, want 1", len(invoked))
		}
	})
}
