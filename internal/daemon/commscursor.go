package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

const cursorLockTimeout = 10 * time.Second

const cursorLockRetryInterval = 25 * time.Millisecond

// CursorStore is a daemon-owned, file-backed store of per-agent cursors.
// The zero value is not usable; construct with NewCursorStore.
type CursorStore struct {
	dir   string                 // base directory, e.g. <ProjectDir>/.harmonik/comms/cursors
	musMu sync.Mutex             // guards mus
	mus   map[string]*sync.Mutex // per-agent mutexes by agent name, created lazily
}

// NewCursorStore returns a CursorStore rooted at dir.
// The directory is created lazily on the first Advance call; Get does not
// require it to exist.
func NewCursorStore(dir string) *CursorStore {
	return &CursorStore{dir: dir}
}

// Get returns the last-consumed event_id for the agent named name.
// Returns "" (empty string) when no cursor has been stored yet; the caller
// should treat "" as "start of log" (i.e. ScanAfter from the beginning).
// Returns an error only on unexpected I/O failures (not on absent cursor).
func (s *CursorStore) Get(name string) (string, error) {
	if err := validateCursorName(name); err != nil {
		return "", err
	}
	path := s.path(name)
	//nolint:gosec // G304: path is constructed from operator-supplied project dir + validated agent name
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("commscursor: Get %q: %w", name, err)
	}
	eventID := strings.TrimSpace(string(data))
	return eventID, nil
}

// Advance monotonically persists eventID as the new cursor for the agent named
// name. An empty eventID is rejected (use "" only when there is nothing to
// persist; the cursor simply stays at its current position in that case —
// callers that have nothing to advance should not call Advance).
//
// # Monotonic + cross-process safe (hk-fvo9e)
//
// Advance NEVER moves the cursor backward. It takes a per-agent cross-process
// advisory exclusive flock on a sidecar lockfile, re-reads the currently-
// persisted cursor under that lock, and writes eventID ONLY if it is strictly
// greater (chronologically later, by UUIDv7 byte order — EV-002). An
// equal-or-older eventID is a no-op (returns nil). This guarantees that two
// daemons/processes racing recv for the same agent can never regress the cursor:
// a laggard write that scanned an older snapshot is simply dropped.
//
// The write uses temp+rename+fsync discipline: a crash mid-write cannot leave a
// partially-written cursor file; the old value is always readable by a concurrent
// Get until the rename commits.
//
// # No context parameter, deliberately
//
// The only thing a context would reach here is the two cleanup log lines below,
// which is why they use context.Background(). It must NOT reach the flock or the
// write: once the read-modify-write starts it has to finish, because a cursor
// left half-advanced re-delivers or drops messages. Adding the parameter for the
// log lines alone would also change the signature for callers in other
// subsystems, which is not worth a log field.
func (s *CursorStore) Advance(name, eventID string) error {
	if err := validateCursorName(name); err != nil {
		return err
	}
	if eventID == "" {
		return fmt.Errorf("commscursor: Advance %q: eventID must be non-empty", name)
	}
	if _, err := uuid.Parse(eventID); err != nil {
		return fmt.Errorf("commscursor: Advance %q: malformed event_id %q: %w", name, eventID, err)
	}

	if err := os.MkdirAll(s.dir, core.HarmonikDirMode); err != nil {
		return fmt.Errorf("commscursor: Advance %q: mkdir %q: %w", name, s.dir, err)
	}

	lockDir := s.lockDir()
	if err := os.MkdirAll(lockDir, core.HarmonikDirMode); err != nil {
		return fmt.Errorf("commscursor: Advance %q: mkdir %q: %w", name, lockDir, err)
	}
	lockPath := s.lockPath(name)
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: lockPath = lockDir + validated agent name
	if err != nil {
		return fmt.Errorf("commscursor: Advance %q: open lockfile %q: %w", name, lockPath, err)
	}
	defer func() {
		if closeErr := lockFd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "commscursor: Advance: close lockfile", "err", closeErr, "path", lockPath)
		}
	}()

	if err := acquireCursorLock(int(lockFd.Fd()), cursorLockTimeout); err != nil {
		return fmt.Errorf("commscursor: Advance %q: acquire lock: %w", name, err)
	}

	current, err := s.Get(name)
	if err != nil {
		return fmt.Errorf("commscursor: Advance %q: read current: %w", name, err)
	}
	if current != "" {
		newer, cmpErr := cursorStrictlyGreater(eventID, current)
		if cmpErr != nil {
			return fmt.Errorf("commscursor: Advance %q: %w", name, cmpErr)
		}
		if !newer {
			return nil
		}
	}

	target := s.path(name)
	tmp, err := os.CreateTemp(s.dir, ".cursor-*.tmp")
	if err != nil {
		return fmt.Errorf("commscursor: Advance %q: create temp: %w", name, err)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			if closeErr := tmp.Close(); closeErr != nil {
				slog.WarnContext(context.Background(), "commscursor: Advance: close temp during cleanup", "err", closeErr, "path", tmpPath)
			}
			_ = os.Remove(tmpPath) //nolint:errcheck // cleanup; unactionable
		}
	}()

	if _, err := fmt.Fprintln(tmp, eventID); err != nil {
		return fmt.Errorf("commscursor: Advance %q: write temp: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("commscursor: Advance %q: fsync temp: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("commscursor: Advance %q: close temp: %w", name, err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("commscursor: Advance %q: rename: %w", name, err)
	}
	ok = true
	return nil
}

func cursorStrictlyGreater(candidate, current string) (bool, error) {
	cu, err := uuid.Parse(candidate)
	if err != nil {
		return false, fmt.Errorf("malformed event_id %q: %w", candidate, err)
	}
	pu, valid := parseCursorUUID(current)
	if !valid {
		return true, nil
	}
	cb := [16]byte(cu)
	pb := [16]byte(pu)
	for i := 0; i < 16; i++ {
		if cb[i] != pb[i] {
			return cb[i] > pb[i], nil
		}
	}
	return false, nil // equal — not strictly greater
}

func parseCursorUUID(value string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(value)
	return parsed, err == nil
}

func (s *CursorStore) lockDir() string {
	return s.dir + ".locks"
}

func (s *CursorStore) lockPath(name string) string {
	return filepath.Join(s.lockDir(), name)
}

func acquireCursorLock(fd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("flock LOCK_EX: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("flock LOCK_EX: acquire timed out after %s", timeout)
		}
		time.Sleep(cursorLockRetryInterval)
	}
}

// AgentMu returns the per-agent mutex for name, creating it lazily.
// Callers must hold this mutex across the Get→scan→Advance critical section to
// prevent concurrent recv ops for the same agent from delivering duplicates.
func (s *CursorStore) AgentMu(name string) *sync.Mutex {
	s.musMu.Lock()
	defer s.musMu.Unlock()
	if s.mus == nil {
		s.mus = make(map[string]*sync.Mutex)
	}
	mu, ok := s.mus[name]
	if !ok {
		mu = &sync.Mutex{}
		s.mus[name] = mu
	}
	return mu
}

func (s *CursorStore) path(name string) string {
	return filepath.Join(s.dir, name)
}

func validateCursorName(name string) error {
	if name == "" {
		return fmt.Errorf("commscursor: agent name must be non-empty")
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return fmt.Errorf("commscursor: agent name %q contains invalid characters", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("commscursor: agent name %q is reserved", name)
	}
	return nil
}
