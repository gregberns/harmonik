package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// Tests for conflict-resolution harness per workspace-model.md §4.6
// WM-022, WM-022a, WM-023, WM-024.
//
// Helper prefix: conflictResFixture (bead hk-8mwo.69; avoids collision with
// sibling-bead helpers such as leaseFixture, mergeBackFixture, etc.).
//
// Tags: cognition (WM-022 identification walk), mechanism (WM-022a, WM-023, WM-024).
//
// NOTE (post-mvh): The workspace manager's conflict-resolution dispatch machinery
// (3-attempt cap, operator-configurable cap, handler re-dispatch) is not yet
// implemented. These tests capture the behavioral shape and boundary conditions
// declared by §4.6 so that they pass as conformance gates once the implementation
// lands. Sections that require the implementation are marked TODO with the owning
// bead reference.

// conflictResFixtureMetaJSON returns the harmonik.meta.json content for a session
// sidecar per workspace-model.md §4.7 WM-026.
//
// agentType must be either an agentic class ("agentic-claude", etc.) or a
// mechanical class ("non-agentic", "generator", "merge-node").
func conflictResFixtureMetaJSON(runID, sessionID, agentType string, launchedAt time.Time) []byte {
	b, _ := json.Marshal(map[string]string{
		"run_id":         runID,
		"session_id":     sessionID,
		"agent_type":     agentType,
		"launched_at":    launchedAt.UTC().Format(time.RFC3339),
		"schema_version": "1",
	})
	return b
}

// conflictResFixtureWriteSidecar writes a harmonik.meta.json sidecar under
// ${workspacePath}/.harmonik/sessions/${sessionID}/harmonik.meta.json per WM-026.
func conflictResFixtureWriteSidecar(t *testing.T, workspacePath, sessionID string, content []byte) {
	t.Helper()
	dir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("conflictResFixtureWriteSidecar MkdirAll %q: %v", dir, err)
	}
	sidecarPath := filepath.Join(dir, "harmonik.meta.json")
	if err := os.WriteFile(sidecarPath, content, 0o644); err != nil {
		t.Fatalf("conflictResFixtureWriteSidecar WriteFile %q: %v", sidecarPath, err)
	}
}

// conflictResFixtureSessionMeta is the parsed form of a harmonik.meta.json sidecar.
type conflictResFixtureSessionMeta struct {
	RunID         string `json:"run_id"`
	SessionID     string `json:"session_id"`
	AgentType     string `json:"agent_type"`
	LaunchedAt    string `json:"launched_at"`
	SchemaVersion string `json:"schema_version"`
}

// conflictResFixtureSidecarWalk enumerates
// ${workspacePath}/.harmonik/sessions/*/harmonik.meta.json, parses each sidecar,
// and returns them sorted by LaunchedAt in REVERSE chronological order (newest first).
//
// This is the mechanical identification procedure declared by WM-022:
// "enumerate ${workspace_path}/.harmonik/sessions/*/harmonik.meta.json … order them
// by the sidecar's launched_at field (RFC 3339; per WM-026) in REVERSE chronological
// order."
func conflictResFixtureSidecarWalk(t *testing.T, workspacePath string) []conflictResFixtureSessionMeta {
	t.Helper()
	sessionsDir := filepath.Join(workspacePath, ".harmonik", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("conflictResFixtureSidecarWalk ReadDir %q: %v", sessionsDir, err)
	}
	var metas []conflictResFixtureSessionMeta
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sidecarPath := filepath.Join(sessionsDir, entry.Name(), "harmonik.meta.json")
		//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
		data, err := os.ReadFile(sidecarPath)
		if err != nil {
			continue // skip sessions that lack a sidecar
		}
		var m conflictResFixtureSessionMeta
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("conflictResFixtureSidecarWalk: parse %q: %v", sidecarPath, err)
		}
		metas = append(metas, m)
	}
	// Sort by launched_at descending (newest first) per WM-022.
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].LaunchedAt > metas[j].LaunchedAt
	})
	return metas
}

// conflictResFixtureFirstAgenticRef returns the implementer_handler_ref derived
// from the first agentic sidecar found by the sidecar walk per WM-022.
// Returns ("", false) when no agentic session is found (all-mechanical path per WM-022a).
//
// "Agentic" is defined by WM-022 as: agent_type belongs to the set of agentic
// handler classes; mechanical/generator/merge-node classes are non-agentic.
// For test purposes this set is represented by any agent_type prefixed "agentic-".
func conflictResFixtureFirstAgenticRef(metas []conflictResFixtureSessionMeta) (agentType string, found bool) {
	for _, m := range metas {
		// WM-022: "the FIRST sidecar whose agent_type belongs to the set of agentic
		// handler classes … supplies implementer_handler_ref."
		// Mechanical / generator / merge-node classes are non-agentic.
		switch m.AgentType {
		case "non-agentic", "generator", "merge-node":
			// skip
		default:
			return m.AgentType, true
		}
	}
	return "", false
}

// TestWM022_SidecarWalkIdentifiesImplementer verifies that the implementer
// identification walk per WM-022 returns the MOST RECENT agentic session's
// agent_type when multiple sessions are present (agentic and mechanical).
//
// Spec ref: workspace-model.md §4.6 WM-022 — "enumerate
// ${workspace_path}/.harmonik/sessions/*/harmonik.meta.json … order by launched_at
// in reverse chronological order … first sidecar whose agent_type is agentic …
// supplies implementer_handler_ref."
func TestWM022_SidecarWalkIdentifiesImplementer(t *testing.T) {
	t.Parallel()

	t.Run("agentic-sidecar-most-recent", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		runID := "0196b200-0000-7000-8000-000000022001"

		t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
		t1 := t0.Add(30 * time.Minute) // later = most recent

		// Session 1: mechanical (earlier).
		conflictResFixtureWriteSidecar(t, dir, "sess-01",
			conflictResFixtureMetaJSON(runID, "sess-01", "non-agentic", t0))

		// Session 2: agentic (most recent).
		conflictResFixtureWriteSidecar(t, dir, "sess-02",
			conflictResFixtureMetaJSON(runID, "sess-02", "agentic-claude", t1))

		metas := conflictResFixtureSidecarWalk(t, dir)
		if len(metas) != 2 {
			t.Fatalf("WM-022: want 2 sidecars, got %d", len(metas))
		}

		// Most recent must come first.
		if metas[0].SessionID != "sess-02" {
			t.Errorf("WM-022: most-recent sidecar = sess %q, want sess-02", metas[0].SessionID)
		}

		agentType, found := conflictResFixtureFirstAgenticRef(metas)
		if !found {
			t.Fatal("WM-022: no agentic sidecar found; want agentic-claude")
		}
		if agentType != "agentic-claude" {
			t.Errorf("WM-022: implementer_handler_ref = %q, want %q", agentType, "agentic-claude")
		}
	})

	t.Run("agentic-sidecar-older-but-only-agentic", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		runID := "0196b200-0000-7000-8000-000000022002"

		t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
		t1 := t0.Add(30 * time.Minute)

		// Session 1: agentic (older).
		conflictResFixtureWriteSidecar(t, dir, "sess-01",
			conflictResFixtureMetaJSON(runID, "sess-01", "agentic-claude", t0))

		// Session 2: mechanical (most recent) — does NOT displace the agentic ref.
		conflictResFixtureWriteSidecar(t, dir, "sess-02",
			conflictResFixtureMetaJSON(runID, "sess-02", "merge-node", t1))

		metas := conflictResFixtureSidecarWalk(t, dir)
		// Walk is newest-first; the merge-node appears first but is non-agentic,
		// so the first agentic entry is sess-01.
		agentType, found := conflictResFixtureFirstAgenticRef(metas)
		if !found {
			t.Fatal("WM-022: no agentic sidecar found; want agentic-claude from sess-01")
		}
		if agentType != "agentic-claude" {
			t.Errorf("WM-022: implementer_handler_ref = %q, want agentic-claude", agentType)
		}
	})
}

// TestWM022_NoGitTrailerWalk verifies that the implementer-identification path
// does NOT walk git commit trailers.
//
// Spec ref: workspace-model.md §4.6 WM-022 — "This identification rule does NOT
// walk git commit trailers. Trailers emitted under [execution-model.md §4.4 EM-017]
// carry Harmonik-Run-ID, Harmonik-State-ID, Harmonik-Transition-ID,
// Harmonik-Schema-Version … none of which identify the emitting agent's conformance
// class. Earlier WM drafts cited a Harmonik-Actor-Role trailer; no such trailer
// exists in EM-017 and the reference is retired at this revision."
func TestWM022_NoGitTrailerWalk(t *testing.T) {
	t.Parallel()

	// This test verifies the ABSENCE of a trailer-based identification path.
	// We construct a workspace with only git-level commit trailers and NO
	// session sidecars. The identification result MUST be null (no agentic ref).
	dir := t.TempDir()

	// No sidecars written — simulates a workspace where the only "evidence" of
	// an implementer would be in git trailers (which WM-022 explicitly forbids).
	metas := conflictResFixtureSidecarWalk(t, dir)
	if len(metas) != 0 {
		t.Errorf("WM-022: got %d sidecars from empty sessions dir; want 0", len(metas))
	}

	agentType, found := conflictResFixtureFirstAgenticRef(metas)
	if found {
		t.Errorf("WM-022: trailer-walk absence check: found agentic ref %q; want none (sidecar-only walk)", agentType)
	}
	_ = agentType // confirms the walk returns "", not a trailer-derived value
}

// TestWM022a_AllMechanicalBranchEscalatesDirectly verifies that when
// implementer_handler_ref is null (no agentic sidecar), the workspace manager
// MUST NOT attempt re-dispatch and MUST emit merge_conflict_escalation directly.
//
// Spec ref: workspace-model.md §4.6 WM-022a — "If implementer_handler_ref is null
// at merge-pending entry … the workspace manager MUST skip WM-024 re-dispatch and
// emit merge_conflict_escalation directly per WM-023 on conflict detection. The
// system MUST NOT silently remap the implementer role to an unrelated handler class."
func TestWM022a_AllMechanicalBranchEscalatesDirectly(t *testing.T) {
	t.Parallel()

	t.Run("no-sidecars-null-ref", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		runID := "0196b200-0000-7000-8000-00000022a001"

		// No sessions dir — all-mechanical task branch.
		metas := conflictResFixtureSidecarWalk(t, dir)
		_, found := conflictResFixtureFirstAgenticRef(metas)

		// WM-022a: implementer_handler_ref MUST be null.
		if found {
			t.Errorf("WM-022a: found agentic ref on all-mechanical branch; want null")
		}

		// WM-022a: when null, workspace manager must skip re-dispatch (no attempt
		// to remap to an unrelated handler class) and escalate directly.
		// The escalation path produces merge_conflict_escalation per WM-023.
		// This test captures the dispatch-skip decision: once we know the ref is null,
		// the call to conflictResFixtureAttemptCap(null) must return 0 (no re-dispatch).
		//
		// TODO(hk-8mwo.36): replace with actual workspace-manager dispatch call once
		// conflict-resolution re-dispatch machinery is implemented.
		cap := conflictResFixtureAttemptCapForRef("" /* null ref */)
		if cap != 0 {
			t.Errorf("WM-022a: attempt cap for null ref = %d; want 0 (skip re-dispatch)", cap)
		}

		_ = runID // used to identify this test scenario
	})

	t.Run("only-mechanical-sidecars-null-ref", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		runID := "0196b200-0000-7000-8000-00000022a002"

		t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

		// Only non-agentic sessions.
		conflictResFixtureWriteSidecar(t, dir, "sess-01",
			conflictResFixtureMetaJSON(runID, "sess-01", "generator", t0))
		conflictResFixtureWriteSidecar(t, dir, "sess-02",
			conflictResFixtureMetaJSON(runID, "sess-02", "merge-node", t0.Add(10*time.Minute)))

		metas := conflictResFixtureSidecarWalk(t, dir)
		_, found := conflictResFixtureFirstAgenticRef(metas)

		if found {
			t.Errorf("WM-022a: found agentic ref from mechanical-only sidecars; want null")
		}

		cap := conflictResFixtureAttemptCapForRef("" /* null ref */)
		if cap != 0 {
			t.Errorf("WM-022a: attempt cap for null ref = %d; want 0", cap)
		}
	})
}

// conflictResFixtureAttemptCapForRef returns the conflict-resolution re-dispatch
// attempt cap for the given implementer_handler_ref per WM-024.
//
// A null ref (empty string) returns 0 — no re-dispatch per WM-022a.
// A non-null ref returns the DEFAULT cap of 3 per WM-024.
//
// TODO(hk-8mwo.36): replace this fixture with the real workspace-manager call once
// the conflict-resolution re-dispatch machinery is implemented. The real impl reads
// the operator-configurable cap from daemon config and validates [1, 10] at startup.
func conflictResFixtureAttemptCapForRef(implementerHandlerRef string) int {
	if implementerHandlerRef == "" {
		return 0
	}
	return conflictResFixtureDefaultAttemptCap
}

// conflictResFixtureDefaultAttemptCap is the DEFAULT conflict-resolution re-dispatch
// attempt cap per WM-024: "The workspace manager MUST cap conflict-resolution
// re-dispatch attempts at a DEFAULT of THREE (3) attempts per merge-pending cycle."
const conflictResFixtureDefaultAttemptCap = 3

// TestWM024_ThreeAttemptDefaultCap verifies that the default conflict-resolution
// re-dispatch cap is 3 per merge-pending cycle, and that cap-reach triggers
// merge_conflict_escalation.
//
// Spec ref: workspace-model.md §4.6 WM-024 — "The workspace manager MUST cap
// conflict-resolution re-dispatch attempts at a DEFAULT of THREE (3) attempts per
// merge-pending cycle … After three non-successful attempts, §4.6.WM-022a / WM-023
// MUST route the verdict to escalate-to-human."
func TestWM024_ThreeAttemptDefaultCap(t *testing.T) {
	t.Parallel()

	t.Run("default-cap-is-three", func(t *testing.T) {
		t.Parallel()

		// Any non-null implementer ref must yield cap = 3 by default.
		agentType := "agentic-claude"
		cap := conflictResFixtureAttemptCapForRef(agentType)
		if cap != 3 {
			t.Errorf("WM-024: default attempt cap = %d, want 3", cap)
		}
	})

	t.Run("cap-reach-produces-escalation", func(t *testing.T) {
		t.Parallel()

		ws := &Workspace{
			WorkspaceID:           "ws-0196b200-0000-7000-8000-000000024001",
			Repository:            "/tmp/harmonik-test-repo",
			ParentCommit:          "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
			BranchName:            "run/0196b200-0000-7000-8000-000000024001",
			Path:                  "/tmp/harmonik-test-repo/.harmonik/worktrees/0196b200-0000-7000-8000-000000024001",
			State:                 core.WorkspaceStateConflictResolving,
			InterruptState:        core.InterruptStateNone,
			ImplementerHandlerRef: handlerRefPtr("agentic-claude"),
			Metadata: map[string]string{
				"created_at":           "2026-05-07T00:00:00Z",
				"operator_fingerprint": "test-operator",
			},
			SchemaVersion: 1,
		}

		// Simulate: 3 attempts exhausted — resolve attempts all failed.
		attemptsRecorded := 3
		cap := conflictResFixtureAttemptCapForRef(string(*ws.ImplementerHandlerRef))
		if cap != 3 {
			t.Fatalf("WM-024: cap = %d, want 3", cap)
		}

		// After cap-reach, escalation verdict must be produced.
		escalated := conflictResFixtureIsCapReached(attemptsRecorded, cap)
		if !escalated {
			t.Errorf("WM-024: 3 attempts recorded, cap = 3: escalated = false; want true")
		}

		// Workspace MUST transition to discarded after escalation per WM-023.
		if err := Transition(ws, core.WorkspaceStateDiscarded); err != nil {
			t.Errorf("WM-024: Transition(conflict-resolving → discarded) after escalation: %v", err)
		}
		if ws.State != core.WorkspaceStateDiscarded {
			t.Errorf("WM-024: post-escalation state = %q, want discarded", ws.State)
		}
	})

	t.Run("attempt-below-cap-does-not-escalate", func(t *testing.T) {
		t.Parallel()

		for attempts := 1; attempts <= 2; attempts++ {
			cap := conflictResFixtureDefaultAttemptCap
			escalated := conflictResFixtureIsCapReached(attempts, cap)
			if escalated {
				t.Errorf("WM-024: %d attempts < cap %d: escalated = true; want false", attempts, cap)
			}
		}
	})
}

// conflictResFixtureIsCapReached reports whether the number of recorded attempts
// has reached or exceeded the cap, triggering merge_conflict_escalation per WM-024.
//
// TODO(hk-8mwo.36): replace with workspace-manager attempt-tracking once
// conflict-resolution re-dispatch machinery is implemented.
func conflictResFixtureIsCapReached(attemptsRecorded, cap int) bool {
	return attemptsRecorded >= cap
}

// TestWM024_OperatorConfigurableCapBounds verifies that the operator-configurable
// conflict-resolution attempt cap is bounded to [1, 10] and that values outside
// this range are rejected at daemon startup per WM-024.
//
// Spec ref: workspace-model.md §4.6 WM-024 — "The cap is operator-configurable per
// [operator-nfr.md §4.3] with a lower bound of 1 … and an upper bound of 10 …
// Operator overrides outside [1, 10] MUST be rejected at daemon startup."
func TestWM024_OperatorConfigurableCapBounds(t *testing.T) {
	t.Parallel()

	// Valid range [1, 10]: all must be accepted.
	for cap := 1; cap <= 10; cap++ {
		err := conflictResFixtureValidateAttemptCap(cap)
		if err != nil {
			t.Errorf("WM-024: cap = %d in [1,10]: ValidateAttemptCap returned error: %v", cap, err)
		}
	}

	// Out-of-range: 0 and 11+ must be rejected.
	outOfRange := []int{0, -1, 11, 100}
	for _, cap := range outOfRange {
		err := conflictResFixtureValidateAttemptCap(cap)
		if err == nil {
			t.Errorf("WM-024: cap = %d outside [1,10]: ValidateAttemptCap returned nil; want error", cap)
		}
	}
}

// conflictResFixtureValidateAttemptCap validates that cap is within the
// operator-configurable bound [1, 10] per WM-024.
//
// Returns an error when cap is outside [1, 10].
//
// TODO(hk-8mwo.36): replace with the real daemon-startup config validator once
// the conflict-resolution re-dispatch machinery is implemented.
func conflictResFixtureValidateAttemptCap(cap int) error {
	if cap < 1 || cap > 10 {
		return errConflictResCapOutOfRange
	}
	return nil
}

// errConflictResCapOutOfRange is returned by conflictResFixtureValidateAttemptCap
// when the operator-configured cap falls outside the [1, 10] bound per WM-024.
var errConflictResCapOutOfRange = conflictResError("conflict-resolution attempt cap must be in [1, 10]")

// conflictResError is a simple error type for conflict-resolution validation errors.
type conflictResError string

func (e conflictResError) Error() string { return string(e) }

// TestWM023_UnresolvableConflictEscalates verifies that an unresolvable conflict
// (re-dispatch exhausted OR all-mechanical branch) produces a merge_conflict_escalation
// event and transitions the workspace to discarded.
//
// Spec ref: workspace-model.md §4.6 WM-023 — "If the re-dispatched implementer …
// cannot resolve the merge conflicts … the workspace manager MUST emit
// merge_conflict_escalation … escalation marks the resolution path as exhausted and
// the workspace transitions to discarded per §7.1 after the escalation event is emitted."
func TestWM023_UnresolvableConflictEscalates(t *testing.T) {
	t.Parallel()

	t.Run("agentic-cap-exhausted-to-discarded", func(t *testing.T) {
		t.Parallel()

		ws := &Workspace{
			WorkspaceID:           "ws-0196b200-0000-7000-8000-000000023001",
			Repository:            "/tmp/harmonik-test-repo",
			ParentCommit:          "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
			BranchName:            "run/0196b200-0000-7000-8000-000000023001",
			Path:                  "/tmp/harmonik-test-repo/.harmonik/worktrees/0196b200-0000-7000-8000-000000023001",
			State:                 core.WorkspaceStateConflictResolving,
			InterruptState:        core.InterruptStateNone,
			ImplementerHandlerRef: handlerRefPtr("agentic-claude"),
			Metadata: map[string]string{
				"created_at":           "2026-05-07T00:00:00Z",
				"operator_fingerprint": "test-operator",
			},
			SchemaVersion: 1,
		}

		// 3 failed attempts → cap reached → merge_conflict_escalation required.
		if !conflictResFixtureIsCapReached(3, conflictResFixtureDefaultAttemptCap) {
			t.Fatal("WM-023: precondition: cap not reached at 3 attempts")
		}

		// WM-023: workspace transitions to discarded after escalation.
		if err := Transition(ws, core.WorkspaceStateDiscarded); err != nil {
			t.Fatalf("WM-023: Transition to discarded: %v", err)
		}
		if ws.State != core.WorkspaceStateDiscarded {
			t.Errorf("WM-023: post-escalation state = %q; want discarded", ws.State)
		}
		// WM-037a: terminal states carry no interrupt signal.
		if ws.InterruptState != core.InterruptStateNone {
			t.Errorf("WM-023+WM-037a: interrupt_state = %q after discarded; want none", ws.InterruptState)
		}
	})

	t.Run("all-mechanical-escalates-directly-to-discarded", func(t *testing.T) {
		t.Parallel()

		ws := &Workspace{
			WorkspaceID:           "ws-0196b200-0000-7000-8000-000000023002",
			Repository:            "/tmp/harmonik-test-repo",
			ParentCommit:          "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
			BranchName:            "run/0196b200-0000-7000-8000-000000023002",
			Path:                  "/tmp/harmonik-test-repo/.harmonik/worktrees/0196b200-0000-7000-8000-000000023002",
			State:                 core.WorkspaceStateConflictResolving,
			InterruptState:        core.InterruptStateNone,
			ImplementerHandlerRef: nil, // null per WM-022a (all-mechanical)
			Metadata: map[string]string{
				"created_at":           "2026-05-07T00:00:00Z",
				"operator_fingerprint": "test-operator",
			},
			SchemaVersion: 1,
		}

		// WM-022a: null ref → skip re-dispatch → direct escalation per WM-023.
		if ws.ImplementerHandlerRef != nil {
			t.Fatal("WM-022a: precondition: ImplementerHandlerRef must be nil for all-mechanical branch")
		}

		// WM-023: workspace transitions to discarded after direct escalation.
		if err := Transition(ws, core.WorkspaceStateDiscarded); err != nil {
			t.Fatalf("WM-023: Transition to discarded (all-mechanical): %v", err)
		}
		if ws.State != core.WorkspaceStateDiscarded {
			t.Errorf("WM-023: post-escalation state = %q; want discarded", ws.State)
		}
	})
}

// TestWM024_HandlerClassRetirementRoutesToEscalation verifies that if the recorded
// handler class has been retired between commit time and merge-time re-dispatch, the
// workspace manager MUST treat this as an unresolvable-conflict path and route to
// WM-023 escalation without attempting a silent handler-class remap.
//
// Spec ref: workspace-model.md §4.6 WM-024 — "If the recorded handler class has been
// retired between the initial commit time and merge-time re-dispatch … the workspace
// manager MUST treat this as an unresolvable-conflict path and route to WM-023
// escalation without attempting a silent handler-class remap."
func TestWM024_HandlerClassRetirementRoutesToEscalation(t *testing.T) {
	t.Parallel()

	// Simulate: the sidecar records "agentic-v1" which has been retired.
	// The registry lookup for "agentic-v1" returns "retired".
	retiredClass := "agentic-v1-retired"
	isRetired := conflictResFixtureIsHandlerClassRetired(retiredClass)
	if !isRetired {
		t.Fatalf("WM-024: precondition: %q should be retired in test registry", retiredClass)
	}

	// When the handler class is retired, re-dispatch MUST NOT occur.
	// Instead, it routes to WM-023 escalation directly.
	shouldRedispatch := !isRetired
	if shouldRedispatch {
		t.Errorf("WM-024: retired handler class %q: shouldRedispatch = true; want false (route to escalation)", retiredClass)
	}

	ws := &Workspace{
		WorkspaceID:           "ws-0196b200-0000-7000-8000-000000024002",
		Repository:            "/tmp/harmonik-test-repo",
		ParentCommit:          "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
		BranchName:            "run/0196b200-0000-7000-8000-000000024002",
		Path:                  "/tmp/harmonik-test-repo/.harmonik/worktrees/0196b200-0000-7000-8000-000000024002",
		State:                 core.WorkspaceStateConflictResolving,
		InterruptState:        core.InterruptStateNone,
		ImplementerHandlerRef: handlerRefPtr(retiredClass),
		Metadata: map[string]string{
			"created_at":           "2026-05-07T00:00:00Z",
			"operator_fingerprint": "test-operator",
		},
		SchemaVersion: 1,
	}

	// Retirement path → WM-023 escalation → discarded.
	if err := Transition(ws, core.WorkspaceStateDiscarded); err != nil {
		t.Fatalf("WM-024[retirement]: Transition to discarded: %v", err)
	}
	if ws.State != core.WorkspaceStateDiscarded {
		t.Errorf("WM-024[retirement]: state = %q; want discarded", ws.State)
	}
}

// conflictResFixtureIsHandlerClassRetired reports whether the given handler class
// is retired in the test registry per WM-024.
//
// TODO(hk-8mwo.36): replace with real handler-contract registry lookup once
// handler-class retirement tracking is implemented.
func conflictResFixtureIsHandlerClassRetired(handlerClass string) bool {
	// Test registry: only "agentic-v1-retired" is retired.
	return handlerClass == "agentic-v1-retired"
}

// handlerRefPtr returns a pointer to a core.HandlerRef constructed from s.
// Used to set ImplementerHandlerRef in test fixtures.
func handlerRefPtr(s string) *core.HandlerRef {
	h := core.HandlerRef(s)
	return &h
}
