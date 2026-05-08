package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestWM028_BeadIDPropagatesIntoSessionMetadata verifies that when a run is
// tied to a bead the sidecar's bead_id field round-trips correctly, and that
// bead_id is absent (or null) when the run has no bead tie.
//
// Spec ref: workspace-model.md §4.7 WM-028 — "When the run is tied to a bead,
// the metadata sidecar's bead_id field MUST carry the same bead_id value the
// checkpoint trailer carries per [execution-model.md §4.4 EM-017] and
// [beads-integration.md §4.6 BI-017, BI-018] — this spec asserts only the
// VALUE correlation. The bead_id field MUST be absent (or explicit null) when
// the run has no bead tie."
func TestWM028_BeadIDPropagatesIntoSessionMetadata(t *testing.T) {
	t.Parallel()

	t.Run("bead-tied-run-roundtrips-bead-id", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)

		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0028"
		sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000002801"
		beadID := "bead-hk-8mwo-70"

		workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
		sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatalf("MkdirAll sessionDir: %v", err)
		}

		sidecarPath := filepath.Join(sessionDir, "harmonik.meta.json")
		content := sessionLogFixtureMakeMetaJSON(t, runID, sessionID, "node-01", "agentic", "wf-01", beadID)

		if err := sessionLogFixtureWriteSidecarAtomic(sidecarPath, content); err != nil {
			t.Fatalf("WM-028: atomic write: %v", err)
		}

		// Parse and assert bead_id round-trips correctly.
		//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
		raw, err := os.ReadFile(sidecarPath)
		if err != nil {
			t.Fatalf("WM-028: ReadFile: %v", err)
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("WM-028: unmarshal: %v", err)
		}

		// Assert bead_id field is present and equals the expected value.
		gotBeadID, ok := parsed["bead_id"]
		if !ok {
			t.Fatalf("WM-028: bead_id field absent from sidecar; want %q", beadID)
		}
		if gotBeadID != beadID {
			t.Errorf("WM-028: bead_id = %q, want %q", gotBeadID, beadID)
		}

		// Assert all other required fields are also present.
		for _, key := range []string{"run_id", "session_id", "node_id", "agent_type", "workflow_id", "launched_at", "schema_version"} {
			if _, ok := parsed[key]; !ok {
				t.Errorf("WM-028: required field %q absent from bead-tied sidecar", key)
			}
		}
	})

	t.Run("non-bead-run-has-no-bead-id", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)

		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0028"
		sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000002802"
		// No bead_id — run has no bead tie.

		workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
		sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatalf("MkdirAll sessionDir: %v", err)
		}

		sidecarPath := filepath.Join(sessionDir, "harmonik.meta.json")
		// Empty beadID → sessionLogFixtureMakeMetaJSON omits the field.
		content := sessionLogFixtureMakeMetaJSON(t, runID, sessionID, "node-01", "agentic", "wf-01", "")

		if err := sessionLogFixtureWriteSidecarAtomic(sidecarPath, content); err != nil {
			t.Fatalf("WM-028: atomic write: %v", err)
		}

		//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
		raw, err := os.ReadFile(sidecarPath)
		if err != nil {
			t.Fatalf("WM-028: ReadFile: %v", err)
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("WM-028: unmarshal: %v", err)
		}

		// Assert bead_id is absent or null when run has no bead tie.
		if beadVal, ok := parsed["bead_id"]; ok && beadVal != nil {
			t.Errorf("WM-028: bead_id present and non-null for non-bead run; got %v", beadVal)
		}
	})

	t.Run("namespaced-node-id-in-sub-workflow", func(t *testing.T) {
		t.Parallel()

		// WM-026: For runs inside expanded sub-workflows, the sidecar's node_id
		// MUST carry the namespaced form <parent_node_id>/<sub_node_id>.
		repo, _ := tempRepo(t)

		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0028"
		sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000002803"
		beadID := "bead-hk-8mwo-70"
		// Namespaced node_id per WM-026 / EM-034a.
		namespacedNodeID := "parent-node-01/sub-node-03"

		workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
		sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatalf("MkdirAll sessionDir: %v", err)
		}

		sidecarPath := filepath.Join(sessionDir, "harmonik.meta.json")
		content := sessionLogFixtureMakeMetaJSON(t, runID, sessionID, namespacedNodeID, "agentic", "wf-01", beadID)

		if err := sessionLogFixtureWriteSidecarAtomic(sidecarPath, content); err != nil {
			t.Fatalf("WM-028: atomic write: %v", err)
		}

		//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
		raw, err := os.ReadFile(sidecarPath)
		if err != nil {
			t.Fatalf("WM-028: ReadFile: %v", err)
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("WM-028: unmarshal: %v", err)
		}

		// Assert node_id carries namespaced form.
		gotNodeID, ok := parsed["node_id"]
		if !ok {
			t.Fatalf("WM-028: node_id absent from sidecar")
		}
		if gotNodeID != namespacedNodeID {
			t.Errorf("WM-028: namespaced node_id = %q, want %q", gotNodeID, namespacedNodeID)
		}

		// Assert bead_id also round-trips in the sub-workflow case.
		gotBeadID, ok := parsed["bead_id"]
		if !ok {
			t.Fatalf("WM-028: bead_id absent for bead-tied sub-workflow run")
		}
		if gotBeadID != beadID {
			t.Errorf("WM-028: bead_id = %q, want %q", gotBeadID, beadID)
		}
	})
}
