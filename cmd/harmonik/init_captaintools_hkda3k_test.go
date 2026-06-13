package main

// init_captaintools_hkda3k_test.go — unit tests for provisionCaptainTools
// (fleet-portability T12, ON-058b, hk-da3k).

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestProvisionCaptainToolsOnlyIfAbsent verifies the default (no --force) path:
// the script is written when absent and skipped when already present.
func TestProvisionCaptainToolsOnlyIfAbsent(t *testing.T) {
	// Override home dir via a temp dir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	var stdout, stderr bytes.Buffer

	// First call: script absent → should be written.
	if code := provisionCaptainTools(false, &stdout, &stderr); code != 0 {
		t.Fatalf("provisionCaptainTools(force=false) first call returned %d; stderr: %s", code, stderr.String())
	}
	destPath := filepath.Join(tmpHome, ".claude", "captain-tools", "captain-launch.sh")
	written, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("expected captain-launch.sh to be written: %v", err)
	}
	if !bytes.Equal(written, captainLaunchSh) {
		t.Fatalf("written content differs from embedded captainLaunchSh")
	}
	// Verify executable bit.
	info, _ := os.Stat(destPath)
	if info.Mode()&0o100 == 0 {
		t.Fatalf("captain-launch.sh is not executable (mode %v)", info.Mode())
	}

	// Replace file contents with a sentinel to detect if it gets overwritten.
	sentinel := []byte("sentinel")
	if err := os.WriteFile(destPath, sentinel, 0o755); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	stdout.Reset()
	stderr.Reset()

	// Second call (force=false): script present → should be skipped.
	if code := provisionCaptainTools(false, &stdout, &stderr); code != 0 {
		t.Fatalf("provisionCaptainTools(force=false) second call returned %d; stderr: %s", code, stderr.String())
	}
	after, _ := os.ReadFile(destPath)
	if !bytes.Equal(after, sentinel) {
		t.Fatalf("expected file to be unchanged (only-if-absent), but it was overwritten")
	}
}

// TestProvisionCaptainToolsForce verifies that --force overwrites an existing script.
func TestProvisionCaptainToolsForce(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	destDir := filepath.Join(tmpHome, ".claude", "captain-tools")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	destPath := filepath.Join(destDir, "captain-launch.sh")
	// Pre-populate with stale/hardcoded content.
	if err := os.WriteFile(destPath, []byte("stale hardcoded content"), 0o755); err != nil {
		t.Fatalf("write stale content: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := provisionCaptainTools(true, &stdout, &stderr); code != 0 {
		t.Fatalf("provisionCaptainTools(force=true) returned %d; stderr: %s", code, stderr.String())
	}

	refreshed, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read after force: %v", err)
	}
	if !bytes.Equal(refreshed, captainLaunchSh) {
		t.Fatalf("force did not refresh: content still stale")
	}
}
