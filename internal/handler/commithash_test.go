// Tests for VerifyCommitHash — handler-contract.md HC-043.
//
// Every test name contains "HC043" so that CI can grep for coverage of the
// specific requirement.
//
// # Synthetic binary fixture
//
// commitHashFixtureBuild compiles a minimal Go program with a known hash
// embedded via -ldflags "-X main.commitHash=<value>".  This fixture is
// sufficient for unit-level coverage: it proves that VerifyCommitHash can
// find an ldflags-injected string in a real compiled binary.
//
// # Integration test gap
//
// A full integration test against a real harmonik-twin-generic build with its
// production Makefile target is not included here because it requires the
// twin-binary build infrastructure (hk-ahvq.48.4 — open).  That test is
// tracked as a follow-up bead; see the TODO comment in commithash.go and the
// bead ID captured at the bottom of this file.
package handler

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commitHashFixtureKnownHash is the SHA-1-shaped hash value embedded into
// every fixture binary built by commitHashFixtureBuild.  It uses a
// recognisable pattern so test failures are easy to spot in output.
const commitHashFixtureKnownHash = "deadbeef01234567890abcdef0123456789abcde"

// commitHashFixtureBuild compiles a tiny Go program with commitHashFixtureKnownHash
// embedded via -ldflags and returns the path to the resulting binary.  The
// binary is placed in a t.TempDir() and is cleaned up automatically.
//
// If the Go toolchain is unavailable (e.g. a stripped CI image) the test is
// skipped via t.Skip, not failed, so that the rest of the suite continues.
func commitHashFixtureBuild(t *testing.T) string {
	t.Helper()

	// Minimal Go program: no output, exits cleanly.  The only requirement is
	// that -ldflags -X can be applied to main.commitHash.
	const src = `package main

// commitHash is set at build time via -ldflags "-X main.commitHash=<sha>".
var commitHash string //nolint:unused

func main() {}
`
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "main.go")

	if err := os.WriteFile(srcFile, []byte(src), 0o600); err != nil {
		t.Fatalf("commitHashFixtureBuild: write source: %v", err)
	}

	outBinary := filepath.Join(dir, "fixture")
	ldflag := "-X main.commitHash=" + commitHashFixtureKnownHash

	cmd := exec.CommandContext(
		context.Background(),
		"go", "build",
		"-ldflags", ldflag,
		"-o", outBinary,
		srcFile,
	)
	cmd.Dir = dir

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("commitHashFixtureBuild: go build unavailable or failed (%v): %s", err, out)
	}

	return outBinary
}

// commitHashFixturePlainFile writes arbitrary bytes to a temp file and returns
// its path.  Used to simulate a binary that does NOT contain the expected hash.
func commitHashFixturePlainFile(t *testing.T, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "notabinary")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("commitHashFixturePlainFile: write: %v", err)
	}
	return path
}

// TestVerifyCommitHash_HC043_MatchFound verifies that VerifyCommitHash returns
// nil when the expected hash IS embedded in the binary via ldflags.
// This is the happy-path gate required by HC-043.
func TestVerifyCommitHash_HC043_MatchFound(t *testing.T) {
	t.Parallel()

	binaryPath := commitHashFixtureBuild(t)

	if err := VerifyCommitHash(binaryPath, commitHashFixtureKnownHash); err != nil {
		t.Errorf("VerifyCommitHash: expected nil for matching hash, got %v", err)
	}
}

// TestVerifyCommitHash_HC043_MismatchReturnsErrStructural verifies that a
// hash that is NOT present in the binary causes VerifyCommitHash to return an
// error wrapping ErrStructural, per HC-043:
// "Mismatch MUST fail launch with ErrStructural."
func TestVerifyCommitHash_HC043_MismatchReturnsErrStructural(t *testing.T) {
	t.Parallel()

	binaryPath := commitHashFixtureBuild(t)
	wrongHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	err := VerifyCommitHash(binaryPath, wrongHash)
	if err == nil {
		t.Fatal("VerifyCommitHash: expected error for mismatched hash, got nil")
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("VerifyCommitHash mismatch: error does not wrap ErrStructural; got %v", err)
	}
}

// TestVerifyCommitHash_HC043_MismatchOnPlainFile verifies that a file whose
// bytes do not contain the expected hash returns an error wrapping ErrStructural.
// This covers the case where the binary was built without ldflags embedding or
// with a different hash.
func TestVerifyCommitHash_HC043_MismatchOnPlainFile(t *testing.T) {
	t.Parallel()

	// Content that is guaranteed NOT to contain commitHashFixtureKnownHash.
	path := commitHashFixturePlainFile(t, []byte("hello world\n"))

	err := VerifyCommitHash(path, commitHashFixtureKnownHash)
	if err == nil {
		t.Fatal("VerifyCommitHash: expected error for plain file without embedded hash, got nil")
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("VerifyCommitHash on plain file: error does not wrap ErrStructural; got %v", err)
	}
}

// TestVerifyCommitHash_HC043_EmptyExpectedIsStructural verifies that an empty
// expected hash returns an error wrapping ErrStructural without attempting a
// file read.  An empty expected hash is a configuration defect — the caller
// MUST NOT proceed with launch.
func TestVerifyCommitHash_HC043_EmptyExpectedIsStructural(t *testing.T) {
	t.Parallel()

	// Use a path that does not exist — the function must fail before the read.
	err := VerifyCommitHash("/nonexistent/binary", "")
	if err == nil {
		t.Fatal("VerifyCommitHash: expected error for empty expected hash, got nil")
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("VerifyCommitHash empty expected: error does not wrap ErrStructural; got %v", err)
	}
}

// TestVerifyCommitHash_HC043_FileNotFound verifies that a missing binary file
// returns a non-nil error (I/O failure).  The caller at the launch site MUST
// treat this as ErrStructural and refuse to proceed.
func TestVerifyCommitHash_HC043_FileNotFound(t *testing.T) {
	t.Parallel()

	err := VerifyCommitHash("/nonexistent/path/to/binary", commitHashFixtureKnownHash)
	if err == nil {
		t.Fatal("VerifyCommitHash: expected error for nonexistent binary, got nil")
	}
}

// TestVerifyCommitHash_HC043_HashSubstringInFile verifies that VerifyCommitHash
// returns nil even when the expected hash appears as a substring within a
// larger byte sequence, as occurs in practice when the ldflags value is
// flanked by null bytes or other linker metadata in the binary.
func TestVerifyCommitHash_HC043_HashSubstringInFile(t *testing.T) {
	t.Parallel()

	// Embed the known hash surrounded by noise bytes, simulating the binary
	// data segment layout produced by the Go linker.
	content := append([]byte("noise-prefix\x00"), []byte(commitHashFixtureKnownHash)...)
	content = append(content, []byte("\x00noise-suffix")...)

	path := commitHashFixturePlainFile(t, content)

	if err := VerifyCommitHash(path, commitHashFixtureKnownHash); err != nil {
		t.Errorf("VerifyCommitHash: expected nil for hash present as substring, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Integration test — hk-uwie
// ---------------------------------------------------------------------------
//
// TestVerifyCommitHash_HC043_RealBinary exercises VerifyCommitHash against the
// actual harmonik-twin-generic binary built with the production ldflags stamp
// (same stamp path as the Makefile's build-twin-generic target).
//
// commitHashFixtureBuildTwin compiles cmd/harmonik-twin-generic with
// -ldflags "-X main.commitHash=<HEAD>" using exec.CommandContext(t.Context(),
// ...) and returns the binary path together with the stamped hash value.  The
// binary is placed in t.TempDir() and is cleaned up automatically.

// commitHashFixtureBuildTwin builds cmd/harmonik-twin-generic with the
// production ldflags stamp (mirroring the Makefile build-twin-generic target)
// and returns (binaryPath, stampedHash).  The binary is written into
// t.TempDir() and cleaned up automatically.
//
// If git or the Go toolchain is unavailable the test is skipped, not failed.
//
// Cite: specs/handler-contract.md §4.10.HC-043, §4.10.HC-045; Makefile
// build-twin-generic target.
func commitHashFixtureBuildTwin(t *testing.T) (binaryPath, stampedHash string) {
	t.Helper()

	// Resolve the repo root so we can reference the package path absolutely.
	rootOut, err := exec.CommandContext(t.Context(), "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("commitHashFixtureBuildTwin: git unavailable: %v", err)
	}
	repoRoot := strings.TrimSpace(string(rootOut))

	// Obtain the current HEAD SHA — this is the value the Makefile stamps via
	// COMMIT_HASH := $(shell git rev-parse HEAD).
	hashOut, err := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skipf("commitHashFixtureBuildTwin: git rev-parse HEAD: %v", err)
	}
	headSHA := strings.TrimSpace(string(hashOut))
	if headSHA == "" {
		t.Skip("commitHashFixtureBuildTwin: git rev-parse HEAD returned empty string")
	}

	outBinary := filepath.Join(t.TempDir(), "generic-twin")
	pkgPath := filepath.Join(repoRoot, "cmd", "harmonik-twin-generic")
	ldflag := "-X main.commitHash=" + headSHA

	cmd := exec.CommandContext(
		t.Context(),
		"go", "build",
		"-ldflags", ldflag,
		"-o", outBinary,
		pkgPath,
	)
	if out, buildErr := cmd.CombinedOutput(); buildErr != nil {
		t.Skipf("commitHashFixtureBuildTwin: go build failed (%v): %s", buildErr, out)
	}

	return outBinary, headSHA
}

// TestVerifyCommitHash_HC043_RealBinary verifies VerifyCommitHash against the
// actual harmonik-twin-generic binary built with the production ldflags stamp.
//
// Round-trip correctness check: the hash embedded by the linker must be found
// by VerifyCommitHash, and a different hash must produce ErrStructural.
//
// Cite: specs/handler-contract.md §4.10.HC-043.  Tracked as hk-uwie.
func TestVerifyCommitHash_HC043_RealBinary(t *testing.T) {
	binaryPath, stampedHash := commitHashFixtureBuildTwin(t)

	// Happy path: the stamped hash must be found in the binary.
	if err := VerifyCommitHash(binaryPath, stampedHash); err != nil {
		t.Errorf("VerifyCommitHash against real twin binary: expected nil, got %v", err)
	}

	// Mismatch path: a different SHA-1-shaped hash must return ErrStructural.
	wrongHash := "0000000000000000000000000000000000000000"
	if wrongHash == stampedHash {
		// Extremely unlikely but guard against accidental equality.
		wrongHash = "ffffffffffffffffffffffffffffffffffffffff"
	}
	err := VerifyCommitHash(binaryPath, wrongHash)
	if err == nil {
		t.Fatal("VerifyCommitHash against real twin binary: expected ErrStructural for wrong hash, got nil")
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("VerifyCommitHash mismatch on real binary: error does not wrap ErrStructural; got %v", err)
	}
}
