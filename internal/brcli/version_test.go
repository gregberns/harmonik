package brcli_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
)

// versionFixtureOutput returns the `br --version` stdout for the given version
// string, mimicking the canonical br CLI output format.
func versionFixtureOutput(version string) string {
	return "br " + version + "\n"
}

func TestCheckBrVersionMatchesExact(t *testing.T) {
	pinned := "0.5.2"
	path := brcliFixtureMockBinary(t, versionFixtureOutput(pinned), "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := adapter.CheckBrVersion(context.Background(), pinned); err != nil {
		t.Fatalf("CheckBrVersion: unexpected error: %v", err)
	}
}

func TestCheckBrVersionMismatch(t *testing.T) {
	pinned := "0.5.2"
	observed := "0.5.3"
	path := brcliFixtureMockBinary(t, versionFixtureOutput(observed), "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = adapter.CheckBrVersion(context.Background(), pinned)
	if err == nil {
		t.Fatal("expected ErrBrVersionIncompatible for version mismatch, got nil")
	}
	if !errors.Is(err, brcli.ErrBrVersionIncompatible) {
		t.Errorf("errors.Is(err, ErrBrVersionIncompatible) = false; got %v", err)
	}
}

func TestCheckBrVersionUnparseableOutput(t *testing.T) {
	path := brcliFixtureMockBinary(t, "not a version string at all", "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = adapter.CheckBrVersion(context.Background(), "0.5.2")
	if err == nil {
		t.Fatal("expected ErrBrVersionIncompatible for unparseable output, got nil")
	}
	if !errors.Is(err, brcli.ErrBrVersionIncompatible) {
		t.Errorf("errors.Is(err, ErrBrVersionIncompatible) = false; got %v", err)
	}
}

func TestCheckBrVersionNonZeroExit(t *testing.T) {
	path := brcliFixtureMockBinary(t, "", "error: unknown flag --version", 1)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = adapter.CheckBrVersion(context.Background(), "0.5.2")
	if err == nil {
		t.Fatal("expected ErrBrVersionIncompatible for non-zero exit, got nil")
	}
	if !errors.Is(err, brcli.ErrBrVersionIncompatible) {
		t.Errorf("errors.Is(err, ErrBrVersionIncompatible) = false; got %v", err)
	}
}

func TestCheckBrVersionExecFailure(t *testing.T) {
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = adapter.CheckBrVersion(context.Background(), "0.5.2")
	if err == nil {
		t.Fatal("expected error for exec failure, got nil")
	}
	// Exec failure does NOT wrap ErrBrVersionIncompatible — the binary could
	// not be launched, so no version output exists to classify.
	if errors.Is(err, brcli.ErrBrVersionIncompatible) {
		t.Error("exec failure should NOT wrap ErrBrVersionIncompatible")
	}
}

func TestCheckBrVersionWithPrereleaseObserved(t *testing.T) {
	// br may output a pre-release suffix; it should be stripped from the
	// observed version before comparison. This test verifies that
	// "br 0.5.2-alpha" is compared as "0.5.2" against pinned "0.5.2".
	path := brcliFixtureMockBinary(t, "br 0.5.2-alpha\n", "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// pinned is the bare numeric form; pre-release suffix is NOT part of the
	// pinned manifest string at MVH.
	if err := adapter.CheckBrVersion(context.Background(), "0.5.2"); err != nil {
		t.Fatalf("CheckBrVersion: unexpected error for pre-release match: %v", err)
	}
}

func TestCheckBrVersionPrereleaseMismatch(t *testing.T) {
	// Even with a pre-release suffix, if the numeric part doesn't match,
	// it must return ErrBrVersionIncompatible.
	path := brcliFixtureMockBinary(t, "br 0.5.3-alpha\n", "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = adapter.CheckBrVersion(context.Background(), "0.5.2")
	if err == nil {
		t.Fatal("expected ErrBrVersionIncompatible for pre-release mismatch, got nil")
	}
	if !errors.Is(err, brcli.ErrBrVersionIncompatible) {
		t.Errorf("errors.Is(err, ErrBrVersionIncompatible) = false; got %v", err)
	}
}

func TestCheckBrVersionOutputWithExtraText(t *testing.T) {
	// Some CLIs include build metadata or additional lines in version output.
	// The regex should extract the version from the first matching occurrence.
	output := "br 1.2.3\nBuilt at 2026-01-01T00:00:00Z\n"
	path := brcliFixtureMockBinary(t, output, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := adapter.CheckBrVersion(context.Background(), "1.2.3"); err != nil {
		t.Fatalf("CheckBrVersion: unexpected error with extra output text: %v", err)
	}
}
