package lifecycle

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// plFixtureEventuallyNoErr retries fn until it returns a nil error or timeout
// elapses, returning the last error seen. This package runs dozens of
// t.Parallel() tests that spawn real subprocesses (exec.Command) alongside
// tests holding flock(LOCK_EX) advisory locks. fork(2) duplicates the whole fd
// table into the child regardless of O_CLOEXEC — only exec(2) closes
// close-on-exec fds — so a fork happening while a flock-holding fd is open can
// transiently keep that flock live (via the child's inherited copy) for a
// short window after the original fd's owner has already called Release(). The
// kernel-level guarantee ("lock released when the fd is closed") still holds;
// it is merely delayed, not violated. A short bounded retry absorbs that
// window without masking a genuine stuck-lock regression.
func plFixtureEventuallyNoErr(t *testing.T, timeout time.Duration, fn func() error) error {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		err := fn()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// plFixtureEventuallyTrue retries fn until it returns true or timeout elapses.
// See plFixtureEventuallyNoErr for why this retry exists.
func plFixtureEventuallyTrue(t *testing.T, timeout time.Duration, fn func() bool) bool {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if fn() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// plFixtureTempProjectDir creates a temporary directory tree that looks like
// a harmonik project root: the returned path contains an initialised
// .harmonik/ sub-directory. All sibling fixtures reuse this helper.
//
// macOS enforces a 104-character limit on Unix domain socket paths
// (sun_path in sockaddr_un). To avoid EINVAL on socket bind, this helper
// always creates the project root under /tmp with a short prefix when the
// default t.TempDir() path would exceed the limit.
//
// Spec ref: process-lifecycle.md §4.1 PL-002 — "The daemon MUST write its PID
// to .harmonik/daemon.pid on startup".
func plFixtureTempProjectDir(t *testing.T) string {
	t.Helper()

	// Candidate: try t.TempDir() first and check whether the resulting socket
	// path fits within the 104-byte macOS sun_path limit.
	candidate := t.TempDir()
	sockCandidate := filepath.Join(candidate, ".harmonik", "daemon.sock")
	const sunPathMax = 104 // sockaddr_un.sun_path array size on macOS, incl. NUL terminator

	var root string
	if len(sockCandidate) < sunPathMax {
		root = candidate
	} else {
		// Fall back to a short /tmp path so the socket path fits.
		dir, err := os.MkdirTemp("/tmp", "pl-")
		if err != nil {
			t.Fatalf("plFixtureTempProjectDir: MkdirTemp /tmp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // cleanup error unactionable
		root = dir
	}

	harmonikDir := filepath.Join(root, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("plFixtureTempProjectDir: MkdirAll .harmonik: %v", err)
	}
	return root
}

// plFixtureAcquirePidfile opens .harmonik/daemon.pid, takes an exclusive
// non-blocking flock (flock(LOCK_EX|LOCK_NB)), writes the three-line pidfile
// content, and returns a releaseFn that closes the fd (releasing the lock).
//
// The write discipline follows PL-002b: open without O_TRUNC, acquire lock,
// ftruncate, write three lines (PID/PGID/instanceID), fsync(fd),
// fsync(parent-dir). The fd is kept alive for the caller's lifetime.
//
// When t is non-nil, t.Helper() is called for better error attribution.
//
// Spec refs:
//   - process-lifecycle.md §4.1 PL-002 — pidfile at .harmonik/daemon.pid
//   - process-lifecycle.md §4.1 PL-002a — fd-lifetime advisory lock (flock LOCK_EX|LOCK_NB)
//   - process-lifecycle.md §4.1 PL-002b — atomic three-line write; truncate-rewrite-keep-fd
func plFixtureAcquirePidfile(t *testing.T, projectDir string, pid, pgid int, instanceID string) (releaseFn func(), err error) {
	if t != nil {
		t.Helper()
	}

	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")

	// Open with O_RDWR|O_CREATE — NOT O_TRUNC. Matching PL-002b step 1.
	f, err := os.OpenFile(pidfilePath, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // mode 0600 is correct per PL-002
	if err != nil {
		return nil, fmt.Errorf("plFixtureAcquirePidfile: open pidfile: %w", err)
	}

	// PL-002a: acquire exclusive non-blocking advisory lock via flock.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close() //nolint:errcheck // cleanup error unactionable
		return nil, fmt.Errorf("plFixtureAcquirePidfile: flock LOCK_EX|LOCK_NB: %w", err)
	}

	// PL-002b step 3: truncate only after lock acquisition.
	if err := f.Truncate(0); err != nil {
		_ = f.Close() //nolint:errcheck // cleanup error unactionable
		return nil, fmt.Errorf("plFixtureAcquirePidfile: ftruncate: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = f.Close() //nolint:errcheck // cleanup error unactionable
		return nil, fmt.Errorf("plFixtureAcquirePidfile: seek: %w", err)
	}

	// PL-002b step 4: write three lines: PID / PGID / daemon_instance_id.
	content := fmt.Sprintf("%d\n%d\n%s\n", pid, pgid, instanceID)
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close() //nolint:errcheck // cleanup error unactionable
		return nil, fmt.Errorf("plFixtureAcquirePidfile: write: %w", err)
	}

	// PL-002b step 5: fsync the fd.
	if err := f.Sync(); err != nil {
		_ = f.Close() //nolint:errcheck // cleanup error unactionable
		return nil, fmt.Errorf("plFixtureAcquirePidfile: fsync fd: %w", err)
	}

	// PL-002b step 5: fsync the parent directory.
	parentDir := filepath.Dir(pidfilePath)
	//nolint:gosec // G304: parentDir derived from t.TempDir(), not user input
	pf, err := os.Open(parentDir)
	if err == nil {
		_ = pf.Sync()  //nolint:errcheck // cleanup error unactionable
		_ = pf.Close() //nolint:errcheck // cleanup error unactionable
	}

	releaseFn = func() {
		_ = f.Close() //nolint:errcheck // closing fd releases the flock; cleanup error unactionable
	}
	return releaseFn, nil
}

// plFixtureBindSocket binds a Unix socket at .harmonik/daemon.sock with mode
// 0600 and returns the net.Listener. Returns an error if the path is already
// in use (EADDRINUSE).
//
// Spec refs:
//   - process-lifecycle.md §4.1 PL-003 — socket at .harmonik/daemon.sock, mode 0600
func plFixtureBindSocket(t *testing.T, projectDir string) (net.Listener, error) {
	t.Helper()

	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")

	// PL-003: remove stale socket file on startup before binding.
	_ = os.Remove(sockPath) //nolint:errcheck // cleanup error unactionable

	// Use ListenConfig with t.Context() so the listener is context-aware.
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("plFixtureBindSocket: listen unix: %w", err)
	}

	// PL-003: chmod 0600 after bind.
	if err := os.Chmod(sockPath, 0o600); err != nil {
		_ = ln.Close() //nolint:errcheck // cleanup error unactionable
		return nil, fmt.Errorf("plFixtureBindSocket: chmod 0600: %w", err)
	}

	return ln, nil
}

// plFixtureReadPidfile reads .harmonik/daemon.pid and parses its content
// with reader-tolerance for one-line (v0.2.x), two-line (v0.4.0), and
// three-line (v0.4.1+) formats. A missing line 3 returns instanceID =
// "unknown"; a missing line 2 returns pgid = 0.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "Readers MUST tolerate
// one-line pidfiles for backward compatibility with v0.2.x format and
// two-line pidfiles for backward compatibility with v0.4.0 format; a missing
// line 3 is treated as daemon_instance_id = unknown."
func plFixtureReadPidfile(t *testing.T, projectDir string) (pid, pgid int, instanceID string, err error) {
	t.Helper()

	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")
	//nolint:gosec // G304: pidfilePath derived from t.TempDir(), not user input
	data, err := os.ReadFile(pidfilePath)
	if err != nil {
		return 0, 0, "", fmt.Errorf("plFixtureReadPidfile: ReadFile: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}

	if len(lines) == 0 {
		return 0, 0, "", errors.New("plFixtureReadPidfile: empty pidfile")
	}

	pid, err = strconv.Atoi(lines[0])
	if err != nil {
		return 0, 0, "", fmt.Errorf("plFixtureReadPidfile: parse PID: %w", err)
	}

	if len(lines) >= 2 {
		pgid, err = strconv.Atoi(lines[1])
		if err != nil {
			return 0, 0, "", fmt.Errorf("plFixtureReadPidfile: parse PGID: %w", err)
		}
	}

	instanceID = "unknown"
	if len(lines) >= 3 {
		instanceID = lines[2]
	}

	return pid, pgid, instanceID, nil
}

// plFixtureIsPidLive probes whether the given PID is a live process by
// sending signal 0 via kill(pid, 0). Returns false if the process is not
// found (ESRCH). Returns true if the process exists (even if it is a zombie).
//
// Spec ref: process-lifecycle.md §4.1 PL-002a + §4.8 PL-024 — "stale pidfile
// detection: flock + kill(pid, 0)"; kill(pid, 0) probes the kernel process
// table for liveness.
func plFixtureIsPidLive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	// EPERM means the process exists but we lack permission to signal it.
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

// plFixtureErrToExitCode maps the fixture-level errors to the ON §8 exit
// codes defined in process-lifecycle.md §4.1 PL-008a. This is a test-side
// helper that exercises the error → exit-code mapping without requiring a
// real daemon binary.
//
// Spec ref: process-lifecycle.md §4.1 PL-008a — exit codes 5 (pidfile-locked)
// and 6 (socket-bind-failed) per [operator-nfr.md §8].
func plFixtureErrToExitCode(err error) int {
	if err == nil {
		return 0
	}
	// Exit code 5: pidfile-locked — flock returned EWOULDBLOCK or EAGAIN.
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return 5
	}
	// Exit code 6: socket-bind-failed — bind returned EADDRINUSE.
	if errors.Is(err, syscall.EADDRINUSE) {
		return 6
	}
	return 1
}

// plFixtureSocketPath returns the canonical Unix socket path for a project.
//
// Spec ref: process-lifecycle.md §4.1 PL-003.
func plFixtureSocketPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "daemon.sock")
}

// plFixturePidfilePath returns the canonical pidfile path for a project.
//
// Spec ref: process-lifecycle.md §4.1 PL-002.
func plFixturePidfilePath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "daemon.pid")
}

// plFixtureExtractErrno digs out the underlying syscall.Errno from a
// *net.OpError wrapping a *os.SyscallError. Returns 0 if the chain does not
// match.
//
// The direct type assertions here are intentional: this helper exists to
// inspect the exact error structure returned by net.Listen, not to handle
// arbitrary wrapped errors. The kernel errno is always at this exact chain
// depth for POSIX socket errors.
//
// Used by PL-INV-004 to check that EADDRINUSE is the exact errno returned by
// the kernel when a live socket is already bound.
func plFixtureExtractErrno(err error) syscall.Errno {
	if err == nil {
		return 0
	}
	//nolint:errorlint // intentional: inspecting exact net.Listen error chain structure
	opErr, ok := err.(*net.OpError)
	if !ok {
		return 0
	}
	//nolint:errorlint // intentional: inspecting exact net.Listen error chain structure
	sysErr, ok := opErr.Err.(*os.SyscallError)
	if !ok {
		return 0
	}
	//nolint:errorlint // intentional: inspecting exact net.Listen error chain structure
	errno, ok := sysErr.Err.(syscall.Errno)
	if !ok {
		return 0
	}
	return errno
}
