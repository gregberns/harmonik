// Package testhelpers — crash-injection harness for adapter idempotency tests
// (hk-872.54).
//
// This file provides shared infrastructure for the BI-029..BI-032 family of
// crash-recovery tests. The harness enables tests to:
//
//  1. Write a durable IntentLogEntry per BI-030 (atomic temp+rename + fsync(fd)
//     + fsync(parent_dir)) — simulating the adapter's pre-write step.
//  2. Represent a "crash" between intent-log fsync and br call completion by
//     simply returning after the write without calling br (equivalent to the OS
//     killing the process between those two points).
//  3. Scan the intent-log directory on "restart" to recover surviving entries.
//  4. Write a mock `br` binary whose exit code and output are caller-controlled,
//     so dependent tests can drive BrOK, BrConflict, BrUnavailable, etc.
//     without a real Beads installation.
//
// All exported helpers use the prefix B87254 per implementer-protocol.md
// helper-prefix discipline (bead hk-872.54).
//
// Spec ref: specs/beads-integration.md §4.10 BI-029..BI-032, §10.2 (test-surface
// obligations for the idempotency family).
package testhelpers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// b87254IntentDirName is the canonical subdirectory name for intent-log files,
// relative to the .harmonik root.
//
// Spec ref: specs/beads-integration.md §6.2 — ".harmonik/beads-intents/".
const b87254IntentDirName = "beads-intents"

// b87254IntentEntryWire is the on-disk JSON shape for an IntentLogEntry written
// by the harness. It mirrors core.IntentLogEntry using the field names from the
// spec's RECORD definition, serialised with snake_case JSON keys.
type b87254IntentEntryWire struct {
	IdempotencyKey    string    `json:"idempotency_key"`
	RunID             string    `json:"run_id"`
	TransitionID      string    `json:"transition_id"`
	Op                string    `json:"op"`
	BeadID            string    `json:"bead_id"`
	IntendedPostState string    `json:"intended_post_state"`
	RequestedAt       time.Time `json:"requested_at"`
	SchemaVersion     int       `json:"schema_version"`
}

// B87254IntentWriteResult carries the paths written by B87254WriteIntentEntry
// so dependent tests can inspect the filesystem state after a simulated crash.
type B87254IntentWriteResult struct {
	// IntentDir is the .harmonik/beads-intents/ directory that received the file.
	IntentDir string

	// FilePath is the final (post-rename) path of the intent file.
	FilePath string

	// Entry is the IntentLogEntry that was serialised to disk.
	Entry core.IntentLogEntry
}

// B87254MockBrSpec describes what the mock br binary should do when invoked.
// Tests inject a mock br binary whose behavior is controlled by this spec so that
// dependent test scenarios can exercise BrOK, BrConflict, BrUnavailable, etc.
// without a real Beads installation.
type B87254MockBrSpec struct {
	// Stdout is the raw string the mock binary prints to stdout.
	Stdout string

	// Stderr is the raw string the mock binary prints to stderr.
	Stderr string

	// ExitCode is the exit code the mock binary exits with.
	ExitCode int

	// SleepMs is an optional sleep in milliseconds before exiting. Used to
	// simulate a slow br call that a timeout or crash can interrupt.
	SleepMs int
}

// B87254WriteIntentEntry writes a durable IntentLogEntry to intentDir following
// the BI-030 atomicity discipline:
//
//  1. Encode entry as JSON.
//  2. Write to intentDir/<encoded_key>.json.tmp-<rand>.
//  3. fsync(temp_fd).
//  4. rename(temp, intentDir/<encoded_key>.json).
//  5. fsync(parent_directory_fd).
//
// Colons in the idempotency key are encoded as underscores per OQ-BI-003 so the
// filename is valid on filesystems that forbid colons (macOS HFS+, etc.).
//
// The caller must ensure entry.Valid() is true; the helper calls t.Fatalf if the
// entry is invalid or any I/O step fails.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030; §6.2 colon-encoding policy.
func B87254WriteIntentEntry(t *testing.T, intentDir string, entry core.IntentLogEntry) B87254IntentWriteResult {
	t.Helper()

	if !entry.Valid() {
		t.Fatalf("B87254WriteIntentEntry: entry.Valid() == false; fields: %+v", entry)
	}

	wire := b87254IntentEntryWire{
		IdempotencyKey:    entry.IdempotencyKey,
		RunID:             entry.RunID.String(),
		TransitionID:      entry.TransitionID.String(),
		Op:                string(entry.Op),
		BeadID:            string(entry.BeadID),
		IntendedPostState: string(entry.IntendedPostState),
		RequestedAt:       entry.RequestedAt,
		SchemaVersion:     entry.SchemaVersion,
	}

	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("B87254WriteIntentEntry: json.Marshal: %v", err)
	}

	// Encode colons in key to underscores per OQ-BI-003 (filesystem portability).
	encodedKey := strings.ReplaceAll(entry.IdempotencyKey, ":", "_")

	// Step 1: write to temp file with random suffix (prevents collision under
	// concurrent recovery per BI-030 note).
	randSuffix, err := b87254RandHex(8)
	if err != nil {
		t.Fatalf("B87254WriteIntentEntry: rand suffix: %v", err)
	}
	tempName := encodedKey + ".json.tmp-" + randSuffix
	tempPath := filepath.Join(intentDir, tempName)

	//nolint:gosec // G304: intentDir is a test temp dir, not user input
	tf, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("B87254WriteIntentEntry: open temp file %q: %v", tempPath, err)
	}

	if _, err := tf.Write(data); err != nil {
		_ = tf.Close()
		t.Fatalf("B87254WriteIntentEntry: write temp file %q: %v", tempPath, err)
	}

	// Step 2: fsync(temp_fd) per BI-030.
	if err := tf.Sync(); err != nil {
		_ = tf.Close()
		t.Fatalf("B87254WriteIntentEntry: fsync temp file %q: %v", tempPath, err)
	}
	if err := tf.Close(); err != nil {
		t.Fatalf("B87254WriteIntentEntry: close temp file %q: %v", tempPath, err)
	}

	// Step 3: rename to final path per BI-030.
	finalName := encodedKey + ".json"
	finalPath := filepath.Join(intentDir, finalName)
	if err := os.Rename(tempPath, finalPath); err != nil {
		t.Fatalf("B87254WriteIntentEntry: rename %q → %q: %v", tempPath, finalPath, err)
	}

	// Step 4: fsync(parent_directory_fd) per BI-030.
	//nolint:gosec // G304: intentDir is a test temp dir, not user input
	pf, err := os.Open(intentDir)
	if err != nil {
		t.Fatalf("B87254WriteIntentEntry: open parent dir %q: %v", intentDir, err)
	}
	if err := pf.Sync(); err != nil {
		_ = pf.Close()
		t.Fatalf("B87254WriteIntentEntry: fsync parent dir %q: %v", intentDir, err)
	}
	defer func() { _ = pf.Close() }()

	return B87254IntentWriteResult{
		IntentDir: intentDir,
		FilePath:  finalPath,
		Entry:     entry,
	}
}

// B87254DeleteIntentEntry deletes an intent file and fsyncs the parent directory
// per BI-030 deletion discipline:
//
//  1. os.Remove(filePath).
//  2. fsync(parent_directory_fd).
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 (deletion step).
func B87254DeleteIntentEntry(t *testing.T, filePath string) {
	t.Helper()

	if err := os.Remove(filePath); err != nil {
		t.Fatalf("B87254DeleteIntentEntry: remove %q: %v", filePath, err)
	}

	parentDir := filepath.Dir(filePath)
	//nolint:gosec // G304: parentDir derived from test temp dir, not user input
	pf, err := os.Open(parentDir)
	if err != nil {
		t.Fatalf("B87254DeleteIntentEntry: open parent dir %q: %v", parentDir, err)
	}
	if err := pf.Sync(); err != nil {
		_ = pf.Close()
		t.Fatalf("B87254DeleteIntentEntry: fsync parent dir %q: %v", parentDir, err)
	}
	defer func() { _ = pf.Close() }()
}

// B87254ReadIntentEntries scans intentDir and returns all surviving
// IntentLogEntry records. Only *.json files are returned; .tmp-* files are
// skipped because they represent writes that crashed before the rename completed
// (the rename never happened, so the write did not land).
//
// Returns a slice of B87254IntentWriteResult. An empty slice means the intent
// log is clean (no surviving writes after recovery).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 — "after a crash, surviving
// files describe exactly the set of writes whose completion is ambiguous."
func B87254ReadIntentEntries(t *testing.T, intentDir string) []B87254IntentWriteResult {
	t.Helper()

	//nolint:gosec // G304: intentDir is a test temp dir, not user input
	des, err := os.ReadDir(intentDir)
	if err != nil {
		t.Fatalf("B87254ReadIntentEntries: ReadDir %q: %v", intentDir, err)
	}

	var results []B87254IntentWriteResult
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		// Skip temp files — these represent crashes during the write step,
		// before the rename completed; the rename never happened so the write
		// did not land.
		if strings.Contains(name, ".tmp-") {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		filePath := filepath.Join(intentDir, name)
		//nolint:gosec // G304: filePath is within a test temp dir, not user input
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("B87254ReadIntentEntries: ReadFile %q: %v", filePath, err)
		}

		var wire b87254IntentEntryWire
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatalf("B87254ReadIntentEntries: json.Unmarshal %q: %v", filePath, err)
		}

		entry, err := b87254WireToEntry(wire)
		if err != nil {
			t.Fatalf("B87254ReadIntentEntries: b87254WireToEntry %q: %v", filePath, err)
		}

		results = append(results, B87254IntentWriteResult{
			IntentDir: intentDir,
			FilePath:  filePath,
			Entry:     entry,
		})
	}

	return results
}

// B87254IntentDirFor returns the beads-intents directory for a given harmonik
// data root (the .harmonik/ directory itself, not its parent), creating it with
// mode 0o700 if it does not exist.
//
// Spec ref: specs/beads-integration.md §6.2 — ".harmonik/beads-intents/" layout.
func B87254IntentDirFor(t *testing.T, harmonikDir string) string {
	t.Helper()
	dir := filepath.Join(harmonikDir, b87254IntentDirName)
	//nolint:gosec // G301: 0700 matches .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("B87254IntentDirFor: MkdirAll %q: %v", dir, err)
	}
	return dir
}

// B87254NewIntentEntry constructs a valid core.IntentLogEntry for use in crash
// tests. The IdempotencyKey is derived as "<runID>:<transitionID>:<op>" per
// BI-029. RequestedAt is set to time.Now().UTC() truncated to millisecond
// precision (matching the precision of the JSON timestamp round-trip).
// SchemaVersion is fixed at 1.
//
// Callers may override any exported field after construction if a non-standard
// value is needed for a specific test scenario.
//
// Spec ref: specs/beads-integration.md §4.10 BI-029 — idempotency key formula.
func B87254NewIntentEntry(
	runID core.RunID,
	transitionID core.TransitionID,
	op core.TerminalOp,
	beadID core.BeadID,
	intendedPostState core.CoarseStatus,
) core.IntentLogEntry {
	key := fmt.Sprintf("%s:%s:%s", runID.String(), transitionID.String(), string(op))
	return core.IntentLogEntry{
		IdempotencyKey:    key,
		RunID:             runID,
		TransitionID:      transitionID,
		Op:                op,
		BeadID:            beadID,
		IntendedPostState: intendedPostState,
		RequestedAt:       time.Now().UTC().Truncate(time.Millisecond),
		SchemaVersion:     1,
	}
}

// B87254WriteMockBr writes a POSIX shell script at binaryPath that behaves
// according to spec:
//
//   - Sleeps spec.SleepMs milliseconds if > 0 (simulates a slow br call).
//   - Prints spec.Stdout to stdout.
//   - Prints spec.Stderr to stderr.
//   - Exits with spec.ExitCode.
//
// binaryPath MUST be an absolute path (typically under t.TempDir()). The file
// is written with mode 0755 for executability. The caller owns cleanup (t.TempDir
// handles it automatically when binaryPath is under TempDir).
//
// Spec ref: specs/beads-integration.md §4.8a BI-025 / BI-025 M2 — injectable
// br binary path via constructor parameter for testability.
func B87254WriteMockBr(t *testing.T, binaryPath string, spec B87254MockBrSpec) {
	t.Helper()

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	if spec.SleepMs > 0 {
		secs := fmt.Sprintf("%d.%03d", spec.SleepMs/1000, spec.SleepMs%1000)
		sb.WriteString(fmt.Sprintf("sleep %s\n", secs))
	}
	sb.WriteString(fmt.Sprintf("printf '%%s' %q\n", spec.Stdout))
	sb.WriteString(fmt.Sprintf("printf '%%s' %q >&2\n", spec.Stderr))
	sb.WriteString(fmt.Sprintf("exit %d\n", spec.ExitCode))

	//nolint:gosec // G306: 0755 required for executability; binaryPath is a test temp dir
	if err := os.WriteFile(binaryPath, []byte(sb.String()), 0o755); err != nil {
		t.Fatalf("B87254WriteMockBr: WriteFile %q: %v", binaryPath, err)
	}
}

// B87254NewMockBrDir creates a fresh temp directory (via TempDir) and returns
// the path of a "br" binary inside it. The binary is NOT written yet — call
// B87254WriteMockBr(t, path, spec) to configure its behavior before use.
//
// The two-step approach lets tests reconfigure the mock between scenarios
// without recreating the directory.
func B87254NewMockBrDir(t *testing.T) string {
	t.Helper()
	dir := TempDir(t)
	return filepath.Join(dir, "br")
}

// b87254WireToEntry converts a b87254IntentEntryWire to a core.IntentLogEntry,
// parsing the typed fields. Returns an error if any field fails to parse.
func b87254WireToEntry(wire b87254IntentEntryWire) (core.IntentLogEntry, error) {
	var runID core.RunID
	if err := runID.UnmarshalText([]byte(wire.RunID)); err != nil {
		return core.IntentLogEntry{}, fmt.Errorf("b87254WireToEntry: run_id: %w", err)
	}

	var transitionID core.TransitionID
	if err := transitionID.UnmarshalText([]byte(wire.TransitionID)); err != nil {
		return core.IntentLogEntry{}, fmt.Errorf("b87254WireToEntry: transition_id: %w", err)
	}

	var op core.TerminalOp
	if err := op.UnmarshalText([]byte(wire.Op)); err != nil {
		return core.IntentLogEntry{}, fmt.Errorf("b87254WireToEntry: op: %w", err)
	}

	var intendedPostState core.CoarseStatus
	if err := intendedPostState.UnmarshalText([]byte(wire.IntendedPostState)); err != nil {
		return core.IntentLogEntry{}, fmt.Errorf("b87254WireToEntry: intended_post_state: %w", err)
	}

	return core.IntentLogEntry{
		IdempotencyKey:    wire.IdempotencyKey,
		RunID:             runID,
		TransitionID:      transitionID,
		Op:                op,
		BeadID:            core.BeadID(wire.BeadID),
		IntendedPostState: intendedPostState,
		RequestedAt:       wire.RequestedAt,
		SchemaVersion:     wire.SchemaVersion,
	}, nil
}

// b87254RandHex returns n cryptographically random lowercase hex characters.
func b87254RandHex(n int) (string, error) {
	const hexChars = "0123456789abcdef"
	out := make([]byte, n)
	for i := range out {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(hexChars))))
		if err != nil {
			return "", err
		}
		out[i] = hexChars[idx.Int64()]
	}
	return string(out), nil
}
