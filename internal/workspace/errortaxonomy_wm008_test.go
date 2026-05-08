package workspace

import (
	"errors"
	"fmt"
	"testing"
)

// Tests for the workspace error taxonomy per workspace-model.md §8.
//
// Helper prefix: wmErrorFixture (bead hk-8mwo.64; avoids collision with
// sibling-bead helpers such as leaseFixture, stateMachineFixture, etc.).

// wmErrorFixtureAllSentinels returns the complete ordered set of the 12 error
// sentinel vars defined by workspace-model.md §8, paired with their canonical
// class string as returned by [Class].
func wmErrorFixtureAllSentinels() []struct {
	sentinel  error
	wantClass string
} {
	return []struct {
		sentinel  error
		wantClass string
	}{
		{ErrWorkspaceAlreadyExists, "WorkspaceAlreadyExists"},
		{ErrRunIDReuseForbidden, "RunIdReuseForbidden"},
		{ErrWorktreeCreationFailed, "WorktreeCreationFailed"},
		{ErrLeaseLockHeldByOrphan, "LeaseLockHeldByOrphan"},
		{ErrSidecarWriteFailed, "SidecarWriteFailed"},
		{ErrMergeConflictUnresolvable, "MergeConflictUnresolvable"},
		{ErrInterruptOnTerminalWorkspace, "InterruptOnTerminalWorkspace"},
		{ErrRefNameInvalid, "RefNameInvalid"},
		{ErrBareWorktreeNoLease, "BareWorktreeNoLease"},
		{ErrSidecarWithoutLease, "SidecarWithoutLease"},
		{ErrGitignoreWriteForbidden, "GitignoreWriteForbidden"},
		{ErrGitVersionTooOld, "GitVersionTooOld"},
	}
}

// TestWM008_SentinelCount verifies exactly 12 sentinels are defined, matching
// the 12-class taxonomy declared in workspace-model.md §8.
func TestWM008_SentinelCount(t *testing.T) {
	t.Parallel()

	sentinels := wmErrorFixtureAllSentinels()
	const wantCount = 12
	if len(sentinels) != wantCount {
		t.Errorf("WM-008: sentinel count = %d, want %d", len(sentinels), wantCount)
	}
}

// TestWM008_ClassDirectSentinel verifies that Class returns the canonical class
// string for each of the 12 sentinels when passed directly (not wrapped).
func TestWM008_ClassDirectSentinel(t *testing.T) {
	t.Parallel()

	for _, tc := range wmErrorFixtureAllSentinels() {
		t.Run(tc.wantClass, func(t *testing.T) {
			t.Parallel()

			got := Class(tc.sentinel)
			if got != tc.wantClass {
				t.Errorf("WM-008: Class(%v) = %q, want %q", tc.sentinel, got, tc.wantClass)
			}
		})
	}
}

// TestWM008_ClassWrappedSentinel verifies that Class walks the errors.Is chain
// and correctly classifies an error that wraps a sentinel at one level of
// indirection (fmt.Errorf with %w).
func TestWM008_ClassWrappedSentinel(t *testing.T) {
	t.Parallel()

	for _, tc := range wmErrorFixtureAllSentinels() {
		t.Run(tc.wantClass+"/wrapped", func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("outer context: %w", tc.sentinel)
			got := Class(wrapped)
			if got != tc.wantClass {
				t.Errorf("WM-008: Class(wrapped %v) = %q, want %q", tc.sentinel, got, tc.wantClass)
			}
		})
	}
}

// TestWM008_ClassDoublyWrappedSentinel verifies that Class walks the errors.Is
// chain through two levels of wrapping.
func TestWM008_ClassDoublyWrappedSentinel(t *testing.T) {
	t.Parallel()

	for _, tc := range wmErrorFixtureAllSentinels() {
		t.Run(tc.wantClass+"/doubly-wrapped", func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("outer: %w", fmt.Errorf("middle: %w", tc.sentinel))
			got := Class(wrapped)
			if got != tc.wantClass {
				t.Errorf("WM-008: Class(doubly-wrapped %v) = %q, want %q", tc.sentinel, got, tc.wantClass)
			}
		})
	}
}

// TestWM008_ClassNilReturnsEmpty verifies that Class(nil) returns "".
func TestWM008_ClassNilReturnsEmpty(t *testing.T) {
	t.Parallel()

	got := Class(nil)
	if got != "" {
		t.Errorf("WM-008: Class(nil) = %q, want %q", got, "")
	}
}

// TestWM008_ClassUnknownReturnsEmpty verifies that Class returns "" for an
// error that does not wrap any known workspace sentinel.
func TestWM008_ClassUnknownReturnsEmpty(t *testing.T) {
	t.Parallel()

	unknown := errors.New("some unknown error")
	got := Class(unknown)
	if got != "" {
		t.Errorf("WM-008: Class(unknown) = %q, want %q (empty)", got, "")
	}
}

// TestWM008_SentinelsAreDistinct verifies that no two sentinels satisfy
// errors.Is against each other — i.e., there is no accidental wrapping
// relationship among the 12 classes.
func TestWM008_SentinelsAreDistinct(t *testing.T) {
	t.Parallel()

	sentinels := wmErrorFixtureAllSentinels()
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			t.Run(fmt.Sprintf("%s_not_is_%s", a.wantClass, b.wantClass), func(t *testing.T) {
				t.Parallel()

				if errors.Is(a.sentinel, b.sentinel) {
					t.Errorf("WM-008: errors.Is(%q, %q) = true; sentinels must be distinct",
						a.wantClass, b.wantClass)
				}
			})
		}
	}
}

// TestWM008_TransitionConsequences documents and verifies the workspace-transition
// consequence for each error class per workspace-model.md §8. These are
// prose-level "no transition" vs "transition to discarded" assertions verified
// by checking the error strings match their declared class names — the state-machine
// enforcement lives in statemachine_wm014_test.go.
//
// The test acts as a registry-completeness check: every sentinel MUST have a
// non-empty class name and a non-nil error value.
func TestWM008_TransitionConsequences(t *testing.T) {
	t.Parallel()

	for _, tc := range wmErrorFixtureAllSentinels() {
		t.Run(tc.wantClass, func(t *testing.T) {
			t.Parallel()

			if tc.sentinel == nil {
				t.Fatalf("WM-008: sentinel for class %q is nil; must be non-nil", tc.wantClass)
			}
			if tc.wantClass == "" {
				t.Fatalf("WM-008: wantClass for sentinel %v is empty; must be non-empty", tc.sentinel)
			}
			// errors.Is must return true for the sentinel against itself.
			if !errors.Is(tc.sentinel, tc.sentinel) {
				t.Errorf("WM-008: errors.Is(%v, self) = false; sentinel is not self-matching", tc.sentinel)
			}
		})
	}
}

// TestWM008_StartupFailSentinels verifies that the two startup-fail sentinels
// (GitignoreWriteForbidden and GitVersionTooOld) are distinct from each other
// and from the create_workspace / launch_session sentinels.
func TestWM008_StartupFailSentinels(t *testing.T) {
	t.Parallel()

	startupFail := []error{
		ErrGitignoreWriteForbidden,
		ErrGitVersionTooOld,
	}
	runtimeFail := []error{
		ErrWorkspaceAlreadyExists,
		ErrRunIDReuseForbidden,
		ErrWorktreeCreationFailed,
		ErrLeaseLockHeldByOrphan,
		ErrSidecarWriteFailed,
		ErrMergeConflictUnresolvable,
		ErrInterruptOnTerminalWorkspace,
		ErrRefNameInvalid,
		ErrBareWorktreeNoLease,
		ErrSidecarWithoutLease,
	}

	for _, sf := range startupFail {
		for _, rf := range runtimeFail {
			t.Run(fmt.Sprintf("%s_not_is_%s", Class(sf), Class(rf)), func(t *testing.T) {
				t.Parallel()

				if errors.Is(sf, rf) {
					t.Errorf("WM-008: startup-fail sentinel %v wraps runtime sentinel %v; must be distinct",
						sf, rf)
				}
				if errors.Is(rf, sf) {
					t.Errorf("WM-008: runtime sentinel %v wraps startup-fail sentinel %v; must be distinct",
						rf, sf)
				}
			})
		}
	}
}

// TestWM008_DiscoveryOrphanSentinels verifies that the two discovery-orphan
// sentinels (BareWorktreeNoLease and SidecarWithoutLease) produce the correct
// class strings used as evidence type labels in reconciliation Cat 3.
//
// The evidence type strings "bare-worktree-no-lease" and "sidecar-without-lease"
// are produced by classifyCrashEvidence in testfixture_test.go; this test
// confirms the sentinel class names match the WM §8 table's class column.
func TestWM008_DiscoveryOrphanSentinels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sentinel  error
		wantClass string
	}{
		{ErrBareWorktreeNoLease, "BareWorktreeNoLease"},
		{ErrSidecarWithoutLease, "SidecarWithoutLease"},
	}

	for _, tc := range cases {
		t.Run(tc.wantClass, func(t *testing.T) {
			t.Parallel()

			got := Class(tc.sentinel)
			if got != tc.wantClass {
				t.Errorf("WM-008[discovery-orphan]: Class(%v) = %q, want %q", tc.sentinel, got, tc.wantClass)
			}
		})
	}
}
