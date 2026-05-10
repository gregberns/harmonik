package lifecycle

import (
	"testing"
	"time"
)

// TestPLINV003_StartupState_MarkAndAssert verifies that MarkOrphanSweepComplete
// followed by AssertOrphanSweepComplete does not panic.
//
// Spec ref: process-lifecycle.md §5 PL-INV-003 — "the daemon maintains an
// in-memory flag orphan_sweep_complete_at: Timestamp."
func TestPLINV003_StartupState_MarkAndAssert(t *testing.T) {
	t.Parallel()

	s := NewStartupState()
	s.MarkOrphanSweepComplete()

	// Must not panic.
	s.AssertOrphanSweepComplete("cat3a-intent-detector")
}

// TestPLINV003_StartupState_AssertPanicsWhenNotMarked verifies that
// AssertOrphanSweepComplete panics when the sweep has not been marked complete.
//
// Spec ref: process-lifecycle.md §5 PL-INV-003 — "An assertion failure is a
// panic per PL-018a."
func TestPLINV003_StartupState_AssertPanicsWhenNotMarked(t *testing.T) {
	t.Parallel()

	s := NewStartupState()

	defer func() {
		r := recover()
		if r == nil {
			t.Error("PL-INV-003: AssertOrphanSweepComplete should have panicked when sweep not marked complete")
		}
	}()

	// Must panic: orphan_sweep_complete_at is nil.
	s.AssertOrphanSweepComplete("startup-recon-dispatch")
}

// TestPLINV003_StartupState_OrphanSweepCompleteAt_NilBeforeMark verifies that
// OrphanSweepCompleteAt returns nil before MarkOrphanSweepComplete is called.
//
// Spec ref: process-lifecycle.md §5 PL-INV-003 — nil flag = sweep not done.
func TestPLINV003_StartupState_OrphanSweepCompleteAt_NilBeforeMark(t *testing.T) {
	t.Parallel()

	s := NewStartupState()
	if s.OrphanSweepCompleteAt() != nil {
		t.Error("PL-INV-003: OrphanSweepCompleteAt should be nil before MarkOrphanSweepComplete")
	}
}

// TestPLINV003_StartupState_OrphanSweepCompleteAt_NonNilAfterMark verifies
// that OrphanSweepCompleteAt returns a non-nil, bracketed timestamp after
// MarkOrphanSweepComplete is called.
//
// Spec ref: process-lifecycle.md §5 PL-INV-003 — "orphan_sweep_complete_at:
// Timestamp" must be set to the completion wall-clock time.
func TestPLINV003_StartupState_OrphanSweepCompleteAt_NonNilAfterMark(t *testing.T) {
	t.Parallel()

	s := NewStartupState()

	before := time.Now()
	s.MarkOrphanSweepComplete()
	after := time.Now()

	ts := s.OrphanSweepCompleteAt()
	if ts == nil {
		t.Fatal("PL-INV-003: OrphanSweepCompleteAt is nil after MarkOrphanSweepComplete")
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("PL-INV-003: OrphanSweepCompleteAt = %v, want in [%v, %v]", *ts, before, after)
	}
}

// TestPLINV003_StartupState_PanicMessageContainsDetectorName verifies that the
// panic message identifies the detector name to aid in crash attribution.
//
// Spec ref: process-lifecycle.md §5 PL-INV-003 — panic per PL-018a, caught by
// top-level recover() barrier.
func TestPLINV003_StartupState_PanicMessageContainsDetectorName(t *testing.T) {
	t.Parallel()

	s := NewStartupState()
	const detectorName = "cat4-checkpoint-detector"

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("PL-INV-003: expected panic, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("PL-INV-003: panic value is %T, want string", r)
		}
		if msg == "" {
			t.Error("PL-INV-003: panic message is empty")
		}
		// Panic message must identify the detector name for crash attribution.
		if len(msg) > 0 {
			found := false
			for i := 0; i <= len(msg)-len(detectorName); i++ {
				if msg[i:i+len(detectorName)] == detectorName {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("PL-INV-003: panic message %q does not contain detector name %q", msg, detectorName)
			}
		}
	}()

	s.AssertOrphanSweepComplete(detectorName)
}

// TestPLINV003_StartupState_MarkIsIdempotent verifies that calling
// MarkOrphanSweepComplete twice does not panic and leaves the flag non-nil.
func TestPLINV003_StartupState_MarkIsIdempotent(t *testing.T) {
	t.Parallel()

	s := NewStartupState()
	s.MarkOrphanSweepComplete()
	s.MarkOrphanSweepComplete() // second call must not panic

	if s.OrphanSweepCompleteAt() == nil {
		t.Error("PL-INV-003: OrphanSweepCompleteAt is nil after two MarkOrphanSweepComplete calls")
	}
}

// TestPLINV003_StartupState_NewReturnsUnmarked verifies that NewStartupState
// returns a StartupState with the sweep flag unset.
func TestPLINV003_StartupState_NewReturnsUnmarked(t *testing.T) {
	t.Parallel()

	s := NewStartupState()
	if s.OrphanSweepCompleteAt() != nil {
		t.Error("PL-INV-003: NewStartupState should return an unmarked state")
	}
}

// TestPLINV003_StartupState_MultipleAssertAfterMark verifies that
// AssertOrphanSweepComplete can be called multiple times after marking,
// each time without panicking (all detector dispatches on step 8 pass).
func TestPLINV003_StartupState_MultipleAssertAfterMark(t *testing.T) {
	t.Parallel()

	s := NewStartupState()
	s.MarkOrphanSweepComplete()

	detectors := []string{
		"cat1-auto-resolve",
		"cat3a-intent",
		"cat3b-recon-lock",
		"cat4-checkpoint",
		"cat6-crash-recovery",
	}

	for _, d := range detectors {
		d := d
		// Must not panic.
		s.AssertOrphanSweepComplete(d)
		_ = d // suppress lint
	}
}
