// Package release holds the harmonik release manifest constants and ledger.
//
// These values are the structured artifact required by BI-024: each harmonik
// release MUST name the Beads version it tested against, and the compatibility
// window at MVH is exact-match (isCompatible(pinned, observed) ≡ pinned ==
// observed).
//
// Callers of internal/brcli.(*Adapter).CheckBrVersion should pass
// [BeadsVersion] as the pinnedVersion argument.  Daemon-startup wiring is
// deferred until cmd/harmonik/ lands.
//
// The release ledger ([Ledger]) records every harmonik release entry. It is the
// compiled-in snapshot of the ledger; the mutable ledger persisted on disk is
// managed by [LedgerFile]. Spec ref: specs/release-pipeline.md §4.
package release

// BeadsVersion is the Beads CLI version that this harmonik release was tested
// against.  At MVH the compatibility window is exact-match per BI-024
// (specs/beads-integration.md §4.8): the observed `br --version` output MUST
// equal this value (after pre-release suffix stripping) or daemon startup MUST
// fail with exit code 8 (beads-unavailable).
//
// Bumping this constant MUST be accompanied by an adapter change for every
// backwards-incompatible Beads change per BI-026 (specs/beads-integration.md
// §4.8).  Silent upgrades are forbidden; see BI-024.
// Bumped 0.1.45 → 0.2.10 (2026-06-23): the installed br has been 0.2.10 since
// 2026-05-19 and the fleet executed 754 run_completed beads on it through the
// full dispatch lifecycle with the existing adapter — empirically proving no
// backwards-incompatible change requiring a BI-026 adapter update. The stale
// pin (never updated when br moved to 0.2.10) was the sole daemon-startup
// blocker once the BI-024a handshake landed on the daemon path.
const BeadsVersion = "0.2.10"

// ReleaseEntry records a single harmonik release in the ledger.
//
// Spec ref: specs/release-pipeline.md §4.2.
type ReleaseEntry struct {
	// Semver is the release version string, e.g. "v0.2.0".
	Semver string `json:"semver"`

	// CommitHash is the full 40-character git SHA of the tagged commit.
	CommitHash string `json:"commit_hash"`

	// Tag is the git tag name, e.g. "v0.2.0".
	Tag string `json:"tag"`

	// Prerelease is true from CREATE through VALIDATE. CERTIFY flips it false.
	Prerelease bool `json:"prerelease"`

	// CertifiedAt is the RFC3339 timestamp when CERTIFY ran. Empty means not yet certified.
	CertifiedAt string `json:"certified_at,omitempty"`

	// Yanked is true if this release was withdrawn after certification.
	Yanked bool `json:"yanked,omitempty"`

	// YankedReason is a human-readable explanation of why the release was yanked.
	// MUST be non-empty whenever Yanked is true.
	YankedReason string `json:"yanked_reason,omitempty"`

	// Artifacts holds per-binary checksums produced by goreleaser.
	Artifacts []ArtifactEntry `json:"artifacts,omitempty"`
}

// ArtifactEntry records one binary artifact in a release.
//
// Spec ref: specs/release-pipeline.md §4.2.
type ArtifactEntry struct {
	// Name is the artifact filename, e.g. "harmonik_linux_amd64".
	Name string `json:"name"`

	// OS is the GOOS value, e.g. "linux".
	OS string `json:"os"`

	// Arch is the GOARCH value, e.g. "amd64".
	Arch string `json:"arch"`

	// SHA256 is the lowercase hex SHA-256 checksum of the artifact binary.
	SHA256 string `json:"sha256"`
}

// Ledger is the compiled-in release ledger. Updated by the CERTIFY CI step via
// code generation. Spec ref: specs/release-pipeline.md §4.4.
//
//nolint:gochecknoglobals // ledger is a compile-time artifact updated by CI
var Ledger = []ReleaseEntry{}
