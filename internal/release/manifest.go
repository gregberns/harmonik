// Package release holds the harmonik release manifest constants.
//
// These values are the structured artifact required by BI-024: each harmonik
// release MUST name the Beads version it tested against, and the compatibility
// window at MVH is exact-match (isCompatible(pinned, observed) ≡ pinned ==
// observed).
//
// Callers of internal/brcli.(*Adapter).CheckBrVersion should pass
// [BeadsVersion] as the pinnedVersion argument.  Daemon-startup wiring is
// deferred until cmd/harmonik/ lands.
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
const BeadsVersion = "0.1.45"
