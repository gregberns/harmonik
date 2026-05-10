package lifecycle

import (
	"fmt"
	"sync"
	"time"
)

// StartupState tracks the daemon's in-memory startup-phase flags required for
// PL-INV-003 enforcement. It is created once at daemon startup (PL-005 step 0
// composition-root bootstrap) and held for the duration of the startup sequence.
//
// Concurrency: all methods are safe for concurrent use.
//
// Spec ref: process-lifecycle.md §5 PL-INV-003 — "§PL-006's orphan sweep
// MUST complete before any reconciliation detector (per reconciliation/spec.md
// §4.3) runs."
type StartupState struct {
	mu sync.Mutex

	// orphanSweepCompleteAt is set to a non-nil time when PL-006 orphan sweep
	// completes. Every PL-005 step 8 reconciliation-dispatch path MUST assert
	// this field is non-nil before invoking any detector. Nil means the sweep
	// has not yet completed.
	//
	// Spec ref: process-lifecycle.md §5 PL-INV-003 — "the daemon maintains an
	// in-memory flag orphan_sweep_complete_at: Timestamp."
	orphanSweepCompleteAt *time.Time
}

// NewStartupState creates an empty StartupState with all flags unset.
// The caller MUST call MarkOrphanSweepComplete() after PL-006 finishes and
// before invoking AssertOrphanSweepComplete() on any reconciliation-dispatch
// path.
func NewStartupState() *StartupState {
	return &StartupState{}
}

// MarkOrphanSweepComplete records the wall-clock time at which PL-006 orphan
// sweep completed. It is idempotent: a second call overwrites the timestamp
// (this is safe; the invariant cares only that the timestamp is non-nil at
// dispatch time).
//
// The caller MUST invoke this immediately after RunOrphanSweep returns
// successfully (PL-005 step 3, before step 3a).
//
// Spec ref: process-lifecycle.md §5 PL-INV-003 — "orphan_sweep_complete_at:
// Timestamp" is set on PL-006 completion.
func (s *StartupState) MarkOrphanSweepComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.orphanSweepCompleteAt = &now
}

// OrphanSweepCompleteAt returns the timestamp at which the orphan sweep
// completed, or nil if the sweep has not yet completed.
//
// Callers MAY inspect this for observability; for enforcement use
// AssertOrphanSweepComplete.
func (s *StartupState) OrphanSweepCompleteAt() *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.orphanSweepCompleteAt == nil {
		return nil
	}
	ts := *s.orphanSweepCompleteAt
	return &ts
}

// AssertOrphanSweepComplete panics if the orphan sweep has not completed.
// Every PL-005 step 8 reconciliation-dispatch path MUST call this before
// invoking any reconciliation detector. The panic message identifies the
// detector name so the failure is attributable in the crash log.
//
// Per PL-018a, any panic is caught by the daemon's top-level recover() barrier
// and causes the daemon to exit with code 19 (runtime-panic).
//
// Spec ref: process-lifecycle.md §5 PL-INV-003 — "every §PL-005 step 8
// reconciliation-dispatch path MUST assert the flag is non-nil before invoking
// any detector. An assertion failure is a panic per PL-018a."
func (s *StartupState) AssertOrphanSweepComplete(detectorName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.orphanSweepCompleteAt == nil {
		panic(fmt.Sprintf(
			"PL-INV-003 violation: orphan_sweep_complete_at is nil when dispatching detector %q; "+
				"orphan sweep (PL-006) MUST complete before reconciliation dispatch (PL-005 step 8)",
			detectorName,
		))
	}
}
