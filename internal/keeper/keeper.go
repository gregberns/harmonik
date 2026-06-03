// Package keeper implements the session-keeper subsystem (codename:session-keeper,
// hk-ekap1). Phase-1 provides lockfile acquisition and the .managed opt-in guard;
// the context watcher and status-line injector ship in later beads.
package keeper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLockHeld is returned by AcquireLock when another live keeper process holds
// the exclusive flock on the agent's lockfile.
var ErrLockHeld = errors.New("keeper: lock is held by another keeper for this agent")

// Lock represents an acquired keeper lockfile. The underlying fd is retained
// for the Lock's lifetime; closing the fd releases the advisory flock.
// Callers MUST call Release on shutdown.
type Lock struct {
	path string
	fd   *os.File
}

// Path returns the absolute lockfile path.
func (l *Lock) Path() string { return l.path }

// Release closes the underlying fd, which releases the flock. Idempotent.
func (l *Lock) Release() error {
	if l.fd == nil {
		return nil
	}
	fd := l.fd
	l.fd = nil
	return fd.Close()
}

// AcquireLock acquires an exclusive non-blocking flock on
// <projectDir>/.harmonik/keeper/<agent>.lock and writes the caller's PID to
// the file. Returns ErrLockHeld if another live process holds the lock.
// The .harmonik/keeper/ directory is created if it does not exist.
//
// The caller MUST call Lock.Release when the keeper exits.
func AcquireLock(projectDir, agent string) (*Lock, error) {
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		return nil, fmt.Errorf("keeper: create keeper dir: %w", err)
	}

	lockPath := filepath.Join(keeperDir, agent+".lock")
	//nolint:gosec // G304: lockPath derived from operator-controlled projectDir and validated agent name
	fd, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("keeper: open lockfile %q: %w", lockPath, err)
	}

	if err := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = fd.Close() //nolint:errcheck // cleanup fd; primary error takes precedence
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLockHeld
		}
		return nil, fmt.Errorf("keeper: flock %q: %w", lockPath, err)
	}

	// Truncate then write our PID after acquiring the lock.
	if err := fd.Truncate(0); err != nil {
		_ = fd.Close() //nolint:errcheck // cleanup fd; primary error takes precedence
		return nil, fmt.Errorf("keeper: truncate lockfile: %w", err)
	}
	if _, err := fd.Seek(0, 0); err != nil {
		_ = fd.Close() //nolint:errcheck // cleanup fd; primary error takes precedence
		return nil, fmt.Errorf("keeper: seek lockfile: %w", err)
	}
	if _, err := fmt.Fprintf(fd, "%d\n", os.Getpid()); err != nil {
		_ = fd.Close() //nolint:errcheck // cleanup fd; primary error takes precedence
		return nil, fmt.Errorf("keeper: write pid to lockfile: %w", err)
	}

	return &Lock{path: lockPath, fd: fd}, nil
}

// IsManaged reports whether the opt-in marker
// <projectDir>/.harmonik/keeper/<agent>.managed exists. Keepers MUST NOT act
// on a non-managed pane; the absent-marker case is the fail-safe default.
func IsManaged(projectDir, agent string) bool {
	markerPath := filepath.Join(projectDir, ".harmonik", "keeper", agent+".managed")
	_, err := os.Stat(markerPath)
	return err == nil
}
