package handler

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenWireTap_EnvUnset proves the production default: no env var → nil
// writer, no file created (WireTap stays nil → byte-identical no-op).
func TestOpenWireTap_EnvUnset(t *testing.T) {
	t.Setenv(EnvWireCaptureDir, "")
	f, err := openWireTap()
	if err != nil {
		t.Fatalf("openWireTap: unexpected error: %v", err)
	}
	if f != nil {
		_ = f.Close()
		t.Fatalf("openWireTap: expected nil file when %s unset, got %v", EnvWireCaptureDir, f.Name())
	}
}

// TestOpenWireTap_EnvSet proves the opt-in: env set → non-nil writer and the
// file lands at <dir>/<scn>/wire.ndjson (the exact path the capture harness
// reads back), using the default scenario when HARMONIK_CAPTURE_SCN is unset.
func TestOpenWireTap_EnvSet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvWireCaptureScn, "")
	t.Setenv(EnvWireCaptureDir, dir)

	f, err := openWireTap()
	if err != nil {
		t.Fatalf("openWireTap: unexpected error: %v", err)
	}
	if f == nil {
		t.Fatalf("openWireTap: expected non-nil file when %s set", EnvWireCaptureDir)
	}
	defer f.Close()

	want := filepath.Join(dir, DefaultWireCaptureScn, "wire.ndjson")
	if f.Name() != want {
		t.Fatalf("openWireTap: file path = %s, want %s", f.Name(), want)
	}
	if _, statErr := os.Stat(want); statErr != nil {
		t.Fatalf("openWireTap: expected file created at %s: %v", want, statErr)
	}
}

// TestOpenWireTap_ScnOverride proves HARMONIK_CAPTURE_SCN selects the subdir,
// matching the harness's filepath.Join(outRoot, scn) layout.
func TestOpenWireTap_ScnOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvWireCaptureDir, dir)
	t.Setenv(EnvWireCaptureScn, "review-loop")

	f, err := openWireTap()
	if err != nil {
		t.Fatalf("openWireTap: unexpected error: %v", err)
	}
	if f == nil {
		t.Fatalf("openWireTap: expected non-nil file")
	}
	defer f.Close()

	want := filepath.Join(dir, "review-loop", "wire.ndjson")
	if f.Name() != want {
		t.Fatalf("openWireTap: file path = %s, want %s", f.Name(), want)
	}
}
