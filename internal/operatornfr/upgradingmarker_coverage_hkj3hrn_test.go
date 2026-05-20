package operatornfr_test

// upgradingmarker_coverage_hkj3hrn_test.go — targeted coverage uplift for
// WriteUpgradingMarker, RemoveUpgradingMarker, and ReadUpgradingMarker error
// branches that were not hit by the existing ON-020a test suite.
//
// Bead: hk-j3hrn (core coverage uplift EPIC, step a).
// Spec: operator-nfr.md §4.6 ON-020a.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// TestON020a_WriteUpgradingMarker_NonexistentDir verifies that
// WriteUpgradingMarker returns an error when the harmonikDir does not exist.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — write must succeed into a live
// .harmonik directory; callers are responsible for creating it first.
func TestON020a_WriteUpgradingMarker_NonexistentDir(t *testing.T) {
	t.Parallel()

	// Point at a path that does not exist.
	nonExistentDir := filepath.Join(t.TempDir(), "no-such-dir", ".harmonik")
	m := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "abc123",
		InitiatedAt:        "2026-05-09T12:00:00Z",
		SessionID:          "sess-001",
	}

	err := operatornfr.WriteUpgradingMarker(nonExistentDir, m)
	if err == nil {
		t.Error("ON-020a WriteUpgradingMarker nonexistent-dir: expected error, got nil")
	}
}

// TestON020a_WriteUpgradingMarker_ExistingFileGetsTruncated verifies that a
// second WriteUpgradingMarker overwrites the existing file with the new contents.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — atomic temp+rename discipline
// ensures overwrite is crash-safe.
func TestON020a_WriteUpgradingMarker_ExistingFileGetsTruncated(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)

	first := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "first-hash",
		InitiatedAt:        "2026-05-09T12:00:00Z",
		SessionID:          "sess-first",
	}
	if err := operatornfr.WriteUpgradingMarker(harmonikDir, first); err != nil {
		t.Fatalf("ON-020a WriteUpgradingMarker overwrite: first write: %v", err)
	}

	second := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "second-hash",
		InitiatedAt:        "2026-05-10T08:00:00Z",
		SessionID:          "sess-second",
	}
	if err := operatornfr.WriteUpgradingMarker(harmonikDir, second); err != nil {
		t.Fatalf("ON-020a WriteUpgradingMarker overwrite: second write: %v", err)
	}

	got, err := operatornfr.ReadUpgradingMarker(harmonikDir)
	if err != nil {
		t.Fatalf("ON-020a WriteUpgradingMarker overwrite: read: %v", err)
	}
	if got.ExpectedCommitHash != second.ExpectedCommitHash {
		t.Errorf("ON-020a WriteUpgradingMarker overwrite: ExpectedCommitHash = %q, want %q",
			got.ExpectedCommitHash, second.ExpectedCommitHash)
	}
	if got.SessionID != second.SessionID {
		t.Errorf("ON-020a WriteUpgradingMarker overwrite: SessionID = %q, want %q",
			got.SessionID, second.SessionID)
	}
}

// TestON020a_RemoveUpgradingMarker_NonexistentParentDir verifies that
// RemoveUpgradingMarker returns an error when harmonikDir itself does not exist
// (the os.Open(parent) call after remove would fail).
//
// Spec ref: operator-nfr.md §4.6 ON-020a — parent-dir fsync after unlink.
func TestON020a_RemoveUpgradingMarker_NonexistentParentDir(t *testing.T) {
	t.Parallel()

	// Use a path that does not exist at all — os.Remove will get IsNotExist
	// (treated as success) but os.Open on the parent dir will fail.
	nonExistentDir := filepath.Join(t.TempDir(), "no-such-harmonik-dir")

	err := operatornfr.RemoveUpgradingMarker(nonExistentDir)
	// The marker is absent (IsNotExist), so os.Remove succeeds.
	// os.Open(nonExistentDir) must then fail because the dir doesn't exist.
	if err == nil {
		t.Error("ON-020a RemoveUpgradingMarker nonexistent-parent: expected error (open parent dir), got nil")
	}
}

// TestON020a_ReadUpgradingMarker_MalformedJSON verifies that ReadUpgradingMarker
// returns an error (not ErrMarkerAbsent) when the file contains invalid JSON.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — parse errors are distinct from
// absent-marker errors.
func TestON020a_ReadUpgradingMarker_MalformedJSON(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)
	markerPath := filepath.Join(harmonikDir, operatornfr.UpgradingMarkerName)

	//nolint:gosec // G306: test file with non-secret content
	if err := os.WriteFile(markerPath, []byte(`{not valid json`), 0o644); err != nil {
		t.Fatalf("ON-020a ReadUpgradingMarker malformed: write fixture: %v", err)
	}

	_, err := operatornfr.ReadUpgradingMarker(harmonikDir)
	if err == nil {
		t.Fatal("ON-020a ReadUpgradingMarker malformed: expected error, got nil")
	}
	if errors.Is(err, operatornfr.ErrMarkerAbsent) {
		t.Errorf("ON-020a ReadUpgradingMarker malformed: got ErrMarkerAbsent, want a parse error")
	}
}

// TestON020a_ReadUpgradingMarker_EmptyFile verifies that ReadUpgradingMarker
// returns a parse error for a zero-byte marker file.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — empty file is not a valid marker.
func TestON020a_ReadUpgradingMarker_EmptyFile(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)
	markerPath := filepath.Join(harmonikDir, operatornfr.UpgradingMarkerName)

	//nolint:gosec // G306: test file with non-secret content
	if err := os.WriteFile(markerPath, []byte{}, 0o644); err != nil {
		t.Fatalf("ON-020a ReadUpgradingMarker empty-file: write fixture: %v", err)
	}

	_, err := operatornfr.ReadUpgradingMarker(harmonikDir)
	if err == nil {
		t.Fatal("ON-020a ReadUpgradingMarker empty-file: expected error, got nil")
	}
	if errors.Is(err, operatornfr.ErrMarkerAbsent) {
		t.Errorf("ON-020a ReadUpgradingMarker empty-file: got ErrMarkerAbsent, want a parse error")
	}
}

// TestON020a_BuildUpgradingMarker_ValidReturnsTrue verifies that the marker
// produced by BuildUpgradingMarker is always Valid() = true.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — BuildUpgradingMarker MUST populate
// all three required fields.
func TestON020a_BuildUpgradingMarker_ValidReturnsTrue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		hash string
		sess string
	}{
		{"typical", "abc123def456", "session-1"},
		{"long-hash", "000000000000000000000000000000000000000000000000000000000000000", "session-long"},
		{"short-hash", "1", "s"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := operatornfr.BuildUpgradingMarker(tc.hash, tc.sess)
			if !m.Valid() {
				t.Errorf("ON-020a BuildUpgradingMarker %s: Valid() = false; hash=%q sess=%q initiated=%q",
					tc.name, m.ExpectedCommitHash, m.SessionID, m.InitiatedAt)
			}
		})
	}
}
