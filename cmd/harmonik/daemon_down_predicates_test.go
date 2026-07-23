package main

// daemon_down_predicates_test.go — table-driven coverage for every "is the
// daemon down?" predicate family in package main.
//
// cmd/harmonik carries FOUR independent implementations of the same question
// (run_via_daemon.go, sleepwake.go, comms.go, confirm_verdict.go), each feeding
// the same operator-visible decision: exit 17 "daemon not running" versus a
// hard error. They are duplicated, so they can — and did — drift: two of them
// matched on err.Error() substrings until this lane converted them to error
// identity.
//
// These tests pin the shared contract across all four so a future edit to one
// cannot silently disagree with the others:
//
//	absent(ENOENT dial)      == true      for every family
//	absent(ECONNREFUSED)     == false     for every family
//	refused(ECONNREFUSED)    == true      for every family
//	refused(ENOENT dial)     == false     for every family
//	neither predicate fires on an unrelated error whose *message text*
//	contains "no such file or directory" or "connection refused"
//
// That last case is the regression that matters: under substring matching an
// error merely quoting those words mapped to "daemon is down", which tells the
// operator to start a daemon that is already running.

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

// dialErr builds the *net.OpError chain net.Dial returns for a failed
// unix-socket connect: errno wrapped in *os.SyscallError wrapped in
// *net.OpError.
func dialErr(errno syscall.Errno) error {
	return &net.OpError{
		Op:   "dial",
		Net:  "unix",
		Addr: &net.UnixAddr{Name: "/tmp/harmonik/daemon.sock", Net: "unix"},
		Err:  os.NewSyscallError("connect", errno),
	}
}

// daemonDownFamily names one package-main implementation of the pair.
type daemonDownFamily struct {
	name    string
	absent  func(error) bool
	refused func(error) bool
}

func daemonDownFamilies() []daemonDownFamily {
	return []daemonDownFamily{
		{"run_via_daemon", isViaSocketAbsent, isViaConnRefused},
		{"sleepwake", isSleepWakeSocketAbsent, isSleepWakeConnRefused},
		{"comms", commsIsSocketAbsent, commsIsConnRefused},
		{"confirm_verdict", isVerdictSocketAbsent, isVerdictConnectionRefused},
	}
}

// TestDaemonDownPredicates_AgreeAcrossFamilies is the cross-implementation
// consistency check: every family must answer the same way for the dial errors
// that actually occur.
func TestDaemonDownPredicates_AgreeAcrossFamilies(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		wantAbsent  bool
		wantRefused bool
	}{
		{
			name:        "socket file does not exist (Linux connect ENOENT)",
			err:         dialErr(syscall.ENOENT),
			wantAbsent:  true,
			wantRefused: false,
		},
		{
			name:        "socket file exists but nothing is listening",
			err:         dialErr(syscall.ECONNREFUSED),
			wantAbsent:  false,
			wantRefused: true,
		},
		{
			name: "an unrelated error that only MENTIONS a missing file",
			// The substring-matching implementations returned true here, which
			// reported a live daemon as down.
			err:         errors.New(`open /etc/harmonik.conf: no such file or directory`),
			wantAbsent:  false,
			wantRefused: false,
		},
		{
			name:        "an unrelated error that only MENTIONS connection refused",
			err:         errors.New(`upstream reported: connection refused`),
			wantAbsent:  false,
			wantRefused: false,
		},
		{
			name:        "a permission failure is neither absence nor refusal",
			err:         dialErr(syscall.EACCES),
			wantAbsent:  false,
			wantRefused: false,
		},
	}

	for _, fam := range daemonDownFamilies() {
		for _, tc := range cases {
			t.Run(fam.name+"/"+tc.name, func(t *testing.T) {
				if got := fam.absent(tc.err); got != tc.wantAbsent {
					t.Errorf("absent(%v) = %v, want %v", tc.err, got, tc.wantAbsent)
				}
				if got := fam.refused(tc.err); got != tc.wantRefused {
					t.Errorf("refused(%v) = %v, want %v", tc.err, got, tc.wantRefused)
				}
			})
		}
	}
}

// TestDaemonDownPredicates_SeeThroughWrapping pins that every family unwraps.
// A caller that adds context with %w must not change the exit code.
func TestDaemonDownPredicates_SeeThroughWrapping(t *testing.T) {
	for _, fam := range daemonDownFamilies() {
		t.Run(fam.name, func(t *testing.T) {
			wrappedAbsent := fmt.Errorf("connect to daemon: %w", dialErr(syscall.ENOENT))
			if !fam.absent(wrappedAbsent) {
				t.Errorf("absent(wrapped ENOENT) = false, want true")
			}
			wrappedRefused := fmt.Errorf("connect to daemon: %w", dialErr(syscall.ECONNREFUSED))
			if !fam.refused(wrappedRefused) {
				t.Errorf("refused(wrapped ECONNREFUSED) = false, want true")
			}
		})
	}
}

// TestDaemonDownPredicates_MutuallyExclusive pins the property the exit-code
// mapping relies on: the two predicates are ORed into one exit-17 decision, so
// a single error must never satisfy both.
func TestDaemonDownPredicates_MutuallyExclusive(t *testing.T) {
	for _, fam := range daemonDownFamilies() {
		for _, errno := range []syscall.Errno{
			syscall.ENOENT, syscall.ECONNREFUSED, syscall.EACCES,
			syscall.EINVAL, syscall.EAGAIN, syscall.EPIPE,
		} {
			err := dialErr(errno)
			if fam.absent(err) && fam.refused(err) {
				t.Errorf("%s: errno %v satisfied both absent and refused", fam.name, errno)
			}
		}
	}
}

// TestCommsAndVerdictAcceptEINVAL documents the platform split the other two
// families do not carry: on macOS, connect(2) to a path with no socket file
// returns EINVAL rather than ENOENT. comms and confirm_verdict treat that as
// absence; run_via_daemon and sleepwake do not.
//
// This test exists to make the divergence VISIBLE and intentional rather than
// accidental. If the two strict families are ever taught EINVAL too, this test
// is the place that says why they were not before.
func TestCommsAndVerdictAcceptEINVAL(t *testing.T) {
	err := dialErr(syscall.EINVAL)

	if !commsIsSocketAbsent(err) {
		t.Errorf("commsIsSocketAbsent(EINVAL) = false, want true (macOS missing-socket shape)")
	}
	if !isVerdictSocketAbsent(err) {
		t.Errorf("isVerdictSocketAbsent(EINVAL) = false, want true (macOS missing-socket shape)")
	}
	if isViaSocketAbsent(err) {
		t.Errorf("isViaSocketAbsent(EINVAL) = true; this family deliberately matches ENOENT only")
	}
	if isSleepWakeSocketAbsent(err) {
		t.Errorf("isSleepWakeSocketAbsent(EINVAL) = true; this family deliberately matches ENOENT only")
	}
}

// TestIsConnectionClosed covers the subscribe-stream shutdown classifier. A
// false negative here prints a spurious "subscribe stream error" every time the
// operator hits Ctrl-C or the daemon shuts down cleanly.
func TestIsConnectionClosed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"use of closed network connection", net.ErrClosed, true},
		{"connection reset by peer", dialErr(syscall.ECONNRESET), true},
		{"broken pipe", dialErr(syscall.EPIPE), true},
		{"a real read failure is not a benign close", dialErr(syscall.EIO), false},
		{"refusal is not a close", dialErr(syscall.ECONNREFUSED), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConnectionClosed(tc.err); got != tc.want {
				t.Errorf("isConnectionClosed(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
