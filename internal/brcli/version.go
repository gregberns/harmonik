package brcli

import (
	"context"
	"fmt"
	"regexp"
)

// brVersionRegex is the regex specified in BI-024a for parsing `br --version`
// output. It matches output of the form `br <major>.<minor>.<patch>[pre-release]`
// and captures the three numeric components plus the optional pre-release suffix.
//
// Spec ref: specs/beads-integration.md §4.8a BI-024a.
//   - Regex: `br\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?`
var brVersionRegex = regexp.MustCompile(`br\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?`)

// errBrVersionMismatch is the unexported type backing ErrBrVersionMismatch.
// Unexported so callers can only match via errors.Is; they cannot construct it.
type errBrVersionMismatch struct{}

func (errBrVersionMismatch) Error() string {
	return "brcli: br version differs from pinned version (BI-024a notice)"
}

// ErrBrVersionMismatch is returned by CheckBrVersion when the installed br
// version does not match the pinned version but br is otherwise usable
// (output parsed successfully, binary executes). Callers MUST treat this as a
// notice, NOT a fatal error — the daemon MUST proceed and log the version
// delta at notice level (an expected, benign condition).
//
// This is distinct from BrSchemaMismatch (closed BrError enum, exit-code 4)
// which signals a genuine schema incompatibility detected at call time. A
// version-string delta alone is not proof of schema incompatibility: the fleet
// executed 754 runs on br=0.2.10 while pinned at 0.1.45 without any adapter
// failure (2026-05-19 → 2026-06-23). Exact-match enforcement was the sole
// blocker in each restart during that period.
//
// Spec ref: specs/beads-integration.md §4.8a BI-024a (amended by hk-m6243).
var ErrBrVersionMismatch = errBrVersionMismatch{}

// CheckBrVersion invokes `br --version`, parses the output, and compares the
// observed version against pinnedVersion per the compatibility policy of
// BI-024a (amended by hk-m6243).
//
// pinnedVersion is the version string declared in the harmonik release manifest
// per BI-024. Pass [internal/release.BeadsVersion] as this argument; that
// constant is the structured release-manifest artifact introduced by hk-872.25.
//
// The version regex `br\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?` extracts
// the full dotted version token from the `br --version` output; the observed
// version is reconstructed as `<major>.<minor>.<patch>` with no pre-release
// suffix, so pinnedVersion MUST be supplied in the same `MAJOR.MINOR.PATCH`
// form.
//
// Spec ref: specs/beads-integration.md §4.8a BI-024a.
//
// Error semantics:
//   - Exec failure launching `br`         → wrapped exec error (no sentinel)
//   - Non-zero br exit (any reason)       → wraps BrSchemaMismatch (unparseable)
//   - Output does not match version regex → wraps BrSchemaMismatch
//   - Observed version != pinnedVersion   → wraps ErrBrVersionMismatch (WARNING only)
//
// The caller MUST treat exec-failure and BrSchemaMismatch-wrapping errors as
// startup-blocking failures (translate to daemon startup exit code 8, emit
// daemon_startup_failed{failure_mode="br-version-incompatible"}).
//
// The caller MUST treat ErrBrVersionMismatch as a loud WARNING: log
// prominently and continue — the daemon MUST NOT exit on a benign version
// delta. Callers test with errors.Is(err, ErrBrVersionMismatch).
//
// Spec ref: specs/beads-integration.md §6.1a, §4.8a BI-024a.
func (a *Adapter) CheckBrVersion(ctx context.Context, pinnedVersion string) error {
	result, err := a.Run(ctx, "--version")
	if err != nil {
		return fmt.Errorf("brcli.CheckBrVersion: exec failed: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf(
			"brcli.CheckBrVersion: br --version exited %d (unparseable): %w",
			result.ExitCode,
			BrSchemaMismatch,
		)
	}

	output := string(result.Stdout)
	match := brVersionRegex.FindStringSubmatch(output)
	if match == nil {
		return fmt.Errorf(
			"brcli.CheckBrVersion: output does not match version regex %q: %w",
			output,
			BrSchemaMismatch,
		)
	}

	// Reconstruct the observed version as "MAJOR.MINOR.PATCH".
	// match[1]=major, match[2]=minor, match[3]=patch.
	observedVersion := match[1] + "." + match[2] + "." + match[3]

	// BI-024a compatibility policy (amended by hk-m6243): a version delta is a
	// WARNING, not a fatal error. Hard-fail only on exec failure or parse failure
	// (above). The fleet proved 754 successful runs across a 0.1.45→0.2.10
	// version gap; exact-match enforcement was the sole restart blocker.
	if observedVersion != pinnedVersion {
		return fmt.Errorf(
			"brcli.CheckBrVersion: version mismatch: pinned=%q observed=%q: %w",
			pinnedVersion,
			observedVersion,
			ErrBrVersionMismatch,
		)
	}

	return nil
}
