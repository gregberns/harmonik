package operatornfr

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// UpgradingMarkerName is the filename of the durable upgrade-intent marker
// under the `.harmonik/` directory.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — ".harmonik/daemon.upgrading."
const UpgradingMarkerName = "daemon.upgrading"

// UpgradingMarker holds the three required fields of the `.harmonik/daemon.upgrading`
// upgrade-intent marker per ON-020a.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — "the daemon MUST atomically write
// `.harmonik/daemon.upgrading` containing: (a) the operator-supplied
// `expected_commit_hash`; (b) the upgrade-initiation timestamp; (c) the
// operator's session_id."
type UpgradingMarker struct {
	// ExpectedCommitHash is the operator-supplied commit hash from the upgrade
	// invocation. Field (a) per ON-020a.
	ExpectedCommitHash string `json:"expected_commit_hash"`

	// InitiatedAt is the upgrade-initiation wall-clock timestamp in RFC 3339
	// format with millisecond precision. Field (b) per ON-020a.
	InitiatedAt string `json:"initiated_at"`

	// SessionID is the operator's session identifier from the daemon-instance
	// handshake per ON-013b. Field (c) per ON-020a.
	SessionID string `json:"session_id"`
}

// Valid reports whether m is a well-formed UpgradingMarker.
//
// Rules:
//   - ExpectedCommitHash must be non-empty.
//   - InitiatedAt must be non-empty.
//   - SessionID must be non-empty.
func (m UpgradingMarker) Valid() bool {
	return m.ExpectedCommitHash != "" && m.InitiatedAt != "" && m.SessionID != ""
}

// BuildUpgradingMarker constructs a new UpgradingMarker with the current
// wall-clock time as InitiatedAt.
//
// expectedCommitHash is the operator-supplied hash from the upgrade invocation.
// sessionID is the operator's session identifier.
//
// Spec ref: operator-nfr.md §4.6 ON-020a.
func BuildUpgradingMarker(expectedCommitHash, sessionID string) UpgradingMarker {
	return UpgradingMarker{
		ExpectedCommitHash: expectedCommitHash,
		InitiatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:          sessionID,
	}
}

// WriteUpgradingMarker atomically writes the upgrade-intent marker to
// `harmonikDir/daemon.upgrading` using the temp+rename+fsync(parent_dir)
// discipline required by [workspace-model.md §4.7 WM-026].
//
// Write sequence:
//  1. Marshal marker to JSON.
//  2. Write JSON to sibling temp file `harmonikDir/daemon.upgrading.tmp-<pid>`.
//  3. fsync the temp file so data is durable before rename.
//  4. rename(2) temp → `harmonikDir/daemon.upgrading` (atomic within same fs).
//  5. fsync the parent directory so the rename is durable.
//
// The marker MUST be written and durable before the daemon invokes execve;
// the write discipline enforced here ensures crash-safety (if the daemon dies
// after rename but before execve, PL-005 step 8a reads the marker on restart).
//
// Spec ref: operator-nfr.md §4.6 ON-020a — "Write MUST follow
// temp+rename+fsync atomicity."
// Spec ref: process-lifecycle.md §4.9 PL-027(iv) — "The outgoing binary MUST
// write the upgrade-intent marker … before invoking execve."
func WriteUpgradingMarker(harmonikDir string, marker UpgradingMarker) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("operatornfr: WriteUpgradingMarker: marshal: %w", err)
	}

	target := harmonikDir + "/" + UpgradingMarkerName
	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())

	// Step 1+2: create and write to sibling temp file.
	//nolint:gosec // G304: tmpPath derived from harmonikDir (daemon-internal .harmonik dir) + Getpid
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("operatornfr: WriteUpgradingMarker: create temp %q: %w", tmpPath, err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()          //nolint:errcheck // cleanup error unactionable
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup error unactionable
		return fmt.Errorf("operatornfr: WriteUpgradingMarker: write temp %q: %w", tmpPath, err)
	}

	// Step 3: fsync temp file.
	if err := f.Sync(); err != nil {
		_ = f.Close()          //nolint:errcheck // cleanup error unactionable
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup error unactionable
		return fmt.Errorf("operatornfr: WriteUpgradingMarker: fsync temp %q: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup error unactionable
		return fmt.Errorf("operatornfr: WriteUpgradingMarker: close temp %q: %w", tmpPath, err)
	}

	// Step 4: rename temp → target (atomic).
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup error unactionable
		return fmt.Errorf("operatornfr: WriteUpgradingMarker: rename %q → %q: %w", tmpPath, target, err)
	}

	// Step 5: fsync parent directory so rename is durable.
	dir, err := os.Open(harmonikDir) //nolint:gosec // G304: harmonikDir is daemon-internal .harmonik dir
	if err != nil {
		return fmt.Errorf("operatornfr: WriteUpgradingMarker: open parent dir %q: %w", harmonikDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close() //nolint:errcheck // cleanup error unactionable
		return fmt.Errorf("operatornfr: WriteUpgradingMarker: fsync parent dir %q: %w", harmonikDir, err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("operatornfr: WriteUpgradingMarker: close parent dir %q: %w", harmonikDir, err)
	}

	return nil
}

// RemoveUpgradingMarker removes the `.harmonik/daemon.upgrading` marker and
// fsyncs the parent directory for durability. This is called by the new daemon
// instance after it transitions to the ready state per ON-020a.
//
// A missing marker file is treated as success (idempotent removal).
//
// Spec ref: operator-nfr.md §4.6 ON-020a — "marker removed on clean
// transition to ready."
// Spec ref: process-lifecycle.md §4.9 PL-027(iv) — "removed via unlink
// followed by parent-directory fsync."
func RemoveUpgradingMarker(harmonikDir string) error {
	target := harmonikDir + "/" + UpgradingMarkerName
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("operatornfr: RemoveUpgradingMarker: remove %q: %w", target, err)
	}

	// fsync parent dir so the unlink is durable.
	dir, err := os.Open(harmonikDir) //nolint:gosec // G304: harmonikDir is daemon-internal .harmonik dir
	if err != nil {
		return fmt.Errorf("operatornfr: RemoveUpgradingMarker: open parent dir %q: %w", harmonikDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close() //nolint:errcheck // cleanup error unactionable
		return fmt.Errorf("operatornfr: RemoveUpgradingMarker: fsync parent dir %q: %w", harmonikDir, err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("operatornfr: RemoveUpgradingMarker: close parent dir %q: %w", harmonikDir, err)
	}
	return nil
}

// ReadUpgradingMarker reads the `.harmonik/daemon.upgrading` marker file and
// unmarshals it into an UpgradingMarker. Returns ErrMarkerAbsent when the file
// does not exist. Returns an error for I/O or parse failures.
//
// Spec ref: operator-nfr.md §4.6 ON-020a — "On daemon startup, PL-005 step 0
// MUST read this marker; if present and the hash matches, startup proceeds
// normally and marker is removed; if hash does not match, refuse startup with
// §8 code 14."
func ReadUpgradingMarker(harmonikDir string) (UpgradingMarker, error) {
	path := harmonikDir + "/" + UpgradingMarkerName
	//nolint:gosec // G304: path derived from harmonikDir (daemon-internal .harmonik dir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return UpgradingMarker{}, ErrMarkerAbsent
		}
		return UpgradingMarker{}, fmt.Errorf("operatornfr: ReadUpgradingMarker: read %q: %w", path, err)
	}

	var m UpgradingMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return UpgradingMarker{}, fmt.Errorf("operatornfr: ReadUpgradingMarker: parse %q: %w", path, err)
	}
	return m, nil
}

// ErrMarkerAbsent is returned by ReadUpgradingMarker when the
// `.harmonik/daemon.upgrading` file does not exist. A missing marker indicates
// a fresh (non-upgrade) startup.
var ErrMarkerAbsent = fmt.Errorf("operatornfr: upgrading marker absent")
