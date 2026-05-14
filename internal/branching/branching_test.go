package branching_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/branching"
)

// writeFile writes content to <dir>/.harmonik/branching.yaml, creating
// intermediate directories as needed.
func writeFile(t *testing.T, dir, content string) string {
	t.Helper()
	d := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	p := filepath.Join(d, "branching.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

// TestLoad_FileAbsent verifies that a missing file returns zero-value Defaults
// and a nil error.
func TestLoad_FileAbsent(t *testing.T) {
	dir := t.TempDir()
	got, err := branching.Load(dir)
	if err != nil {
		t.Fatalf("Load with absent file: unexpected error: %v", err)
	}
	if got != (branching.Defaults{}) {
		t.Fatalf("Load with absent file: expected zero Defaults, got %+v", got)
	}
}

// TestLoad_FileEmpty verifies that an empty file is treated the same as absent.
func TestLoad_FileEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "")
	got, err := branching.Load(dir)
	if err != nil {
		t.Fatalf("Load with empty file: unexpected error: %v", err)
	}
	if got != (branching.Defaults{}) {
		t.Fatalf("Load with empty file: expected zero Defaults, got %+v", got)
	}
}

// TestLoad_Valid verifies a well-formed file is decoded correctly.
func TestLoad_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, `
version: 1
defaults:
  start_from: main
  lands_on: main
  landing_strategy: squash
`)
	got, err := branching.Load(dir)
	if err != nil {
		t.Fatalf("Load with valid file: unexpected error: %v", err)
	}
	want := branching.Defaults{
		Version:         1,
		StartFrom:       "main",
		LandsOn:         "main",
		LandingStrategy: branching.LandingStrategySquash,
	}
	if got != want {
		t.Fatalf("Load with valid file:\n  got  %+v\n  want %+v", got, want)
	}
}

// TestLoad_ValidCherryPick verifies that landing_strategy=cherry-pick is accepted.
func TestLoad_ValidCherryPick(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, `
version: 1
defaults:
  start_from: feature/base
  lands_on: harmonik/integration
  landing_strategy: cherry-pick
`)
	got, err := branching.Load(dir)
	if err != nil {
		t.Fatalf("Load (cherry-pick): unexpected error: %v", err)
	}
	if got.LandingStrategy != branching.LandingStrategyCherryPick {
		t.Fatalf("LandingStrategy: got %q, want %q", got.LandingStrategy, branching.LandingStrategyCherryPick)
	}
	if got.StartFrom != "feature/base" {
		t.Fatalf("StartFrom: got %q, want %q", got.StartFrom, "feature/base")
	}
	if got.LandsOn != "harmonik/integration" {
		t.Fatalf("LandsOn: got %q, want %q", got.LandsOn, "harmonik/integration")
	}
}

// TestLoad_MalformedYAML verifies that unparseable YAML returns *ErrMalformedYAML.
func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "version: [not a number")
	_, err := branching.Load(dir)
	if err == nil {
		t.Fatal("Load with malformed YAML: expected error, got nil")
	}
	var target *branching.ErrMalformedYAML
	if !errors.As(err, &target) {
		t.Fatalf("Load with malformed YAML: expected *ErrMalformedYAML, got %T: %v", err, err)
	}
}

// TestLoad_UnknownKeys verifies that unknown keys under defaults do not cause
// an error (they produce an slog warning but the load succeeds).
func TestLoad_UnknownKeys(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, `
version: 1
defaults:
  start_from: main
  lands_on: main
  landing_strategy: squash
  unknown_future_key: ignored
`)
	got, err := branching.Load(dir)
	if err != nil {
		t.Fatalf("Load with unknown key: unexpected error: %v", err)
	}
	// Known fields must still decode correctly.
	if got.StartFrom != "main" {
		t.Fatalf("StartFrom: got %q, want %q", got.StartFrom, "main")
	}
}

// TestLoad_BadLandingStrategy verifies that an invalid landing_strategy value
// returns *ErrInvalidLandingStrategy.
func TestLoad_BadLandingStrategy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, `
version: 1
defaults:
  landing_strategy: rebase
`)
	_, err := branching.Load(dir)
	if err == nil {
		t.Fatal("Load with bad landing_strategy: expected error, got nil")
	}
	var target *branching.ErrInvalidLandingStrategy
	if !errors.As(err, &target) {
		t.Fatalf("Load with bad landing_strategy: expected *ErrInvalidLandingStrategy, got %T: %v", err, err)
	}
	if target.Value != "rebase" {
		t.Fatalf("ErrInvalidLandingStrategy.Value: got %q, want %q", target.Value, "rebase")
	}
}

// TestLoad_WrongVersion verifies that a version field other than 1 returns
// *ErrUnsupportedVersion.
func TestLoad_WrongVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, `
version: 2
defaults:
  start_from: main
`)
	_, err := branching.Load(dir)
	if err == nil {
		t.Fatal("Load with version=2: expected error, got nil")
	}
	var target *branching.ErrUnsupportedVersion
	if !errors.As(err, &target) {
		t.Fatalf("Load with version=2: expected *ErrUnsupportedVersion, got %T: %v", err, err)
	}
	if target.Version != 2 {
		t.Fatalf("ErrUnsupportedVersion.Version: got %d, want 2", target.Version)
	}
}

// TestLoadCached_MtimeInvalidation verifies that LoadCached re-reads the file
// when its mtime advances.
func TestLoadCached_MtimeInvalidation(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, `
version: 1
defaults:
  start_from: original
  lands_on: main
  landing_strategy: squash
`)

	got1, err := branching.LoadCached(dir)
	if err != nil {
		t.Fatalf("LoadCached first call: %v", err)
	}
	if got1.StartFrom != "original" {
		t.Fatalf("first load StartFrom: got %q, want %q", got1.StartFrom, "original")
	}

	// Overwrite with a new value and bump mtime by 2 seconds to ensure it differs.
	newContent := `
version: 1
defaults:
  start_from: updated
  lands_on: main
  landing_strategy: squash
`
	if err := os.WriteFile(p, []byte(newContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Explicitly set a future mtime so the cache detects the change even in
	// environments where mtime resolution is coarse.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	got2, err := branching.LoadCached(dir)
	if err != nil {
		t.Fatalf("LoadCached second call: %v", err)
	}
	if got2.StartFrom != "updated" {
		t.Fatalf("second load StartFrom: got %q, want %q — cache invalidation did not trigger", got2.StartFrom, "updated")
	}
}

// TestLoadCached_AbsentThenPresent verifies that LoadCached correctly transitions
// from the absent (zero-value) state to a populated result when the file is
// created between calls.
func TestLoadCached_AbsentThenPresent(t *testing.T) {
	dir := t.TempDir()

	// First call: file absent.
	got1, err := branching.LoadCached(dir)
	if err != nil {
		t.Fatalf("LoadCached absent: %v", err)
	}
	if got1 != (branching.Defaults{}) {
		t.Fatalf("LoadCached absent: expected zero Defaults, got %+v", got1)
	}

	// Create the file and bump its mtime.
	p := writeFile(t, dir, `
version: 1
defaults:
  start_from: main
  lands_on: main
  landing_strategy: squash
`)
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// Second call: file present; cache must miss.
	got2, err := branching.LoadCached(dir)
	if err != nil {
		t.Fatalf("LoadCached present: %v", err)
	}
	if got2.StartFrom != "main" {
		t.Fatalf("LoadCached present: StartFrom: got %q, want %q", got2.StartFrom, "main")
	}
}
