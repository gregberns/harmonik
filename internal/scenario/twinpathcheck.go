package scenario

// twinpathcheck.go — SH-INV-003 pre-launch twin-binary path-prefix sensor.
//
// Implements the path-prefix predicate check declared in
// specs/scenario-harness.md §5 SH-INV-003 (Harness operates only on twin
// binaries, never on real-model handlers).
//
// The sensor MUST be called at handler-config resolution time (§4.3), before
// DriveOrchestration is called. A binary that fails the check causes the
// scenario to fail with failure_class=harness-internal-error per §5 SH-INV-003
// and §8.5 closed-list detection item (ii).
//
// HC-043 commit-hash verification is the daemon's responsibility and is NOT
// performed here; this function covers the path-prefix gate only.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-003.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrTwinBinaryOutsideSearchPath is the sentinel error returned by
// CheckTwinBinaryPath when the resolved binary path is not under any
// configured twin-search-path prefix.
//
// The caller MUST classify this as FailureClassHarnessInternalError per
// specs/scenario-harness.md §5 SH-INV-003 and §8.5 closed-list detection
// item (ii): "real-model handler attempted launch per SH-INV-003".
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-003, §8.5.
var ErrTwinBinaryOutsideSearchPath = errors.New("twin binary outside search-path prefix: harness-internal-error per SH-INV-003")

// CheckTwinBinaryPath verifies that resolvedBinary's absolute path is under
// at least one of the configured twinSearchPaths, implementing the path-prefix
// predicate of specs/scenario-harness.md §5 SH-INV-003.
//
// The predicate: a binary is a twin iff its absolute resolved path is under
// the configured twin-binary search-path prefix per SH-009 item (b). Name-only
// heuristics (e.g. strings.HasSuffix("-twin")) are NOT sufficient and MUST NOT
// be used by the sensor; the predicate is purely path-prefix-based per §5
// SH-INV-003.
//
// HC-043 commit-hash verification is the daemon's responsibility and is NOT
// performed here; this function covers the path-prefix gate that must run at
// handler-config resolution time (§4.3) before orchestration begins.
//
// Path prefix detection uses filepath.Rel, which normalises both paths via
// filepath.Clean before comparison, making it robust against redundant
// separators and dot segments (including traversal attempts like
// <prefix>/../other that filepath.Clean resolves before the check).
//
// Returns nil when resolvedBinary is under at least one twinSearchPath.
// Returns an error wrapping ErrTwinBinaryOutsideSearchPath when no prefix
// matches. An empty twinSearchPaths slice always returns an error.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-003.
func CheckTwinBinaryPath(resolvedBinary string, twinSearchPaths []string) error {
	if len(twinSearchPaths) == 0 {
		return fmt.Errorf("%w: binary %q rejected (no twin-search-paths configured)",
			ErrTwinBinaryOutsideSearchPath, resolvedBinary)
	}

	cleanBinary := filepath.Clean(resolvedBinary)
	for _, searchPath := range twinSearchPaths {
		cleanPrefix := filepath.Clean(searchPath)
		rel, err := filepath.Rel(cleanPrefix, cleanBinary)
		if err != nil {
			// Paths on different volumes or other OS-level errors — not under
			// this prefix.
			continue
		}
		if !strings.HasPrefix(rel, "..") {
			// rel is "." (binary == prefix) or a sub-path without a leading
			// "..": the binary is under this search-path prefix.
			return nil
		}
	}

	return fmt.Errorf("%w: binary %q is not under any of the configured twin-search-paths %v",
		ErrTwinBinaryOutsideSearchPath, resolvedBinary, twinSearchPaths)
}
