package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// Tests for FindImplementerHandlerRef and SetImplementerHandlerRefAtMergePending
// per workspace-model.md §4.6.WM-022 and WM-022a.
//
// Helper prefix: implRefFixture (bead hk-8mwo.33; avoids collision with sibling
// helpers such as conflictResFixture, leaseFixture, mergeBackFixture, etc.).
//
// Tags: cognition (WM-022 sidecar-walk identification), mechanism (WM-022a null fallback).

// implRefFixtureWriteSidecar writes a minimal harmonik.meta.json at
// ${workspacePath}/.harmonik/sessions/${sessionID}/harmonik.meta.json with the
// given agentType and launchedAt per WM-026.
func implRefFixtureWriteSidecar(t *testing.T, workspacePath, sessionID string, agentType core.AgentType, launchedAt time.Time) {
	t.Helper()
	dir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("implRefFixtureWriteSidecar MkdirAll %q: %v", dir, err)
	}
	m := map[string]interface{}{
		"run_id":         "0196b200-0000-7000-8000-00000033" + sessionID,
		"node_id":        "node-01",
		"agent_type":     string(agentType),
		"workflow_id":    "wf-0196b200-0000-7000-8000-000000000033",
		"launched_at":    launchedAt.UTC().Format(time.RFC3339),
		"schema_version": 1,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("implRefFixtureWriteSidecar Marshal: %v", err)
	}
	sidecarPath := filepath.Join(dir, "harmonik.meta.json")
	if err := os.WriteFile(sidecarPath, data, 0o644); err != nil {
		t.Fatalf("implRefFixtureWriteSidecar WriteFile %q: %v", sidecarPath, err)
	}
}

// TestWM022_FindImplementerHandlerRef_OneAgenticSidecar verifies that
// FindImplementerHandlerRef returns the agentic sidecar's agent_type as a HandlerRef
// when exactly one agentic sidecar is present.
//
// Spec ref: workspace-model.md §4.6.WM-022 — "the FIRST sidecar whose agent_type
// belongs to the set of agentic handler classes … supplies implementer_handler_ref."
func TestWM022_FindImplementerHandlerRef_OneAgenticSidecar(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	t0 := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)

	implRefFixtureWriteSidecar(t, dir, "sess-01", "claude-code", t0)

	ref, err := FindImplementerHandlerRef(dir)
	if err != nil {
		t.Fatalf("WM-022: FindImplementerHandlerRef error: %v", err)
	}
	if ref == nil {
		t.Fatal("WM-022: FindImplementerHandlerRef returned nil; want non-nil HandlerRef for agentic session")
	}
	want := core.HandlerRef("claude-code")
	if *ref != want {
		t.Errorf("WM-022: implementer_handler_ref = %q, want %q", *ref, want)
	}
}

// TestWM022_FindImplementerHandlerRef_MostRecentAgenticChosen verifies that
// FindImplementerHandlerRef selects the MOST RECENT agentic session when multiple
// sessions are present (both agentic and non-agentic).
//
// Spec ref: workspace-model.md §4.6.WM-022 — "order them by the sidecar's
// launched_at field (RFC 3339; per WM-026) in reverse chronological order … the
// FIRST sidecar whose agent_type … is agentic … supplies implementer_handler_ref."
func TestWM022_FindImplementerHandlerRef_MostRecentAgenticChosen(t *testing.T) {
	t.Parallel()

	t.Run("most-recent-agentic-wins-over-older-agentic", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		t0 := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
		t1 := t0.Add(30 * time.Minute) // later = most recent

		// Session 1: agentic (older), different class.
		implRefFixtureWriteSidecar(t, dir, "sess-01", "pi", t0)

		// Session 2: agentic (most recent), different class.
		implRefFixtureWriteSidecar(t, dir, "sess-02", "claude-code", t1)

		ref, err := FindImplementerHandlerRef(dir)
		if err != nil {
			t.Fatalf("WM-022: FindImplementerHandlerRef error: %v", err)
		}
		if ref == nil {
			t.Fatal("WM-022: FindImplementerHandlerRef returned nil; want non-nil")
		}
		// Most-recent agentic (sess-02 = claude-code) must win over older (sess-01 = pi).
		want := core.HandlerRef("claude-code")
		if *ref != want {
			t.Errorf("WM-022: implementer_handler_ref = %q, want %q (most-recent agentic)", *ref, want)
		}
	})

	t.Run("most-recent-non-agentic-skipped-older-agentic-selected", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		t0 := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
		t1 := t0.Add(1 * time.Hour)

		// Session 1: agentic (older).
		implRefFixtureWriteSidecar(t, dir, "sess-01", "claude-code", t0)

		// Session 2: non-agentic (most recent — merge-node does NOT displace agentic).
		implRefFixtureWriteSidecar(t, dir, "sess-02", "merge-node", t1)

		// Session 3: non-agentic (generator).
		implRefFixtureWriteSidecar(t, dir, "sess-03", "generator", t1.Add(10*time.Minute))

		ref, err := FindImplementerHandlerRef(dir)
		if err != nil {
			t.Fatalf("WM-022: FindImplementerHandlerRef error: %v", err)
		}
		if ref == nil {
			t.Fatal("WM-022: FindImplementerHandlerRef returned nil; want non-nil (older agentic sess-01)")
		}
		want := core.HandlerRef("claude-code")
		if *ref != want {
			t.Errorf("WM-022: implementer_handler_ref = %q, want %q", *ref, want)
		}
	})
}

// TestWM022a_FindImplementerHandlerRef_OnlyNonAgenticSidecars verifies that
// FindImplementerHandlerRef returns nil when all sidecars are non-agentic.
//
// Spec ref: workspace-model.md §4.6.WM-022a — "If the sidecar walk finds no session
// whose agent_type is agentic … implementer_handler_ref MUST be set to null."
func TestWM022a_FindImplementerHandlerRef_OnlyNonAgenticSidecars(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	t0 := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)

	implRefFixtureWriteSidecar(t, dir, "sess-01", "non-agentic", t0)
	implRefFixtureWriteSidecar(t, dir, "sess-02", "generator", t0.Add(10*time.Minute))
	implRefFixtureWriteSidecar(t, dir, "sess-03", "merge-node", t0.Add(20*time.Minute))

	ref, err := FindImplementerHandlerRef(dir)
	if err != nil {
		t.Fatalf("WM-022a: FindImplementerHandlerRef error: %v", err)
	}
	if ref != nil {
		t.Errorf("WM-022a: FindImplementerHandlerRef = %q; want nil (all-mechanical branch)", *ref)
	}
}

// TestWM022a_FindImplementerHandlerRef_ZeroSidecars verifies that
// FindImplementerHandlerRef returns nil when the sessions directory is absent
// (no sessions have been started yet).
//
// Spec ref: workspace-model.md §4.6.WM-022a — null fallback when no agentic
// session is found.
func TestWM022a_FindImplementerHandlerRef_ZeroSidecars(t *testing.T) {
	t.Parallel()

	// t.TempDir() creates an empty directory — no .harmonik/sessions/ subdirectory.
	dir := t.TempDir()

	ref, err := FindImplementerHandlerRef(dir)
	if err != nil {
		t.Fatalf("WM-022a: FindImplementerHandlerRef error: %v", err)
	}
	if ref != nil {
		t.Errorf("WM-022a: FindImplementerHandlerRef = %q; want nil (no sessions)", *ref)
	}
}

// TestWM022_SetImplementerHandlerRefAtMergePending verifies that
// SetImplementerHandlerRefAtMergePending sets ws.ImplementerHandlerRef correctly
// and returns an error for invalid inputs.
//
// Spec ref: workspace-model.md §4.6.WM-022 — "That field MUST be set by the workspace
// manager at merge-pending entry (§7.1) to the handler_ref derived from the most-recent
// agentic session sidecar."
func TestWM022_SetImplementerHandlerRefAtMergePending(t *testing.T) {
	t.Parallel()

	t.Run("sets-agentic-ref-on-workspace", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		t0 := time.Date(2026, 5, 3, 11, 0, 0, 0, time.UTC)
		implRefFixtureWriteSidecar(t, dir, "sess-01", "claude-code", t0)

		ws := &Workspace{
			WorkspaceID:   "ws-0196b200-0000-7000-8000-000000033001",
			Path:          dir,
			State:         core.WorkspaceStateLeased,
			SchemaVersion: 1,
		}

		if err := SetImplementerHandlerRefAtMergePending(ws); err != nil {
			t.Fatalf("SetImplementerHandlerRefAtMergePending error: %v", err)
		}
		if ws.ImplementerHandlerRef == nil {
			t.Fatal("ws.ImplementerHandlerRef is nil; want non-nil HandlerRef")
		}
		want := core.HandlerRef("claude-code")
		if *ws.ImplementerHandlerRef != want {
			t.Errorf("ws.ImplementerHandlerRef = %q, want %q", *ws.ImplementerHandlerRef, want)
		}
	})

	t.Run("sets-nil-for-all-mechanical", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		t0 := time.Date(2026, 5, 3, 11, 0, 0, 0, time.UTC)
		implRefFixtureWriteSidecar(t, dir, "sess-01", "non-agentic", t0)

		ws := &Workspace{
			WorkspaceID:   "ws-0196b200-0000-7000-8000-000000033002",
			Path:          dir,
			State:         core.WorkspaceStateLeased,
			SchemaVersion: 1,
		}

		if err := SetImplementerHandlerRefAtMergePending(ws); err != nil {
			t.Fatalf("SetImplementerHandlerRefAtMergePending error: %v", err)
		}
		if ws.ImplementerHandlerRef != nil {
			t.Errorf("ws.ImplementerHandlerRef = %q; want nil (all-mechanical)", *ws.ImplementerHandlerRef)
		}
	})

	t.Run("error-on-nil-workspace", func(t *testing.T) {
		t.Parallel()

		err := SetImplementerHandlerRefAtMergePending(nil)
		if err == nil {
			t.Error("SetImplementerHandlerRefAtMergePending(nil): want error, got nil")
		}
	})

	t.Run("error-on-empty-path", func(t *testing.T) {
		t.Parallel()

		ws := &Workspace{Path: ""}
		err := SetImplementerHandlerRefAtMergePending(ws)
		if err == nil {
			t.Error("SetImplementerHandlerRefAtMergePending(empty path): want error, got nil")
		}
	})
}

// TestWM022_AgentTypeIsAgentic verifies the agentic/non-agentic classification
// per workspace-model.md §4.6.WM-022.
//
// Non-agentic classes: "non-agentic", "generator", "merge-node".
// All other valid agent_type values are agentic.
func TestWM022_AgentTypeIsAgentic(t *testing.T) {
	t.Parallel()

	nonAgenticCases := []core.AgentType{
		"non-agentic",
		"generator",
		"merge-node",
	}
	for _, at := range nonAgenticCases {
		at := at
		t.Run("non-agentic/"+string(at), func(t *testing.T) {
			t.Parallel()
			if agentTypeIsAgentic(at) {
				t.Errorf("agentTypeIsAgentic(%q) = true; want false", at)
			}
		})
	}

	agenticCases := []core.AgentType{
		"claude-code",
		"pi",
		"claude-twin",
		"pi-twin",
	}
	for _, at := range agenticCases {
		at := at
		t.Run("agentic/"+string(at), func(t *testing.T) {
			t.Parallel()
			if !agentTypeIsAgentic(at) {
				t.Errorf("agentTypeIsAgentic(%q) = false; want true", at)
			}
		})
	}
}
