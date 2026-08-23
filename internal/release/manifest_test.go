package release_test

import (
	"regexp"
	"testing"

	"github.com/gregberns/harmonik/internal/release"
)

var manifestFixtureVersionRegex = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// TestBeadsVersionMatchesVersionRegex verifies that BeadsVersion is a valid
// MAJOR.MINOR.PATCH string.
//
// BI-024 requires each harmonik release to NAME the Beads version it tested
// against, so the constant must read as a version and not as "latest" or "v1.2".
// No code compares it against the installed `br`: BI-024a is now an existence
// check only. This test guards the manifest's readability, nothing else.
//
// Spec ref: specs/beads-integration.md §4.8 BI-024.
func TestBeadsVersionMatchesVersionRegex(t *testing.T) {
	if !manifestFixtureVersionRegex.MatchString(release.BeadsVersion) {
		t.Errorf(
			"release.BeadsVersion %q does not match MAJOR.MINOR.PATCH shape %q",
			release.BeadsVersion,
			manifestFixtureVersionRegex.String(),
		)
	}
}
