package lifecycle

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// ErrPidfileLocked is returned by AcquirePidfile when the pidfile's advisory
// flock is already held by another daemon (flock returned EAGAIN or
// EWOULDBLOCK). The daemon MUST exit with code 5 ("pidfile-locked") on this
// error per PL-008a.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a, PL-002b, PL-008a.
var ErrPidfileLocked = errors.New("lifecycle: pidfile is locked by another daemon")

// ErrPidfileLockError is returned by AcquirePidfile when flock fails with a
// non-contention errno (e.g., ENOLCK, EBADF, ENOTSUP). The wrapped error
// carries the underlying errno via %w. The daemon MUST exit with code 9
// ("filesystem-unwritable") on this error per PL-008a.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a, PL-002b, PL-008a.
var ErrPidfileLockError = errors.New("lifecycle: pidfile lock failed with non-contention errno")

// Pidfile represents an acquired-and-locked pidfile handle. The underlying fd
// is retained for the handle's lifetime (PL-002b step 6 — intermediate close
// is FORBIDDEN while the daemon is running, as closing the fd releases the
// advisory flock). Callers MUST call Release when the daemon is shutting down.
//
// Spec ref: process-lifecycle.md §4.1 PL-002, PL-002a, PL-002b.
type Pidfile struct {
	path string
	fd   *os.File
	mu   sync.Mutex // guards fd for idempotent Release
}

// Path returns the absolute path of the pidfile on disk.
//
// Spec ref: process-lifecycle.md §4.1 PL-002 — "The daemon MUST write its PID
// to .harmonik/daemon.pid on startup."
func (p *Pidfile) Path() string {
	return p.path
}

// Release closes the underlying fd, which also releases the advisory flock
// (PL-002a: fd-lifetime advisory lock). Release is idempotent: a second call
// returns nil without a double-close. The daemon MUST call Release on shutdown.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — "The lock MUST be released
// automatically by the kernel on daemon-process termination (clean OR crash)."
func (p *Pidfile) Release() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.fd == nil {
		return nil
	}
	fd := p.fd
	p.fd = nil
	return fd.Close()
}

// AcquirePidfile opens <projectDir>/.harmonik/daemon.pid, acquires an
// exclusive non-blocking advisory flock (LOCK_EX|LOCK_NB), and writes the
// three-line pidfile content atomically using the truncate-rewrite pattern.
// The fd is retained in the returned *Pidfile for the daemon's lifetime.
//
// The open flags are O_RDWR|O_CREAT|O_CLOEXEC (mode 0600). O_TRUNC is NOT
// used — see PL-002b for why (truncate before lock would create a torn read
// window). O_CLOEXEC prevents the fd from leaking into spawned subprocesses.
//
// On flock failure: EAGAIN or EWOULDBLOCK → ErrPidfileLocked (exit code 5);
// any other errno → ErrPidfileLockError wrapping the underlying errno (exit
// code 9). Both paths close the fd before returning.
//
// After a successful lock: ftruncate(0), seek(0,0), write three
// newline-terminated lines (PID / PGID / instanceID), loop on short writes,
// fsync(fd), fsync(parent-directory-fd). Any I/O failure returns a wrapped
// error and closes the fd.
//
// Spec ref: process-lifecycle.md §4.1 PL-002, PL-002a, PL-002b.
func AcquirePidfile(projectDir string, pid, pgid int, instanceID string) (*Pidfile, error) {
	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")

	// Step 1: O_RDWR|O_CREAT|O_CLOEXEC, mode 0600, NO O_TRUNC.
	// O_CLOEXEC is mandatory per PL-002b to prevent fd leaking into spawned
	// subprocesses. projectDir is operator-controlled at call site.
	//nolint:gosec // G304: pidfilePath derived from projectDir, an operator-controlled parameter (daemon startup arg); not user input
	fd, err := os.OpenFile(pidfilePath, os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: AcquirePidfile: open pidfile %q: %w", pidfilePath, err)
	}

	if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: AcquirePidfile: close pidfile fd after flock failure", "err", closeErr, "path", pidfilePath)
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrPidfileLocked
		}
		return nil, fmt.Errorf("%w: %w", ErrPidfileLockError, err)
	}

	if err := fd.Truncate(0); err != nil {
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: AcquirePidfile: close pidfile fd after ftruncate failure", "err", closeErr, "path", pidfilePath)
		}
		return nil, fmt.Errorf("lifecycle: AcquirePidfile: ftruncate: %w", err)
	}

	if _, err := fd.Seek(0, 0); err != nil {
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: AcquirePidfile: close pidfile fd after seek failure", "err", closeErr, "path", pidfilePath)
		}
		return nil, fmt.Errorf("lifecycle: AcquirePidfile: seek: %w", err)
	}

	content := []byte(fmt.Sprintf("%d\n%d\n%s\n", pid, pgid, instanceID))
	if err := writeAll(fd, content); err != nil {
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: AcquirePidfile: close pidfile fd after write failure", "err", closeErr, "path", pidfilePath)
		}
		return nil, fmt.Errorf("lifecycle: AcquirePidfile: write: %w", err)
	}

	if err := fd.Sync(); err != nil {
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: AcquirePidfile: close pidfile fd after fsync failure", "err", closeErr, "path", pidfilePath)
		}
		return nil, fmt.Errorf("lifecycle: AcquirePidfile: fsync fd: %w", err)
	}

	parentDir := filepath.Dir(pidfilePath)
	//nolint:gosec // G304: parentDir is derived from projectDir, an operator-controlled parameter; not user input
	pfd, err := os.Open(parentDir)
	if err != nil {
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: AcquirePidfile: close pidfile fd after parent-dir open failure", "err", closeErr, "path", pidfilePath)
		}
		return nil, fmt.Errorf("lifecycle: AcquirePidfile: open parent dir for fsync: %w", err)
	}
	syncErr := pfd.Sync()
	if closeErr := pfd.Close(); closeErr != nil {
		slog.WarnContext(context.Background(), "lifecycle: AcquirePidfile: close parent-dir fd after fsync", "err", closeErr, "path", parentDir)
	}
	if syncErr != nil {
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "lifecycle: AcquirePidfile: close pidfile fd after parent-dir fsync failure", "err", closeErr, "path", pidfilePath)
		}
		return nil, fmt.Errorf("lifecycle: AcquirePidfile: fsync parent dir: %w", syncErr)
	}

	return &Pidfile{
		path: pidfilePath,
		fd:   fd,
	}, nil
}

func writeAll(w *os.File, buf []byte) error {
	for len(buf) > 0 {
		n, err := w.Write(buf)
		if err != nil {
			return err
		}
		buf = buf[n:]
	}
	return nil
}

// IsDeadPID reports whether kill(pid, 0) returns ESRCH for pid.
func IsDeadPID(pid int) bool {
	err := syscall.Kill(pid, 0)
	return errors.Is(err, syscall.ESRCH)
}

// ReadPidfile reads <projectDir>/.harmonik/daemon.pid and parses its content
// with backward-compatibility tolerance for one-line (v0.2.x), two-line
// (v0.4.0), and three-line (v0.4.1+) formats:
//
//   - Missing line 3 → instanceID = "unknown"
//   - Missing line 2 → pgid = 0, instanceID = "unknown"
//   - Empty file → error
//   - Unparseable PID (line 1) → error
//   - Unparseable PGID (line 2, when present) → error
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "Readers MUST tolerate
// one-line pidfiles for backward compatibility with v0.2.x format and
// two-line pidfiles for backward compatibility with v0.4.0 format; a missing
// line 3 is treated as daemon_instance_id = unknown."
func ReadPidfile(projectDir string) (pid, pgid int, instanceID string, err error) {
	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")
	//nolint:gosec // G304: pidfilePath derived from projectDir, an operator-controlled parameter (daemon startup arg); not user input
	data, err := os.ReadFile(pidfilePath)
	if err != nil {
		return 0, 0, "", fmt.Errorf("lifecycle: ReadPidfile: read %q: %w", pidfilePath, err)
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
		return 0, 0, "", fmt.Errorf("lifecycle: ReadPidfile: empty pidfile at %q", pidfilePath)
	}

	pid, err = strconv.Atoi(lines[0])
	if err != nil {
		return 0, 0, "", fmt.Errorf("lifecycle: ReadPidfile: parse PID %q: %w", lines[0], err)
	}

	if len(lines) >= 2 {
		pgid, err = strconv.Atoi(lines[1])
		if err != nil {
			return 0, 0, "", fmt.Errorf("lifecycle: ReadPidfile: parse PGID %q: %w", lines[1], err)
		}
	}

	instanceID = "unknown"
	if len(lines) >= 3 {
		instanceID = lines[2]
	}

	return pid, pgid, instanceID, nil
}
