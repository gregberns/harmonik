package brcli_test

// resetbead_bi010d_test.go — BI-010d ResetBead adapter op unit tests.
//
// Tests for ResetBead — the BI-010d orphan-sweep write method on Adapter.
// The reset op transitions in_progress → open; it is issued exclusively by
// the daemon startup orphan-sweep (PL-006 extended per hk-iuaed.2) and carries
// an idempotency key of the form:
//
//	<project_hash>:<bead_id>:reset:<daemon_start_ns>
//
// Coverage:
//   - Clean reset: intent file deleted after successful br call (BI-030 step 6).
//   - Idempotency key shape: <project_hash>:<bead_id>:reset:<daemon_start_ns>.
//   - IntendedPostState = open per BI-010a (reset row).
//   - Op = reset in the retained intent file.
//   - Intent file retained on br failure (BI-031 crash-recovery sentinel).
//   - BrArgv: "update <bead_id> --status open" forwarded to br.
//   - Zero-valued RunID / TransitionID in intent entry (BI-010d startup-sweep semantics).
//
// Spec ref: specs/beads-integration.md §4.4 BI-010d; §4.4 BI-010a (reset row);
// §4.10 BI-029, BI-030; §6.1 RECORD IntentLogEntry.
// Bead: hk-iuaed.3.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// imrestFixtureProjectHash returns a well-formed 12-char hex ProjectHash for tests.
func imrestFixtureProjectHash(t *testing.T) core.ProjectHash {
	t.Helper()
	return core.ProjectHash("aabbccddeeff")
}

// imrestFixtureDaemonStartNS returns a stable daemon start nanosecond timestamp.
func imrestFixtureDaemonStartNS(t *testing.T) int64 {
	t.Helper()
	return int64(1_747_000_000_000_000_000)
}

// imrestFixtureBeadID returns a test bead ID for ResetBead tests.
func imrestFixtureBeadID(t *testing.T) core.BeadID {
	t.Helper()
	return core.BeadID("hk-iuaed.3")
}

// imrestFixtureIntentLogDir creates a temp directory for the intent log.
func imrestFixtureIntentLogDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// imrestFixtureEchoAdapter returns an Adapter backed by a mock br that echoes
// its argv to stdout and exits 0.
func imrestFixtureEchoAdapter(t *testing.T) *brcli.Adapter {
	t.Helper()
	path := brcliFixtureEchoArgsBinary(t)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("imrestFixtureEchoAdapter: New: %v", err)
	}
	return adapter
}

// imrestFixtureFailAdapter returns an Adapter backed by a mock br that exits 1
// (BrNotFound), causing the intent file to be retained for BI-031 recovery.
func imrestFixtureFailAdapter(t *testing.T) *brcli.Adapter {
	t.Helper()
	path := brcliFixtureMockBinary(t, "", "mock error", 1)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("imrestFixtureFailAdapter: New: %v", err)
	}
	return adapter
}

// imrestFixtureCountIntentFiles counts the number of *.json files (not *.json.tmp-*)
// in intentLogDir.
func imrestFixtureCountIntentFiles(t *testing.T, intentLogDir string) int {
	t.Helper()
	entries, err := os.ReadDir(intentLogDir)
	if err != nil {
		t.Fatalf("imrestFixtureCountIntentFiles: ReadDir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && !strings.Contains(e.Name(), ".json.tmp-") {
			count++
		}
	}
	return count
}

// imrestFixtureReadIntentEntry reads and decodes the first *.json intent file in
// intentLogDir. It fails the test if no file is found or the file is malformed.
func imrestFixtureReadIntentEntry(t *testing.T, intentLogDir string) core.IntentLogEntry {
	t.Helper()
	entries, err := os.ReadDir(intentLogDir)
	if err != nil {
		t.Fatalf("imrestFixtureReadIntentEntry: ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && !strings.Contains(e.Name(), ".json.tmp-") {
			//nolint:gosec // G304: path is constructed from t.TempDir() — test-controlled
			data, readErr := os.ReadFile(filepath.Join(intentLogDir, e.Name()))
			if readErr != nil {
				t.Fatalf("imrestFixtureReadIntentEntry: ReadFile: %v", readErr)
			}
			var entry core.IntentLogEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				t.Fatalf("imrestFixtureReadIntentEntry: Unmarshal: %v", err)
			}
			return entry
		}
	}
	t.Fatal("imrestFixtureReadIntentEntry: no *.json intent file found in " + intentLogDir)
	return core.IntentLogEntry{}
}

// imrestFixtureAppendArgsAdapter returns an Adapter backed by a mock br that
// appends each invocation's argv (space-separated) as a new line to argsFile
// and exits 0. Used to spy on the argv forwarded by ResetBead.
func imrestFixtureAppendArgsAdapter(t *testing.T, argsFile string) *brcli.Adapter {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexit 0\n", argsFile)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("imrestFixtureAppendArgsAdapter: write mock: %v", err)
	}
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("imrestFixtureAppendArgsAdapter: New: %v", err)
	}
	return adapter
}

// imrestFixtureReadArgsLines reads the spy file and returns each non-empty line.
func imrestFixtureReadArgsLines(t *testing.T, argsFile string) []string {
	t.Helper()
	//nolint:gosec // G304: argsFile constructed from t.TempDir() — test-controlled path
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("imrestFixtureReadArgsLines: ReadFile %s: %v", argsFile, err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// ─────────────────────────────────────────────────────────────────────────────
// ResetBead — clean path
// ─────────────────────────────────────────────────────────────────────────────

// TestBI010d_ResetBead_IntentFileDeletedOnSuccess verifies that the intent file
// is deleted after a successful reset write (BI-030 step 6).
//
// Spec ref: beads-integration.md §4.10 BI-030 step 6; §4.4 BI-010d.
func TestBI010d_ResetBead_IntentFileDeletedOnSuccess(t *testing.T) {
	t.Parallel()

	adapter := imrestFixtureEchoAdapter(t)
	intentLogDir := imrestFixtureIntentLogDir(t)
	beadID := imrestFixtureBeadID(t)
	projectHash := imrestFixtureProjectHash(t)
	daemonStartNS := imrestFixtureDaemonStartNS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.ResetBead(ctx, intentLogDir, brcli.TimeoutConfig{}, beadID, projectHash, daemonStartNS)
	if err != nil {
		t.Fatalf("ResetBead: unexpected error: %v", err)
	}

	count := imrestFixtureCountIntentFiles(t, intentLogDir)
	if count != 0 {
		t.Errorf("BI-030 step 6: expected 0 intent files after successful reset, got %d", count)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResetBead — intent file retained on failure
// ─────────────────────────────────────────────────────────────────────────────

// TestBI010d_ResetBead_IntentFileRetainedOnFailure verifies that the intent file
// is retained when br fails (BI-031 crash-recovery sentinel).
//
// Spec ref: beads-integration.md §4.10 BI-030; BI-031.
func TestBI010d_ResetBead_IntentFileRetainedOnFailure(t *testing.T) {
	t.Parallel()

	adapter := imrestFixtureFailAdapter(t)
	intentLogDir := imrestFixtureIntentLogDir(t)
	beadID := imrestFixtureBeadID(t)
	projectHash := imrestFixtureProjectHash(t)
	daemonStartNS := imrestFixtureDaemonStartNS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.ResetBead(ctx, intentLogDir, brcli.TimeoutConfig{}, beadID, projectHash, daemonStartNS)
	if err == nil {
		t.Fatal("BI-010d ResetBead: expected error on br failure, got nil")
	}

	count := imrestFixtureCountIntentFiles(t, intentLogDir)
	if count != 1 {
		t.Errorf("BI-031: expected 1 intent file retained on failure, got %d", count)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResetBead — intent log entry shape
// ─────────────────────────────────────────────────────────────────────────────

// TestBI010d_ResetBead_IntendedPostState_Open verifies that the intent file
// records IntendedPostState=open per BI-010a (reset row).
//
// Spec ref: beads-integration.md §4.4 BI-010a (in_progress → open on reset).
func TestBI010d_ResetBead_IntendedPostState_Open(t *testing.T) {
	t.Parallel()

	adapter := imrestFixtureFailAdapter(t) // retain intent file
	intentLogDir := imrestFixtureIntentLogDir(t)
	beadID := imrestFixtureBeadID(t)
	projectHash := imrestFixtureProjectHash(t)
	daemonStartNS := imrestFixtureDaemonStartNS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ResetBead(ctx, intentLogDir, brcli.TimeoutConfig{}, beadID, projectHash, daemonStartNS)

	entry := imrestFixtureReadIntentEntry(t, intentLogDir)
	if entry.IntendedPostState != core.CoarseStatusOpen {
		t.Errorf("BI-010a reset row: IntendedPostState = %q, want %q", entry.IntendedPostState, core.CoarseStatusOpen)
	}
}

// TestBI010d_ResetBead_OpIsReset verifies that the intent file records Op=reset.
//
// Spec ref: beads-integration.md §6.1 ENUM TerminalOp; §4.4 BI-010d.
func TestBI010d_ResetBead_OpIsReset(t *testing.T) {
	t.Parallel()

	adapter := imrestFixtureFailAdapter(t) // retain intent file
	intentLogDir := imrestFixtureIntentLogDir(t)
	beadID := imrestFixtureBeadID(t)
	projectHash := imrestFixtureProjectHash(t)
	daemonStartNS := imrestFixtureDaemonStartNS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ResetBead(ctx, intentLogDir, brcli.TimeoutConfig{}, beadID, projectHash, daemonStartNS)

	entry := imrestFixtureReadIntentEntry(t, intentLogDir)
	if entry.Op != core.TerminalOpReset {
		t.Errorf("BI-010d: Op = %q, want %q", entry.Op, core.TerminalOpReset)
	}
}

// TestBI010d_ResetBead_IdempotencyKeyShape verifies that the intent-log entry
// carries an idempotency key of the form
// "<project_hash>:<bead_id>:reset:<daemon_start_ns>" per BI-010d / BI-029.
//
// Spec ref: beads-integration.md §4.4 BI-010d (NOTE after BI-010a table);
// §4.10 BI-029.
func TestBI010d_ResetBead_IdempotencyKeyShape(t *testing.T) {
	t.Parallel()

	adapter := imrestFixtureFailAdapter(t) // retain intent file
	intentLogDir := imrestFixtureIntentLogDir(t)
	beadID := imrestFixtureBeadID(t)
	projectHash := imrestFixtureProjectHash(t)
	daemonStartNS := imrestFixtureDaemonStartNS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ResetBead(ctx, intentLogDir, brcli.TimeoutConfig{}, beadID, projectHash, daemonStartNS)

	entry := imrestFixtureReadIntentEntry(t, intentLogDir)
	wantKey := core.ResetBeadIdempotencyKey(projectHash, beadID, daemonStartNS)
	if entry.IdempotencyKey != wantKey {
		t.Errorf("BI-029/BI-010d: IdempotencyKey = %q, want %q", entry.IdempotencyKey, wantKey)
	}
}

// TestBI010d_ResetBead_ZeroRunIDAndTransitionID verifies that the intent-log
// entry written by ResetBead carries zero-valued RunID and TransitionID fields,
// as required by BI-010d (a startup-sweep reset has no associated in-flight run
// or transition).
//
// Spec ref: beads-integration.md §4.4 BI-010d; §6.1 RECORD IntentLogEntry.
func TestBI010d_ResetBead_ZeroRunIDAndTransitionID(t *testing.T) {
	t.Parallel()

	adapter := imrestFixtureFailAdapter(t) // retain intent file
	intentLogDir := imrestFixtureIntentLogDir(t)
	beadID := imrestFixtureBeadID(t)
	projectHash := imrestFixtureProjectHash(t)
	daemonStartNS := imrestFixtureDaemonStartNS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ResetBead(ctx, intentLogDir, brcli.TimeoutConfig{}, beadID, projectHash, daemonStartNS)

	entry := imrestFixtureReadIntentEntry(t, intentLogDir)
	if uuid.UUID(entry.RunID) != uuid.Nil {
		t.Errorf("BI-010d: RunID should be zero-valued for reset; got %s", entry.RunID)
	}
	if uuid.UUID(entry.TransitionID) != uuid.Nil {
		t.Errorf("BI-010d: TransitionID should be zero-valued for reset; got %s", entry.TransitionID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResetBead — br argv
// ─────────────────────────────────────────────────────────────────────────────

// TestBI010d_ResetBead_BrArgvIsUpdateStatusOpen verifies that ResetBead forwards
// "update <bead_id> --status open" to br — the same argv shape as ReopenBead —
// which is the only br command that reliably transitions in_progress → open.
//
// Spec ref: beads-integration.md §4.4 BI-010d; hk-wdeen (ReopenBead argv rationale).
func TestBI010d_ResetBead_BrArgvIsUpdateStatusOpen(t *testing.T) {
	t.Parallel()

	argsFile := filepath.Join(t.TempDir(), "spy-args.txt")
	adapter := imrestFixtureAppendArgsAdapter(t, argsFile)
	intentLogDir := imrestFixtureIntentLogDir(t)
	beadID := imrestFixtureBeadID(t)
	projectHash := imrestFixtureProjectHash(t)
	daemonStartNS := imrestFixtureDaemonStartNS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.ResetBead(ctx, intentLogDir, brcli.TimeoutConfig{}, beadID, projectHash, daemonStartNS)
	if err != nil {
		t.Fatalf("ResetBead: unexpected error: %v", err)
	}

	lines := imrestFixtureReadArgsLines(t, argsFile)
	if len(lines) != 1 {
		t.Fatalf("BI-010d: expected exactly 1 br invocation, got %d: %v", len(lines), lines)
	}

	wantArgv := "update " + string(beadID) + " --status open"
	if !strings.Contains(lines[0], wantArgv) {
		t.Errorf("BI-010d: br argv should be %q; got %q", wantArgv, lines[0])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResetBead — schema version
// ─────────────────────────────────────────────────────────────────────────────

// TestBI010d_ResetBead_SchemaVersion1 verifies that the intent-log entry written
// by ResetBead carries SchemaVersion=1 per ON-018 N-1 readability contract.
//
// Spec ref: beads-integration.md §6.1 RECORD IntentLogEntry — SchemaVersion;
// operator-nfr.md §4.5 ON-018.
func TestBI010d_ResetBead_SchemaVersion1(t *testing.T) {
	t.Parallel()

	adapter := imrestFixtureFailAdapter(t) // retain intent file
	intentLogDir := imrestFixtureIntentLogDir(t)
	beadID := imrestFixtureBeadID(t)
	projectHash := imrestFixtureProjectHash(t)
	daemonStartNS := imrestFixtureDaemonStartNS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ResetBead(ctx, intentLogDir, brcli.TimeoutConfig{}, beadID, projectHash, daemonStartNS)

	entry := imrestFixtureReadIntentEntry(t, intentLogDir)
	if entry.SchemaVersion != brcli.IntentLogEntrySchemaVersion {
		t.Errorf("BI-010d: SchemaVersion = %d, want %d", entry.SchemaVersion, brcli.IntentLogEntrySchemaVersion)
	}
	if entry.SchemaVersion != 1 {
		t.Errorf("ON-018: SchemaVersion = %d, want 1", entry.SchemaVersion)
	}
}
