package lifecycle

import (
	"syscall"

	"github.com/gregberns/harmonik/internal/core"
)

// SpawnChildSysProcAttr returns a *syscall.SysProcAttr that places the handler
// subprocess into the daemon's process group AND installs PR_SET_PDEATHSIG(SIGTERM)
// so the kernel sends SIGTERM to the child when the daemon thread exits.
//
// Linux-specific: Pdeathsig is a Linux-only field in SysProcAttr; the darwin
// build (spawndaemonchild_darwin.go) omits it because macOS has no equivalent
// (see §4.10.HC-044a in specs/handler-contract.md).
//
// Spec ref: specs/handler-contract.md §4.10.HC-044 — "The daemon MUST spawn
// every handler subprocess as a direct child process. On Linux, handler
// subprocesses SHOULD install PR_SET_PDEATHSIG(SIGTERM) at spawn time."
func SpawnChildSysProcAttr(pgid core.PGID) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pgid:      pgid.Int(),
		Pdeathsig: syscall.SIGTERM,
	}
}
