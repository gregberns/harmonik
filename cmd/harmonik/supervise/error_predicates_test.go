package supervisecmd

// error_predicates_test.go — table-driven coverage for the supervisor's
// socket/lock error predicates.
//
// isSocketAbsent and isConnectionRefused gate the supervisor's "is the daemon
// up?" probe, and isWouldBlock gates the flock-based single-instance guard: a
// false isWouldBlock means the supervisor treats "another supervisor already
// holds the lock" as a hard error instead of the documented exit-17 path.
// All three are identity checks over errors the syscall layer wraps, so the
// wrapped cases below are the ones that actually matter.

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"syscall"
	"testing"
)

func opErr(errno syscall.Errno) error {
	return &net.OpError{
		Op:  "dial",
		Net: "unix",
		Err: os.NewSyscallError("connect", errno),
	}
}

func TestIsSocketAbsent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"PathError ENOENT", &os.PathError{Op: "stat", Path: "/x", Err: syscall.ENOENT}, true},
		{"wrapped PathError ENOENT", fmt.Errorf("probe: %w", &os.PathError{Op: "stat", Path: "/x", Err: syscall.ENOENT}), true},
		{"fs.ErrNotExist sentinel", fs.ErrNotExist, true},
		{"dial ENOENT", opErr(syscall.ENOENT), true},
		{"permission denied", &os.PathError{Op: "open", Path: "/x", Err: syscall.EACCES}, false},
		{"unrelated error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSocketAbsent(tc.err); got != tc.want {
				t.Errorf("isSocketAbsent(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsConnectionRefused(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"OpError wrapping SyscallError ECONNREFUSED", opErr(syscall.ECONNREFUSED), true},
		{"OpError carrying a bare errno", &net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED}, true},
		{"further wrapped by fmt.Errorf", fmt.Errorf("probe daemon: %w", opErr(syscall.ECONNREFUSED)), true},
		{"bare errno", syscall.ECONNREFUSED, true},
		{"ENOENT is not refusal", opErr(syscall.ENOENT), false},
		{"unrelated error", errors.New("connection refused"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConnectionRefused(tc.err); got != tc.want {
				t.Errorf("isConnectionRefused(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsWouldBlock(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"bare EWOULDBLOCK", syscall.EWOULDBLOCK, true},
		{"bare EAGAIN", syscall.EAGAIN, true},
		{"flock EWOULDBLOCK wrapped in SyscallError", os.NewSyscallError("flock", syscall.EWOULDBLOCK), true},
		{"wrapped by fmt.Errorf", fmt.Errorf("acquire supervisor lock: %w", syscall.EWOULDBLOCK), true},
		{"EACCES is not would-block", syscall.EACCES, false},
		{"unrelated error", errors.New("resource temporarily unavailable"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWouldBlock(tc.err); got != tc.want {
				t.Errorf("isWouldBlock(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
