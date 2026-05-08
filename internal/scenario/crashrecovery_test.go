package scenario

import (
	"encoding/json"
	"reflect"
	"testing"
)

// crashRecoveryFixtureAfterWriteTree returns a minimally valid
// CrashRecoveryFixture for the AfterWriteTree crash point with its canonical
// invariant set.
func crashRecoveryFixtureAfterWriteTree(t *testing.T) CrashRecoveryFixture {
	t.Helper()
	return CrashRecoveryFixture{
		CrashAt:     CrashPointAfterWriteTree,
		Invariants:  canonicalInvariantsFor(CrashPointAfterWriteTree),
		Description: "crash after write-tree; before update-ref",
	}
}

// crashRecoveryFixtureAfterUpdateRef returns a minimally valid
// CrashRecoveryFixture for the AfterUpdateRef crash point with its canonical
// invariant set.
func crashRecoveryFixtureAfterUpdateRef(t *testing.T) CrashRecoveryFixture {
	t.Helper()
	return CrashRecoveryFixture{
		CrashAt:     CrashPointAfterUpdateRef,
		Invariants:  canonicalInvariantsFor(CrashPointAfterUpdateRef),
		Description: "crash after update-ref; before event emission",
	}
}

// crashRecoveryFixtureENOSPC returns a minimally valid CrashRecoveryFixture
// for the DuringCommitTreeENOSPC crash point with its canonical invariant set.
func crashRecoveryFixtureENOSPC(t *testing.T) CrashRecoveryFixture {
	t.Helper()
	return CrashRecoveryFixture{
		CrashAt:     CrashPointDuringCommitTreeENOSPC,
		Invariants:  canonicalInvariantsFor(CrashPointDuringCommitTreeENOSPC),
		Description: "ENOSPC during commit-tree; before update-ref",
	}
}

// ---------------------------------------------------------------------------
// CrashPoint tests
// ---------------------------------------------------------------------------

func TestCrashPointValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CrashPoint
		want  bool
	}{
		{name: "after_write_tree", input: CrashPointAfterWriteTree, want: true},
		{name: "after_update_ref", input: CrashPointAfterUpdateRef, want: true},
		{name: "during_commit_tree_enospc", input: CrashPointDuringCommitTreeENOSPC, want: true},
		{name: "empty", input: CrashPoint(""), want: false},
		{name: "arbitrary", input: CrashPoint("before_commit"), want: false},
		{name: "uppercase", input: CrashPoint("AFTER_WRITE_TREE"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("CrashPoint(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestCrashPointMarshalText(t *testing.T) {
	t.Parallel()

	validPoints := []CrashPoint{
		CrashPointAfterWriteTree,
		CrashPointAfterUpdateRef,
		CrashPointDuringCommitTreeENOSPC,
	}

	for _, p := range validPoints {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()

			text, err := p.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText(%q) error: %v", string(p), err)
			}
			if string(text) != string(p) {
				t.Errorf("MarshalText(%q) = %q, want %q", string(p), string(text), string(p))
			}

			var got CrashPoint
			if err := got.UnmarshalText(text); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", string(text), err)
			}
			if got != p {
				t.Errorf("round-trip mismatch: got %q, want %q", string(got), string(p))
			}
		})
	}

	t.Run("unknown rejects marshal", func(t *testing.T) {
		t.Parallel()
		unknown := CrashPoint("unknown-point")
		if _, err := unknown.MarshalText(); err == nil {
			t.Errorf("MarshalText on unknown value %q should return error", string(unknown))
		}
	})

	t.Run("unknown rejects unmarshal", func(t *testing.T) {
		t.Parallel()
		var p CrashPoint
		if err := p.UnmarshalText([]byte("unknown-point")); err == nil {
			t.Errorf("UnmarshalText on unknown value should return error")
		}
	})
}

func TestCrashPointJSONRoundTrip(t *testing.T) {
	t.Parallel()

	// CrashPoint is embedded in CrashRecoveryFixture. Test via the fixture.
	for _, p := range []CrashPoint{
		CrashPointAfterWriteTree,
		CrashPointAfterUpdateRef,
		CrashPointDuringCommitTreeENOSPC,
	} {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()

			input := CrashRecoveryFixture{
				CrashAt:     p,
				Invariants:  canonicalInvariantsFor(p),
				Description: "round-trip test",
			}

			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got CrashRecoveryFixture
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(input, got) {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", input, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CrashRecoveryInvariant tests
// ---------------------------------------------------------------------------

func TestCrashRecoveryInvariantValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CrashRecoveryInvariant
		want  bool
	}{
		{
			name:  "no_observable_partial_state",
			input: InvariantNoObservablePartialState,
			want:  true,
		},
		{
			name:  "orphan_loose_objects_eligible_for_gc",
			input: InvariantOrphanLooseObjectsEligibleForGC,
			want:  true,
		},
		{
			name:  "reconciliation_dispatched",
			input: InvariantReconciliationDispatched,
			want:  true,
		},
		{
			name:  "new_transition_id_on_retry",
			input: InvariantNewTransitionIDOnRetry,
			want:  true,
		},
		{
			name:  "branch_tip_monotonicity",
			input: InvariantBranchTipMonotonicity,
			want:  true,
		},
		{
			name:  "state_reconstructable_from_git_and_beads",
			input: InvariantStateReconstructableFromGitAndBeads,
			want:  true,
		},
		{name: "empty", input: CrashRecoveryInvariant(""), want: false},
		{name: "arbitrary", input: CrashRecoveryInvariant("unknown_invariant"), want: false},
		{name: "uppercase", input: CrashRecoveryInvariant("NO_OBSERVABLE_PARTIAL_STATE"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("CrashRecoveryInvariant(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestCrashRecoveryInvariantMarshalText(t *testing.T) {
	t.Parallel()

	allInvariants := []CrashRecoveryInvariant{
		InvariantNoObservablePartialState,
		InvariantOrphanLooseObjectsEligibleForGC,
		InvariantReconciliationDispatched,
		InvariantNewTransitionIDOnRetry,
		InvariantBranchTipMonotonicity,
		InvariantStateReconstructableFromGitAndBeads,
	}

	for _, inv := range allInvariants {
		t.Run(string(inv), func(t *testing.T) {
			t.Parallel()

			text, err := inv.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText(%q) error: %v", string(inv), err)
			}
			if string(text) != string(inv) {
				t.Errorf("MarshalText(%q) = %q, want %q", string(inv), string(text), string(inv))
			}

			var got CrashRecoveryInvariant
			if err := got.UnmarshalText(text); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", string(text), err)
			}
			if got != inv {
				t.Errorf("round-trip mismatch: got %q, want %q", string(got), string(inv))
			}
		})
	}

	t.Run("unknown rejects marshal", func(t *testing.T) {
		t.Parallel()
		unknown := CrashRecoveryInvariant("bad_invariant")
		if _, err := unknown.MarshalText(); err == nil {
			t.Errorf("MarshalText on unknown value %q should return error", string(unknown))
		}
	})

	t.Run("unknown rejects unmarshal", func(t *testing.T) {
		t.Parallel()
		var inv CrashRecoveryInvariant
		if err := inv.UnmarshalText([]byte("bad_invariant")); err == nil {
			t.Errorf("UnmarshalText on unknown value should return error")
		}
	})
}

// ---------------------------------------------------------------------------
// CrashRecoveryFixture.Valid tests
// ---------------------------------------------------------------------------

func TestCrashRecoveryFixtureValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CrashRecoveryFixture
		want  bool
	}{
		{
			name:  "valid: after_write_tree canonical invariants",
			input: crashRecoveryFixtureAfterWriteTree(t),
			want:  true,
		},
		{
			name:  "valid: after_update_ref canonical invariants",
			input: crashRecoveryFixtureAfterUpdateRef(t),
			want:  true,
		},
		{
			name:  "valid: enospc canonical invariants",
			input: crashRecoveryFixtureENOSPC(t),
			want:  true,
		},
		{
			name: "valid: single invariant",
			input: CrashRecoveryFixture{
				CrashAt:     CrashPointAfterWriteTree,
				Invariants:  []CrashRecoveryInvariant{InvariantNoObservablePartialState},
				Description: "single invariant is sufficient",
			},
			want: true,
		},
		{
			name: "invalid: unknown CrashAt",
			input: CrashRecoveryFixture{
				CrashAt:     CrashPoint("unknown"),
				Invariants:  []CrashRecoveryInvariant{InvariantNoObservablePartialState},
				Description: "bad crash point",
			},
			want: false,
		},
		{
			name: "invalid: empty Description",
			input: CrashRecoveryFixture{
				CrashAt:    CrashPointAfterWriteTree,
				Invariants: []CrashRecoveryInvariant{InvariantNoObservablePartialState},
			},
			want: false,
		},
		{
			name: "invalid: empty Invariants slice",
			input: CrashRecoveryFixture{
				CrashAt:     CrashPointAfterWriteTree,
				Invariants:  []CrashRecoveryInvariant{},
				Description: "no invariants declared",
			},
			want: false,
		},
		{
			name: "invalid: nil Invariants",
			input: CrashRecoveryFixture{
				CrashAt:     CrashPointAfterWriteTree,
				Invariants:  nil,
				Description: "nil invariants",
			},
			want: false,
		},
		{
			name: "invalid: unknown invariant in slice",
			input: CrashRecoveryFixture{
				CrashAt:     CrashPointAfterWriteTree,
				Invariants:  []CrashRecoveryInvariant{InvariantNoObservablePartialState, CrashRecoveryInvariant("unknown")},
				Description: "bad invariant value",
			},
			want: false,
		},
		{
			name: "invalid: duplicate invariant",
			input: CrashRecoveryFixture{
				CrashAt: CrashPointAfterWriteTree,
				Invariants: []CrashRecoveryInvariant{
					InvariantNoObservablePartialState,
					InvariantNoObservablePartialState,
				},
				Description: "duplicate invariant",
			},
			want: false,
		},
		{
			name:  "invalid: zero value",
			input: CrashRecoveryFixture{},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("CrashRecoveryFixture.Valid() = %v, want %v (fixture: %+v)", got, tc.want, tc.input)
			}
		})
	}
}

func TestCrashRecoveryFixtureJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CrashRecoveryFixture
	}{
		{name: "after_write_tree", input: crashRecoveryFixtureAfterWriteTree(t)},
		{name: "after_update_ref", input: crashRecoveryFixtureAfterUpdateRef(t)},
		{name: "enospc", input: crashRecoveryFixtureENOSPC(t)},
		{
			name: "single invariant",
			input: CrashRecoveryFixture{
				CrashAt:     CrashPointAfterUpdateRef,
				Invariants:  []CrashRecoveryInvariant{InvariantReconciliationDispatched},
				Description: "minimal fixture for reconciliation check",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got CrashRecoveryFixture
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(tc.input, got) {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", tc.input, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// canonicalInvariantsFor tests
// ---------------------------------------------------------------------------

// TestCanonicalInvariantsFor verifies the mapping between each CrashPoint and
// its canonical invariant set per the bead hk-b3f.87 specification.
func TestCanonicalInvariantsFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       CrashPoint
		wantLen     int
		wantContain []CrashRecoveryInvariant
		wantAbsent  []CrashRecoveryInvariant
	}{
		{
			name:    "after_write_tree: four invariants, no reconciliation, no retry",
			input:   CrashPointAfterWriteTree,
			wantLen: 4,
			wantContain: []CrashRecoveryInvariant{
				InvariantNoObservablePartialState,
				InvariantOrphanLooseObjectsEligibleForGC,
				InvariantBranchTipMonotonicity,
				InvariantStateReconstructableFromGitAndBeads,
			},
			wantAbsent: []CrashRecoveryInvariant{
				InvariantReconciliationDispatched,
				InvariantNewTransitionIDOnRetry,
			},
		},
		{
			name:    "after_update_ref: four invariants, reconciliation, no orphan/retry",
			input:   CrashPointAfterUpdateRef,
			wantLen: 4,
			wantContain: []CrashRecoveryInvariant{
				InvariantNoObservablePartialState,
				InvariantReconciliationDispatched,
				InvariantBranchTipMonotonicity,
				InvariantStateReconstructableFromGitAndBeads,
			},
			wantAbsent: []CrashRecoveryInvariant{
				InvariantOrphanLooseObjectsEligibleForGC,
				InvariantNewTransitionIDOnRetry,
			},
		},
		{
			name:    "enospc: five invariants, all except reconciliation",
			input:   CrashPointDuringCommitTreeENOSPC,
			wantLen: 5,
			wantContain: []CrashRecoveryInvariant{
				InvariantNoObservablePartialState,
				InvariantOrphanLooseObjectsEligibleForGC,
				InvariantNewTransitionIDOnRetry,
				InvariantBranchTipMonotonicity,
				InvariantStateReconstructableFromGitAndBeads,
			},
			wantAbsent: []CrashRecoveryInvariant{
				InvariantReconciliationDispatched,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := canonicalInvariantsFor(tc.input)

			if len(got) != tc.wantLen {
				t.Errorf("canonicalInvariantsFor(%q) len = %d, want %d; got %v", string(tc.input), len(got), tc.wantLen, got)
			}

			// Build a set for membership checks.
			gotSet := make(map[CrashRecoveryInvariant]struct{}, len(got))
			for _, inv := range got {
				gotSet[inv] = struct{}{}
			}

			for _, want := range tc.wantContain {
				if _, ok := gotSet[want]; !ok {
					t.Errorf("canonicalInvariantsFor(%q): missing expected invariant %q", string(tc.input), string(want))
				}
			}

			for _, absent := range tc.wantAbsent {
				if _, ok := gotSet[absent]; ok {
					t.Errorf("canonicalInvariantsFor(%q): unexpected invariant %q present", string(tc.input), string(absent))
				}
			}
		})
	}

	t.Run("unknown crash point returns nil", func(t *testing.T) {
		t.Parallel()
		if got := canonicalInvariantsFor(CrashPoint("unknown")); got != nil {
			t.Errorf("canonicalInvariantsFor(unknown) = %v, want nil", got)
		}
	})
}

// TestCanonicalInvariantsForProducesValidFixture verifies that every
// canonical set produces a Valid CrashRecoveryFixture when composed.
func TestCanonicalInvariantsForProducesValidFixture(t *testing.T) {
	t.Parallel()

	for _, p := range []CrashPoint{
		CrashPointAfterWriteTree,
		CrashPointAfterUpdateRef,
		CrashPointDuringCommitTreeENOSPC,
	} {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()

			f := CrashRecoveryFixture{
				CrashAt:     p,
				Invariants:  canonicalInvariantsFor(p),
				Description: "canonical fixture for " + string(p),
			}

			if !f.Valid() {
				t.Errorf("CrashRecoveryFixture{CrashAt: %q, canonical invariants}.Valid() = false, want true", string(p))
			}
		})
	}
}

// TestCanonicalInvariantsForNoDuplicates verifies that each canonical set
// contains no duplicate invariants (which would fail CrashRecoveryFixture.Valid).
func TestCanonicalInvariantsForNoDuplicates(t *testing.T) {
	t.Parallel()

	for _, p := range []CrashPoint{
		CrashPointAfterWriteTree,
		CrashPointAfterUpdateRef,
		CrashPointDuringCommitTreeENOSPC,
	} {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()

			invariants := canonicalInvariantsFor(p)
			seen := make(map[CrashRecoveryInvariant]struct{}, len(invariants))
			for _, inv := range invariants {
				if _, dup := seen[inv]; dup {
					t.Errorf("canonicalInvariantsFor(%q) contains duplicate invariant %q", string(p), string(inv))
				}
				seen[inv] = struct{}{}
			}
		})
	}
}
