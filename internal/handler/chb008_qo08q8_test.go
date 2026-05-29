package handler_test

// chb008_qo08q8_test.go — CHB-008 cross-vector session ID consistency.
//
// Bead: hk-qo08q.8
// Spec: specs/claude-hook-bridge.md §4.3 CHB-008
//
// CHB-008 requires that for agent_type=claude-code the handler-process:
//
//   - Mints a fresh UUIDv7 for phase ∈ {single, implementer-initial, reviewer}
//     (CHB-009: reviewer always fresh, never inherited).
//   - Reuses LaunchSpec.claude_session_id for phase=implementer-resume.
//   - Passes the same UUID via --session-id (or --resume for resume phase).
//   - Sets HARMONIK_CLAUDE_SESSION_ID to the same UUID.
//   - Includes the same UUID in the handler_capabilities payload (PreExecMessages[0]).
//
// Cross-vector consistency is the load-bearing invariant: all three propagation
// vectors MUST carry the identical UUID within one launch. The hook-relay's
// bridge_session_id_mismatch guard (CHB-012) and the daemon's watcher routing
// (CHB-023) both depend on this invariant holding.
//
// This file covers the consistency surface; unit tests for each individual
// function (minting logic, env shape, PreExecMessages order) live in
// claudehandler_chb006_024_test.go.
//
// Helper prefix: chb008Fixture (implementer-protocol.md §Helper-prefix discipline).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// chb008FixtureEnvMap parses a "KEY=VALUE" env slice into a map.
func chb008FixtureEnvMap(t *testing.T, env []string) map[string]string {
	t.Helper()
	m := make(map[string]string, len(env))
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		m[kv[:idx]] = kv[idx+1:]
	}
	return m
}

// chb008FixtureEnvConfig builds a minimal ClaudeEnvConfig for testing.
// claudeSessionID and handlerSessionID are injected by the caller.
func chb008FixtureEnvConfig(claudeSessionID, handlerSessionID string) handler.ClaudeEnvConfig {
	return handler.ClaudeEnvConfig{
		RunID:            "chb008-run-001",
		DaemonSocket:     "/tmp/chb008.sock",
		WorkspacePath:    "/workspace/chb008",
		HandlerSessionID: handlerSessionID,
		ClaudeSessionID:  claudeSessionID,
		WorkflowID:       "chb008-wf-001",
		NodeID:           "chb008-node-001",
	}
}

// chb008FixtureHandlerCapabilitiesSessionID unmarshals msgs[0] as
// handler_capabilities and returns its claude_session_id field.
func chb008FixtureHandlerCapabilitiesSessionID(t *testing.T, msgs [][]byte) string {
	t.Helper()
	if len(msgs) == 0 {
		t.Fatal("chb008FixtureHandlerCapabilitiesSessionID: msgs is empty")
	}
	var hc handlercontract.HandlerCapabilitiesMsg
	if err := json.Unmarshal(msgs[0], &hc); err != nil {
		t.Fatalf("chb008FixtureHandlerCapabilitiesSessionID: unmarshal handler_capabilities: %v", err)
	}
	return hc.ClaudeSessionID
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-008: Cross-vector session ID consistency (fresh-mint phases)
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB008_SinglePhase_AllVectorsCarrySameID verifies that for single (empty)
// phase, the session ID minted by MintClaudeSessionID appears identically in:
//  1. ClaudeEnvVars → HARMONIK_CLAUDE_SESSION_ID
//  2. PreExecMessages → handler_capabilities.claude_session_id
//
// This is the cross-vector consistency invariant required by CHB-008.
func TestCHB008_SinglePhase_AllVectorsCarrySameID(t *testing.T) {
	t.Parallel()

	res, err := handler.MintClaudeSessionID("", nil)
	if err != nil {
		t.Fatalf("MintClaudeSessionID: %v", err)
	}
	claudeSessionID := res.ClaudeSessionID

	// Vector 1: HARMONIK_CLAUDE_SESSION_ID env var.
	env := handler.ClaudeEnvVars(chb008FixtureEnvConfig(claudeSessionID, "handler-sess-chb008-single"))
	envMap := chb008FixtureEnvMap(t, env)
	if envMap["HARMONIK_CLAUDE_SESSION_ID"] != claudeSessionID {
		t.Errorf("CHB-008: HARMONIK_CLAUDE_SESSION_ID = %q; want %q (must match mint result)",
			envMap["HARMONIK_CLAUDE_SESSION_ID"], claudeSessionID)
	}

	// Vector 2: handler_capabilities.claude_session_id.
	msgs, err := handler.PreExecMessages(
		"chb008-run-001", "handler-sess-chb008-single", "chb008-node-001",
		claudeSessionID, "/tmp/chb008-single.jsonl", nil,
	)
	if err != nil {
		t.Fatalf("PreExecMessages: %v", err)
	}
	hcSessionID := chb008FixtureHandlerCapabilitiesSessionID(t, msgs)
	if hcSessionID != claudeSessionID {
		t.Errorf("CHB-008: handler_capabilities.claude_session_id = %q; want %q (must match mint result)",
			hcSessionID, claudeSessionID)
	}
}

// TestCHB008_ImplementerInitial_AllVectorsCarrySameID verifies cross-vector
// consistency for implementer-initial (fresh mint).
func TestCHB008_ImplementerInitial_AllVectorsCarrySameID(t *testing.T) {
	t.Parallel()

	res, err := handler.MintClaudeSessionID("implementer-initial", nil)
	if err != nil {
		t.Fatalf("MintClaudeSessionID: %v", err)
	}
	claudeSessionID := res.ClaudeSessionID

	if res.ResumeMode {
		t.Error("CHB-008: ResumeMode must be false for implementer-initial")
	}

	env := handler.ClaudeEnvVars(chb008FixtureEnvConfig(claudeSessionID, "handler-sess-chb008-impl-init"))
	envMap := chb008FixtureEnvMap(t, env)
	if envMap["HARMONIK_CLAUDE_SESSION_ID"] != claudeSessionID {
		t.Errorf("CHB-008: HARMONIK_CLAUDE_SESSION_ID = %q; want %q", envMap["HARMONIK_CLAUDE_SESSION_ID"], claudeSessionID)
	}

	msgs, err := handler.PreExecMessages(
		"chb008-run-002", "handler-sess-chb008-impl-init", "chb008-node-001",
		claudeSessionID, "/tmp/chb008-impl-init.jsonl", nil,
	)
	if err != nil {
		t.Fatalf("PreExecMessages: %v", err)
	}
	hcSessionID := chb008FixtureHandlerCapabilitiesSessionID(t, msgs)
	if hcSessionID != claudeSessionID {
		t.Errorf("CHB-008: handler_capabilities.claude_session_id = %q; want %q", hcSessionID, claudeSessionID)
	}
}

// TestCHB008_ImplementerResume_AllVectorsCarrySamePriorID verifies that for
// implementer-resume, all three vectors carry the PRIOR session ID (not a fresh one).
func TestCHB008_ImplementerResume_AllVectorsCarrySamePriorID(t *testing.T) {
	t.Parallel()

	priorID := "prior-claude-sess-chb008-001"

	res, err := handler.MintClaudeSessionID("implementer-resume", &priorID)
	if err != nil {
		t.Fatalf("MintClaudeSessionID: %v", err)
	}

	if res.ClaudeSessionID != priorID {
		t.Errorf("CHB-008: resume must reuse prior session ID; got %q, want %q", res.ClaudeSessionID, priorID)
	}
	if !res.ResumeMode {
		t.Error("CHB-008: ResumeMode must be true for implementer-resume")
	}

	// All vectors must carry priorID, not a fresh UUID.
	env := handler.ClaudeEnvVars(chb008FixtureEnvConfig(res.ClaudeSessionID, "handler-sess-chb008-impl-res"))
	envMap := chb008FixtureEnvMap(t, env)
	if envMap["HARMONIK_CLAUDE_SESSION_ID"] != priorID {
		t.Errorf("CHB-008: HARMONIK_CLAUDE_SESSION_ID = %q; want prior ID %q", envMap["HARMONIK_CLAUDE_SESSION_ID"], priorID)
	}

	msgs, err := handler.PreExecMessages(
		"chb008-run-003", "handler-sess-chb008-impl-res", "chb008-node-001",
		res.ClaudeSessionID, "/tmp/chb008-impl-res.jsonl", nil,
	)
	if err != nil {
		t.Fatalf("PreExecMessages: %v", err)
	}
	hcSessionID := chb008FixtureHandlerCapabilitiesSessionID(t, msgs)
	if hcSessionID != priorID {
		t.Errorf("CHB-008: handler_capabilities.claude_session_id = %q; want prior ID %q", hcSessionID, priorID)
	}
}

// TestCHB008_Reviewer_AllVectorsCarrySameFreshID verifies cross-vector
// consistency for reviewer phase (fresh mint, not inherited from prior reviewer).
func TestCHB008_Reviewer_AllVectorsCarrySameFreshID(t *testing.T) {
	t.Parallel()

	// CHB-009: reviewer call site MUST pass nil priorClaudeSessionID.
	res, err := handler.MintClaudeSessionID("reviewer", nil)
	if err != nil {
		t.Fatalf("MintClaudeSessionID: %v", err)
	}

	if res.ResumeMode {
		t.Error("CHB-008/009: ResumeMode must be false for reviewer")
	}
	if res.ClaudeSessionID == "" {
		t.Error("CHB-008/009: reviewer must mint a non-empty session ID")
	}

	env := handler.ClaudeEnvVars(chb008FixtureEnvConfig(res.ClaudeSessionID, "handler-sess-chb008-reviewer"))
	envMap := chb008FixtureEnvMap(t, env)
	if envMap["HARMONIK_CLAUDE_SESSION_ID"] != res.ClaudeSessionID {
		t.Errorf("CHB-008: HARMONIK_CLAUDE_SESSION_ID = %q; want %q", envMap["HARMONIK_CLAUDE_SESSION_ID"], res.ClaudeSessionID)
	}

	msgs, err := handler.PreExecMessages(
		"chb008-run-004", "handler-sess-chb008-reviewer", "chb008-node-001",
		res.ClaudeSessionID, "/tmp/chb008-reviewer.jsonl", nil,
	)
	if err != nil {
		t.Fatalf("PreExecMessages: %v", err)
	}
	hcSessionID := chb008FixtureHandlerCapabilitiesSessionID(t, msgs)
	if hcSessionID != res.ClaudeSessionID {
		t.Errorf("CHB-008: handler_capabilities.claude_session_id = %q; want %q", hcSessionID, res.ClaudeSessionID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-008: --session-id vs --resume flag selection
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB008_NonResumePhases_ResumeModeFalse verifies that MintClaudeSessionID
// returns ResumeMode=false for all non-resume phases, ensuring the caller
// will use --session-id (not --resume) per CHB-008.
func TestCHB008_NonResumePhases_ResumeModeFalse(t *testing.T) {
	t.Parallel()

	phases := []string{"", "implementer-initial", "reviewer"}
	for _, phase := range phases {
		phase := phase
		t.Run("phase="+phase, func(t *testing.T) {
			t.Parallel()
			res, err := handler.MintClaudeSessionID(phase, nil)
			if err != nil {
				t.Fatalf("MintClaudeSessionID(phase=%q): %v", phase, err)
			}
			if res.ResumeMode {
				t.Errorf("CHB-008: phase=%q must yield ResumeMode=false (caller uses --session-id), got true", phase)
			}
			if res.ClaudeSessionID == "" {
				t.Errorf("CHB-008: phase=%q must yield non-empty ClaudeSessionID", phase)
			}
		})
	}
}

// TestCHB008_ImplementerResume_ResumeModeTrue verifies that MintClaudeSessionID
// returns ResumeMode=true for implementer-resume, ensuring the caller will
// use --resume (not --session-id) per CHB-008.
func TestCHB008_ImplementerResume_ResumeModeTrue(t *testing.T) {
	t.Parallel()

	priorID := "prior-sess-chb008-resumemode"
	res, err := handler.MintClaudeSessionID("implementer-resume", &priorID)
	if err != nil {
		t.Fatalf("MintClaudeSessionID: %v", err)
	}
	if !res.ResumeMode {
		t.Error("CHB-008: implementer-resume must yield ResumeMode=true (caller uses --resume)")
	}
	if res.ClaudeSessionID != priorID {
		t.Errorf("CHB-008: implementer-resume ClaudeSessionID = %q; want prior %q", res.ClaudeSessionID, priorID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CHB-008 + CHB-009: distinctness and independence invariants
// ─────────────────────────────────────────────────────────────────────────────

// TestCHB008_SequentialMints_AreDistinct verifies that consecutive calls to
// MintClaudeSessionID for non-resume phases produce distinct UUIDs.
// This guards against implementation regressions that could re-use a global
// counter or timestamp with insufficient entropy.
func TestCHB008_SequentialMints_AreDistinct(t *testing.T) {
	t.Parallel()

	const count = 5
	ids := make([]string, count)
	for i := range ids {
		res, err := handler.MintClaudeSessionID("implementer-initial", nil)
		if err != nil {
			t.Fatalf("MintClaudeSessionID[%d]: %v", i, err)
		}
		ids[i] = res.ClaudeSessionID
	}

	seen := make(map[string]bool, count)
	for i, id := range ids {
		if seen[id] {
			t.Errorf("CHB-008: mint[%d] produced duplicate ID %q; all mints must yield distinct UUIDs", i, id)
		}
		seen[id] = true
	}
}

// TestCHB008_Reviewer_DoesNotInheritPrior_CHB009 verifies that when the reviewer
// phase is given a non-nil priorClaudeSessionID (a call-site defect), MintClaudeSessionID
// returns an error per CHB-009 rather than silently reusing the prior ID.
// The caller MUST always pass nil for reviewer.
func TestCHB008_Reviewer_DoesNotInheritPrior_CHB009(t *testing.T) {
	t.Parallel()

	priorID := "prior-reviewer-sess-chb008-009"
	_, err := handler.MintClaudeSessionID("reviewer", &priorID)
	if err == nil {
		t.Fatal("CHB-008/009: passing non-nil priorClaudeSessionID for phase=reviewer must return error; got nil")
	}
}
