package lifecycle

import (
	"syscall"

	"github.com/gregberns/harmonik/internal/core"
)

// SpawnChildSysProcAttr returns a *syscall.SysProcAttr that places the handler
// subprocess into the daemon's process group.
//
// darwin-specific: macOS has no equivalent to Linux PR_SET_PDEATHSIG; the
// Pdeathsig field does not exist on darwin's SysProcAttr. Subprocess survival
// across daemon death is a platform reality addressed by
// specs/handler-contract.md §4.10.HC-044a.
//
// Spec ref: specs/handler-contract.md §4.10.HC-044 — "macOS has no equivalent
// and subprocess survival across daemon death is a platform reality addressed
// by §4.10.HC-044a."
func SpawnChildSysProcAttr(pgid core.PGID) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    pgid.Int(),
	}
}
