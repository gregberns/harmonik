package testhelpers_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/testhelpers"
)

// TestB87254_WriteIntentEntry_RoundTrip verifies that B87254WriteIntentEntry
// writes a durable, parseable IntentLogEntry and that B87254ReadIntentEntries
// recovers it with field-for-field fidelity.
//
// This is the self-test for the harness's BI-030 atomic write + BI-031 read-back
// path.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 (atomic write + parent-dir
// fsync), BI-031 (startup recovery reads surviving intent files).
func TestB87254_WriteIntentEntry_RoundTrip(t *testing.T) {
	t.Parallel()

	env := testhelpers.NewEnv(t)
	intentDir := testhelpers.B87254IntentDirFor(t, env.Harmonik)

	runID := core.RunID(uuid.Must(uuid.NewV7()))
	transitionID := core.TransitionID(uuid.Must(uuid.NewV7()))
	entry := testhelpers.B87254NewIntentEntry(
		runID, transitionID,
		core.TerminalOpClaim,
		core.BeadID("hk-test.1"),
		core.CoarseStatusInProgress,
	)

	written := testhelpers.B87254WriteIntentEntry(t, intentDir, entry)

	if _, err := os.Stat(written.FilePath); err != nil {
		t.Fatalf("B87254WriteIntentEntry: intent file not found at %q: %v", written.FilePath, err)
	}

	des, err := os.ReadDir(intentDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, de := range des {
		if strings.Contains(de.Name(), ".tmp-") {
			t.Errorf("temp file survived rename: %q", de.Name())
		}
	}

	recovered := testhelpers.B87254ReadIntentEntries(t, intentDir)
	if len(recovered) != 1 {
		t.Fatalf("want 1 recovered entry, got %d", len(recovered))
	}

	got := recovered[0].Entry
	if got.IdempotencyKey != entry.IdempotencyKey {
		t.Errorf("IdempotencyKey: want %q, got %q", entry.IdempotencyKey, got.IdempotencyKey)
	}
	if got.RunID != entry.RunID {
		t.Errorf("RunID: want %v, got %v", entry.RunID, got.RunID)
	}
	if got.TransitionID != entry.TransitionID {
		t.Errorf("TransitionID: want %v, got %v", entry.TransitionID, got.TransitionID)
	}
	if got.Op != entry.Op {
		t.Errorf("Op: want %q, got %q", entry.Op, got.Op)
	}
	if got.BeadID != entry.BeadID {
		t.Errorf("BeadID: want %q, got %q", entry.BeadID, got.BeadID)
	}
	if got.IntendedPostState != entry.IntendedPostState {
		t.Errorf("IntendedPostState: want %q, got %q", entry.IntendedPostState, got.IntendedPostState)
	}
	if got.SchemaVersion != entry.SchemaVersion {
		t.Errorf("SchemaVersion: want %d, got %d", entry.SchemaVersion, got.SchemaVersion)
	}
}

// TestB87254_CrashAndRecover_ConvergesIdempotent is the canonical canary test
// for the crash-injection harness. It drives the full crash-and-recover loop:
//
//  1. Write an intent entry (BI-030 atomic fsync) — simulates pre-crash state.
//  2. Do NOT call br — simulates crash between fsync and br completion.
//  3. Scan the intent dir on "restart" — the entry must survive (BI-031).
//  4. Execute a mock br that exits 0 (BrOK) — simulates successful reissue.
//  5. Delete the intent file (BI-030 deletion discipline).
//  6. Assert intent dir is empty — idempotent completion confirmed.
//
// This test validates the harness infrastructure. Dependent beads (hk-872.38,
// .37, .36, .40, .44) will use these same primitives to drive their adapter
// implementations against crash scenarios.
//
// Spec ref: specs/beads-integration.md §10.2 — "Crash-injection tests kill the
// adapter between intent-log fsync and br call completion, then restart and
// verify idempotent completion via the audit-log check."
func TestB87254_CrashAndRecover_ConvergesIdempotent(t *testing.T) {
	t.Parallel()

	env := testhelpers.NewEnv(t)
	intentDir := testhelpers.B87254IntentDirFor(t, env.Harmonik)

	runID := core.RunID(uuid.Must(uuid.NewV7()))
	transitionID := core.TransitionID(uuid.Must(uuid.NewV7()))
	entry := testhelpers.B87254NewIntentEntry(
		runID, transitionID,
		core.TerminalOpClose,
		core.BeadID("hk-crash.99"),
		core.CoarseStatusClosed,
	)

	written := testhelpers.B87254WriteIntentEntry(t, intentDir, entry)
	_ = written // adapter is "killed" here; br was never called

	surviving := testhelpers.B87254ReadIntentEntries(t, intentDir)
	if len(surviving) != 1 {
		t.Fatalf("Phase3: expected 1 surviving intent entry, got %d", len(surviving))
	}
	recovered := surviving[0]
	if recovered.Entry.IdempotencyKey != entry.IdempotencyKey {
		t.Errorf("Phase3: IdempotencyKey: want %q, got %q",
			entry.IdempotencyKey, recovered.Entry.IdempotencyKey)
	}
	if recovered.Entry.BeadID != entry.BeadID {
		t.Errorf("Phase3: BeadID: want %q, got %q", entry.BeadID, recovered.Entry.BeadID)
	}

	mockBrPath := testhelpers.B87254NewMockBrDir(t)
	testhelpers.B87254WriteMockBr(t, mockBrPath, testhelpers.B87254MockBrSpec{
		Stdout:   `{"status":"ok"}`,
		ExitCode: 0,
	})

	//nolint:gosec // G204: mockBrPath is within a test temp dir, not user input
	cmd := exec.CommandContext(t.Context(), mockBrPath,
		"status", string(recovered.Entry.BeadID),
		"--idempotency-key", recovered.Entry.IdempotencyKey,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Phase4: mock br call failed: %v", err)
	}
	if !strings.Contains(string(out), "ok") {
		t.Errorf("Phase4: mock br stdout unexpected: %q", string(out))
	}

	testhelpers.B87254DeleteIntentEntry(t, recovered.FilePath)

	finalEntries := testhelpers.B87254ReadIntentEntries(t, intentDir)
	if len(finalEntries) != 0 {
		t.Errorf("Phase6: expected 0 entries after recovery, got %d", len(finalEntries))
		for _, e := range finalEntries {
			t.Logf("  surviving: %q", e.FilePath)
		}
	}
}

// TestB87254_WriteIntentEntry_ColonEncodingInKey verifies that idempotency keys
// containing colons (the BI-029 format "<run_id>:<transition_id>:<op>") are
// written as valid filenames via colon-to-underscore encoding per OQ-BI-003, and
// that the original key is preserved in the JSON payload for round-trip fidelity.
//
// Spec ref: specs/beads-integration.md §6.2 — "Colons are permitted in filenames
// on supported filesystems; on filesystems that forbid colons, the adapter is
// allowed to encode them as `_` (see OQ-BI-003)."
func TestB87254_WriteIntentEntry_ColonEncodingInKey(t *testing.T) {
	t.Parallel()

	env := testhelpers.NewEnv(t)
	intentDir := testhelpers.B87254IntentDirFor(t, env.Harmonik)

	runID := core.RunID(uuid.Must(uuid.NewV7()))
	transitionID := core.TransitionID(uuid.Must(uuid.NewV7()))
	entry := testhelpers.B87254NewIntentEntry(
		runID, transitionID,
		core.TerminalOpClaim,
		core.BeadID("hk-colon.1"),
		core.CoarseStatusInProgress,
	)

	if !strings.Contains(entry.IdempotencyKey, ":") {
		t.Fatalf("expected idempotency key to contain colons, got %q", entry.IdempotencyKey)
	}

	written := testhelpers.B87254WriteIntentEntry(t, intentDir, entry)

	base := filepath.Base(written.FilePath)
	if strings.Contains(base, ":") {
		t.Errorf("intent filename contains raw colon (encoding not applied): %q", base)
	}

	recovered := testhelpers.B87254ReadIntentEntries(t, intentDir)
	if len(recovered) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(recovered))
	}
	if recovered[0].Entry.IdempotencyKey != entry.IdempotencyKey {
		t.Errorf("IdempotencyKey round-trip: want %q, got %q",
			entry.IdempotencyKey, recovered[0].Entry.IdempotencyKey)
	}
}

// TestB87254_ReadIntentEntries_SkipsTmpFiles verifies that temp files (left from
// a crash during the write step, before rename) are excluded from the surviving
// entry scan.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 — "rename(2) to
// <idempotency_key>.json. The rename is atomic at the filesystem layer." A
// .tmp-* file means the rename never happened; the write did not land.
func TestB87254_ReadIntentEntries_SkipsTmpFiles(t *testing.T) {
	t.Parallel()

	env := testhelpers.NewEnv(t)
	intentDir := testhelpers.B87254IntentDirFor(t, env.Harmonik)

	runID := core.RunID(uuid.Must(uuid.NewV7()))
	transitionID := core.TransitionID(uuid.Must(uuid.NewV7()))
	entry := testhelpers.B87254NewIntentEntry(
		runID, transitionID,
		core.TerminalOpReopen,
		core.BeadID("hk-tmp.1"),
		core.CoarseStatusOpen,
	)
	_ = testhelpers.B87254WriteIntentEntry(t, intentDir, entry)

	tmpPath := filepath.Join(intentDir, "stale_key.json.tmp-aabbccdd")
	if err := os.WriteFile(tmpPath, []byte(`{"stale":"yes"}`), 0o600); err != nil {
		t.Fatalf("WriteFile stale tmp: %v", err)
	}

	recovered := testhelpers.B87254ReadIntentEntries(t, intentDir)
	if len(recovered) != 1 {
		t.Errorf("expected exactly 1 entry (real), got %d; tmp file may have leaked in",
			len(recovered))
	}
	for _, r := range recovered {
		if strings.Contains(r.FilePath, ".tmp-") {
			t.Errorf("tmp file leaked into recovered entries: %q", r.FilePath)
		}
	}
}

// TestB87254_MockBr_ExitCode verifies that B87254WriteMockBr produces a binary
// that exits with the configured code. This exercises the mock factory used by
// all dependent crash-recovery tests.
func TestB87254_MockBr_ExitCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		exitCode int
	}{
		{"ok", 0},
		{"not-found", 1},
		{"conflict", 2},
		{"schema-mismatch", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mockBrPath := testhelpers.B87254NewMockBrDir(t)
			testhelpers.B87254WriteMockBr(t, mockBrPath, testhelpers.B87254MockBrSpec{
				Stdout:   "out",
				Stderr:   "err",
				ExitCode: tc.exitCode,
			})

			//nolint:gosec // G204: mockBrPath is within a test temp dir, not user input
			cmd := exec.CommandContext(t.Context(), mockBrPath)
			err := cmd.Run()
			if tc.exitCode == 0 {
				if err != nil {
					t.Fatalf("exit 0 case: expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("exit %d case: expected error, got nil", tc.exitCode)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
			}
			if exitErr.ExitCode() != tc.exitCode {
				t.Errorf("ExitCode: want %d, got %d", tc.exitCode, exitErr.ExitCode())
			}
		})
	}
}

// TestB87254_MockBr_SleepMs verifies that B87254WriteMockBr with SleepMs > 0
// sleeps before exiting, enabling timeout and kill tests in dependent beads.
func TestB87254_MockBr_SleepMs(t *testing.T) {
	t.Parallel()

	const sleepMs = 200

	mockBrPath := testhelpers.B87254NewMockBrDir(t)
	testhelpers.B87254WriteMockBr(t, mockBrPath, testhelpers.B87254MockBrSpec{
		SleepMs:  sleepMs,
		ExitCode: 0,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// Run the mock once and discard the result, so that the timed run below
	// does not pay the first-exec cost. macOS validates the FIRST execution of
	// a newly written executable through a machine-wide serialized gate of
	// about 6 execs per second. Measured on a 10-CPU arm64 box: a novel exec
	// costs about 390ms on an idle box, and up to 5s while a `go test ./...`
	// sweep first-execs its own 110 freshly linked test binaries. The SECOND
	// exec of the same path costs about 20-35ms and the gate does not touch it.
	//
	// This warm-up is load-bearing twice over. It is what lets the floor below
	// measure the mock instead of the operating system: the novel-exec cost by
	// itself cleared the old 150ms floor, so the test passed even with the
	// sleep deleted from B87254WriteMockBr. It is also what keeps the timed
	// run off the gate, which is where the 5s deadline this test used to carry
	// died during a full-tree sweep.
	//
	//nolint:gosec // G204: mockBrPath is within a test temp dir, not user input
	if err := exec.CommandContext(ctx, mockBrPath).Run(); err != nil {
		t.Fatalf("mock br warm-up run: %v", err)
	}

	start := time.Now()
	//nolint:gosec // G204: mockBrPath is within a test temp dir, not user input
	cmd := exec.CommandContext(ctx, mockBrPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("mock br timed run failed after %v: %v", time.Since(start), err)
	}
	elapsed := time.Since(start)

	const minElapsed = sleepMs * time.Millisecond
	if elapsed < minElapsed {
		t.Errorf("mock br did not sleep for its full SleepMs: want ≥%v, got %v", minElapsed, elapsed)
	}
}

// TestB87254_DeleteIntentEntry_CleansUp verifies that B87254DeleteIntentEntry
// removes the intent file and that B87254ReadIntentEntries no longer sees it.
func TestB87254_DeleteIntentEntry_CleansUp(t *testing.T) {
	t.Parallel()

	env := testhelpers.NewEnv(t)
	intentDir := testhelpers.B87254IntentDirFor(t, env.Harmonik)

	runID := core.RunID(uuid.Must(uuid.NewV7()))
	transitionID := core.TransitionID(uuid.Must(uuid.NewV7()))
	entry := testhelpers.B87254NewIntentEntry(
		runID, transitionID,
		core.TerminalOpClose,
		core.BeadID("hk-del.1"),
		core.CoarseStatusClosed,
	)
	written := testhelpers.B87254WriteIntentEntry(t, intentDir, entry)

	before := testhelpers.B87254ReadIntentEntries(t, intentDir)
	if len(before) != 1 {
		t.Fatalf("before delete: expected 1 entry, got %d", len(before))
	}

	testhelpers.B87254DeleteIntentEntry(t, written.FilePath)

	after := testhelpers.B87254ReadIntentEntries(t, intentDir)
	if len(after) != 0 {
		t.Errorf("after delete: expected 0 entries, got %d", len(after))
	}

	if _, err := os.Stat(written.FilePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still exists after delete: stat error = %v", err)
	}
}

// TestB87254_NewIntentEntry_BuildsValidKey verifies that B87254NewIntentEntry
// produces an entry with Valid() == true and an idempotency key that embeds the
// run ID, transition ID, and op string per BI-029.
//
// Spec ref: specs/beads-integration.md §4.10 BI-029 — key formula
// "<run_id>:<transition_id>:<op>".
func TestB87254_NewIntentEntry_BuildsValidKey(t *testing.T) {
	t.Parallel()

	runID := core.RunID(uuid.Must(uuid.NewV7()))
	transitionID := core.TransitionID(uuid.Must(uuid.NewV7()))
	entry := testhelpers.B87254NewIntentEntry(
		runID, transitionID,
		core.TerminalOpClaim,
		core.BeadID("hk-key.1"),
		core.CoarseStatusInProgress,
	)

	if !entry.Valid() {
		t.Fatalf("B87254NewIntentEntry produced invalid entry: %+v", entry)
	}

	expectedKey := runID.String() + ":" + transitionID.String() + ":claim"
	if entry.IdempotencyKey != expectedKey {
		t.Errorf("IdempotencyKey: want %q, got %q", expectedKey, entry.IdempotencyKey)
	}
}
