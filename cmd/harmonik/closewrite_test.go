package main

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

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
			name: "a bare EINVAL is a caller defect and stays fatal",
			err:  syscall.EINVAL,
			want: false,
		},
		{
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
