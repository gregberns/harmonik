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

const plFixtureEventuallyTimeout = 2 * time.Second

func plFixtureEventuallyNoErr(t *testing.T, fn func() error) error {
	t.Helper()

	deadline := time.Now().Add(plFixtureEventuallyTimeout)
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

func plFixtureTempProjectDir(t *testing.T) string {
	t.Helper()

	candidate := t.TempDir()
	sockCandidate := filepath.Join(candidate, ".harmonik", "daemon.sock")
	const sunPathMax = 104 // sockaddr_un.sun_path array size on macOS, incl. NUL terminator

	var root string
	if len(sockCandidate) < sunPathMax {
		root = candidate
	} else {
		dir, err := os.MkdirTemp("/tmp", "pl-")
		if err != nil {
			t.Fatalf("plFixtureTempProjectDir: MkdirTemp /tmp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // cleanup error unactionable
		root = dir
	}

	harmonikDir := filepath.Join(root, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o750); err != nil {
		t.Fatalf("plFixtureTempProjectDir: MkdirAll .harmonik: %v", err)
	}
	return root
}

func plFixtureAcquirePidfile(t *testing.T, projectDir string, pid, pgid int, instanceID string) (releaseFn func(), err error) {
	if t != nil {
		t.Helper()
	}

	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")

	f, err := os.OpenFile(pidfilePath, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // mode 0600 is correct per PL-002
	if err != nil {
		return nil, fmt.Errorf("plFixtureAcquirePidfile: open pidfile: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("plFixtureAcquirePidfile: flock LOCK_EX|LOCK_NB: %w", err)
	}

	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("plFixtureAcquirePidfile: ftruncate: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("plFixtureAcquirePidfile: seek: %w", err)
	}

	content := fmt.Sprintf("%d\n%d\n%s\n", pid, pgid, instanceID)
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("plFixtureAcquirePidfile: write: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("plFixtureAcquirePidfile: fsync fd: %w", err)
	}

	parentDir := filepath.Dir(pidfilePath)
	//nolint:gosec // G304: parentDir derived from t.TempDir(), not user input
	pf, err := os.Open(parentDir)
	if err == nil {
		_ = pf.Sync() //nolint:errcheck // cleanup error unactionable
		_ = pf.Close()
	}

	releaseFn = func() {
		_ = f.Close()
	}
	return releaseFn, nil
}

func plFixtureBindSocket(t *testing.T, projectDir string) (net.Listener, error) {
	t.Helper()

	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")

	_ = os.Remove(sockPath) //nolint:errcheck // cleanup error unactionable

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("plFixtureBindSocket: listen unix: %w", err)
	}

	if err := os.Chmod(sockPath, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("plFixtureBindSocket: chmod 0600: %w", err)
	}

	return ln, nil
}

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

func plFixtureIsPidLive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func plFixtureErrToExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return 5
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return 6
	}
	return 1
}

func plFixtureSocketPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "daemon.sock")
}

func plFixturePidfilePath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "daemon.pid")
}

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
