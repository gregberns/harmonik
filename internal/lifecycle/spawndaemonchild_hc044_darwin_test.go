package lifecycle

import (
	"syscall"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// i3151PlatformVerifyPdeathsig is the darwin implementation of the Pdeathsig
// assertion helper. On darwin, SysProcAttr has no Pdeathsig field; subprocess
// survival across daemon death is a platform reality per HC-044a.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044 — "macOS has no equivalent
// and subprocess survival across daemon death is a platform reality addressed
// by §4.10.HC-044a."
func i3151PlatformVerifyPdeathsig(_ *testing.T, _ *syscall.SysProcAttr) {
	// darwin: no Pdeathsig field on SysProcAttr; nothing to assert.
}

// TestHC044_SpawnChildSysProcAttr_DarwinNoPdeathsig documents that on darwin
// SpawnChildSysProcAttr correctly omits Pdeathsig (the field does not exist on
// this platform). The test is structural: if the function compiled with a
// Pdeathsig assignment on darwin, this file would fail to compile.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044 — "macOS has no equivalent."
func TestHC044_SpawnChildSysProcAttr_DarwinNoPdeathsig(t *testing.T) {
	t.Parallel()

	attr := SpawnChildSysProcAttr(core.PGID(i3151PGIDValue))

	// Structural: compilation of this file proves no Pdeathsig was set.
	// Runtime: confirm the attr is non-nil and Setpgid is correct.
	if attr == nil {
		t.Fatal("HC-044 darwin: SpawnChildSysProcAttr returned nil")
	}
	if !attr.Setpgid {
		t.Error("HC-044 darwin: Setpgid = false, want true")
	}
}
