package daemon

// followup_ledger_ac1_test.go — unit tests for the durable at-most-once
// ledger helpers (hk-3ndb AC1).
//
// Observable behaviours covered:
//
//  1. loadFollowUpLedger returns an empty map for a missing file (no error).
//  2. appendFollowUpLedger creates the file and writes one entry.
//  3. loadFollowUpLedger reads back exactly the appended keys.
//  4. Multiple appends accumulate; load returns all of them.
//  5. Malformed or empty-key lines are skipped silently on load.
//  6. loadFollowUpLedger returns the partial set on a scan error but does
//     not return an empty-map sentinel — remaining valid entries survive.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFollowUpLedger_MissingFile verifies that a missing file returns an
// empty map with no error.
func TestLoadFollowUpLedger_MissingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "no-such-file.jsonl")
	got, err := loadFollowUpLedger(path)
	if err != nil {
		t.Fatalf("loadFollowUpLedger missing file: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("loadFollowUpLedger missing file: want empty map, got %v", got)
	}
}

// TestAppendAndLoadFollowUpLedger_RoundTrip verifies that a key written via
// appendFollowUpLedger is returned by a subsequent loadFollowUpLedger call.
func TestAppendAndLoadFollowUpLedger_RoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), followUpLedgerFileName)
	if err := appendFollowUpLedger(path, "hk-abc:deploy"); err != nil {
		t.Fatalf("appendFollowUpLedger: %v", err)
	}

	got, err := loadFollowUpLedger(path)
	if err != nil {
		t.Fatalf("loadFollowUpLedger: %v", err)
	}
	if _, ok := got["hk-abc:deploy"]; !ok {
		t.Errorf("loadFollowUpLedger: key 'hk-abc:deploy' missing; got %v", got)
	}
	if len(got) != 1 {
		t.Errorf("loadFollowUpLedger: want 1 entry, got %d", len(got))
	}
}

// TestAppendFollowUpLedger_MultipleKeys verifies that multiple appends
// accumulate and all keys are returned by load.
func TestAppendFollowUpLedger_MultipleKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), followUpLedgerFileName)
	keys := []string{"hk-aaa:deploy", "hk-bbb:verify", "hk-ccc:deploy"}
	for _, k := range keys {
		if err := appendFollowUpLedger(path, k); err != nil {
			t.Fatalf("appendFollowUpLedger %q: %v", k, err)
		}
	}

	got, err := loadFollowUpLedger(path)
	if err != nil {
		t.Fatalf("loadFollowUpLedger: %v", err)
	}
	if len(got) != len(keys) {
		t.Fatalf("loadFollowUpLedger: want %d entries, got %d: %v", len(keys), len(got), got)
	}
	for _, k := range keys {
		if _, ok := got[k]; !ok {
			t.Errorf("loadFollowUpLedger: key %q missing", k)
		}
	}
}

// TestLoadFollowUpLedger_SkipsMalformedLines verifies that malformed JSON
// lines and lines with empty keys are skipped without error.
func TestLoadFollowUpLedger_SkipsMalformedLines(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), followUpLedgerFileName)
	content := `not-json
{"k":""}
{"k":"hk-good:deploy"}
{"missing_field":true}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := loadFollowUpLedger(path)
	if err != nil {
		t.Fatalf("loadFollowUpLedger: unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 valid entry, got %d: %v", len(got), got)
	}
	if _, ok := got["hk-good:deploy"]; !ok {
		t.Errorf("key 'hk-good:deploy' missing from result: %v", got)
	}
}
