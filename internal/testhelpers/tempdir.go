package testhelpers

import (
	"os"
	"testing"
)

// TempDir creates a temporary directory scoped to the test and registers
// cleanup with t.Cleanup so the caller never has to remember to remove it.
// The directory path is returned.
//
//	dir := testhelpers.TempDir(t)
//	// dir is removed when t finishes
func TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-test-*")
	if err != nil {
		t.Fatalf("TempDir: failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("TempDir cleanup: failed to remove %q: %v", dir, err)
		}
	})
	return dir
}
