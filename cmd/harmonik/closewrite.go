package main

// closewrite.go — the one predicate in package main for the half-close race
// every daemon client here runs into.
//
// Six files call it (comms.go, confirm_verdict.go, crew.go, decisions.go,
// run_via_daemon.go and sleepwake.go), so it lives on its own rather than in
// any one of them.
//
// Bead ref: hk-rycs9.

import (
	"errors"
	"syscall"
)

// isBenignCloseWrite reports whether a CloseWrite error can be ignored.
//
// Every daemon client in this package writes its request, calls CloseWrite to
// signal end of write, and only then reads the reply. The daemon can decode,
// respond and close before that statement runs, at which point CloseWrite
// returns ENOTCONN for an operation that already succeeded — and the reply is
// sitting in the receive buffer, complete, readable and ok. EPIPE is the same
// event reported from the other end of the shutdown. Neither one says the
// request failed, so a caller that exits non-zero on either tells the sender
// that a message the daemon accepted, recorded and answered had failed.
//
// What makes it safe to relax the check at all is the read that follows it.
// Every call site decodes the reply straight after this block and checks that
// error. So a request that truly never reached the daemon still fails, at the
// decode, where there is no reply to read — this predicate cannot turn a lost
// request into a success. It can only stop reporting a syscall that succeeded.
//
// ENOTCONN is the measured case. EPIPE is not: shutdown(2) does not document
// it on Darwin or Linux, so it is here as the same shutdown seen from the far
// end, and the decode covers us if that reasoning is wrong.
//
// The predicate is total: a nil error is benign, so a call site needs one check
// and not two. Every other error stays fatal. Two of them are worth naming,
// because both mean the caller made a mistake and neither is this race:
// CloseWrite returns a bare EINVAL for a nil connection, and it reports
// net.ErrClosed for a connection the caller already closed.
func isBenignCloseWrite(err error) bool {
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.ENOTCONN) || errors.Is(err, syscall.EPIPE)
}
