package operatornfr_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// onFixtureTempHarmonikDir creates a temporary .harmonik directory for ON-020a tests.
func onFixtureTempHarmonikDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("onFixtureTempHarmonikDir: MkdirAll: %v", err)
	}
	return harmonikDir
}

// TestON020a_UpgradingMarker_Valid verifies that a well-formed UpgradingMarker
// reports Valid() = true.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — three required fields.
func TestON020a_UpgradingMarker_Valid(t *testing.T) {
	t.Parallel()

	m := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "abc123",
		InitiatedAt:        "2026-05-09T12:00:00Z",
		SessionID:          "sess-001",
	}
	if !m.Valid() {
		t.Errorf("ON-020a: well-formed UpgradingMarker.Valid() = false, want true; marker = %+v", m)
	}
}

// TestON020a_UpgradingMarker_InvalidWhenFieldsEmpty verifies that missing
// any of the three ON-020a required fields makes Valid() = false.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — "(a) expected_commit_hash; (b)
// upgrade-initiation timestamp; (c) operator's session_id."
func TestON020a_UpgradingMarker_InvalidWhenFieldsEmpty(t *testing.T) {
	t.Parallel()

	full := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "abc123",
		InitiatedAt:        "2026-05-09T12:00:00Z",
		SessionID:          "sess-001",
	}

	cases := []struct {
		name   string
		marker operatornfr.UpgradingMarker
	}{
		{"empty-hash", operatornfr.UpgradingMarker{InitiatedAt: full.InitiatedAt, SessionID: full.SessionID}},
		{"empty-initiated-at", operatornfr.UpgradingMarker{ExpectedCommitHash: full.ExpectedCommitHash, SessionID: full.SessionID}},
		{"empty-session-id", operatornfr.UpgradingMarker{ExpectedCommitHash: full.ExpectedCommitHash, InitiatedAt: full.InitiatedAt}},
		{"zero-value", operatornfr.UpgradingMarker{}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.marker.Valid() {
				t.Errorf("ON-020a: %s: UpgradingMarker.Valid() = true, want false; marker = %+v", tc.name, tc.marker)
			}
		})
	}
}

// TestON020a_BuildUpgradingMarker_ContainsAllFields verifies that
// BuildUpgradingMarker populates all three ON-020a required fields.
//
// Spec ref: operator-nfr.md §4.6 ON-020a.
func TestON020a_BuildUpgradingMarker_ContainsAllFields(t *testing.T) {
	t.Parallel()

	const hash = "abc123def456"
	const sess = "session-42"
	before := time.Now().UTC()
	m := operatornfr.BuildUpgradingMarker(hash, sess)
	after := time.Now().UTC()

	if m.ExpectedCommitHash != hash {
		t.Errorf("ON-020a BuildUpgradingMarker: ExpectedCommitHash = %q, want %q", m.ExpectedCommitHash, hash)
	}
	if m.SessionID != sess {
		t.Errorf("ON-020a BuildUpgradingMarker: SessionID = %q, want %q", m.SessionID, sess)
	}
	if m.InitiatedAt == "" {
		t.Error("ON-020a BuildUpgradingMarker: InitiatedAt is empty")
	}

	parsed, err := time.Parse(time.RFC3339Nano, m.InitiatedAt)
	if err != nil {
		t.Fatalf("ON-020a BuildUpgradingMarker: InitiatedAt %q is not RFC3339: %v", m.InitiatedAt, err)
	}
	if parsed.Before(before) || parsed.After(after) {
		t.Errorf("ON-020a BuildUpgradingMarker: InitiatedAt %v not in [%v, %v]", parsed, before, after)
	}
}

// TestON020a_WriteUpgradingMarker_FileExists verifies that WriteUpgradingMarker
// creates the daemon.upgrading file in the .harmonik directory.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — "atomically write
// `.harmonik/daemon.upgrading`."
func TestON020a_WriteUpgradingMarker_FileExists(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)
	m := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "abc123",
		InitiatedAt:        "2026-05-09T12:00:00Z",
		SessionID:          "sess-001",
	}

	if err := operatornfr.WriteUpgradingMarker(harmonikDir, m); err != nil {
		t.Fatalf("ON-020a WriteUpgradingMarker: unexpected error: %v", err)
	}

	markerPath := filepath.Join(harmonikDir, operatornfr.UpgradingMarkerName)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Errorf("ON-020a WriteUpgradingMarker: marker file not present at %s", markerPath)
	}
}

// TestON020a_WriteUpgradingMarker_ContentContainsAllFields verifies that the
// written marker JSON contains all three ON-020a required fields.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — "(a) expected_commit_hash; (b)
// initiated_at; (c) session_id."
func TestON020a_WriteUpgradingMarker_ContentContainsAllFields(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)
	m := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "abc123sha1hash",
		InitiatedAt:        "2026-05-09T12:00:00Z",
		SessionID:          "sess-xyz",
	}

	if err := operatornfr.WriteUpgradingMarker(harmonikDir, m); err != nil {
		t.Fatalf("ON-020a WriteUpgradingMarker content: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(harmonikDir, operatornfr.UpgradingMarkerName))
	if err != nil {
		t.Fatalf("ON-020a WriteUpgradingMarker content: ReadFile: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "expected_commit_hash") {
		t.Error("ON-020a WriteUpgradingMarker: JSON missing expected_commit_hash field")
	}
	if !strings.Contains(content, m.ExpectedCommitHash) {
		t.Errorf("ON-020a WriteUpgradingMarker: JSON missing hash value %q", m.ExpectedCommitHash)
	}
	if !strings.Contains(content, "initiated_at") {
		t.Error("ON-020a WriteUpgradingMarker: JSON missing initiated_at field")
	}
	if !strings.Contains(content, "session_id") {
		t.Error("ON-020a WriteUpgradingMarker: JSON missing session_id field")
	}
	if !strings.Contains(content, m.SessionID) {
		t.Errorf("ON-020a WriteUpgradingMarker: JSON missing session ID value %q", m.SessionID)
	}
}

// TestON020a_WriteUpgradingMarker_NoTempFileAfterWrite verifies that the
// temp file is cleaned up after the atomic write succeeds.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — temp+rename discipline.
func TestON020a_WriteUpgradingMarker_NoTempFileAfterWrite(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)
	m := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "abc123",
		InitiatedAt:        "2026-05-09T12:00:00Z",
		SessionID:          "sess-001",
	}

	if err := operatornfr.WriteUpgradingMarker(harmonikDir, m); err != nil {
		t.Fatalf("ON-020a WriteUpgradingMarker no-temp: %v", err)
	}

	// No temp file (*.tmp-*) should remain.
	entries, err := os.ReadDir(harmonikDir)
	if err != nil {
		t.Fatalf("ON-020a WriteUpgradingMarker no-temp: ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Errorf("ON-020a WriteUpgradingMarker: temp file %q still present after atomic write", entry.Name())
		}
	}
}

// TestON020a_ReadUpgradingMarker_RoundTrip verifies that WriteUpgradingMarker +
// ReadUpgradingMarker preserves all three fields.
//
// Spec ref: operator-nfr.md §4.6 ON-020a.
func TestON020a_ReadUpgradingMarker_RoundTrip(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)
	want := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "abc123sha1",
		InitiatedAt:        "2026-05-09T12:00:00Z",
		SessionID:          "sess-round-trip",
	}

	if err := operatornfr.WriteUpgradingMarker(harmonikDir, want); err != nil {
		t.Fatalf("ON-020a ReadUpgradingMarker round-trip write: %v", err)
	}

	got, err := operatornfr.ReadUpgradingMarker(harmonikDir)
	if err != nil {
		t.Fatalf("ON-020a ReadUpgradingMarker round-trip read: %v", err)
	}

	if got.ExpectedCommitHash != want.ExpectedCommitHash {
		t.Errorf("ON-020a round-trip: ExpectedCommitHash = %q, want %q", got.ExpectedCommitHash, want.ExpectedCommitHash)
	}
	if got.InitiatedAt != want.InitiatedAt {
		t.Errorf("ON-020a round-trip: InitiatedAt = %q, want %q", got.InitiatedAt, want.InitiatedAt)
	}
	if got.SessionID != want.SessionID {
		t.Errorf("ON-020a round-trip: SessionID = %q, want %q", got.SessionID, want.SessionID)
	}
}

// TestON020a_ReadUpgradingMarker_AbsentReturnsErrMarkerAbsent verifies that
// ReadUpgradingMarker returns ErrMarkerAbsent when the file does not exist.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — "On daemon startup, PL-005 step 0
// MUST read this marker; if present … if hash does not match …"
func TestON020a_ReadUpgradingMarker_AbsentReturnsErrMarkerAbsent(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)
	// Do not write the marker — it should be absent.

	_, err := operatornfr.ReadUpgradingMarker(harmonikDir)
	if err == nil {
		t.Fatal("ON-020a ReadUpgradingMarker absent: expected error, got nil")
	}
	if !errors.Is(err, operatornfr.ErrMarkerAbsent) {
		t.Errorf("ON-020a ReadUpgradingMarker absent: error = %v, want ErrMarkerAbsent", err)
	}
}

// TestON020a_RemoveUpgradingMarker_RemovesFile verifies that RemoveUpgradingMarker
// removes the marker file.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — "marker removed on clean transition
// to ready."
func TestON020a_RemoveUpgradingMarker_RemovesFile(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)
	m := operatornfr.UpgradingMarker{
		ExpectedCommitHash: "abc123",
		InitiatedAt:        "2026-05-09T12:00:00Z",
		SessionID:          "sess-001",
	}

	if err := operatornfr.WriteUpgradingMarker(harmonikDir, m); err != nil {
		t.Fatalf("ON-020a RemoveUpgradingMarker: write: %v", err)
	}

	if err := operatornfr.RemoveUpgradingMarker(harmonikDir); err != nil {
		t.Fatalf("ON-020a RemoveUpgradingMarker: remove: %v", err)
	}

	markerPath := filepath.Join(harmonikDir, operatornfr.UpgradingMarkerName)
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("ON-020a RemoveUpgradingMarker: marker still present after remove")
	}
}

// TestON020a_RemoveUpgradingMarker_IdempotentWhenAbsent verifies that
// RemoveUpgradingMarker returns nil when the marker does not exist.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — idempotent removal.
func TestON020a_RemoveUpgradingMarker_IdempotentWhenAbsent(t *testing.T) {
	t.Parallel()

	harmonikDir := onFixtureTempHarmonikDir(t)
	// Do not write the marker.

	if err := operatornfr.RemoveUpgradingMarker(harmonikDir); err != nil {
		t.Errorf("ON-020a RemoveUpgradingMarker idempotent: expected nil for absent marker, got: %v", err)
	}
}

// TestON020a_UpgradingMarkerName_IsCorrect verifies the marker filename constant.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — ".harmonik/daemon.upgrading."
func TestON020a_UpgradingMarkerName_IsCorrect(t *testing.T) {
	t.Parallel()

	if operatornfr.UpgradingMarkerName != "daemon.upgrading" {
		t.Errorf("ON-020a: UpgradingMarkerName = %q, want %q",
			operatornfr.UpgradingMarkerName, "daemon.upgrading")
	}
}
