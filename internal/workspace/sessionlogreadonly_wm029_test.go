package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestWM029_SessionLogDirReadOnlyConsumptionByS08 verifies that the session-log
// directory and its metadata sidecar can be opened and read without any write
// operations — asserting the S08 read-only contract. The test also asserts that
// reads succeed after the sidecar is written (durability shape for S08 indexing).
//
// Spec ref: workspace-model.md §4.7 WM-029 — "The session-log directory
// (including the metadata sidecar and handler-written logs) MUST be treated as
// read-only from the memory-layer subsystem's perspective. S08 indexes contents
// into CASS without mutating any file under ${workspace_path}/.harmonik/sessions/."
//
// Note: this test does not invoke S08 itself (deferred subsystem). It asserts the
// durability shape: the sidecar is present and readable; opening with O_RDONLY
// succeeds; no write path is exercised from the read side.
func TestWM029_SessionLogDirReadOnlyConsumptionByS08(t *testing.T) {
	t.Parallel()

	repo, _ := tempRepo(t)

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0029"
	sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000002901"

	workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
	sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessionDir: %v", err)
	}

	// Write the sidecar (S06 action — write side).
	sidecarPath := filepath.Join(sessionDir, "harmonik.meta.json")
	content := sessionLogFixture_makeMetaJSON(runID, sessionID, "node-01", "agentic", "wf-01", "")
	if err := sessionLogFixture_writeSidecarAtomic(sidecarPath, content); err != nil {
		t.Fatalf("WM-029: sidecar write: %v", err)
	}

	// Also write a session.log to simulate handler output in the session dir.
	sessionLog := filepath.Join(sessionDir, "session.log")
	if err := os.WriteFile(sessionLog, []byte("handler output line 1\n"), 0o644); err != nil {
		t.Fatalf("WM-029: WriteFile session.log: %v", err)
	}

	// S08 read-only access pattern: open sidecar with O_RDONLY and parse.
	f, err := os.OpenFile(sidecarPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("WM-029: O_RDONLY open sidecar failed: %v", err)
	}
	defer f.Close()

	var parsed map[string]interface{}
	if err := json.NewDecoder(f).Decode(&parsed); err != nil {
		t.Fatalf("WM-029: JSON decode via O_RDONLY: %v", err)
	}

	// Assert required fields are readable.
	for _, key := range []string{"run_id", "session_id", "node_id", "agent_type", "launched_at", "schema_version"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("WM-029: required field %q not readable from sidecar", key)
		}
	}

	// S08 read-only access: read session.log without writing.
	logF, err := os.OpenFile(sessionLog, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("WM-029: O_RDONLY open session.log failed: %v", err)
	}
	defer logF.Close()

	// Assert: we can read from the log.
	buf := make([]byte, 256)
	n, _ := logF.Read(buf)
	if n == 0 {
		t.Errorf("WM-029: session.log is empty; expected handler output")
	}

	// Assert: the session directory still contains exactly the files written by S06/S04.
	// S08 read path must not create any new files in the directory.
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		t.Fatalf("WM-029: ReadDir: %v", err)
	}
	fileNames := make(map[string]bool)
	for _, e := range entries {
		fileNames[e.Name()] = true
	}
	if !fileNames["harmonik.meta.json"] {
		t.Errorf("WM-029: harmonik.meta.json absent after S08-style read")
	}
	if !fileNames["session.log"] {
		t.Errorf("WM-029: session.log absent after S08-style read")
	}
	// Assert: no unexpected files were created (no write side-effects from reads).
	for name := range fileNames {
		if name != "harmonik.meta.json" && name != "session.log" {
			t.Errorf("WM-029: unexpected file in session dir after read-only pass: %q", name)
		}
	}
}
