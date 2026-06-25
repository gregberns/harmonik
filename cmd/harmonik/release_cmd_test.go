package main

import (
	"os"
	"testing"

	"github.com/gregberns/harmonik/internal/release"
)

func TestRunReleaseRecordCreate_WritesPrereleaseLedgerEntry(t *testing.T) {
	dir := t.TempDir()
	code := runReleaseRecordCreate([]string{
		"v0.3.0",
		"--commit", "0123456789abcdef0123456789abcdef01234567",
		"--project", dir,
	})
	if code != 0 {
		t.Fatalf("runReleaseRecordCreate: want exit 0, got %d", code)
	}

	entries, err := release.LoadLedgerFile(release.LedgerPath(dir))
	if err != nil {
		t.Fatalf("LoadLedgerFile: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ledger entries: got %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Semver != "v0.3.0" || e.Tag != "v0.3.0" {
		t.Fatalf("entry identity mismatch: %+v", e)
	}
	if !e.Prerelease {
		t.Error("record-create entry should be prerelease")
	}
}

func TestRunReleaseCertify_AllowsOfflineLedgerOnlyCertification(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	if code := runReleaseRecordCreate([]string{
		"v0.3.0",
		"--commit", "0123456789abcdef0123456789abcdef01234567",
		"--project", dir,
	}); code != 0 {
		t.Fatalf("runReleaseRecordCreate: want exit 0, got %d", code)
	}
	if code := runReleaseCertify([]string{"v0.3.0", "--project", dir}); code != 0 {
		t.Fatalf("runReleaseCertify: want exit 0, got %d", code)
	}

	entries, err := release.LoadLedgerFile(release.LedgerPath(dir))
	if err != nil {
		t.Fatalf("LoadLedgerFile: %v", err)
	}
	if entries[0].Prerelease {
		t.Error("certify should flip ledger prerelease=false")
	}
	if entries[0].CertifiedAt == "" {
		t.Error("certify should stamp certified_at")
	}
}

func TestRunReleaseRecordCreate_RequiresCommit(t *testing.T) {
	dir := t.TempDir()
	code := runReleaseRecordCreate([]string{"v0.3.0", "--project", dir})
	if code != 1 {
		t.Fatalf("runReleaseRecordCreate without --commit: want exit 1, got %d", code)
	}
	if _, err := os.Stat(release.LedgerPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("ledger should not be written on arg error, stat err=%v", err)
	}
}
