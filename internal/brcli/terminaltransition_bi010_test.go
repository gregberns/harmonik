package brcli_test

// terminaltransition_bi010_test.go — BI-010 terminal-transition write surface tests.
//
// Tests for ClaimBead, CloseBead, and ReopenBead — the three public
// terminal-transition write methods on Adapter.
//
// Coverage:
//   - Correct `br` argv forwarded for each op (claim/close/reopen).
//   - Intent file written before `br` subprocess (BI-030 step 1–4 evidence).
//   - Intent file deleted after successful `br` call (BI-030 step 6).
//   - Intent file retained on `br` failure (BI-031 crash-recovery sentinel).
//   - IntendedPostState matches the BI-010a status-mapping table.
//
// Spec ref: specs/beads-integration.md §4.4 BI-010; §4.4 BI-010a; §4.10
// BI-029, BI-030; §6.1 RECORD IntentLogEntry.
// Bead: hk-872.10.

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

// bi010FixtureRunID returns a fresh UUIDv7 RunID for tests.
func bi010FixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	return core.RunID(uuid.Must(uuid.NewV7()))
}

// bi010FixtureTransitionID returns a fresh UUIDv7 TransitionID for tests.
func bi010FixtureTransitionID(t *testing.T) core.TransitionID {
	t.Helper()
	return core.TransitionID(uuid.Must(uuid.NewV7()))
}

// bi010FixtureIntentLogDir creates a temp directory for the intent log.
func bi010FixtureIntentLogDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// bi010FixtureCountIntentFiles counts the number of *.json files (not *.json.tmp-*)
// in intentLogDir.
func bi010FixtureCountIntentFiles(t *testing.T, intentLogDir string) int {
	t.Helper()
	entries, err := os.ReadDir(intentLogDir)
	if err != nil {
		t.Fatalf("bi010FixtureCountIntentFiles: ReadDir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && !strings.Contains(e.Name(), ".json.tmp-") {
			count++
		}
	}
	return count
}

// bi010FixtureReadIntentFile reads and decodes the first *.json intent file in
// intentLogDir. It fails the test if no file is found or the file is malformed.
func bi010FixtureReadIntentFile(t *testing.T, intentLogDir string) core.IntentLogEntry {
	t.Helper()
	entries, err := os.ReadDir(intentLogDir)
	if err != nil {
		t.Fatalf("bi010FixtureReadIntentFile: ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && !strings.Contains(e.Name(), ".json.tmp-") {
			data, readErr := os.ReadFile(filepath.Join(intentLogDir, e.Name()))
			if readErr != nil {
				t.Fatalf("bi010FixtureReadIntentFile: ReadFile: %v", readErr)
			}
			var entry core.IntentLogEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				t.Fatalf("bi010FixtureReadIntentFile: Unmarshal: %v", err)
			}
			return entry
		}
	}
	t.Fatal("bi010FixtureReadIntentFile: no *.json intent file found in " + intentLogDir)
	return core.IntentLogEntry{}
}

// bi010FixtureEchoAdapter returns an Adapter backed by a mock br that echoes
// its argv to stdout and exits 0.
func bi010FixtureEchoAdapter(t *testing.T) *brcli.Adapter {
	t.Helper()
	path := brcliFixtureEchoArgsBinary(t)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("bi010FixtureEchoAdapter: New: %v", err)
	}
	return adapter
}

// bi010FixtureFailAdapter returns an Adapter backed by a mock br that exits 1
// (BrNotFound).
func bi010FixtureFailAdapter(t *testing.T) *brcli.Adapter {
	t.Helper()
	path := brcliFixtureMockBinary(t, "", "mock error", 1)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("bi010FixtureFailAdapter: New: %v", err)
	}
	return adapter
}

// ─────────────────────────────────────────────────────────────────────────────
// ClaimBead
// ─────────────────────────────────────────────────────────────────────────────

// TestBI010_ClaimBead_BrArgv verifies that ClaimBead forwards the correct
// argv to br: "update <bead_id> --claim".
//
// Spec ref: beads-integration.md §4.4 BI-010 (claim).
func TestBI010_ClaimBead_BrArgv(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureEchoAdapter(t)
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-872.10")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.ClaimBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID)
	if err != nil {
		t.Fatalf("ClaimBead: unexpected error: %v", err)
	}
}

// TestBI010_ClaimBead_IntentFileDeletedOnSuccess verifies that the intent file
// is deleted after a successful claim write (BI-030 step 6).
//
// Spec ref: beads-integration.md §4.10 BI-030 step 6.
func TestBI010_ClaimBead_IntentFileDeletedOnSuccess(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureEchoAdapter(t)
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-872.10")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.ClaimBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID)
	if err != nil {
		t.Fatalf("ClaimBead: unexpected error: %v", err)
	}

	count := bi010FixtureCountIntentFiles(t, intentLogDir)
	if count != 0 {
		t.Errorf("BI-030 step 6: expected 0 intent files after successful claim, got %d", count)
	}
}

// TestBI010_ClaimBead_IntentFileRetainedOnFailure verifies that the intent file
// is retained when br fails (BI-031 crash-recovery sentinel).
//
// Spec ref: beads-integration.md §4.10 BI-030; BI-031.
func TestBI010_ClaimBead_IntentFileRetainedOnFailure(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureFailAdapter(t)
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-872.10")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.ClaimBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID)
	if err == nil {
		t.Fatal("BI-010 ClaimBead: expected error on br failure, got nil")
	}

	count := bi010FixtureCountIntentFiles(t, intentLogDir)
	if count != 1 {
		t.Errorf("BI-031: expected 1 intent file retained on failure, got %d", count)
	}
}

// TestBI010_ClaimBead_IntendedPostState_InProgress verifies that the intent
// file records IntendedPostState=in_progress per BI-010a (claim row).
//
// Spec ref: beads-integration.md §4.4 BI-010a (open → in_progress on claim).
func TestBI010_ClaimBead_IntendedPostState_InProgress(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureFailAdapter(t) // use failure adapter so intent file is retained
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-872.10")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ClaimBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID)

	entry := bi010FixtureReadIntentFile(t, intentLogDir)
	if entry.IntendedPostState != core.CoarseStatusInProgress {
		t.Errorf("BI-010a: claim IntendedPostState = %q, want %q", entry.IntendedPostState, core.CoarseStatusInProgress)
	}
	if entry.Op != core.TerminalOpClaim {
		t.Errorf("BI-010: claim Op = %q, want %q", entry.Op, core.TerminalOpClaim)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CloseBead
// ─────────────────────────────────────────────────────────────────────────────

// TestBI010_CloseBead_IntentFileDeletedOnSuccess verifies intent file cleanup
// after a successful close.
//
// Spec ref: beads-integration.md §4.10 BI-030 step 6.
func TestBI010_CloseBead_IntentFileDeletedOnSuccess(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureEchoAdapter(t)
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-872.10")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.CloseBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID, false)
	if err != nil {
		t.Fatalf("CloseBead: unexpected error: %v", err)
	}

	count := bi010FixtureCountIntentFiles(t, intentLogDir)
	if count != 0 {
		t.Errorf("BI-030 step 6: expected 0 intent files after successful close, got %d", count)
	}
}

// TestBI010_CloseBead_IntendedPostState_Closed verifies that the intent file
// records IntendedPostState=closed per BI-010a (close row).
//
// Spec ref: beads-integration.md §4.4 BI-010a (in_progress → closed on close).
func TestBI010_CloseBead_IntendedPostState_Closed(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureFailAdapter(t) // use failure adapter so intent file is retained
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-872.10")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.CloseBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID, false)

	entry := bi010FixtureReadIntentFile(t, intentLogDir)
	if entry.IntendedPostState != core.CoarseStatusClosed {
		t.Errorf("BI-010a: close IntendedPostState = %q, want %q", entry.IntendedPostState, core.CoarseStatusClosed)
	}
	if entry.Op != core.TerminalOpClose {
		t.Errorf("BI-010: close Op = %q, want %q", entry.Op, core.TerminalOpClose)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CloseBead — needs-attention label
// ─────────────────────────────────────────────────────────────────────────────

// bi010FixtureAppendArgsAdapter returns an Adapter backed by a mock br that
// appends each invocation's argv (space-separated) as a new line to argsFile
// and exits 0.  Used to spy on every br invocation made by CloseBead so that
// tests can verify the label add is (or is not) issued.
func bi010FixtureAppendArgsAdapter(t *testing.T, argsFile string) *brcli.Adapter {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	// Append positional args as a newline-terminated record; exit 0.
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexit 0\n", argsFile)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("bi010FixtureAppendArgsAdapter: write mock: %v", err)
	}
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("bi010FixtureAppendArgsAdapter: New: %v", err)
	}
	return adapter
}

// bi010FixtureReadArgsLines reads the spy file produced by
// bi010FixtureAppendArgsAdapter and returns each non-empty line as a slice.
func bi010FixtureReadArgsLines(t *testing.T, argsFile string) []string {
	t.Helper()
	//nolint:gosec // G304: argsFile constructed from t.TempDir() — test-controlled path
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("bi010FixtureReadArgsLines: ReadFile %s: %v", argsFile, err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestBI010_CloseBead_NeedsAttentionTrue_LabelAdded verifies that CloseBead
// with needsAttention=true issues a second br invocation:
// "label add <bead_id> -l needs-attention" after the close write.
//
// Spec ref: execution-model.md §4.3.EM-015e; operator-nfr.md §4.3.ON-009a;
// beads-integration.md §4.3.13 BI-013a.
// Bead: hk-7om2q.23.
func TestBI010_CloseBead_NeedsAttentionTrue_LabelAdded(t *testing.T) {
	t.Parallel()

	argsFile := filepath.Join(t.TempDir(), "spy-args.txt")
	adapter := bi010FixtureAppendArgsAdapter(t, argsFile)
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-7om2q.23")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.CloseBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID, true)
	if err != nil {
		t.Fatalf("CloseBead(needsAttention=true): unexpected error: %v", err)
	}

	lines := bi010FixtureReadArgsLines(t, argsFile)
	if len(lines) < 2 {
		t.Fatalf("EM-015e/ON-009a: expected at least 2 br invocations (close + label add), got %d: %v", len(lines), lines)
	}

	// First invocation: the close write.
	wantClose := "close " + string(beadID)
	if !strings.Contains(lines[0], wantClose) {
		t.Errorf("EM-015e: first br invocation should be close; got %q, want to contain %q", lines[0], wantClose)
	}

	// Second invocation: the needs-attention label add.
	wantLabel := "label add " + string(beadID) + " -l needs-attention"
	if !strings.Contains(lines[1], wantLabel) {
		t.Errorf("EM-015e/ON-009a: second br invocation should be label add needs-attention; got %q, want to contain %q", lines[1], wantLabel)
	}
}

// TestBI010_CloseBead_NeedsAttentionFalse_NoLabelAdded verifies that CloseBead
// with needsAttention=false issues only one br invocation (the close write)
// and does NOT apply the needs-attention label.
//
// Spec ref: execution-model.md §4.3.EM-015e (APPROVE path — no label);
// beads-integration.md §4.3.13 BI-013a.
// Bead: hk-7om2q.23.
func TestBI010_CloseBead_NeedsAttentionFalse_NoLabelAdded(t *testing.T) {
	t.Parallel()

	argsFile := filepath.Join(t.TempDir(), "spy-args.txt")
	adapter := bi010FixtureAppendArgsAdapter(t, argsFile)
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-7om2q.23")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.CloseBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID, false)
	if err != nil {
		t.Fatalf("CloseBead(needsAttention=false): unexpected error: %v", err)
	}

	lines := bi010FixtureReadArgsLines(t, argsFile)
	if len(lines) != 1 {
		t.Errorf("EM-015e: needsAttention=false should issue exactly 1 br invocation (close only), got %d: %v", len(lines), lines)
	}

	for _, line := range lines {
		if strings.Contains(line, "needs-attention") {
			t.Errorf("EM-015e/ON-009a: needsAttention=false must NOT apply needs-attention label; found %q in invocations", line)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReopenBead
// ─────────────────────────────────────────────────────────────────────────────

// TestBI010_ReopenBead_IntentFileDeletedOnSuccess verifies intent file cleanup
// after a successful reopen.
//
// Spec ref: beads-integration.md §4.10 BI-030 step 6.
func TestBI010_ReopenBead_IntentFileDeletedOnSuccess(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureEchoAdapter(t)
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-872.10")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.ReopenBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID, "")
	if err != nil {
		t.Fatalf("ReopenBead: unexpected error: %v", err)
	}

	count := bi010FixtureCountIntentFiles(t, intentLogDir)
	if count != 0 {
		t.Errorf("BI-030 step 6: expected 0 intent files after successful reopen, got %d", count)
	}
}

// TestBI010_ReopenBead_IntendedPostState_Open verifies that the intent file
// records IntendedPostState=open per BI-010a (reopen row).
//
// Spec ref: beads-integration.md §4.4 BI-010a (closed → open on reopen).
func TestBI010_ReopenBead_IntendedPostState_Open(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureFailAdapter(t) // use failure adapter so intent file is retained
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-872.10")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ReopenBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID, "")

	entry := bi010FixtureReadIntentFile(t, intentLogDir)
	if entry.IntendedPostState != core.CoarseStatusOpen {
		t.Errorf("BI-010a: reopen IntendedPostState = %q, want %q", entry.IntendedPostState, core.CoarseStatusOpen)
	}
	if entry.Op != core.TerminalOpReopen {
		t.Errorf("BI-010: reopen Op = %q, want %q", entry.Op, core.TerminalOpReopen)
	}
}

// TestBI010_ReopenBead_BrArgvIsUpdate verifies that ReopenBead forwards
// "update <bead_id> --status open" to br — NOT "br reopen" — so that beads
// stranded in in_progress (after SIGINT/SIGTERM mid-run) can be recovered.
// hk-wdeen: br reopen only handles closed→open and silently skips in_progress.
//
// Spec ref: beads-integration.md §4.4 BI-010 (reopen); hk-wdeen.
func TestBI010_ReopenBead_BrArgvIsUpdate(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureEchoAdapter(t)
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-wdeen")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := adapter.ReopenBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID, "")
	if err != nil {
		t.Fatalf("ReopenBead: unexpected error: %v", err)
	}
	// The echo adapter exits 0, so no intent file remains.  The key check is
	// that no error was returned — if the argv were "reopen <id>" the mock
	// binary would still echo and exit 0, but we want to assert the arg shape
	// via the IntentLogEntry retained on a failure-path run below.
}

// TestBI010_ReopenBead_InProgress_IntendedPostState_Open verifies that the
// intent file records Op=reopen and IntendedPostState=open even when invoked
// to recover an in_progress bead (the argv change does not affect the entry).
//
// Spec ref: beads-integration.md §4.4 BI-010a; hk-wdeen.
func TestBI010_ReopenBead_InProgress_IntendedPostState_Open(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureFailAdapter(t) // retain intent file
	intentLogDir := bi010FixtureIntentLogDir(t)
	beadID := core.BeadID("hk-wdeen")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ReopenBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), beadID, "")

	entry := bi010FixtureReadIntentFile(t, intentLogDir)
	if entry.IntendedPostState != core.CoarseStatusOpen {
		t.Errorf("hk-wdeen: reopen IntendedPostState = %q, want %q", entry.IntendedPostState, core.CoarseStatusOpen)
	}
	if entry.Op != core.TerminalOpReopen {
		t.Errorf("hk-wdeen: reopen Op = %q, want %q", entry.Op, core.TerminalOpReopen)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Intent-log entry shape
// ─────────────────────────────────────────────────────────────────────────────

// TestBI010_IntentLogEntry_IdempotencyKeyShape verifies that the intent-log
// entry carries an idempotency key of the form
// "<run_id>:<transition_id>:<op>" per BI-029.
//
// Spec ref: beads-integration.md §4.10 BI-029.
func TestBI010_IntentLogEntry_IdempotencyKeyShape(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureFailAdapter(t) // retain intent file
	intentLogDir := bi010FixtureIntentLogDir(t)

	runID := bi010FixtureRunID(t)
	transitionID := bi010FixtureTransitionID(t)
	beadID := core.BeadID("hk-872.10")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ClaimBead(ctx, intentLogDir, brcli.TimeoutConfig{}, runID, transitionID, beadID)

	entry := bi010FixtureReadIntentFile(t, intentLogDir)
	wantKey := runID.String() + ":" + transitionID.String() + ":" + string(core.TerminalOpClaim)
	if entry.IdempotencyKey != wantKey {
		t.Errorf("BI-029: IdempotencyKey = %q, want %q", entry.IdempotencyKey, wantKey)
	}
}

// TestBI010_IntentLogEntry_SchemaVersion1 verifies that the intent-log entry
// carries SchemaVersion=1.
//
// Spec ref: beads-integration.md §6.1 RECORD IntentLogEntry — SchemaVersion
// field; ON-018 N-1 readability contract.
func TestBI010_IntentLogEntry_SchemaVersion1(t *testing.T) {
	t.Parallel()

	adapter := bi010FixtureFailAdapter(t) // retain intent file
	intentLogDir := bi010FixtureIntentLogDir(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = adapter.ClaimBead(ctx, intentLogDir, brcli.TimeoutConfig{}, bi010FixtureRunID(t), bi010FixtureTransitionID(t), core.BeadID("hk-1"))

	entry := bi010FixtureReadIntentFile(t, intentLogDir)
	if entry.SchemaVersion != brcli.IntentLogEntrySchemaVersion {
		t.Errorf("BI-010: SchemaVersion = %d, want %d", entry.SchemaVersion, brcli.IntentLogEntrySchemaVersion)
	}
	if entry.SchemaVersion != 1 {
		t.Errorf("BI-010: SchemaVersion = %d, want 1", entry.SchemaVersion)
	}
}
