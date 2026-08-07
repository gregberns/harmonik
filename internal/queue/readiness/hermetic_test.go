package readiness

import (
	"testing"

	"github.com/gregberns/harmonik/internal/testhelpers/hermetic"
)

// TestMain makes this package's tests give the same answer on any machine.
//
// hermetic.Main redirects the global gitconfig and Claude Code's config and
// transcript paths at a temporary directory, so a fixture never reads or writes
// the operator's real home. Without it a common operator setting such as
// commit.gpgsign=true fails every git fixture here, and the answer an outside
// assessor gets is not the answer we get.
//
// See internal/testhelpers/hermetic for the full list of seams and the reason
// each one is on it.
func TestMain(m *testing.M) { hermetic.Main(m) }
