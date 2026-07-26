package main

// socket_error_predicates_test.go — table-driven coverage for the daemon-socket
// error predicates in run_via_daemon.go.
//
// These predicates decide whether `harmonik run` reports "daemon is down"
// (exit 17) or a hard failure, so a false negative silently turns a routine
// "daemon not running" into an opaque error, and a false positive hides a real
// dial failure behind exit 17. They used to match on err.Error() substrings;
// they now match on error identity, which only holds if every wrapper in the
// real dial chain unwraps to the sentinel. The wrapped-chain cases below are
// the shapes net.Dial actually produces for a unix socket, so a regression in
// the unwrap chain fails here rather than in production.

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"syscall"
	"testing"
)

// unixOpError builds the *net.OpError shape net.Dial returns for a unix-socket
// dial failure: the syscall error is wrapped in *os.SyscallError, which is
// wrapped in *net.OpError.
func unixOpError(errno syscall.Errno) error {
	return &net.OpError{
		Op:   "dial",
		Net:  "unix",
		Addr: &net.UnixAddr{Name: "/tmp/harmonik.sock", Net: "unix"},
		Err:  os.NewSyscallError("connect", errno),
	}
}

func TestIsViaSocketAbsent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unix dial ENOENT (the real missing-socket shape)",
			err:  unixOpError(syscall.ENOENT),
			want: true,
		},
		{
			name: "OpError wrapping a PathError with ENOENT",
			err: &net.OpError{
				Op:  "dial",
				Net: "unix",
				Err: &os.PathError{Op: "stat", Path: "/tmp/harmonik.sock", Err: syscall.ENOENT},
			},
			want: true,
		},
		{
			name: "OpError further wrapped by fmt.Errorf %w",
			err:  fmt.Errorf("connect to daemon: %w", unixOpError(syscall.ENOENT)),
			want: true,
		},
		{
			name: "connection refused is not absence",
			err:  unixOpError(syscall.ECONNREFUSED),
			want: false,
		},
		{
			name: "permission denied is not absence",
			err:  unixOpError(syscall.EACCES),
			want: false,
		},
		{
			name: "a bare ENOENT that never reached the socket layer",
			err:  fs.ErrNotExist,
			want: false,
		},
		{
			name: "an unrelated error whose text mentions the old substring",
			err:  errors.New("config file: no such file or directory"),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isViaSocketAbsent(tc.err); got != tc.want {
				t.Errorf("isViaSocketAbsent(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsViaConnRefused(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unix dial ECONNREFUSED (the real stale-socket shape)",
			err:  unixOpError(syscall.ECONNREFUSED),
			want: true,
		},
		{
			name: "ECONNREFUSED further wrapped by fmt.Errorf %w",
			err:  fmt.Errorf("submit: %w", unixOpError(syscall.ECONNREFUSED)),
			want: true,
		},
		{
			name: "bare errno",
			err:  syscall.ECONNREFUSED,
			want: true,
		},
		{
			name: "ENOENT is not refusal",
			err:  unixOpError(syscall.ENOENT),
			want: false,
		},
		{
			name: "an unrelated error whose text mentions the old substring",
			err:  errors.New("upstream said: connection refused"),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isViaConnRefused(tc.err); got != tc.want {
				t.Errorf("isViaConnRefused(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestViaSocketPredicates_AreMutuallyExclusive pins the property the exit-17
// mapping depends on: a single dial error must not satisfy both predicates,
// because the caller ORs them into one exit code and a double match would
// hide which of the two conditions actually occurred.
func TestViaSocketPredicates_AreMutuallyExclusive(t *testing.T) {
	for _, errno := range []syscall.Errno{
		syscall.ENOENT, syscall.ECONNREFUSED, syscall.EACCES, syscall.EAGAIN,
	} {
		err := unixOpError(errno)
		if isViaSocketAbsent(err) && isViaConnRefused(err) {
			t.Errorf("errno %v matched both isViaSocketAbsent and isViaConnRefused", errno)
		}
	}
}
