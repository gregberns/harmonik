package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"syscall"
	"testing"
)

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
