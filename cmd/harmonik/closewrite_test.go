package main

// closewrite_test.go — table-driven coverage for isBenignCloseWrite, the
// predicate that decides whether a failed CloseWrite means the request failed.
//
// This test is the REPLACEMENT for coverage the tree lost. The canned test
// socket in comms_daemon_down_provenance_11zpm_test.go used to answer and close
// before it read the request, which made it the one fixture that generated the
// ordering the real daemon produces. It was repaired at 8480175eb because it
// also produced a DIFFERENT and spurious failure — a broken pipe on the request
// write, which the real daemon cannot cause. The repair made the fixture read
// to EOF first, so it can no longer close between a client's write and its
// CloseWrite, and nothing else asserts on that ordering.
//
// The error values below are built the same way daemon_down_predicates_test.go
// builds them: the real chain, errno inside *os.SyscallError inside
// *net.OpError, never a message string.
//
// Bead ref: hk-rycs9.

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

// closeWriteErr builds the *net.OpError chain (*net.UnixConn).CloseWrite
// returns for a failed shutdown(2): errno wrapped in *os.SyscallError wrapped
// in *net.OpError.
func closeWriteErr(errno syscall.Errno) error {
	return &net.OpError{
		Op:   "close",
		Net:  "unix",
		Addr: &net.UnixAddr{Name: "/tmp/harmonik/daemon.sock", Net: "unix"},
		Err:  os.NewSyscallError("shutdown", errno),
	}
}

// TestIsBenignCloseWrite pins the whole contract: the two errnos the race
// produces are benign, everything else stays fatal, nil is benign so a caller
// needs one check, and a caller's %w wrap does not change the answer.
func TestIsBenignCloseWrite(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			// The measured race: the daemon answered and closed between the
			// client's write and its CloseWrite.
			name: "ENOTCONN is the daemon closing after it answered",
			err:  closeWriteErr(syscall.ENOTCONN),
			want: true,
		},
		{
			name: "EPIPE is the same event from the other end of the shutdown",
			err:  closeWriteErr(syscall.EPIPE),
			want: true,
		},
		{
			name: "a foreign errno is still a real failure",
			err:  closeWriteErr(syscall.EIO),
			want: false,
		},
		{
			// What (*net.UnixConn).CloseWrite returns for a nil connection: a
			// bare errno, not wrapped in anything. A caller defect, not this
			// race, so it must stay fatal.
			name: "a bare EINVAL is a caller defect and stays fatal",
			err:  syscall.EINVAL,
			want: false,
		},
		{
			// The shape CloseWrite really produces for a connection the caller
			// already closed. It carries no errno at all, so it can only stay
			// fatal by staying unmatched.
			name: "an already-closed connection is a caller defect too",
			err:  &net.OpError{Op: "close", Net: "unix", Err: net.ErrClosed},
			want: false,
		},
		{
			name: "no error at all is benign, so one check does",
			err:  nil,
			want: true,
		},
		{
			name: "a caller's %w context does not change the answer",
			err:  fmt.Errorf("close request write side: %w", closeWriteErr(syscall.ENOTCONN)),
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBenignCloseWrite(tc.err); got != tc.want {
				t.Errorf("isBenignCloseWrite(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
