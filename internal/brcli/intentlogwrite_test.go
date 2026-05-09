package brcli

// intentlogwrite_test.go — BI-030 steps 1–4 and step 6: write IntentLogEntry
// to temp file, fsync(temp_fd), rename(2) to canonical path,
// fsync(parent_dir_fd) on create, and unlink + fsync(parent_dir_fd) on delete.
//
// Tests are in package brcli (white-box) to access intentLogEntryWire,
// intentLogRandHex, intentLogSyncFile, intentLogRenameFile, intentLogSyncDir,
// and intentLogUnlinkFile directly.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 steps 1–6; §6.1 RECORD
// IntentLogEntry; §6.2 on-disk layout.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// intentLogWriteFixtureEntry returns a fully-populated core.IntentLogEntry
// with all required fields set to valid values, suitable for WriteIntentLogTmp
// tests.
func intentLogWriteFixtureEntry(t *testing.T) core.IntentLogEntry {
	t.Helper()
	runID := core.RunID(uuid.Must(uuid.NewV7()))
	transitionID := core.TransitionID(uuid.Must(uuid.NewV7()))
	op := core.TerminalOpClaim
	key := runID.String() + ":" + transitionID.String() + ":" + string(op)
	return core.IntentLogEntry{
		IdempotencyKey:    key,
		RunID:             runID,
		TransitionID:      transitionID,
		Op:                op,
		BeadID:            core.BeadID("hk-872"),
		IntendedPostState: core.CoarseStatusInProgress,
		RequestedAt:       time.Now().UTC().Truncate(time.Millisecond),
		SchemaVersion:     1,
	}
}

// --- Happy-path tests ---

// TestWriteIntentLogTmpHappyPath verifies that WriteIntentLogTmp creates a
// temp file with the correct naming shape and encodes the entry as JSON with
// spec-compliant snake_case keys.
func TestWriteIntentLogTmpHappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)

	tmpPath, err := WriteIntentLogTmp(dir, entry)
	if err != nil {
		t.Fatalf("WriteIntentLogTmp: unexpected error: %v", err)
	}

	// Returned path must be within dir.
	if filepath.Dir(tmpPath) != dir {
		t.Errorf("tmpPath dir = %q; want %q", filepath.Dir(tmpPath), dir)
	}

	// Filename must end with ".json.tmp-" followed by 8 hex chars.
	name := filepath.Base(tmpPath)
	if !strings.Contains(name, ".json.tmp-") {
		t.Errorf("tmpPath base %q does not contain .json.tmp-", name)
	}
	parts := strings.SplitN(name, ".json.tmp-", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected filename shape %q", name)
	}
	randSuffix := parts[1]
	if len(randSuffix) != 8 {
		t.Errorf("rand suffix len = %d; want 8 hex chars, got %q", len(randSuffix), randSuffix)
	}
	for _, c := range randSuffix {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("rand suffix %q contains non-hex char %q", randSuffix, c)
		}
	}

	// Encoded key in filename (colons replaced with underscores per §6.2 OQ-BI-003).
	encodedKey := strings.ReplaceAll(entry.IdempotencyKey, ":", "_")
	if !strings.HasPrefix(name, encodedKey) {
		t.Errorf("filename %q does not start with encoded key %q", name, encodedKey)
	}

	// File must exist on disk.
	if _, statErr := os.Stat(tmpPath); statErr != nil {
		t.Fatalf("temp file %q does not exist: %v", tmpPath, statErr)
	}
}

// TestWriteIntentLogTmpJSONContent verifies that the written file contains
// valid JSON with spec-compliant snake_case keys and the correct values
// from the supplied entry.
func TestWriteIntentLogTmpJSONContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)

	tmpPath, err := WriteIntentLogTmp(dir, entry)
	if err != nil {
		t.Fatalf("WriteIntentLogTmp: %v", err)
	}

	//nolint:gosec // G304: tmpPath is a test temp file, not user input
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("ReadFile %q: %v", tmpPath, err)
	}

	var wire intentLogEntryWire
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("json.Unmarshal: %v; raw: %s", err, data)
	}

	if wire.IdempotencyKey != entry.IdempotencyKey {
		t.Errorf("idempotency_key = %q; want %q", wire.IdempotencyKey, entry.IdempotencyKey)
	}
	if wire.RunID != entry.RunID.String() {
		t.Errorf("run_id = %q; want %q", wire.RunID, entry.RunID.String())
	}
	if wire.TransitionID != entry.TransitionID.String() {
		t.Errorf("transition_id = %q; want %q", wire.TransitionID, entry.TransitionID.String())
	}
	if wire.Op != string(entry.Op) {
		t.Errorf("op = %q; want %q", wire.Op, string(entry.Op))
	}
	if wire.BeadID != string(entry.BeadID) {
		t.Errorf("bead_id = %q; want %q", wire.BeadID, string(entry.BeadID))
	}
	if wire.IntendedPostState != string(entry.IntendedPostState) {
		t.Errorf("intended_post_state = %q; want %q", wire.IntendedPostState, string(entry.IntendedPostState))
	}
	if !wire.RequestedAt.Equal(entry.RequestedAt) {
		t.Errorf("requested_at = %v; want %v", wire.RequestedAt, entry.RequestedAt)
	}
	if wire.SchemaVersion != entry.SchemaVersion {
		t.Errorf("schema_version = %d; want %d", wire.SchemaVersion, entry.SchemaVersion)
	}

	// Verify JSON keys are snake_case (not PascalCase) — confirm the wire struct
	// controls serialisation format.
	if strings.Contains(string(data), `"IdempotencyKey"`) {
		t.Errorf("JSON contains PascalCase key 'IdempotencyKey'; want snake_case 'idempotency_key'")
	}
	if !strings.Contains(string(data), `"idempotency_key"`) {
		t.Errorf("JSON missing snake_case key 'idempotency_key'; raw: %s", data)
	}
}

// TestWriteIntentLogTmpColonEncoding verifies that colons in the idempotency
// key are encoded as underscores in the filename (§6.2 OQ-BI-003 portability).
func TestWriteIntentLogTmpColonEncoding(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)
	// The fixture entry already uses the canonical "<run_id>:<transition_id>:<op>"
	// format with two colons. Verify both are encoded.

	tmpPath, err := WriteIntentLogTmp(dir, entry)
	if err != nil {
		t.Fatalf("WriteIntentLogTmp: %v", err)
	}

	name := filepath.Base(tmpPath)
	if strings.Contains(name, ":") {
		t.Errorf("filename %q contains colon; want colons encoded as underscores", name)
	}
}

// TestWriteIntentLogTmpUniqueOnConcurrentCalls verifies that two calls with the
// same entry produce different temp paths (randomness guards concurrent recovery).
func TestWriteIntentLogTmpUniqueOnConcurrentCalls(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)

	pathA, err := WriteIntentLogTmp(dir, entry)
	if err != nil {
		t.Fatalf("WriteIntentLogTmp call A: %v", err)
	}
	pathB, err := WriteIntentLogTmp(dir, entry)
	if err != nil {
		t.Fatalf("WriteIntentLogTmp call B: %v", err)
	}
	if pathA == pathB {
		t.Errorf("two consecutive calls produced the same tmp path %q; want distinct paths", pathA)
	}
}

// TestWriteIntentLogTmpFileMode verifies the temp file is created with mode
// 0600 (owner read/write only, per the O_EXCL|0600 open call).
func TestWriteIntentLogTmpFileMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)

	tmpPath, err := WriteIntentLogTmp(dir, entry)
	if err != nil {
		t.Fatalf("WriteIntentLogTmp: %v", err)
	}

	info, err := os.Stat(tmpPath)
	if err != nil {
		t.Fatalf("Stat %q: %v", tmpPath, err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("file mode = %04o; want 0600", mode)
	}
}

// --- Error-path tests ---

// TestWriteIntentLogTmpInvalidEntry verifies that WriteIntentLogTmp returns an
// error when the entry fails Valid() — no file must be written.
func TestWriteIntentLogTmpInvalidEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var zero core.IntentLogEntry // all zero — fails Valid()

	tmpPath, err := WriteIntentLogTmp(dir, zero)
	if err == nil {
		t.Errorf("WriteIntentLogTmp: expected error for invalid entry, got nil")
	}
	if tmpPath != "" {
		t.Errorf("WriteIntentLogTmp: expected empty tmpPath on error, got %q", tmpPath)
	}
}

// TestWriteIntentLogTmpDirNotExist verifies that WriteIntentLogTmp returns an
// error when the target directory does not exist.
func TestWriteIntentLogTmpDirNotExist(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nonexistent-subdir")
	entry := intentLogWriteFixtureEntry(t)

	tmpPath, err := WriteIntentLogTmp(dir, entry)
	if err == nil {
		t.Errorf("WriteIntentLogTmp: expected error for nonexistent dir, got nil")
	}
	if tmpPath != "" {
		t.Errorf("WriteIntentLogTmp: expected empty tmpPath on error, got %q", tmpPath)
	}
}

// --- Unit tests for intentLogRandHex ---

// TestIntentLogRandHexLength verifies that intentLogRandHex returns exactly n
// lowercase hex characters.
func TestIntentLogRandHexLength(t *testing.T) {
	for _, n := range []int{1, 4, 8, 16} {
		n := n
		t.Run("n="+strings.Repeat("x", n)[:0]+string(rune('0'+n)), func(t *testing.T) {
			t.Parallel()
			got, err := intentLogRandHex(n)
			if err != nil {
				t.Fatalf("intentLogRandHex(%d): %v", n, err)
			}
			if len(got) != n {
				t.Errorf("len = %d; want %d", len(got), n)
			}
			for _, c := range got {
				if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
					t.Errorf("non-hex char %q in output %q", c, got)
				}
			}
		})
	}
}

// TestIntentLogRandHexNotConstant verifies that two calls do not always produce
// the same output (probabilistic: P(false positive) = 16^{-8} ≈ 6e-10).
func TestIntentLogRandHexNotConstant(t *testing.T) {
	t.Parallel()
	a, err := intentLogRandHex(8)
	if err != nil {
		t.Fatalf("call A: %v", err)
	}
	b, err := intentLogRandHex(8)
	if err != nil {
		t.Fatalf("call B: %v", err)
	}
	if a == b {
		t.Errorf("two calls returned the same value %q; collision probability is ~6e-10", a)
	}
}

// --- BI-030 step 2: fsync(temp_fd) tests (hk-872.37.2) ---

// TestWriteIntentLogTmpFsyncCalled verifies that WriteIntentLogTmp calls
// intentLogSyncFile (fsync(2)) on the temp file fd before closing it
// (BI-030 step 2).  Uses the package-level intentLogSyncFile hook to count
// invocations without requiring OS-level fsync introspection.
//
// NOTE: this test mutates a package-level variable and MUST NOT run in
// parallel with other tests that also replace intentLogSyncFile.
func TestWriteIntentLogTmpFsyncCalled(t *testing.T) {
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)

	var syncCallCount atomic.Int64
	orig := intentLogSyncFile
	t.Cleanup(func() { intentLogSyncFile = orig })
	intentLogSyncFile = func(f *os.File) error {
		syncCallCount.Add(1)
		return f.Sync() // still perform the real fsync
	}

	tmpPath, err := WriteIntentLogTmp(dir, entry)
	if err != nil {
		t.Fatalf("WriteIntentLogTmp: unexpected error: %v", err)
	}
	if tmpPath == "" {
		t.Fatal("WriteIntentLogTmp: returned empty path on success")
	}

	if n := syncCallCount.Load(); n != 1 {
		t.Errorf("intentLogSyncFile called %d times; want exactly 1", n)
	}
}

// TestWriteIntentLogTmpFsyncErrorRemovesTmpFile verifies that when
// intentLogSyncFile returns an error (BI-030 step 2 failure), the temp file
// is removed and WriteIntentLogTmp returns a non-nil error with an empty path.
//
// NOTE: this test mutates a package-level variable and MUST NOT run in
// parallel with other tests that also replace intentLogSyncFile.
func TestWriteIntentLogTmpFsyncErrorRemovesTmpFile(t *testing.T) {
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)

	var capturedPath string
	orig := intentLogSyncFile
	t.Cleanup(func() { intentLogSyncFile = orig })
	intentLogSyncFile = func(f *os.File) error {
		capturedPath = f.Name()
		return errors.New("injected fsync error")
	}

	tmpPath, err := WriteIntentLogTmp(dir, entry)
	if err == nil {
		t.Fatal("WriteIntentLogTmp: expected error on fsync failure, got nil")
	}
	if tmpPath != "" {
		t.Errorf("WriteIntentLogTmp: expected empty tmpPath on error, got %q", tmpPath)
	}

	// Temp file must be cleaned up on sync failure.
	if capturedPath == "" {
		t.Fatal("sync hook was not called (capturedPath empty)")
	}
	if _, statErr := os.Stat(capturedPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("temp file %q still exists after fsync failure; want removed", capturedPath)
	}

	// Error message must mention fsync.
	if !strings.Contains(err.Error(), "fsync") {
		t.Errorf("error %q does not mention 'fsync'", err.Error())
	}
}

// --- BI-030 step 3: rename(2) to canonical <key>.json tests (hk-872.37.3) ---

// intentLogRenameFixture creates a temp file in dir with a .json.tmp-<rand>
// suffix and returns its path, simulating the output of WriteIntentLogTmp.
func intentLogRenameFixture(t *testing.T, dir string) string {
	t.Helper()
	suffix, err := intentLogRandHex(8)
	if err != nil {
		t.Fatalf("intentLogRandHex: %v", err)
	}
	name := "hk_123_abc_claim.json.tmp-" + suffix
	path := filepath.Join(dir, name)
	//nolint:gosec // G304: path is a test temp file, not user input
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create fixture tmp file %q: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close fixture tmp file %q: %v", path, err)
	}
	return path
}

// TestRenameIntentLogTmpToFinalHappyPath verifies that RenameIntentLogTmpToFinal
// renames the temp file to the canonical <encoded_key>.json path, returns the
// final path, and removes the temp file.
func TestRenameIntentLogTmpToFinalHappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)
	tmpPath := intentLogRenameFixture(t, dir)

	finalPath, err := RenameIntentLogTmpToFinal(tmpPath, dir, entry.IdempotencyKey)
	if err != nil {
		t.Fatalf("RenameIntentLogTmpToFinal: unexpected error: %v", err)
	}

	// Final file must exist.
	if _, statErr := os.Stat(finalPath); statErr != nil {
		t.Errorf("final file %q does not exist: %v", finalPath, statErr)
	}

	// Temp file must be gone.
	if _, statErr := os.Stat(tmpPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("tmp file %q still exists after rename; want removed", tmpPath)
	}

	// Final filename must be <encoded_key>.json (colons encoded as underscores).
	encodedKey := strings.ReplaceAll(entry.IdempotencyKey, ":", "_")
	wantName := encodedKey + ".json"
	if filepath.Base(finalPath) != wantName {
		t.Errorf("final filename = %q; want %q", filepath.Base(finalPath), wantName)
	}

	// Final path must live in dir.
	if filepath.Dir(finalPath) != dir {
		t.Errorf("finalPath dir = %q; want %q", filepath.Dir(finalPath), dir)
	}
}

// TestRenameIntentLogTmpToFinalColonEncoding verifies that colons in the
// idempotency key are encoded as underscores in the canonical filename,
// matching the encoding used in WriteIntentLogTmp (§6.2 OQ-BI-003).
func TestRenameIntentLogTmpToFinalColonEncoding(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tmpPath := intentLogRenameFixture(t, dir)
	keyWithColons := "run-abc:trans-xyz:claim"

	finalPath, err := RenameIntentLogTmpToFinal(tmpPath, dir, keyWithColons)
	if err != nil {
		t.Fatalf("RenameIntentLogTmpToFinal: %v", err)
	}

	name := filepath.Base(finalPath)
	if strings.Contains(name, ":") {
		t.Errorf("canonical filename %q contains colon; want underscores", name)
	}
	wantName := "run-abc_trans-xyz_claim.json"
	if name != wantName {
		t.Errorf("canonical filename = %q; want %q", name, wantName)
	}
}

// TestRenameIntentLogTmpToFinalOverwriteExisting verifies that
// RenameIntentLogTmpToFinal succeeds when the canonical path already exists
// (idempotent retry on crash between step 3 and step 4 — overwrite is safe
// because the key is deterministic).
func TestRenameIntentLogTmpToFinalOverwriteExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)
	encodedKey := strings.ReplaceAll(entry.IdempotencyKey, ":", "_")
	existingFinal := filepath.Join(dir, encodedKey+".json")

	// Pre-create the canonical file to simulate a prior partial write.
	//nolint:gosec // G304: existingFinal is a test temp path, not user input
	if err := os.WriteFile(existingFinal, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatalf("pre-create canonical file: %v", err)
	}

	tmpPath := intentLogRenameFixture(t, dir)

	finalPath, err := RenameIntentLogTmpToFinal(tmpPath, dir, entry.IdempotencyKey)
	if err != nil {
		t.Fatalf("RenameIntentLogTmpToFinal: unexpected error on overwrite: %v", err)
	}
	if finalPath != existingFinal {
		t.Errorf("finalPath = %q; want %q", finalPath, existingFinal)
	}

	// Canonical file must still exist.
	if _, statErr := os.Stat(finalPath); statErr != nil {
		t.Errorf("final file %q does not exist after overwrite rename: %v", finalPath, statErr)
	}

	// Temp file must be gone.
	if _, statErr := os.Stat(tmpPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("tmp file %q still exists after overwrite rename", tmpPath)
	}
}

// TestRenameIntentLogTmpToFinalErrorLeavesTompFile verifies that when the
// rename fails (injected error), the original temp file is left on disk and
// RenameIntentLogTmpToFinal returns a non-nil error with an empty finalPath.
//
// NOTE: this test mutates a package-level variable and MUST NOT run in
// parallel with other tests that also replace intentLogRenameFile.
func TestRenameIntentLogTmpToFinalErrorLeavesTmpFile(t *testing.T) {
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)
	tmpPath := intentLogRenameFixture(t, dir)

	orig := intentLogRenameFile
	t.Cleanup(func() { intentLogRenameFile = orig })
	intentLogRenameFile = func(_, _ string) error {
		return errors.New("injected rename error")
	}

	finalPath, err := RenameIntentLogTmpToFinal(tmpPath, dir, entry.IdempotencyKey)
	if err == nil {
		t.Fatal("RenameIntentLogTmpToFinal: expected error on rename failure, got nil")
	}
	if finalPath != "" {
		t.Errorf("RenameIntentLogTmpToFinal: expected empty finalPath on error, got %q", finalPath)
	}

	// Temp file must still be present (caller can retry or recover it).
	if _, statErr := os.Stat(tmpPath); statErr != nil {
		t.Errorf("tmp file %q missing after rename failure; want retained for recovery: %v", tmpPath, statErr)
	}

	// Error message must identify the rename operation.
	if !strings.Contains(err.Error(), "rename") {
		t.Errorf("error %q does not mention 'rename'", err.Error())
	}
}

// --- BI-030 step 4: fsync(parent_directory_fd) tests (hk-872.37.4) ---

// TestFsyncIntentLogParentDirHappyPath verifies that FsyncIntentLogParentDir
// succeeds on a real directory and calls intentLogSyncDir exactly once
// (BI-030 step 4).
//
// NOTE: this test mutates a package-level variable and MUST NOT run in
// parallel with other tests that also replace intentLogSyncDir.
func TestFsyncIntentLogParentDirHappyPath(t *testing.T) {
	dir := t.TempDir()

	var syncDirCallCount atomic.Int64
	orig := intentLogSyncDir
	t.Cleanup(func() { intentLogSyncDir = orig })
	intentLogSyncDir = func(f *os.File) error {
		syncDirCallCount.Add(1)
		return f.Sync() // still perform the real fsync
	}

	if err := FsyncIntentLogParentDir(dir); err != nil {
		t.Fatalf("FsyncIntentLogParentDir: unexpected error: %v", err)
	}

	if n := syncDirCallCount.Load(); n != 1 {
		t.Errorf("intentLogSyncDir called %d times; want exactly 1", n)
	}
}

// TestFsyncIntentLogParentDirDirNotExist verifies that FsyncIntentLogParentDir
// returns a non-nil error when the directory does not exist (open fails).
func TestFsyncIntentLogParentDirDirNotExist(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nonexistent-subdir")

	err := FsyncIntentLogParentDir(dir)
	if err == nil {
		t.Fatal("FsyncIntentLogParentDir: expected error for nonexistent dir, got nil")
	}

	// Error message must identify the directory and mention the open failure.
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not contain dir path %q", err.Error(), dir)
	}
}

// TestFsyncIntentLogParentDirSyncError verifies that when intentLogSyncDir
// returns an error (BI-030 step 4 fsync failure), FsyncIntentLogParentDir
// returns a non-nil error mentioning the directory path.
//
// NOTE: this test mutates a package-level variable and MUST NOT run in
// parallel with other tests that also replace intentLogSyncDir.
func TestFsyncIntentLogParentDirSyncError(t *testing.T) {
	dir := t.TempDir()

	orig := intentLogSyncDir
	t.Cleanup(func() { intentLogSyncDir = orig })
	intentLogSyncDir = func(_ *os.File) error {
		return errors.New("injected dir fsync error")
	}

	err := FsyncIntentLogParentDir(dir)
	if err == nil {
		t.Fatal("FsyncIntentLogParentDir: expected error on dir fsync failure, got nil")
	}

	// Error message must contain "fsync" and the directory path.
	if !strings.Contains(err.Error(), "fsync") {
		t.Errorf("error %q does not mention 'fsync'", err.Error())
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not contain dir path %q", err.Error(), dir)
	}
}

// --- BI-030 step 6: unlink + fsync(parent_directory_fd) tests (hk-872.37.6) ---

// intentLogDeleteFixture creates the canonical intent-log file for
// idempotencyKey in dir and returns the file path, simulating the state after
// a successful rename (steps 3–4) and `br` invocation (step 5).
func intentLogDeleteFixture(t *testing.T, dir, idempotencyKey string) string {
	t.Helper()
	encodedKey := strings.ReplaceAll(idempotencyKey, ":", "_")
	path := filepath.Join(dir, encodedKey+".json")
	//nolint:gosec // G304: path is a test temp file, not user input
	if err := os.WriteFile(path, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatalf("intentLogDeleteFixture: create canonical file %q: %v", path, err)
	}
	return path
}

// TestDeleteIntentLogAndSyncParentHappyPath verifies that
// DeleteIntentLogAndSyncParent removes the canonical intent-log file, calls
// the parent-dir fsync exactly once, and returns nil (BI-030 step 6).
//
// NOTE: this test mutates a package-level variable and MUST NOT run in
// parallel with other tests that also replace intentLogSyncDir.
func TestDeleteIntentLogAndSyncParentHappyPath(t *testing.T) {
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)

	canonicalPath := intentLogDeleteFixture(t, dir, entry.IdempotencyKey)

	var syncDirCallCount atomic.Int64
	origSync := intentLogSyncDir
	t.Cleanup(func() { intentLogSyncDir = origSync })
	intentLogSyncDir = func(f *os.File) error {
		syncDirCallCount.Add(1)
		return f.Sync() // still perform the real fsync
	}

	if err := DeleteIntentLogAndSyncParent(dir, entry.IdempotencyKey); err != nil {
		t.Fatalf("DeleteIntentLogAndSyncParent: unexpected error: %v", err)
	}

	// Intent file must be gone.
	if _, statErr := os.Stat(canonicalPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("intent file %q still exists after delete; want removed", canonicalPath)
	}

	// Parent-dir fsync must be called exactly once.
	if n := syncDirCallCount.Load(); n != 1 {
		t.Errorf("intentLogSyncDir called %d times; want exactly 1", n)
	}
}

// TestDeleteIntentLogAndSyncParentColonEncoding verifies that colons in the
// idempotency key are encoded as underscores when constructing the canonical
// filename to unlink (§6.2 OQ-BI-003 portability; mirrors WriteIntentLogTmp
// and RenameIntentLogTmpToFinal encoding).
func TestDeleteIntentLogAndSyncParentColonEncoding(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyWithColons := "run-abc:trans-xyz:claim"

	// Create the file using the encoded name so unlink can find it.
	canonicalPath := intentLogDeleteFixture(t, dir, keyWithColons)

	if err := DeleteIntentLogAndSyncParent(dir, keyWithColons); err != nil {
		t.Fatalf("DeleteIntentLogAndSyncParent: unexpected error: %v", err)
	}

	// Canonical (encoded) file must be gone.
	if _, statErr := os.Stat(canonicalPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("encoded intent file %q still exists after delete", canonicalPath)
	}
}

// TestDeleteIntentLogAndSyncParentUnlinkError verifies that when
// intentLogUnlinkFile returns an error, DeleteIntentLogAndSyncParent returns a
// non-nil error mentioning "unlink" and does NOT proceed to fsync the parent
// directory.
//
// NOTE: this test mutates package-level variables and MUST NOT run in parallel
// with other tests that replace intentLogUnlinkFile or intentLogSyncDir.
func TestDeleteIntentLogAndSyncParentUnlinkError(t *testing.T) {
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)

	origUnlink := intentLogUnlinkFile
	t.Cleanup(func() { intentLogUnlinkFile = origUnlink })
	intentLogUnlinkFile = func(_ string) error {
		return errors.New("injected unlink error")
	}

	var syncDirCallCount atomic.Int64
	origSync := intentLogSyncDir
	t.Cleanup(func() { intentLogSyncDir = origSync })
	intentLogSyncDir = func(f *os.File) error {
		syncDirCallCount.Add(1)
		return f.Sync()
	}

	err := DeleteIntentLogAndSyncParent(dir, entry.IdempotencyKey)
	if err == nil {
		t.Fatal("DeleteIntentLogAndSyncParent: expected error on unlink failure, got nil")
	}

	// Error message must identify the unlink operation.
	if !strings.Contains(err.Error(), "unlink") {
		t.Errorf("error %q does not mention 'unlink'", err.Error())
	}

	// Parent-dir fsync must NOT be called when unlink fails.
	if n := syncDirCallCount.Load(); n != 0 {
		t.Errorf("intentLogSyncDir called %d times on unlink failure; want 0", n)
	}
}

// TestDeleteIntentLogAndSyncParentFsyncError verifies that when the parent-dir
// fsync fails after a successful unlink, DeleteIntentLogAndSyncParent returns a
// non-nil error mentioning "fsync".
//
// NOTE: this test mutates package-level variables and MUST NOT run in parallel
// with other tests that replace intentLogUnlinkFile or intentLogSyncDir.
func TestDeleteIntentLogAndSyncParentFsyncError(t *testing.T) {
	dir := t.TempDir()
	entry := intentLogWriteFixtureEntry(t)

	// Stub unlink to succeed without touching the filesystem.
	origUnlink := intentLogUnlinkFile
	t.Cleanup(func() { intentLogUnlinkFile = origUnlink })
	intentLogUnlinkFile = func(_ string) error { return nil }

	// Stub parent-dir fsync to fail.
	origSync := intentLogSyncDir
	t.Cleanup(func() { intentLogSyncDir = origSync })
	intentLogSyncDir = func(_ *os.File) error {
		return errors.New("injected dir fsync error")
	}

	err := DeleteIntentLogAndSyncParent(dir, entry.IdempotencyKey)
	if err == nil {
		t.Fatal("DeleteIntentLogAndSyncParent: expected error on parent-dir fsync failure, got nil")
	}

	// Error message must mention "fsync".
	if !strings.Contains(err.Error(), "fsync") {
		t.Errorf("error %q does not mention 'fsync'", err.Error())
	}
}
