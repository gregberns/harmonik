package daemon_test

// dot_auto_status_c2_test.go — C2 deny-side marker integration tests for
// runAutoStatusInspection (EM-068 C1+C2 OR gate).
//
// # What this file proves
//
//  1. (a) C1 clean + valid FAIL marker with explicit class → FAIL with marker class.
//  2. (b) C1 clean + no marker → pass=true (SUCCESS pass-through unchanged).
//  3. (c) C1 clean + non-FAIL marker (status="SUCCESS") → pass=true (treat-as-absent).
//  4. (d) C1 FAIL (broken Go) + FAIL marker → C1 deterministic wins (D3: C1 authoritative).
//  5. (e) C1 clean + compilation_loop marker → FAIL with "structural" (HC-059 override).
//
// Spec refs: EM-068, HC-068, HC-059. Bead ref: hk-kbne (hk-cq1).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// makeC2Worktree creates a temp dir with a valid go.mod + main.go (C1-passing).
func makeC2Worktree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	c2WriteFile(t, dir, "go.mod", "module example.com/c2test\n\ngo 1.21\n")
	c2WriteFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	return dir
}

// writeAutoStatusMarker writes .harmonik/auto_status.json with the given fields.
func writeAutoStatusMarker(t *testing.T, dir, status, failureClass, notes string) {
	t.Helper()
	hdir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(hdir, 0o700); err != nil {
		t.Fatalf("writeAutoStatusMarker: MkdirAll: %v", err)
	}
	m := map[string]any{
		"schema_version": 1,
		"status":         status,
		"failure_class":  failureClass,
		"notes":          notes,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("writeAutoStatusMarker: json.Marshal: %v", err)
	}
	c2WriteFile(t, dir, filepath.Join(".harmonik", "auto_status.json"), string(data))
}

func c2WriteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("c2WriteFile MkdirAll %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("c2WriteFile %s: %v", path, err)
	}
}

// TestAutoStatusC2_ValidFailMarker_Fail verifies (a): C1 clean + valid FAIL marker
// with explicit class → FAIL outcome with the marker's failure class (EM-068 C2 fires).
func TestAutoStatusC2_ValidFailMarker_Fail(t *testing.T) {
	t.Parallel()

	dir := makeC2Worktree(t)
	writeAutoStatusMarker(t, dir, "FAIL", "structural", "agent detected structural defect")

	outcome, pass := daemon.ExportedRunAutoStatusInspection(context.Background(), dir)
	if pass {
		t.Fatal("want pass=false when valid FAIL marker is present")
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Errorf("outcome.Status = %q; want FAIL", outcome.Status)
	}
	if outcome.FailureClass == nil {
		t.Fatal("outcome.FailureClass = nil; want non-nil")
	}
	if *outcome.FailureClass != core.FailureClassStructural {
		t.Errorf("outcome.FailureClass = %q; want structural", *outcome.FailureClass)
	}
}

// TestAutoStatusC2_NoMarker_Pass verifies (b): C1 clean + no marker → pass=true
// (C1-only gate, existing behaviour preserved, EM-068 D1 optionality).
func TestAutoStatusC2_NoMarker_Pass(t *testing.T) {
	t.Parallel()

	dir := makeC2Worktree(t)
	// No .harmonik/auto_status.json written.

	_, pass := daemon.ExportedRunAutoStatusInspection(context.Background(), dir)
	if !pass {
		t.Error("want pass=true when no marker is present")
	}
}

// TestAutoStatusC2_NonFailMarker_Pass verifies (c): C1 clean + non-FAIL marker
// (status="SUCCESS") → treat-as-absent → pass=true (EM-068 D1 deny-side-only).
func TestAutoStatusC2_NonFailMarker_Pass(t *testing.T) {
	t.Parallel()

	dir := makeC2Worktree(t)
	writeAutoStatusMarker(t, dir, "SUCCESS", "deterministic", "agent claims success")

	_, pass := daemon.ExportedRunAutoStatusInspection(context.Background(), dir)
	if !pass {
		t.Error("want pass=true when marker status is not FAIL (treat-as-absent)")
	}
}

// TestAutoStatusC2_C1FailMarkerPresent_C1Wins verifies (d): C1 FAIL (broken Go) +
// FAIL marker → C1 deterministic class wins (D3: C1 is authoritative, C2 never reached).
func TestAutoStatusC2_C1FailMarkerPresent_C1Wins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Broken Go — C1 will fail.
	c2WriteFile(t, dir, "go.mod", "module example.com/c2test\n\ngo 1.21\n")
	c2WriteFile(t, dir, "broken.go", "package main\n\nfunc main() { not valid go }\n")
	// FAIL marker with a different class than what C1 would emit.
	writeAutoStatusMarker(t, dir, "FAIL", "transient", "agent thinks it is transient")

	outcome, pass := daemon.ExportedRunAutoStatusInspection(context.Background(), dir)
	if pass {
		t.Fatal("want pass=false when C1 fails")
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Errorf("outcome.Status = %q; want FAIL", outcome.Status)
	}
	if outcome.FailureClass == nil {
		t.Fatal("outcome.FailureClass = nil; want non-nil")
	}
	// C1 fires first and is authoritative: class must be deterministic (from go build),
	// not transient (from the marker).
	if *outcome.FailureClass != core.FailureClassDeterministic {
		t.Errorf("outcome.FailureClass = %q; want deterministic (C1 authoritative, D3)", *outcome.FailureClass)
	}
}

// TestAutoStatusC2_CompilationLoopMarker_Structural verifies (e): C1 clean +
// compilation_loop marker → FAIL with structural (HC-059 override applied by reader).
func TestAutoStatusC2_CompilationLoopMarker_Structural(t *testing.T) {
	t.Parallel()

	dir := makeC2Worktree(t)
	writeAutoStatusMarker(t, dir, "FAIL", "compilation_loop", "daemon-class not valid for agents")

	outcome, pass := daemon.ExportedRunAutoStatusInspection(context.Background(), dir)
	if pass {
		t.Fatal("want pass=false when FAIL marker is present")
	}
	if outcome.Status != core.OutcomeStatusFail {
		t.Errorf("outcome.Status = %q; want FAIL", outcome.Status)
	}
	if outcome.FailureClass == nil {
		t.Fatal("outcome.FailureClass = nil; want non-nil")
	}
	// HC-059 override: compilation_loop → structural.
	if *outcome.FailureClass != core.FailureClassStructural {
		t.Errorf("outcome.FailureClass = %q; want structural (HC-059 override)", *outcome.FailureClass)
	}
}
