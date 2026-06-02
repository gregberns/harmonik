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
//   - advance: atomically overwrites the cursor using temp+rename+fsync so that a
//     crash mid-write cannot corrupt the previous cursor value.
//   - single-writer: the daemon is the sole writer (socket-op serialisation
//     enforces this); no concurrent writes.
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
)

// CursorStore is a daemon-owned, file-backed store of per-agent cursors.
// The zero value is not usable; construct with NewCursorStore.
type CursorStore struct {
	dir string // base directory, e.g. <ProjectDir>/.harmonik/comms/cursors
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

// Advance atomically persists eventID as the new cursor for the agent named name.
// An empty eventID is rejected (use "" only when there is nothing to persist;
// the cursor simply stays at its current position in that case — callers that
// have nothing to advance should not call Advance).
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
