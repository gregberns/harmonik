package release_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/release"
)

func TestReadLastGoodBinary_Missing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-good-binary")
	_, err := release.ReadLastGoodBinary(path)
	if !errors.Is(err, release.ErrNoLastGood) {
		t.Errorf("missing file: want ErrNoLastGood, got %v", err)
	}
}

func TestReadLastGoodBinary_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-good-binary")
	if err := os.WriteFile(path, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := release.ReadLastGoodBinary(path)
	if !errors.Is(err, release.ErrNoLastGood) {
		t.Errorf("empty file: want ErrNoLastGood, got %v", err)
	}
}

func TestWriteAndReadLastGoodBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-good-binary")
	binPath := "/usr/local/bin/harmonik.last-good"

	if err := release.WriteLastGoodBinary(path, binPath); err != nil {
		t.Fatalf("WriteLastGoodBinary: %v", err)
	}

	got, err := release.ReadLastGoodBinary(path)
	if err != nil {
		t.Fatalf("ReadLastGoodBinary: %v", err)
	}
	if got != binPath {
		t.Errorf("ReadLastGoodBinary: got %q, want %q", got, binPath)
	}
}

func TestPinLastGoodBinary(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state", "last-good-binary")

	// Create a fake binary to pin.
	srcBin := filepath.Join(dir, "harmonik")
	content := []byte("fake binary content for testing")
	if err := os.WriteFile(srcBin, content, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := release.PinLastGoodBinary(statePath, srcBin); err != nil {
		t.Fatalf("PinLastGoodBinary: %v", err)
	}

	// Verify the .last-good file was created.
	dstBin := srcBin + ".last-good"
	got, err := os.ReadFile(dstBin)
	if err != nil {
		t.Fatalf("read last-good file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("last-good content mismatch: got %q, want %q", got, content)
	}

	// Verify state file records the .last-good path.
	recorded, err := release.ReadLastGoodBinary(statePath)
	if err != nil {
		t.Fatalf("ReadLastGoodBinary after pin: %v", err)
	}
	if recorded != dstBin {
		t.Errorf("state records %q, want %q", recorded, dstBin)
	}
}

func TestRestoreLastGoodBinary(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "last-good-binary")

	// Set up a last-good binary.
	lastGoodContent := []byte("last-good binary content")
	lastGoodBin := filepath.Join(dir, "harmonik.last-good")
	if err := os.WriteFile(lastGoodBin, lastGoodContent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := release.WriteLastGoodBinary(statePath, lastGoodBin); err != nil {
		t.Fatal(err)
	}

	// Restore to a new target path.
	dstBin := filepath.Join(dir, "harmonik")
	if err := os.WriteFile(dstBin, []byte("bad binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := release.RestoreLastGoodBinary(statePath, dstBin); err != nil {
		t.Fatalf("RestoreLastGoodBinary: %v", err)
	}

	got, err := os.ReadFile(dstBin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(lastGoodContent) {
		t.Errorf("restored content mismatch: got %q, want %q", got, lastGoodContent)
	}
}

func TestRestoreLastGoodBinary_NoLastGood(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "last-good-binary")
	dstBin := filepath.Join(dir, "harmonik")

	err := release.RestoreLastGoodBinary(statePath, dstBin)
	if !errors.Is(err, release.ErrNoLastGood) {
		t.Errorf("want ErrNoLastGood, got %v", err)
	}
}
