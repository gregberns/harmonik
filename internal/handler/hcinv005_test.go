package handler_test

// hcinv005_test.go — sensor asserting HC-INV-005 (no handler subprocess
// launched without a verified binary path).
//
// Spec ref: specs/handler-contract.md §5.HC-INV-005, §4.10.HC-042,
// §4.10.HC-043, §4.10.HC-045, §10.2 HC-042–HC-045 obligations.
// Bead: hk-8i31.68.
//
// Helper prefix: hcinv005Fixture (per implementer-protocol.md
// §Helper-prefix discipline).
//
// HC-INV-005 states:
//
//	"For every successful Launch, the binary that was exec'd MUST have passed
//	the launch-path and commit-hash rules of §4.10.HC-042 and §4.10.HC-043
//	(or, for system handlers declared via system_handler=true, the
//	$PATH-resolved absolute path and --version log). A Launch return value of
//	(Session, nil) where the verification did not occur is a daemon defect."
//
// §10.2 test obligations for HC-042–HC-045:
//
//	"Launch-path negative test: missing binary, mismatched commit hash.
//	System-handler path (via system_handler=true declaration) exercised via
//	Claude Code fixture."
//
// This file contains:
//
//  1. Composition sensor: verifies that both ResolveLaunchPath and
//     VerifyCommitHash are required before a launch can proceed; tests
//     the negative paths mandated by §10.2 (missing binary, hash mismatch)
//     at the invariant level.
//
//  2. System-handler sensor: verifies that system handlers resolved via PATH
//     satisfy the ResolveLaunchPath contract; attempts the Claude Code fixture
//     if the binary is on PATH, skips gracefully if absent.
//
//  3. Invariant label test: asserts that the error class for launch-gate
//     failures is always ErrStructural, satisfying §8.2's classification rule.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
)

// hcinv005FixtureRepoWithBinary creates a temporary repo root containing a
// binary at "twins/<name>" whose bytes either include or exclude knownHash.
// Returns the repoRoot and the repo-relative binaryRef.
func hcinv005FixtureRepoWithBinary(t *testing.T, name string, includeHash bool, knownHash string) (repoRoot, binaryRef string) {
	t.Helper()
	repoRoot = t.TempDir()
	twinsDir := filepath.Join(repoRoot, "twins")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(twinsDir, 0o755); err != nil {
		t.Fatalf("hcinv005FixtureRepoWithBinary: mkdir: %v", err)
	}
	binPath := filepath.Join(twinsDir, name)
	var content []byte
	if includeHash {
		content = append([]byte("binary-prefix\x00"), []byte(knownHash)...)
		content = append(content, []byte("\x00binary-suffix")...)
	} else {
		content = []byte("wrong-binary-content-no-hash-here\n")
	}
	//nolint:gosec // G304: path is constructed from t.TempDir() in test, not user input
	if err := os.WriteFile(binPath, content, 0o700); err != nil {
		t.Fatalf("hcinv005FixtureRepoWithBinary: write binary: %v", err)
	}
	return repoRoot, filepath.Join("twins", name)
}

// hcinv005FixtureKnownHash is the SHA-1-shaped hash used across hcinv005 tests.
const hcinv005FixtureKnownHash = "cafebabe0123456789abcdef0123456789abcdef"

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-INV-005 composition — missing binary
// ─────────────────────────────────────────────────────────────────────────────

// TestHCINV005_MissingBinaryBlocksLaunch verifies that a missing binary at the
// repo-relative path produces a non-nil error from VerifyTwinLaunch, blocking
// launch per HC-INV-005 + HC-042.
//
// §10.2 "launch-path negative test: missing binary."
func TestHCINV005_MissingBinaryBlocksLaunch(t *testing.T) {
	t.Parallel()

	// BinaryRef points to a file that does not exist under repoRoot.
	cfg := handler.TwinLaunchConfig{
		BinaryRef:    "twins/nonexistent-handler",
		ExpectedHash: hcinv005FixtureKnownHash,
	}
	_, err := handler.VerifyTwinLaunch(t.TempDir(), cfg)
	if err == nil {
		t.Fatal(
			"HC-INV-005: VerifyTwinLaunch returned nil error for missing binary; " +
				"a missing binary MUST block launch (§4.10.HC-042 / §5.HC-INV-005)",
		)
	}
	// Not required to be ErrStructural for I/O failures (file not found), but
	// the gate MUST be non-nil — any non-nil error satisfies the fail-fast requirement.
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-INV-005 composition — hash mismatch
// ─────────────────────────────────────────────────────────────────────────────

// TestHCINV005_HashMismatchBlocksLaunch verifies that a commit-hash mismatch
// produces ErrStructural from VerifyTwinLaunch, blocking launch per HC-INV-005
// + HC-043.
//
// §10.2 "launch-path negative test: mismatched commit hash."
func TestHCINV005_HashMismatchBlocksLaunch(t *testing.T) {
	t.Parallel()

	// Binary exists but does NOT contain the expected hash.
	repoRoot, binaryRef := hcinv005FixtureRepoWithBinary(t, "handler", false, hcinv005FixtureKnownHash)
	cfg := handler.TwinLaunchConfig{
		BinaryRef:    binaryRef,
		ExpectedHash: hcinv005FixtureKnownHash,
	}
	_, err := handler.VerifyTwinLaunch(repoRoot, cfg)
	if err == nil {
		t.Fatal(
			"HC-INV-005: VerifyTwinLaunch returned nil error for hash mismatch; " +
				"a hash mismatch MUST block launch with ErrStructural (§4.10.HC-043 / §5.HC-INV-005)",
		)
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf(
			"HC-INV-005: hash-mismatch error does not wrap ErrStructural; got %v — "+
				"§8.2 requires structural classification for launch-path verification failures",
			err,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-INV-005 composition — absolute binary ref rejected
// ─────────────────────────────────────────────────────────────────────────────

// TestHCINV005_AbsoluteRefBlocksLaunch verifies that an absolute BinaryRef
// is rejected by VerifyTwinLaunch with ErrStructural, preventing bypass of
// the repo-relative resolution contract of HC-042.
//
// This is a structural form of the "launch-path negative test" in §10.2:
// an absolute ref would allow a twin to be launched from outside the repo,
// circumventing the path audit that makes HC-INV-005 observable.
func TestHCINV005_AbsoluteRefBlocksLaunch(t *testing.T) {
	t.Parallel()

	cfg := handler.TwinLaunchConfig{
		BinaryRef:    "/absolute/path/to/handler",
		ExpectedHash: hcinv005FixtureKnownHash,
	}
	_, err := handler.VerifyTwinLaunch("/any/repo", cfg)
	if err == nil {
		t.Fatal(
			"HC-INV-005: VerifyTwinLaunch returned nil error for absolute BinaryRef; " +
				"absolute refs MUST be rejected to preserve repo-relative path audit (HC-042)",
		)
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf(
			"HC-INV-005: absolute-ref error does not wrap ErrStructural; got %v",
			err,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-INV-005 happy path — launch gate clears
// ─────────────────────────────────────────────────────────────────────────────

// TestHCINV005_GateClears verifies that when both path resolution and hash
// verification succeed, VerifyTwinLaunch returns (absolutePath, nil), confirming
// the launch gate allows exactly the binary that passed both checks.
//
// This is the positive arm of HC-INV-005: the launch SHOULD succeed after the
// gate, and the returned path is the one that was verified.
func TestHCINV005_GateClears(t *testing.T) {
	t.Parallel()

	repoRoot, binaryRef := hcinv005FixtureRepoWithBinary(t, "handler", true, hcinv005FixtureKnownHash)
	cfg := handler.TwinLaunchConfig{
		BinaryRef:    binaryRef,
		ExpectedHash: hcinv005FixtureKnownHash,
	}

	absPath, err := handler.VerifyTwinLaunch(repoRoot, cfg)
	if err != nil {
		t.Fatalf("HC-INV-005: VerifyTwinLaunch gate should clear for valid binary+hash, got error: %v", err)
	}
	wantPath := filepath.Join(repoRoot, binaryRef)
	if absPath != wantPath {
		t.Errorf("HC-INV-005: gate returned absPath = %q, want %q", absPath, wantPath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: system-handler path via ResolveLaunchPath (§10.2 "Claude Code fixture")
// ─────────────────────────────────────────────────────────────────────────────

// TestHCINV005_SystemHandlerViaPath verifies that a system handler declared
// with system_handler=true resolves via $PATH (ResolveLaunchPath behavior for
// bare names with systemHandler=true).
//
// §10.2: "System-handler path (via system_handler=true declaration) exercised
// via Claude Code fixture."
//
// The test uses "sh" as the universal proxy: it is always on PATH on POSIX
// systems and exercises the identical code path as "claude". If "claude" is
// available on PATH, the test additionally verifies the Claude Code fixture
// name resolves without error. Both skip gracefully if the binary is absent.
func TestHCINV005_SystemHandlerViaPath(t *testing.T) {
	t.Parallel()

	// Use "sh" as the universal system-handler proxy (always present on POSIX).
	// This exercises the system_handler=true PATH-lookup branch of ResolveLaunchPath.
	const systemHandlerProxy = "sh"

	expected, err := exec.LookPath(systemHandlerProxy)
	if err != nil {
		t.Skipf("system-handler proxy %q not found on PATH: %v", systemHandlerProxy, err)
	}

	got, err := handler.ResolveLaunchPath("/any/repo", systemHandlerProxy, true /*systemHandler*/)
	if err != nil {
		t.Fatalf(
			"HC-INV-005 system-handler: ResolveLaunchPath(%q, systemHandler=true): %v",
			systemHandlerProxy, err,
		)
	}
	if got != expected {
		t.Errorf(
			"HC-INV-005 system-handler: ResolveLaunchPath returned %q, want exec.LookPath result %q",
			got, expected,
		)
	}
}

// TestHCINV005_ClaudeCodeFixture verifies the Claude Code CLI (the canonical
// system handler) resolves via PATH when present.
//
// §10.2: "System-handler path via Claude Code fixture."
//
// The test SKIPS gracefully when the "claude" binary is not found — it is not
// installed in standard CI. It passes in environments where Claude Code is
// installed globally (developer laptops, configured integration runners).
func TestHCINV005_ClaudeCodeFixture(t *testing.T) {
	t.Parallel()

	const claudeBinary = "claude"
	expected, err := exec.LookPath(claudeBinary)
	if err != nil {
		t.Skipf(
			"HC-INV-005 Claude Code fixture: %q not found on PATH — skipping "+
				"(install Claude Code CLI to exercise this path; §10.2 Claude Code fixture obligation)",
			claudeBinary,
		)
	}

	// Resolve via system-handler path (system_handler=true).
	got, err := handler.ResolveLaunchPath("/any/repo", claudeBinary, true /*systemHandler*/)
	if err != nil {
		t.Fatalf(
			"HC-INV-005 Claude Code fixture: ResolveLaunchPath(%q, systemHandler=true): %v",
			claudeBinary, err,
		)
	}
	if got != expected {
		t.Errorf(
			"HC-INV-005 Claude Code fixture: ResolveLaunchPath returned %q, want exec.LookPath result %q (§10.2)",
			got, expected,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-INV-005 error-class invariant (all gate failures → ErrStructural)
// ─────────────────────────────────────────────────────────────────────────────

// TestHCINV005_GateFailuresAreStructural verifies that every launch-gate failure
// in the composition (invalid config, hash mismatch, absolute ref) yields
// ErrStructural per §8.2's structural-class rule.
//
// The missing-binary case is excluded because file-not-found is an I/O error
// that may wrap os.ErrNotExist rather than ErrStructural directly (VerifyCommitHash
// reads the binary; if it cannot read, the error class depends on implementation).
// The negative cases that ARE spec-required to return ErrStructural are:
// config-invalid (HC-045) and hash-mismatch (HC-043).
func TestHCINV005_GateFailuresAreStructural(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name string
		cfg  handler.TwinLaunchConfig
		root string
	}

	repoRoot, binaryRefNoHash := hcinv005FixtureRepoWithBinary(
		t, "handler-nohash", false, hcinv005FixtureKnownHash,
	)

	cases := []testCase{
		{
			name: "empty_binary_ref",
			cfg:  handler.TwinLaunchConfig{BinaryRef: "", ExpectedHash: hcinv005FixtureKnownHash},
			root: "/any/repo",
		},
		{
			name: "empty_expected_hash",
			cfg:  handler.TwinLaunchConfig{BinaryRef: "twins/handler", ExpectedHash: ""},
			root: "/any/repo",
		},
		{
			name: "absolute_binary_ref",
			cfg:  handler.TwinLaunchConfig{BinaryRef: "/absolute/path", ExpectedHash: hcinv005FixtureKnownHash},
			root: "/any/repo",
		},
		{
			name: "hash_mismatch",
			cfg:  handler.TwinLaunchConfig{BinaryRef: binaryRefNoHash, ExpectedHash: hcinv005FixtureKnownHash},
			root: repoRoot,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := handler.VerifyTwinLaunch(tc.root, tc.cfg)
			if err == nil {
				t.Fatalf("HC-INV-005 %q: expected error, got nil", tc.name)
			}
			if !errors.Is(err, handler.ErrStructural) {
				t.Errorf(
					"HC-INV-005 %q: error does not wrap ErrStructural; got %v — "+
						"all launch-gate failures MUST yield ErrStructural (§8.2 / §5.HC-INV-005)",
					tc.name, err,
				)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-INV-005 returns empty path on any gate failure
// ─────────────────────────────────────────────────────────────────────────────

// TestHCINV005_EmptyPathOnGateFailure verifies that VerifyTwinLaunch returns
// an empty absolute path string whenever it returns a non-nil error, so callers
// cannot accidentally exec a stale path value from a failed gate call.
//
// This is a defensive invariant: the HC-INV-005 gate MUST be fail-closed.
func TestHCINV005_EmptyPathOnGateFailure(t *testing.T) {
	t.Parallel()

	// Config that is guaranteed to fail (empty BinaryRef).
	cfg := handler.TwinLaunchConfig{BinaryRef: "", ExpectedHash: hcinv005FixtureKnownHash}
	absPath, err := handler.VerifyTwinLaunch("/any/repo", cfg)
	if err == nil {
		t.Fatal("HC-INV-005: expected error for empty BinaryRef, got nil")
	}
	if absPath != "" {
		t.Errorf(
			"HC-INV-005: VerifyTwinLaunch returned non-empty absPath %q on gate failure; "+
				"MUST return empty string on error to prevent accidental use of stale path",
			absPath,
		)
	}
}
