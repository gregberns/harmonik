package brcli

// TODO(hk-872.28): When BrError enum lands, classify Run's exit codes via that
// taxonomy; ErrBrVersionIncompatible will be informed by BrUnavailable / BrSchemaMismatch.
// TODO(hk-872.30): When read-timeout discipline lands, the 5s read timeout will
// wrap ctx automatically; no explicit timeout needed here.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// ErrBrVersionIncompatible is returned by CheckBrVersion when the observed
// `br --version` output does not match the pinned version OR the output does
// not match the required version regex.
//
// Per BI-024a: either condition MUST fail daemon startup with exit code 8
// (beads-unavailable). The caller (daemon startup, PL-005 step 4 Cat 0
// pre-check) is responsible for translating this error to exit code 8 and
// emitting daemon_startup_failed{failure_mode="br-version-incompatible"}.
//
// TODO(hk-872.28): Full BrError integration may absorb this sentinel as BrSchemaMismatch.
var ErrBrVersionIncompatible = errors.New("brcli: br version incompatible")

// brVersionRegex is the regex specified in BI-024a for parsing `br --version`
// output. It matches output of the form `br <major>.<minor>.<patch>[pre-release]`
// and captures the three numeric components plus the optional pre-release suffix.
//
// Spec ref: specs/beads-integration.md §4.8a BI-024a.
//   - Regex: `br\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?`
var brVersionRegex = regexp.MustCompile(`br\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?`)

// CheckBrVersion invokes `br --version`, parses the output, and compares the
// observed version against pinnedVersion using the exact-match compatibility
// window defined by BI-024 (isCompatible(pinned, observed) ≡ pinned ==
// observed).
//
// pinnedVersion is the version string declared in the harmonik release manifest
// per BI-024.  Pass [internal/release.BeadsVersion] as this argument; that
// constant is the structured release-manifest artifact introduced by hk-872.25.
// Daemon-startup wiring (PL-005 step 4) is deferred until cmd/harmonik/ lands.
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
//   - Exec failure launching `br`        → wrapped exec error (no sentinel)
//   - Non-zero br exit (any reason)      → ErrBrVersionIncompatible (unparseable)
//   - Output does not match version regex → ErrBrVersionIncompatible
//   - Observed version != pinnedVersion  → ErrBrVersionIncompatible
//
// The caller MUST treat ANY non-nil error from CheckBrVersion as a startup-
// blocking failure: translate to daemon startup exit code 8 and emit
// daemon_startup_failed{failure_mode="br-version-incompatible"}. This includes
// both ErrBrVersionIncompatible (sentinel-wrapped) AND the plain exec-failure
// case above (br binary missing / not executable / etc.) — exit code 8 is
// uniform for "br unavailable in any way at startup."
func (a *Adapter) CheckBrVersion(ctx context.Context, pinnedVersion string) error {
	result, err := a.Run(ctx, "--version")
	if err != nil {
		return fmt.Errorf("brcli.CheckBrVersion: exec failed: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf(
			"brcli.CheckBrVersion: br --version exited %d (unparseable): %w",
			result.ExitCode,
			ErrBrVersionIncompatible,
		)
	}

	output := string(result.Stdout)
	match := brVersionRegex.FindStringSubmatch(output)
	if match == nil {
		return fmt.Errorf(
			"brcli.CheckBrVersion: output does not match version regex %q: %w",
			output,
			ErrBrVersionIncompatible,
		)
	}

	// Reconstruct the observed version as "MAJOR.MINOR.PATCH".
	// match[1]=major, match[2]=minor, match[3]=patch.
	observedVersion := match[1] + "." + match[2] + "." + match[3]

	// BI-024 exact-match compatibility window: isCompatible(pinned, observed) ≡ pinned == observed.
	if observedVersion != pinnedVersion {
		return fmt.Errorf(
			"brcli.CheckBrVersion: version mismatch: pinned=%q observed=%q: %w",
			pinnedVersion,
			observedVersion,
			ErrBrVersionIncompatible,
		)
	}

	return nil
}
