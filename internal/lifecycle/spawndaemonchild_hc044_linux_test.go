package lifecycle

import (
	"syscall"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// i3151PlatformVerifyPdeathsig is the Linux implementation of the Pdeathsig
// assertion helper. On Linux, SysProcAttr.Pdeathsig MUST be syscall.SIGTERM
// per HC-044.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044 — "On Linux, handler
// subprocesses SHOULD install PR_SET_PDEATHSIG(SIGTERM) at spawn time."
func i3151PlatformVerifyPdeathsig(t *testing.T, attr *syscall.SysProcAttr) {
	t.Helper()

	if attr.Pdeathsig != syscall.SIGTERM {
		t.Errorf("HC-044 Linux: Pdeathsig = %v, want SIGTERM (%v)", attr.Pdeathsig, syscall.SIGTERM)
	}
}

// TestHC044_SpawnChildSysProcAttr_LinuxPdeathsig verifies that on Linux,
// SpawnChildSysProcAttr installs PR_SET_PDEATHSIG(SIGTERM) so the kernel
// sends SIGTERM to the handler subprocess when the daemon thread exits.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044 — "On Linux, handler
// subprocesses SHOULD install PR_SET_PDEATHSIG(SIGTERM) at spawn time."
func TestHC044_SpawnChildSysProcAttr_LinuxPdeathsig(t *testing.T) {
	t.Parallel()

	attr := SpawnChildSysProcAttr(core.PGID(i3151PGIDValue))

	if attr == nil {
		t.Fatal("HC-044 Linux: SpawnChildSysProcAttr returned nil")
	}
	i3151VerifyPdeathsig(t, attr)
}
