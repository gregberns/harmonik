package lifecycle

// probePidCmdline attempts to read the executable path for pid on darwin.
// darwin's proc_pidpath requires cgo (libproc.h); since this project does not
// use cgo, corroboration is unavailable on darwin and we return ("", false).
//
// OQ-PL-007 tracks the resolution of PID-reuse disambiguation on darwin. Until
// that OQ is resolved and cgo support is added, the caller falls through to the
// PidfileLockStatusAmbiguous path on darwin when kill(pid, 0) succeeds.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — "The daemon MAY corroborate
// via proc_pidpath (darwin) to disambiguate recycled PIDs."
func probePidCmdline(_ int) (string, bool) {
	return "", false
}
