// Package keeper implements the session-keeper subsystem (codename:session-keeper,
// hk-ekap1). Phase-1 provides lockfile acquisition and the .managed opt-in guard;
// the context watcher and status-line injector ship in later beads.
package keeper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// ErrInvalidAgent is returned by AcquireLock when the agent name contains
// path-traversal sequences that could escape the keeper directory.
var ErrInvalidAgent = errors.New("keeper: agent name must not contain '/' or '..'")

// validateAgent rejects names that could escape the keeper directory via path
// traversal. Operator-controlled but still worth enforcing defensively.
func validateAgent(agent string) error {
	if strings.Contains(agent, "/") || strings.Contains(agent, "..") {
		return ErrInvalidAgent
	}
	return nil
}

// AcquireLock acquires an exclusive non-blocking flock on
// <projectDir>/.harmonik/keeper/<agent>.lock and writes the caller's PID to
// the file. Returns ErrLockHeld if another live process holds the lock.
// The .harmonik/keeper/ directory is created if it does not exist.
//
// The caller MUST call Lock.Release when the keeper exits.
func AcquireLock(projectDir, agent string) (*Lock, error) {
	if err := validateAgent(agent); err != nil {
		return nil, err
	}

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		return nil, fmt.Errorf("keeper: create keeper dir: %w", err)
	}

	lockPath := filepath.Join(keeperDir, agent+".lock")
	//nolint:gosec // G304: lockPath derived from operator-controlled projectDir and agent name validated by validateAgent
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
// Returns false for any agent name that fails validateAgent.
func IsManaged(projectDir, agent string) bool {
	if validateAgent(agent) != nil {
		return false
	}
	markerPath := filepath.Join(projectDir, ".harmonik", "keeper", agent+".managed")
	_, err := os.Stat(markerPath)
	return err == nil
}

// ReadManagedSessionID reads the session_id stored in the .managed marker file
// for the given agent. The .managed file format is: first non-empty line is the
// session_id; subsequent lines (if any) are ignored. Returns ("", nil) when the
// file is absent, empty, or contains only whitespace — all indicate no session
// binding is in effect. Returns ("", ErrInvalidAgent) for path-traversal names.
//
// Refs: hk-igt (session_id clobber fix — two same-agent sessions writing to .ctx).
func ReadManagedSessionID(projectDir, agent string) (string, error) {
	if err := validateAgent(agent); err != nil {
		return "", err
	}
	path := filepath.Join(projectDir, ".harmonik", "keeper", agent+".managed")
	//nolint:gosec // G304: path derived from operator-controlled projectDir and agent validated above
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("keeper: read managed session_id %q: %w", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line, nil
		}
	}
	return "", nil // empty or whitespace-only file — no binding in effect
}

// isUUIDv7 reports whether sid is a well-formed UUIDv7.
// UUID layout: xxxxxxxx-xxxx-Vxxx-Sxxx-xxxxxxxxxxxx (36 bytes).
// The version digit V occupies index 14. Daemon-spawned implementers write
// UUIDv7 session IDs; interactive captain sessions write UUIDv4. (Refs: hk-lap)
func isUUIDv7(sid string) bool {
	return len(sid) == 36 && sid[14] == '7'
}

// WriteManagedSessionID writes sessionID into the .managed marker file for the
// given agent, establishing or updating the session binding. The .managed file
// is created if absent (which also makes IsManaged return true). Passing an
// empty sessionID clears the binding while preserving the managed marker.
//
// Called by the watcher when it latches the first observed session_id, and by
// the cycler after a cycle completes to bind to the resumed session.
//
// Refs: hk-igt (session_id clobber fix — two same-agent sessions writing to .ctx).
func WriteManagedSessionID(projectDir, agent, sessionID string) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		return fmt.Errorf("keeper: create keeper dir: %w", err)
	}
	path := filepath.Join(keeperDir, agent+".managed")
	content := sessionID
	if content != "" {
		content += "\n"
	}
	//nolint:gosec // G304,G306: path from validated operator-controlled inputs; 0600 keeper-owned
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("keeper: write managed session_id %q: %w", path, err)
	}
	return nil
}
