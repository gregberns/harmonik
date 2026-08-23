package lifecycle

import (
	"syscall"
	"testing"
)

func i3151PlatformVerifyPdeathsig(_ *testing.T, _ *syscall.SysProcAttr) {
}

// TestHC044_SpawnChildSysProcAttr_DarwinNoPdeathsig documents that on darwin
// SpawnChildSysProcAttr correctly omits Pdeathsig (the field does not exist on
// this platform). The test is structural: if the function compiled with a
// Pdeathsig assignment on darwin, this file would fail to compile.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044 — "macOS has no equivalent."
func TestHC044_SpawnChildSysProcAttr_DarwinNoPdeathsig(t *testing.T) {
	t.Parallel()

	attr := SpawnChildSysProcAttr(i3151PGIDValue)

	if attr == nil {
		t.Fatal("HC-044 darwin: SpawnChildSysProcAttr returned nil")
	}
	if !attr.Setpgid {
		t.Error("HC-044 darwin: Setpgid = false, want true")
	}
	i3151PlatformVerifyPdeathsig(t, attr)
}
