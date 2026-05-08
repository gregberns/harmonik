package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sessionLogFixture_makeMetaJSON builds a minimal harmonik.meta.json payload
// with all required WM-026 fields. beadID may be empty to omit bead_id.
func sessionLogFixture_makeMetaJSON(runID, sessionID, nodeID, agentType, workflowID, beadID string) []byte {
	type meta struct {
		RunID         string  `json:"run_id"`
		SessionID     string  `json:"session_id"`
		NodeID        string  `json:"node_id"`
		AgentType     string  `json:"agent_type"`
		WorkflowID    string  `json:"workflow_id"`
		LaunchedAt    string  `json:"launched_at"`
		SchemaVersion string  `json:"schema_version"`
		BeadID        *string `json:"bead_id,omitempty"`
	}
	m := meta{
		RunID:         runID,
		SessionID:     sessionID,
		NodeID:        nodeID,
		AgentType:     agentType,
		WorkflowID:    workflowID,
		LaunchedAt:    time.Now().UTC().Format(time.RFC3339),
		SchemaVersion: "1",
	}
	if beadID != "" {
		m.BeadID = &beadID
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("sessionLogFixture_makeMetaJSON: %v", err))
	}
	return b
}

// sessionLogFixture_writeSidecarAtomic implements the WM-026 atomic discipline:
//
//	(i)   write JSON to <sidecar-path>.tmp-<pid>
//	(ii)  fsync the temp file
//	(iii) rename(2) temp to canonical path (POSIX atomic)
//	(iv)  fsync the parent directory
//
// Returns nil on success.
func sessionLogFixture_writeSidecarAtomic(sidecarPath string, content []byte) error {
	pid := os.Getpid()
	tmpPath := fmt.Sprintf("%s.tmp-%d", sidecarPath, pid)

	// (i) Write to temp file.
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	// (ii) fsync temp file.
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}

	// (iii) Atomic rename.
	if err := os.Rename(tmpPath, sidecarPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	// (iv) fsync the parent directory.
	parentDir := filepath.Dir(sidecarPath)
	d, err := os.Open(parentDir)
	if err != nil {
		return fmt.Errorf("open parent dir: %w", err)
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return fmt.Errorf("fsync parent dir: %w", err)
	}
	return d.Close()
}

// sessionLogFixture_sweepOrphans removes .tmp-<pid> orphan files from sessionDir.
// Per WM-026: "The startup sweep MUST tolerate orphan .tmp-<pid> files by removing them."
func sessionLogFixture_sweepOrphans(sessionDir string) error {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return fmt.Errorf("readDir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Match the .tmp-<pid> pattern: starts with "harmonik.meta.json.tmp-"
		// or any canonical name + ".tmp-" suffix.
		if strings.Contains(name, ".tmp-") {
			if err := os.Remove(filepath.Join(sessionDir, name)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove orphan %q: %w", name, err)
			}
		}
	}
	return nil
}

// TestWM026_SidecarAtomicWrite verifies the atomic-write discipline for the
// metadata sidecar: temp file + fsync + rename + parent-dir fsync. On the
// happy path only the final file exists; no .tmp-* orphan remains.
//
// Spec ref: workspace-model.md §4.7 WM-026 — "The sidecar MUST be written with
// the same atomic discipline as the lease-lock (§4.3.WM-013a): (i) write the
// JSON content to a sibling temp file (e.g., harmonik.meta.json.tmp-<pid>);
// (ii) fsync the temp file; (iii) rename(2) the temp file to the canonical
// harmonik.meta.json name (POSIX rename is atomic); (iv) fsync the parent
// directory ${workspace_path}/.harmonik/sessions/${session_id}/ to durably
// record the rename."
func TestWM026_SidecarAtomicWrite(t *testing.T) {
	t.Parallel()

	repo, _ := tempRepo(t)

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0026"
	sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000002601"

	workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
	sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessionDir: %v", err)
	}

	sidecarPath := filepath.Join(sessionDir, "harmonik.meta.json")
	content := sessionLogFixture_makeMetaJSON(runID, sessionID, "node-01", "agentic", "wf-01", "")

	if err := sessionLogFixture_writeSidecarAtomic(sidecarPath, content); err != nil {
		t.Fatalf("WM-026: atomic write failed: %v", err)
	}

	// Assert: canonical sidecar file exists.
	if _, err := os.Stat(sidecarPath); err != nil {
		t.Fatalf("WM-026: canonical sidecar missing: %v", err)
	}

	// Assert: no .tmp-* orphan remains on the happy path.
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		t.Fatalf("WM-026: readDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("WM-026: orphan temp file present after successful write: %q", e.Name())
		}
	}

	// Assert: the file content parses and contains required fields.
	raw, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("WM-026: ReadFile: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("WM-026: sidecar is not valid JSON: %v", err)
	}
	for _, key := range []string{"run_id", "session_id", "node_id", "agent_type", "workflow_id", "launched_at", "schema_version"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("WM-026: sidecar missing required field %q", key)
		}
	}
}

// TestWM026_OrphanTmpSweep verifies that the startup sweep removes .tmp-<pid>
// orphan files left by an interrupted sidecar write.
//
// Spec ref: workspace-model.md §4.7 WM-026 — "An interrupted sidecar write
// (process death between steps (i) and (iii)) leaves a .tmp-<pid> file and no
// canonical harmonik.meta.json. The startup sweep MUST tolerate orphan .tmp-<pid>
// files by removing them."
func TestWM026_OrphanTmpSweep(t *testing.T) {
	t.Parallel()

	repo, _ := tempRepo(t)

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0026"
	sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000002602"

	workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
	sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessionDir: %v", err)
	}

	// Simulate a crashed write: pre-write a .tmp-<somepid> orphan with no canonical file.
	orphanPID := 99999
	orphanPath := filepath.Join(sessionDir, fmt.Sprintf("harmonik.meta.json.tmp-%d", orphanPID))
	if err := os.WriteFile(orphanPath, []byte(`{"partial":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile orphan: %v", err)
	}

	// Verify the orphan is present before the sweep.
	if _, err := os.Stat(orphanPath); err != nil {
		t.Fatalf("WM-026: orphan not present before sweep: %v", err)
	}

	// Run the startup sweep.
	if err := sessionLogFixture_sweepOrphans(sessionDir); err != nil {
		t.Fatalf("WM-026: sweepOrphans: %v", err)
	}

	// Assert: the orphan is removed.
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("WM-026: orphan temp file still exists after sweep: %q", orphanPath)
	}

	// Assert: no canonical sidecar was created by the sweep (the sweep only removes).
	canonicalPath := filepath.Join(sessionDir, "harmonik.meta.json")
	if _, err := os.Stat(canonicalPath); !os.IsNotExist(err) {
		t.Errorf("WM-026: sweep unexpectedly created canonical sidecar at %q", canonicalPath)
	}
}
