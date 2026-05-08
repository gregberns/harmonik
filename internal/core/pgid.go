package core

import "fmt"

// PGID is a typed process group identifier.  It wraps the int value returned
// by syscall.Getpgrp() and recorded in the pidfile (PL-002b line 2).  Every
// handler subprocess spawn sets SysProcAttr.Pgid to PGID.Int().
//
// PGID is a named type (not a Go alias) so that PGID and bare int values are
// not interchangeable at compile time.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "This PGID MUST be recorded
// in the pidfile per PL-002b (line 2).  On every handler subprocess spawn,
// the daemon MUST set Go's SysProcAttr{Setpgid: true, Pgid: <recorded_pgid>}."
type PGID int

// String returns the decimal string representation of the PGID.
func (p PGID) String() string {
	return fmt.Sprintf("%d", int(p))
}

// Int returns the underlying int value.  Use this when passing the PGID to
// syscall.SysProcAttr.Pgid or similar OS-level int parameters.
func (p PGID) Int() int {
	return int(p)
}
