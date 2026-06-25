package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProvisionWorktreeFiles_EmptyList verifies that an empty/nil paths slice is
// an immediate no-op with nil error (the backward-compatible default).
func TestProvisionWorktreeFiles_EmptyList(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := ProvisionWorktreeFiles(src, dst, nil); err != nil {
		t.Fatalf("nil relPaths: want nil error, got %v", err)
	}
	if err := ProvisionWorktreeFiles(src, dst, []string{}); err != nil {
		t.Fatalf("empty relPaths: want nil error, got %v", err)
	}

	// No files should have been created in the destination.
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatalf("ReadDir(dst): %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("no-op should not write to dst; found %d entries", len(entries))
	}
}

// TestProvisionWorktreeFiles_CopiesPreservingContentAndMode verifies a regular
// file is copied with its content and permission bits intact, including into a
// nested directory that does not yet exist in the destination.
func TestProvisionWorktreeFiles_CopiesPreservingContentAndMode(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// A root-level file with a distinctive (executable) mode.
	const envContent = "FOO=bar\nNUGET_FEED_PAT=secret\n"
	const envMode = os.FileMode(0o640)
	if err := os.WriteFile(filepath.Join(src, ".env"), []byte(envContent), envMode); err != nil {
		t.Fatalf("write source .env: %v", err)
	}

	// A nested file to exercise parent-directory creation in the destination.
	const nestedContent = "#!/bin/sh\necho hi\n"
	const nestedMode = os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir source scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "scripts", "gate.sh"), []byte(nestedContent), nestedMode); err != nil {
		t.Fatalf("write source gate.sh: %v", err)
	}

	if err := ProvisionWorktreeFiles(src, dst, []string{".env", "scripts/gate.sh"}); err != nil {
		t.Fatalf("ProvisionWorktreeFiles: %v", err)
	}

	// .env: content + mode.
	gotEnv, err := os.ReadFile(filepath.Join(dst, ".env"))
	if err != nil {
		t.Fatalf("read dst .env: %v", err)
	}
	if string(gotEnv) != envContent {
		t.Errorf(".env content mismatch:\n got %q\nwant %q", gotEnv, envContent)
	}
	envInfo, err := os.Stat(filepath.Join(dst, ".env"))
	if err != nil {
		t.Fatalf("stat dst .env: %v", err)
	}
	if envInfo.Mode().Perm() != envMode {
		t.Errorf(".env mode: got %v, want %v", envInfo.Mode().Perm(), envMode)
	}

	// nested file: content + mode + parent dir created.
	gotNested, err := os.ReadFile(filepath.Join(dst, "scripts", "gate.sh"))
	if err != nil {
		t.Fatalf("read dst gate.sh: %v", err)
	}
	if string(gotNested) != nestedContent {
		t.Errorf("gate.sh content mismatch:\n got %q\nwant %q", gotNested, nestedContent)
	}
	nestedInfo, err := os.Stat(filepath.Join(dst, "scripts", "gate.sh"))
	if err != nil {
		t.Fatalf("stat dst gate.sh: %v", err)
	}
	if nestedInfo.Mode().Perm() != nestedMode {
		t.Errorf("gate.sh mode: got %v, want %v", nestedInfo.Mode().Perm(), nestedMode)
	}
}

// TestProvisionWorktreeFiles_MissingSourceWarnSkips verifies that a configured
// source file that does not exist is skipped with a warning (no error), while a
// sibling file that DOES exist is still copied.
func TestProvisionWorktreeFiles_MissingSourceWarnSkips(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "present.txt"), []byte("here"), 0o644); err != nil {
		t.Fatalf("write present.txt: %v", err)
	}

	// "absent.env" does not exist in src; it must be warn-skipped, not error.
	if err := ProvisionWorktreeFiles(src, dst, []string{"absent.env", "present.txt"}); err != nil {
		t.Fatalf("missing source should warn-skip, got error: %v", err)
	}

	// The absent file must NOT have been created.
	if _, err := os.Stat(filepath.Join(dst, "absent.env")); !os.IsNotExist(err) {
		t.Errorf("absent.env should not exist in dst; stat err = %v", err)
	}
	// The present file must still have been copied (skip is per-entry, not fatal).
	if _, err := os.Stat(filepath.Join(dst, "present.txt")); err != nil {
		t.Errorf("present.txt should have been copied: %v", err)
	}
}

// TestProvisionWorktreeFiles_RejectsUnsafePaths verifies that an absolute path
// and a "..".-escaping path are both rejected with an error (provisioning must
// never read/write outside the configured roots).
func TestProvisionWorktreeFiles_RejectsUnsafePaths(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	t.Run("absolute", func(t *testing.T) {
		err := ProvisionWorktreeFiles(src, dst, []string{filepath.Join(src, ".env")})
		if err == nil {
			t.Fatalf("absolute path: want error, got nil")
		}
	})

	t.Run("dotdot-escape", func(t *testing.T) {
		err := ProvisionWorktreeFiles(src, dst, []string{"../outside.env"})
		if err == nil {
			t.Fatalf("..-escape path: want error, got nil")
		}
	})

	t.Run("bare-dotdot", func(t *testing.T) {
		err := ProvisionWorktreeFiles(src, dst, []string{".."})
		if err == nil {
			t.Fatalf("bare .. : want error, got nil")
		}
	})
}
