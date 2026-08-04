// Package release holds the harmonik release manifest constants and ledger.
//
// These values are the structured artifact required by BI-024: each harmonik
// release MUST name the Beads version it tested against.
//
// [BeadsVersion] is a record, not a gate. No code compares it against the
// installed `br`. Daemon startup only confirms `br` is runnable — see
// internal/brcli.(*Adapter).CheckBrRunnable.
//
// The release ledger ([Ledger]) records every harmonik release entry. It is the
// compiled-in snapshot of the ledger; the mutable ledger persisted on disk is
// managed by [LedgerFile]. Spec ref: specs/release-pipeline.md §4.
package release

// BeadsVersion is the Beads CLI version that this harmonik release was tested
// against. It is documentation for a human reading the manifest. Nothing reads
// it at run time.
//
// It used to be the pin that daemon startup enforced. That enforcement is gone
// (operator direction, 2026-08-04): the fleet ran 754 beads across a version gap
// with no adapter failure, and the pin was the sole cause of every restart
// failure over the same period. Startup now only asks whether `br` runs — see
// internal/brcli.(*Adapter).CheckBrRunnable.
//
// Bumping this constant MUST still be accompanied by an adapter change for every
// backwards-incompatible Beads change per BI-026 (specs/beads-integration.md
// §4.8). Silent upgrades are forbidden; see BI-024.
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
