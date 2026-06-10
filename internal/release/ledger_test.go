package release_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/release"
)

// fixture returns a minimal ledger with one pre-release entry for v0.2.0.
func fixture() []release.ReleaseEntry {
	return []release.ReleaseEntry{
		{
			Semver:     "v0.2.0",
			CommitHash: "aabbccdd" + "0000000000000000000000000000000000000000"[:32],
			Tag:        "v0.2.0",
			Prerelease: true,
		},
	}
}

// certifiedFixture returns a ledger where v0.2.0 has already been certified.
func certifiedFixture() []release.ReleaseEntry {
	entries := fixture()
	var err error
	entries, err = release.Certify(entries, "v0.2.0", "2026-06-10T12:00:00Z")
	if err != nil {
		panic("certifiedFixture: " + err.Error())
	}
	return entries
}

// --- Certify tests ---

func TestCertify_UncertifiedToStable(t *testing.T) {
	entries := fixture()
	result, err := release.Certify(entries, "v0.2.0", "2026-06-10T12:00:00Z")
	if err != nil {
		t.Fatalf("Certify: unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("Certify: want 1 entry, got %d", len(result))
	}
	e := result[0]
	if e.Prerelease {
		t.Error("Certify: expected Prerelease=false after certify")
	}
	if e.CertifiedAt != "2026-06-10T12:00:00Z" {
		t.Errorf("Certify: CertifiedAt=%q, want %q", e.CertifiedAt, "2026-06-10T12:00:00Z")
	}
}

func TestCertify_DoubleCertifyRejected(t *testing.T) {
	entries := certifiedFixture()
	_, err := release.Certify(entries, "v0.2.0", "2026-06-10T13:00:00Z")
	if !errors.Is(err, release.ErrAlreadyCertified) {
		t.Errorf("double-certify: want ErrAlreadyCertified, got %v", err)
	}
}

func TestCertify_AfterYankRejected(t *testing.T) {
	entries := certifiedFixture()
	yanked, err := release.Yank(entries, "v0.2.0", "critical regression")
	if err != nil {
		t.Fatalf("Yank setup: %v", err)
	}
	_, err = release.Certify(yanked, "v0.2.0", "2026-06-10T14:00:00Z")
	if !errors.Is(err, release.ErrYankIsIrreversible) {
		t.Errorf("certify-after-yank: want ErrYankIsIrreversible, got %v", err)
	}
}

func TestCertify_SemverNotFound(t *testing.T) {
	entries := fixture()
	_, err := release.Certify(entries, "v9.9.9", "2026-06-10T12:00:00Z")
	if !errors.Is(err, release.ErrSemverNotFound) {
		t.Errorf("unknown semver: want ErrSemverNotFound, got %v", err)
	}
}

func TestCertify_DoesNotMutateInput(t *testing.T) {
	entries := fixture()
	original := entries[0].Prerelease
	_, _ = release.Certify(entries, "v0.2.0", "2026-06-10T12:00:00Z")
	if entries[0].Prerelease != original {
		t.Error("Certify mutated the input slice")
	}
}

// --- Yank tests ---

func TestYank_CertifiedToYanked(t *testing.T) {
	entries := certifiedFixture()
	result, err := release.Yank(entries, "v0.2.0", "critical regression in merge logic")
	if err != nil {
		t.Fatalf("Yank: unexpected error: %v", err)
	}
	e := result[0]
	if !e.Yanked {
		t.Error("Yank: expected Yanked=true")
	}
	if e.YankedReason != "critical regression in merge logic" {
		t.Errorf("Yank: YankedReason=%q", e.YankedReason)
	}
}

func TestYank_EmptyReasonRejected(t *testing.T) {
	entries := certifiedFixture()
	_, err := release.Yank(entries, "v0.2.0", "")
	if !errors.Is(err, release.ErrYankReasonEmpty) {
		t.Errorf("empty reason: want ErrYankReasonEmpty, got %v", err)
	}
}

func TestYank_PrereleaseRejected(t *testing.T) {
	entries := fixture() // still pre-release
	_, err := release.Yank(entries, "v0.2.0", "some reason")
	if !errors.Is(err, release.ErrNotCertified) {
		t.Errorf("yank pre-release: want ErrNotCertified, got %v", err)
	}
}

func TestYank_AlreadyYankedRejected(t *testing.T) {
	entries := certifiedFixture()
	yanked, err := release.Yank(entries, "v0.2.0", "first reason")
	if err != nil {
		t.Fatalf("first Yank: %v", err)
	}
	_, err = release.Yank(yanked, "v0.2.0", "second reason")
	if !errors.Is(err, release.ErrAlreadyYanked) {
		t.Errorf("double-yank: want ErrAlreadyYanked, got %v", err)
	}
}

func TestYank_SemverNotFound(t *testing.T) {
	entries := certifiedFixture()
	_, err := release.Yank(entries, "v9.9.9", "reason")
	if !errors.Is(err, release.ErrSemverNotFound) {
		t.Errorf("unknown semver: want ErrSemverNotFound, got %v", err)
	}
}

func TestYank_DoesNotMutateInput(t *testing.T) {
	entries := certifiedFixture()
	original := entries[0].Yanked
	_, _ = release.Yank(entries, "v0.2.0", "reason")
	if entries[0].Yanked != original {
		t.Error("Yank mutated the input slice")
	}
}

// --- CurrentStable tests ---

func TestCurrentStable_Empty(t *testing.T) {
	if s := release.CurrentStable([]release.ReleaseEntry{}); s != nil {
		t.Errorf("expected nil for empty ledger, got %+v", s)
	}
}

func TestCurrentStable_Prerelease(t *testing.T) {
	entries := fixture()
	if s := release.CurrentStable(entries); s != nil {
		t.Errorf("expected nil for pre-release only, got %+v", s)
	}
}

func TestCurrentStable_AfterCertify(t *testing.T) {
	entries := certifiedFixture()
	s := release.CurrentStable(entries)
	if s == nil {
		t.Fatal("expected a current stable entry, got nil")
	}
	if s.Semver != "v0.2.0" {
		t.Errorf("CurrentStable.Semver=%q, want v0.2.0", s.Semver)
	}
}

func TestCurrentStable_AfterYank(t *testing.T) {
	entries := certifiedFixture()
	yanked, _ := release.Yank(entries, "v0.2.0", "reason")
	if s := release.CurrentStable(yanked); s != nil {
		t.Errorf("expected nil after yank, got %+v", s)
	}
}

// --- LedgerFile round-trip tests ---

func TestLedgerFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := release.LedgerPath(dir)

	entries := certifiedFixture()
	if err := release.SaveLedgerFile(path, entries); err != nil {
		t.Fatalf("SaveLedgerFile: %v", err)
	}

	loaded, err := release.LoadLedgerFile(path)
	if err != nil {
		t.Fatalf("LoadLedgerFile: %v", err)
	}
	if len(loaded) != len(entries) {
		t.Fatalf("round-trip length: got %d, want %d", len(loaded), len(entries))
	}
	if loaded[0].Semver != entries[0].Semver {
		t.Errorf("round-trip Semver mismatch: %q vs %q", loaded[0].Semver, entries[0].Semver)
	}
	if loaded[0].CertifiedAt != entries[0].CertifiedAt {
		t.Errorf("round-trip CertifiedAt mismatch: %q vs %q", loaded[0].CertifiedAt, entries[0].CertifiedAt)
	}
}

func TestLedgerFile_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent", "ledger.json")
	entries, err := release.LoadLedgerFile(path)
	if err != nil {
		t.Fatalf("LoadLedgerFile on missing file: want nil error, got %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("LoadLedgerFile on missing file: want empty slice, got %d entries", len(entries))
	}
}

func TestLedgerFile_SchemaVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	path := release.LedgerPath(dir)

	// Write a file with a future schema version.
	badEnv := map[string]interface{}{
		"schema_version": 99,
		"entries":        []interface{}{},
	}
	data, _ := json.MarshalIndent(badEnv, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := release.LoadLedgerFile(path)
	if err == nil {
		t.Error("expected error for schema_version mismatch, got nil")
	}
}
