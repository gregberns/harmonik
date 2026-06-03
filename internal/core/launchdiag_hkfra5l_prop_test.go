package core

// Property tests for the Valid() methods in launchdiag_hkfra5l.go.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Bead ref: hk-z02yj (part of hk-j3hrn core coverage uplift).

import (
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// PasteInjectFailedPayload
// ---------------------------------------------------------------------------

func TestProp_PasteInjectFailedPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := PasteInjectFailedPayload{
			RunID:  rapid.StringN(1, 64, -1).Draw(rt, "run_id"),
			Phase:  rapid.StringN(1, 64, -1).Draw(rt, "phase"),
			Reason: rapid.StringN(1, 128, -1).Draw(rt, "reason"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated PasteInjectFailedPayload, want true")
		}
	})
}

func TestProp_PasteInjectFailedPayload_Valid_AcceptsEmptyPhase(t *testing.T) {
	// Phase is optional (single-mode path uses empty string).
	rapid.Check(t, func(rt *rapid.T) {
		p := PasteInjectFailedPayload{
			RunID:  rapid.StringN(1, 64, -1).Draw(rt, "run_id"),
			Phase:  "",
			Reason: rapid.StringN(1, 128, -1).Draw(rt, "reason"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false with empty Phase (optional), want true")
		}
	})
}

func TestProp_PasteInjectFailedPayload_Valid_RejectsEmptyRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := PasteInjectFailedPayload{
			RunID:  "",
			Phase:  rapid.StringN(0, 64, -1).Draw(rt, "phase"),
			Reason: rapid.StringN(1, 128, -1).Draw(rt, "reason"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty RunID, want false")
		}
	})
}

func TestProp_PasteInjectFailedPayload_Valid_RejectsEmptyReason(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := PasteInjectFailedPayload{
			RunID:  rapid.StringN(1, 64, -1).Draw(rt, "run_id"),
			Phase:  rapid.StringN(0, 64, -1).Draw(rt, "phase"),
			Reason: "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty Reason, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// LaunchStallDetectedPayload
// ---------------------------------------------------------------------------

func TestProp_LaunchStallDetectedPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := LaunchStallDetectedPayload{
			RunID:        rapid.StringN(1, 64, -1).Draw(rt, "run_id"),
			BeadID:       rapid.StringN(1, 64, -1).Draw(rt, "bead_id"),
			StallSeconds: rapid.Int64Range(1, 3600).Draw(rt, "stall_seconds"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated LaunchStallDetectedPayload, want true")
		}
	})
}

func TestProp_LaunchStallDetectedPayload_Valid_RejectsEmptyRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := LaunchStallDetectedPayload{
			RunID:        "",
			BeadID:       rapid.StringN(1, 64, -1).Draw(rt, "bead_id"),
			StallSeconds: rapid.Int64Range(1, 3600).Draw(rt, "stall_seconds"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty RunID, want false")
		}
	})
}

func TestProp_LaunchStallDetectedPayload_Valid_RejectsEmptyBeadID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := LaunchStallDetectedPayload{
			RunID:        rapid.StringN(1, 64, -1).Draw(rt, "run_id"),
			BeadID:       "",
			StallSeconds: rapid.Int64Range(1, 3600).Draw(rt, "stall_seconds"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty BeadID, want false")
		}
	})
}

func TestProp_LaunchStallDetectedPayload_Valid_RejectsZeroStallSeconds(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := LaunchStallDetectedPayload{
			RunID:        rapid.StringN(1, 64, -1).Draw(rt, "run_id"),
			BeadID:       rapid.StringN(1, 64, -1).Draw(rt, "bead_id"),
			StallSeconds: rapid.Int64Range(-100, 0).Draw(rt, "stall_seconds"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with StallSeconds <= 0, want false")
		}
	})
}
