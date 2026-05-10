// Tests for TwinLaunchConfig and VerifyTwinLaunch — handler-contract.md HC-045.
//
// Every test name contains "HC045" so that CI can grep for coverage of the
// specific requirement.
//
// # Fixtures
//
// twinLaunchFixtureBinaryWithHash writes a file whose bytes contain the given
// hash string, simulating a twin binary with an embedded commit hash.
// twinLaunchFixtureBinaryWithoutHash writes a file that does NOT contain the hash.
// twinLaunchFixtureRepoWithBinary creates a temp directory tree simulating a
// repo root with a twin binary at a repo-relative path.
package handler

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// twinLaunchFixtureKnownHash is the SHA-1-shaped hash used across all HC-045
// fixture helpers.  The recognisable pattern makes test failures easy to spot.
const twinLaunchFixtureKnownHash = "aabbccdd00112233445566778899aabbccddeeff"

// twinLaunchFixtureBinaryWithHash writes a file at path containing
// twinLaunchFixtureKnownHash in its bytes, simulating a twin binary built with
// -ldflags embedding.
func twinLaunchFixtureBinaryWithHash(t *testing.T, dir, name string) string {
	t.Helper()
	content := append([]byte("twin-binary-prefix\x00"), []byte(twinLaunchFixtureKnownHash)...)
	content = append(content, []byte("\x00twin-binary-suffix")...)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o700); err != nil {
		t.Fatalf("twinLaunchFixtureBinaryWithHash: write: %v", err)
	}
	return path
}

// twinLaunchFixtureBinaryWithoutHash writes a file at path that does NOT
// contain twinLaunchFixtureKnownHash, simulating a binary built without or
// with a different commit hash.
func twinLaunchFixtureBinaryWithoutHash(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("wrong binary content\n"), 0o700); err != nil {
		t.Fatalf("twinLaunchFixtureBinaryWithoutHash: write: %v", err)
	}
	return path
}

// twinLaunchFixtureRepo creates a temp directory simulating a repo root with a
// twin binary at "twins/<name>".  Returns the repoRoot dir and the repo-relative
// ref string (e.g. "twins/claude-twin").
//
// If withHash is true, the binary contains twinLaunchFixtureKnownHash.
// If withHash is false, the binary does NOT contain the known hash.
func twinLaunchFixtureRepo(t *testing.T, name string, withHash bool) (repoRoot, binaryRef string) {
	t.Helper()
	repoRoot = t.TempDir()
	twinsDir := filepath.Join(repoRoot, "twins")
	if err := os.MkdirAll(twinsDir, 0o755); err != nil { //nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		t.Fatalf("twinLaunchFixtureRepo: mkdir: %v", err)
	}
	if withHash {
		twinLaunchFixtureBinaryWithHash(t, twinsDir, name)
	} else {
		twinLaunchFixtureBinaryWithoutHash(t, twinsDir, name)
	}
	binaryRef = filepath.Join("twins", name)
	return repoRoot, binaryRef
}

// ---------------------------------------------------------------------------
// TwinLaunchConfig.Validate tests
// ---------------------------------------------------------------------------

// TestTwinLaunchConfig_HC045_ValidateEmptyBinaryRef verifies that Validate
// returns ErrTwinLaunchConfigInvalid (wrapping ErrStructural) when BinaryRef
// is empty.
func TestTwinLaunchConfig_HC045_ValidateEmptyBinaryRef(t *testing.T) {
	t.Parallel()

	cfg := TwinLaunchConfig{BinaryRef: "", ExpectedHash: twinLaunchFixtureKnownHash}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate: expected error for empty BinaryRef, got nil")
	}
	if !errors.Is(err, ErrTwinLaunchConfigInvalid) {
		t.Errorf("Validate: error does not wrap ErrTwinLaunchConfigInvalid; got %v", err)
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("Validate: error does not wrap ErrStructural; got %v", err)
	}
}

// TestTwinLaunchConfig_HC045_ValidateEmptyExpectedHash verifies that Validate
// returns ErrTwinLaunchConfigInvalid (wrapping ErrStructural) when ExpectedHash
// is empty.
func TestTwinLaunchConfig_HC045_ValidateEmptyExpectedHash(t *testing.T) {
	t.Parallel()

	cfg := TwinLaunchConfig{BinaryRef: "twins/claude-twin", ExpectedHash: ""}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate: expected error for empty ExpectedHash, got nil")
	}
	if !errors.Is(err, ErrTwinLaunchConfigInvalid) {
		t.Errorf("Validate: error does not wrap ErrTwinLaunchConfigInvalid; got %v", err)
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("Validate: error does not wrap ErrStructural; got %v", err)
	}
}

// TestTwinLaunchConfig_HC045_ValidateComplete verifies that Validate returns
// nil for a well-formed TwinLaunchConfig.
func TestTwinLaunchConfig_HC045_ValidateComplete(t *testing.T) {
	t.Parallel()

	cfg := TwinLaunchConfig{
		BinaryRef:    "twins/claude-twin",
		ExpectedHash: twinLaunchFixtureKnownHash,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: expected nil for complete config, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// VerifyTwinLaunch tests
// ---------------------------------------------------------------------------

// TestVerifyTwinLaunch_HC045_HappyPath verifies that VerifyTwinLaunch returns
// the resolved absolute path and nil error when the binary exists at the
// repo-relative path and contains the expected commit hash.
//
// This is the HC-045 happy-path gate.
func TestVerifyTwinLaunch_HC045_HappyPath(t *testing.T) {
	t.Parallel()

	repoRoot, binaryRef := twinLaunchFixtureRepo(t, "claude-twin", true)

	cfg := TwinLaunchConfig{
		BinaryRef:    binaryRef,
		ExpectedHash: twinLaunchFixtureKnownHash,
	}

	absPath, err := VerifyTwinLaunch(repoRoot, cfg)
	if err != nil {
		t.Fatalf("VerifyTwinLaunch: expected nil, got %v", err)
	}
	wantPath := filepath.Join(repoRoot, binaryRef)
	if absPath != wantPath {
		t.Errorf("VerifyTwinLaunch: absPath = %q, want %q", absPath, wantPath)
	}
}

// TestVerifyTwinLaunch_HC045_EmptyBinaryRefIsStructural verifies that
// VerifyTwinLaunch returns an ErrStructural-wrapping error when BinaryRef is
// empty, without attempting path resolution or file I/O.
func TestVerifyTwinLaunch_HC045_EmptyBinaryRefIsStructural(t *testing.T) {
	t.Parallel()

	cfg := TwinLaunchConfig{BinaryRef: "", ExpectedHash: twinLaunchFixtureKnownHash}

	_, err := VerifyTwinLaunch("/any/repo", cfg)
	if err == nil {
		t.Fatal("VerifyTwinLaunch: expected error for empty BinaryRef, got nil")
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("VerifyTwinLaunch: error does not wrap ErrStructural; got %v", err)
	}
}

// TestVerifyTwinLaunch_HC045_EmptyExpectedHashIsStructural verifies that
// VerifyTwinLaunch returns an ErrStructural-wrapping error when ExpectedHash is
// empty — a misconfiguration that MUST block launch.
func TestVerifyTwinLaunch_HC045_EmptyExpectedHashIsStructural(t *testing.T) {
	t.Parallel()

	cfg := TwinLaunchConfig{BinaryRef: "twins/claude-twin", ExpectedHash: ""}

	_, err := VerifyTwinLaunch("/any/repo", cfg)
	if err == nil {
		t.Fatal("VerifyTwinLaunch: expected error for empty ExpectedHash, got nil")
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("VerifyTwinLaunch: error does not wrap ErrStructural; got %v", err)
	}
}

// TestVerifyTwinLaunch_HC045_BinaryMissingIsError verifies that VerifyTwinLaunch
// returns a non-nil error when the twin binary does not exist at the resolved
// path.  HC-045 requires the same commit-hash gate as HC-043; a missing binary
// is an I/O failure that MUST block launch.
func TestVerifyTwinLaunch_HC045_BinaryMissingIsError(t *testing.T) {
	t.Parallel()

	cfg := TwinLaunchConfig{
		BinaryRef:    "twins/nonexistent-twin",
		ExpectedHash: twinLaunchFixtureKnownHash,
	}

	_, err := VerifyTwinLaunch(t.TempDir(), cfg)
	if err == nil {
		t.Fatal("VerifyTwinLaunch: expected error for missing binary, got nil")
	}
}

// TestVerifyTwinLaunch_HC045_HashMismatchIsStructural verifies that
// VerifyTwinLaunch returns an ErrStructural-wrapping error when the twin binary
// exists but does NOT contain the expected commit hash.
//
// Per HC-045 (and HC-043 which twins inherit): "Mismatch MUST fail launch with
// ErrStructural."
func TestVerifyTwinLaunch_HC045_HashMismatchIsStructural(t *testing.T) {
	t.Parallel()

	repoRoot, binaryRef := twinLaunchFixtureRepo(t, "claude-twin", false /* without hash */)

	cfg := TwinLaunchConfig{
		BinaryRef:    binaryRef,
		ExpectedHash: twinLaunchFixtureKnownHash,
	}

	_, err := VerifyTwinLaunch(repoRoot, cfg)
	if err == nil {
		t.Fatal("VerifyTwinLaunch: expected error for hash mismatch, got nil")
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("VerifyTwinLaunch: error does not wrap ErrStructural; got %v", err)
	}
}

// TestVerifyTwinLaunch_HC045_AbsoluteBinaryRefIsRejected verifies that
// VerifyTwinLaunch rejects an absolute BinaryRef for a twin, because twins are
// in-repo binaries and MUST be resolved from repo-relative paths per HC-042
// (which HC-045 extends to twins).  An absolute ref would bypass the
// repo-relative resolution contract.
func TestVerifyTwinLaunch_HC045_AbsoluteBinaryRefIsRejected(t *testing.T) {
	t.Parallel()

	cfg := TwinLaunchConfig{
		BinaryRef:    "/absolute/path/to/claude-twin",
		ExpectedHash: twinLaunchFixtureKnownHash,
	}

	_, err := VerifyTwinLaunch("/any/repo", cfg)
	if err == nil {
		t.Fatal("VerifyTwinLaunch: expected error for absolute BinaryRef, got nil")
	}
	if !errors.Is(err, ErrStructural) {
		t.Errorf("VerifyTwinLaunch: error does not wrap ErrStructural; got %v", err)
	}
}

// TestVerifyTwinLaunch_HC045_ReturnsEmptyPathOnError verifies that
// VerifyTwinLaunch returns an empty string as the path whenever it returns a
// non-nil error, so callers cannot accidentally use a stale path value from a
// failed call.
func TestVerifyTwinLaunch_HC045_ReturnsEmptyPathOnError(t *testing.T) {
	t.Parallel()

	cfg := TwinLaunchConfig{BinaryRef: "", ExpectedHash: twinLaunchFixtureKnownHash}

	got, err := VerifyTwinLaunch("/any/repo", cfg)
	if err == nil {
		t.Fatal("VerifyTwinLaunch: expected error, got nil")
	}
	if got != "" {
		t.Errorf("VerifyTwinLaunch: on error, absPath = %q, want empty string", got)
	}
}

// TestVerifyTwinLaunch_HC045_ConfigTimePinning verifies the HC-045 config-time
// pinning invariant: two calls with identical TwinLaunchConfig and repoRoot
// produce identical results.  This demonstrates that the expected hash is a
// static pin from configuration, not derived at launch time.
func TestVerifyTwinLaunch_HC045_ConfigTimePinning(t *testing.T) {
	t.Parallel()

	repoRoot, binaryRef := twinLaunchFixtureRepo(t, "pi-twin", true)

	cfg := TwinLaunchConfig{
		BinaryRef:    binaryRef,
		ExpectedHash: twinLaunchFixtureKnownHash,
	}

	path1, err1 := VerifyTwinLaunch(repoRoot, cfg)
	path2, err2 := VerifyTwinLaunch(repoRoot, cfg)

	if err1 != nil || err2 != nil {
		t.Fatalf("VerifyTwinLaunch: unexpected errors: err1=%v err2=%v", err1, err2)
	}
	if path1 != path2 {
		t.Errorf("VerifyTwinLaunch: non-deterministic results: %q != %q", path1, path2)
	}
}
