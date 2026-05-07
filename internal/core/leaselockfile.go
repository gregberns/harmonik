package core

import (
	"time"

	"github.com/google/uuid"
)

// LeaseLockFile records the JSON contents of a workspace lease-lock file
// (workspace-model.md §6.1, §6.2, WM-013a).
//
// The lease-lock file lives at ${workspace_path}/.harmonik/lease.lock.
// Its presence represents the lease; its absence represents a released workspace.
// Birth is at workspace_leased; death is at merged/discarded per §4.3.WM-013b.
type LeaseLockFile struct {
	// RunID is the owning run (workspace-model.md §6.1).
	RunID RunID

	// PID is the daemon process ID that wrote the lock (workspace-model.md §6.1).
	// Positive integers only; os.Getpid() returns int on all supported platforms.
	PID int

	// CreatedAt is the RFC 3339 wall-clock time the lock was written
	// (workspace-model.md §6.1).
	CreatedAt time.Time

	// TTLSec is the advisory lifetime in seconds (workspace-model.md §6.1).
	// Informative for the orphan sweep; does not enforce auto-expiry per WM-013a.
	TTLSec int
}

// Valid reports whether all required fields carry non-zero values.
// A LeaseLockFile is considered valid iff:
//   - RunID is not the zero UUID
//   - PID is positive (os.Getpid() always returns a positive integer)
//   - CreatedAt is not the zero time
//   - TTLSec is positive (a zero or negative advisory TTL is meaningless)
func (l LeaseLockFile) Valid() bool {
	if uuid.UUID(l.RunID) == uuid.Nil {
		return false
	}
	if l.PID <= 0 {
		return false
	}
	if l.CreatedAt.IsZero() {
		return false
	}
	if l.TTLSec <= 0 {
		return false
	}
	return true
}
