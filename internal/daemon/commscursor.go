package daemon

// commscursor.go — daemon-owned per-agent cursor store for durable comms recv.
//
// Each agent that reads agent_message events via "comms recv" has a persistent
// cursor recording the last-consumed event_id. On reconnect (or daemon restart)
// recv resumes from the stored cursor — nothing is re-delivered beyond the
// unacked tail, nothing is silently lost.
//
// # Layout
//
//	<cursorDir>/<name>    — plain UTF-8 file containing one event_id per line (last wins)
//
// In practice the cursor directory is <ProjectDir>/.harmonik/comms/cursors/.
//
// # Durability contract (agent-comms spec §5 / Q1 / T7)
//
//   - get: reads the stored cursor; returns "" when no cursor exists (= scan from
//     beginning of events.jsonl, i.e. deliver all matching events).
//   - advance: monotonically advances the cursor using temp+rename+fsync so that
//     a crash mid-write cannot corrupt the previous cursor value. Advance NEVER
//     moves the cursor backward: under a cross-process exclusive lock it re-reads
//     the currently-persisted value and writes the new event_id ONLY if it is
//     strictly greater (chronologically later, by UUIDv7 byte order — EV-002).
//     An equal-or-older event_id is a no-op. This is the load-bearing invariant.
//   - per-agent serialization: CursorStore.AgentMu(name) returns a per-agent
//     mutex that callers MUST hold across the Get→scan→Advance critical section.
//     This prevents concurrent recv ops for the same agent within ONE process
//     from delivering duplicate messages. Concurrent ops for different agents are
//     independent.
//
// # Multi-daemon / cross-process race (hk-fvo9e)
//
// AgentMu is an in-process mutex; it does NOT span processes. Two daemons (or
// two separate processes) running `comms recv` for the SAME agent share no
// in-process lock: each Gets the same cursor, scans, and Advances. A process
// that scanned an OLDER snapshot would, with a blind overwrite, rename an OLDER
// event_id over a NEWER one — moving the cursor BACKWARD and re-delivering
// already-consumed messages (lost-advance). The single-daemon mitigation
// (hk-fww4e per-agent mutex) cannot prevent this because the serialization must
// span processes.
//
// Two mechanisms close the cross-process gap, both inside Advance:
//
//  1. A cross-process advisory exclusive flock on a per-agent sidecar lockfile
//     (<cursorDir>.locks/<name>) serializes the read-current-then-write critical
//     section across ALL processes, not just within one.
//  2. A monotonic guard: under the lock, Advance re-reads the persisted cursor
//     and refuses to write an event_id that is not strictly greater. This makes a
//     laggard write a no-op even if it somehow interleaved — the cursor can only
//     ever move forward.
//
// # Name validation
//
// Agent names must be non-empty and must not contain path separators or other
// filesystem-unsafe characters. Validation is enforced at Advance/Get boundaries
// so that a malformed name cannot escape the cursor directory.
//
// Bead ref: hk-0ezlo (T7).
// Spec ref: agent-comms spec §5 Q1 / T7 (07-tasks.md).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// cursorLockTimeout bounds how long Advance waits to acquire the per-agent
// cross-process flock before failing. Generous enough to absorb a normal
// read-modify-write cycle under contention, but bounded so a wedged peer turns
// an indefinite hang into a prompt error rather than blocking recv forever.
const cursorLockTimeout = 10 * time.Second

// cursorLockRetryInterval is the poll interval for the bounded LOCK_EX|LOCK_NB
// acquire loop — short enough to grab a freed lock promptly, long enough not to
// spin-burn a core.
const cursorLockRetryInterval = 25 * time.Millisecond

// CursorStore is a daemon-owned, file-backed store of per-agent cursors.
// The zero value is not usable; construct with NewCursorStore.
type CursorStore struct {
	dir string   // base directory, e.g. <ProjectDir>/.harmonik/comms/cursors
	mus sync.Map // per-agent mutexes (agent name → *sync.Mutex), created lazily
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
func (s *CursorStore) Advance(name, eventID string) error {
	if err := validateCursorName(name); err != nil {
		return err
	}
	if eventID == "" {
		return fmt.Errorf("commscursor: Advance %q: eventID must be non-empty", name)
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil { //nolint:gosec // G301: 0755 matches .harmonik conventions
		return fmt.Errorf("commscursor: Advance %q: mkdir %q: %w", name, s.dir, err)
	}

	// Cross-process serialization (hk-fvo9e): take an advisory exclusive flock on
	// a per-agent sidecar lockfile so the read-current-then-write critical section
	// is atomic across separate processes, not just within one (AgentMu is
	// in-process only). The sidecar is keyed per agent so different agents never
	// contend. Lockfiles live in a dedicated subdirectory so they never appear
	// alongside cursor files. Held only for one RMW cycle, then released on close.
	lockDir := s.lockDir()
	if err := os.MkdirAll(lockDir, 0o755); err != nil { //nolint:gosec // G301: 0755 matches .harmonik conventions
		return fmt.Errorf("commscursor: Advance %q: mkdir %q: %w", name, lockDir, err)
	}
	lockPath := s.lockPath(name)
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: lockPath = lockDir + validated agent name
	if err != nil {
		return fmt.Errorf("commscursor: Advance %q: open lockfile %q: %w", name, lockPath, err)
	}
	defer lockFd.Close() //nolint:errcheck // closing an advisory lock fd; error is non-actionable

	if err := acquireCursorLock(int(lockFd.Fd()), cursorLockTimeout); err != nil {
		return fmt.Errorf("commscursor: Advance %q: acquire lock: %w", name, err)
	}
	// Lock is released automatically when lockFd is closed by the deferred call.

	// Monotonic guard: re-read the persisted cursor UNDER the lock and refuse to
	// move backward. We compare raw UUIDv7 bytes (lexicographic == chronological,
	// EV-002). A malformed stored cursor is treated as "no usable floor" so we do
	// not wedge on a corrupt file — the new (well-formed) eventID wins.
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
			// eventID is equal to or older than the persisted cursor — a laggard
			// or duplicate advance. Drop it: the cursor must not regress.
			return nil
		}
	}

	target := s.path(name)
	// Write to a sibling temp file, then rename into place.
	tmp, err := os.CreateTemp(s.dir, ".cursor-*.tmp")
	if err != nil {
		return fmt.Errorf("commscursor: Advance %q: create temp: %w", name, err)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
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

// cursorStrictlyGreater reports whether candidate is chronologically later than
// current by comparing their raw UUIDv7 bytes (lexicographic byte order ==
// chronological order, EV-002 — the same comparison ScanAfter uses). Both must
// parse as UUIDs; a parse failure on candidate is an error (the caller passes a
// well-formed event_id). A parse failure on current is signalled separately so
// the caller can choose to overwrite a corrupt floor rather than wedge.
func cursorStrictlyGreater(candidate, current string) (bool, error) {
	cu, err := uuid.Parse(candidate)
	if err != nil {
		return false, fmt.Errorf("malformed event_id %q: %w", candidate, err)
	}
	pu, err := uuid.Parse(current)
	if err != nil {
		// Corrupt persisted cursor: treat as "no usable floor" so a well-formed
		// advance can recover rather than the cursor wedging forever.
		return true, nil //nolint:nilerr // intentional: corrupt floor → allow forward write
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

// lockDir is the directory holding per-agent sidecar lockfiles. It is a SIBLING
// of the cursor directory (<cursorDir>.locks), not a child, so a directory
// listing of the cursor dir shows only cursor files (one per agent) and never a
// lock artifact — preserving the "one cursor file per agent, nothing else"
// invariant.
func (s *CursorStore) lockDir() string {
	return s.dir + ".locks"
}

// lockPath returns the per-agent sidecar lockfile path, under lockDir. Agent
// names are validated (no path separators, not "."/".."), so the join cannot
// escape the lock directory.
func (s *CursorStore) lockPath(name string) string {
	return filepath.Join(s.lockDir(), name)
}

// acquireCursorLock acquires an advisory exclusive flock on fd, retrying the
// non-blocking LOCK_EX|LOCK_NB attempt every cursorLockRetryInterval until it
// succeeds or timeout elapses. On timeout it returns an error so Advance fails
// promptly rather than blocking recv indefinitely behind a wedged peer. Mirrors
// the bounded-acquire idiom in internal/workspace (hk-bfvby).
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
	v, _ := s.mus.LoadOrStore(name, &sync.Mutex{})
	return v.(*sync.Mutex) //nolint:forcetypeassert // we always store *sync.Mutex
}

// path returns the file path for the given agent name.
func (s *CursorStore) path(name string) string {
	return filepath.Join(s.dir, name)
}

// validateCursorName rejects names that are empty or contain characters that
// could escape the cursor directory (path separators, null bytes, dots that
// resolve to parent directories).
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
