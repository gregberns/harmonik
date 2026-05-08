package release_test

import (
	"regexp"
	"testing"

	"github.com/gregberns/harmonik/internal/release"
)

// manifestFixtureVersionRegex is a local copy of the BI-024a version regex
// (specs/beads-integration.md §4.8a) used to validate BeadsVersion's shape
// without importing internal/brcli (which owns the authoritative copy).
//
// The regex is reproduced here so the release package remains a leaf with no
// dependencies on the brcli adapter.  If the regex spec changes in BI-024a,
// both copies must be updated in the same commit.
var manifestFixtureVersionRegex = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// TestBeadsVersionMatchesVersionRegex verifies that BeadsVersion is a valid
// MAJOR.MINOR.PATCH string accepted by the BI-024a version regex shape.
//
// The BI-024a regex includes a `br\s+` prefix and an optional pre-release
// suffix; BeadsVersion is the bare numeric form (pre-release stripped) that
// CheckBrVersion compares against, so only the numeric core is validated here.
//
// Spec ref: specs/beads-integration.md §4.8a BI-024a.
func TestBeadsVersionMatchesVersionRegex(t *testing.T) {
	if !manifestFixtureVersionRegex.MatchString(release.BeadsVersion) {
		t.Errorf(
			"release.BeadsVersion %q does not match MAJOR.MINOR.PATCH shape %q",
			release.BeadsVersion,
			manifestFixtureVersionRegex.String(),
		)
	}
}
