package core

// agentevents_hqwn59_prop_test.go — property tests for the Valid() methods
// declared in agentevents_hqwn59.go that are not already covered by
// agentevents_hqwn59_test.go (AgentRateLimitStatus / AgentRateLimitStatusPayload
// are excluded here to avoid duplicate coverage).
//
// Naming: TestProp_* per testing.md §Decisions #10.
// Approach: rapid generator builds a valid payload, flips exactly one required
// field to its zero/invalid value, asserts Valid()==false; all-valid -> true.
//
// Refs: hk-qgzso (property-test coverage uplift for hk-j3hrn core uplift).

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ============================================================
// AgentStartedPayload
// ============================================================

func TestProp_AgentStartedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentStartedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			StartedAt: drawNonEmptyString(rt, "started_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed AgentStartedPayload")
		}
	})
}

func TestProp_AgentStartedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentStartedPayload{
			RunID:     RunID(uuid.Nil),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			StartedAt: drawNonEmptyString(rt, "started_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_AgentStartedPayload_EmptySessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentStartedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: "",
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			StartedAt: drawNonEmptyString(rt, "started_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty SessionID")
		}
	})
}

func TestProp_AgentStartedPayload_EmptyNodeIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentStartedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    "",
			AgentType: AgentTypeClaudeCode,
			StartedAt: drawNonEmptyString(rt, "started_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty NodeID")
		}
	})
}

func TestProp_AgentStartedPayload_InvalidAgentTypeRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Uppercase letters violate the AgentType regex.
		s := "Invalid-Type-" + rapid.StringN(1, 10, -1).Draw(rt, "suffix")
		p := AgentStartedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentType(s),
			StartedAt: drawNonEmptyString(rt, "started_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for invalid AgentType %q", s)
		}
	})
}

func TestProp_AgentStartedPayload_EmptyStartedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentStartedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			StartedAt: "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty StartedAt")
		}
	})
}

// ============================================================
// AgentReadyPayload
// ============================================================

func TestProp_AgentReadyPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentReadyPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:    SessionID(drawNonEmptyString(rt, "session_id")),
			Capabilities: []string{},
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed AgentReadyPayload")
		}
	})
}

func TestProp_AgentReadyPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentReadyPayload{
			RunID:        RunID(uuid.Nil),
			SessionID:    SessionID(drawNonEmptyString(rt, "session_id")),
			Capabilities: []string{},
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_AgentReadyPayload_EmptySessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentReadyPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:    "",
			Capabilities: []string{},
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty SessionID")
		}
	})
}

func TestProp_AgentReadyPayload_NilCapabilitiesRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentReadyPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:    SessionID(drawNonEmptyString(rt, "session_id")),
			Capabilities: nil,
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil Capabilities")
		}
	})
}

// ============================================================
// LaunchInitiatedPayload
// ============================================================

func TestProp_LaunchInitiatedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := LaunchInitiatedPayload{
			RunID:           RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:       SessionID(drawNonEmptyString(rt, "session_id")),
			ClaudeSessionID: drawNonEmptyString(rt, "claude_session_id"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed LaunchInitiatedPayload")
		}
	})
}

func TestProp_LaunchInitiatedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := LaunchInitiatedPayload{
			RunID:           RunID(uuid.Nil),
			SessionID:       SessionID(drawNonEmptyString(rt, "session_id")),
			ClaudeSessionID: drawNonEmptyString(rt, "claude_session_id"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_LaunchInitiatedPayload_EmptySessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := LaunchInitiatedPayload{
			RunID:           RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:       "",
			ClaudeSessionID: drawNonEmptyString(rt, "claude_session_id"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty SessionID")
		}
	})
}

func TestProp_LaunchInitiatedPayload_EmptyClaudeSessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := LaunchInitiatedPayload{
			RunID:           RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:       SessionID(drawNonEmptyString(rt, "session_id")),
			ClaudeSessionID: "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ClaudeSessionID")
		}
	})
}

// ============================================================
// AgentReadyTimeoutPayload
// ============================================================

func TestProp_AgentReadyTimeoutPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentReadyTimeoutPayload{
			RunID:           RunID(drawNonNilUUID(rt, "run_id")),
			ClaudeSessionID: drawNonEmptyString(rt, "claude_session_id"),
			TimeoutMs:       rapid.Int64Range(1, 300000).Draw(rt, "timeout_ms"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed AgentReadyTimeoutPayload")
		}
	})
}

func TestProp_AgentReadyTimeoutPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentReadyTimeoutPayload{
			RunID:           RunID(uuid.Nil),
			ClaudeSessionID: drawNonEmptyString(rt, "claude_session_id"),
			TimeoutMs:       rapid.Int64Range(1, 300000).Draw(rt, "timeout_ms"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_AgentReadyTimeoutPayload_EmptyClaudeSessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentReadyTimeoutPayload{
			RunID:           RunID(drawNonNilUUID(rt, "run_id")),
			ClaudeSessionID: "",
			TimeoutMs:       rapid.Int64Range(1, 300000).Draw(rt, "timeout_ms"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ClaudeSessionID")
		}
	})
}

func TestProp_AgentReadyTimeoutPayload_ZeroTimeoutMsRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentReadyTimeoutPayload{
			RunID:           RunID(drawNonNilUUID(rt, "run_id")),
			ClaudeSessionID: drawNonEmptyString(rt, "claude_session_id"),
			TimeoutMs:       rapid.Int64Range(-100000, 0).Draw(rt, "timeout_ms"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for TimeoutMs=%d (must be > 0)", p.TimeoutMs)
		}
	})
}

// ============================================================
// AgentOutputChunkPayload
// ============================================================

func TestProp_AgentOutputChunkPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentOutputChunkPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:    SessionID(drawNonEmptyString(rt, "session_id")),
			ChunkIndex:   rapid.IntRange(0, 10000).Draw(rt, "chunk_index"),
			BytesEmitted: rapid.IntRange(0, 65536).Draw(rt, "bytes_emitted"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed AgentOutputChunkPayload")
		}
	})
}

func TestProp_AgentOutputChunkPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentOutputChunkPayload{
			RunID:        RunID(uuid.Nil),
			SessionID:    SessionID(drawNonEmptyString(rt, "session_id")),
			ChunkIndex:   0,
			BytesEmitted: 0,
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_AgentOutputChunkPayload_EmptySessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentOutputChunkPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:    "",
			ChunkIndex:   0,
			BytesEmitted: 0,
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty SessionID")
		}
	})
}

func TestProp_AgentOutputChunkPayload_NegativeChunkIndexRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentOutputChunkPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:    SessionID(drawNonEmptyString(rt, "session_id")),
			ChunkIndex:   rapid.IntRange(-10000, -1).Draw(rt, "chunk_index"),
			BytesEmitted: 0,
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for ChunkIndex=%d (must be >= 0)", p.ChunkIndex)
		}
	})
}

func TestProp_AgentOutputChunkPayload_NegativeBytesEmittedRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentOutputChunkPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:    SessionID(drawNonEmptyString(rt, "session_id")),
			ChunkIndex:   0,
			BytesEmitted: rapid.IntRange(-65536, -1).Draw(rt, "bytes_emitted"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for BytesEmitted=%d (must be >= 0)", p.BytesEmitted)
		}
	})
}

func TestProp_AgentOutputChunkPayload_EmptyChunkDigestRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		empty := ""
		p := AgentOutputChunkPayload{
			RunID:        RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:    SessionID(drawNonEmptyString(rt, "session_id")),
			ChunkIndex:   0,
			BytesEmitted: 0,
			ChunkDigest:  &empty, // non-nil but empty
		}
		if p.Valid() {
			rt.Error("Valid() should be false when ChunkDigest is non-nil but empty")
		}
	})
}

// ============================================================
// AgentFailedPayload
// ============================================================

func TestProp_AgentFailedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentFailedPayload{
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:     SessionID(drawNonEmptyString(rt, "session_id")),
			EndedAt:       drawNonEmptyString(rt, "ended_at"),
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:        drawNonEmptyString(rt, "reason"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed AgentFailedPayload")
		}
	})
}

func TestProp_AgentFailedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentFailedPayload{
			RunID:         RunID(uuid.Nil),
			SessionID:     SessionID(drawNonEmptyString(rt, "session_id")),
			EndedAt:       drawNonEmptyString(rt, "ended_at"),
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:        drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_AgentFailedPayload_EmptySessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentFailedPayload{
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:     "",
			EndedAt:       drawNonEmptyString(rt, "ended_at"),
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:        drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty SessionID")
		}
	})
}

func TestProp_AgentFailedPayload_EmptyEndedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentFailedPayload{
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:     SessionID(drawNonEmptyString(rt, "session_id")),
			EndedAt:       "",
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:        drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EndedAt")
		}
	})
}

func TestProp_AgentFailedPayload_InvalidErrorCategoryRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allErrorCategories {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "err_cat_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := AgentFailedPayload{
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:     SessionID(drawNonEmptyString(rt, "session_id")),
			EndedAt:       drawNonEmptyString(rt, "ended_at"),
			ErrorCategory: ErrorCategory(s),
			Reason:        drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown error category %q", s)
		}
	})
}

func TestProp_AgentFailedPayload_EmptyReasonRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentFailedPayload{
			RunID:         RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:     SessionID(drawNonEmptyString(rt, "session_id")),
			EndedAt:       drawNonEmptyString(rt, "ended_at"),
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:        "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty Reason")
		}
	})
}

// ============================================================
// SkillsProvisionedPayload
// ============================================================

func TestProp_SkillsProvisionedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SkillsProvisionedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			Skills:    []ProvisionedSkill{},
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed SkillsProvisionedPayload (empty skills)")
		}
	})
}

func TestProp_SkillsProvisionedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SkillsProvisionedPayload{
			RunID:     RunID(uuid.Nil),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			Skills:    []ProvisionedSkill{},
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_SkillsProvisionedPayload_EmptySessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SkillsProvisionedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: "",
			Skills:    []ProvisionedSkill{},
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty SessionID")
		}
	})
}

func TestProp_SkillsProvisionedPayload_NilSkillsRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SkillsProvisionedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			Skills:    nil,
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil Skills")
		}
	})
}

func TestProp_SkillsProvisionedPayload_EmptySkillNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SkillsProvisionedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			Skills: []ProvisionedSkill{
				{Name: "", SourcePath: drawNonEmptyString(rt, "source_path")},
			},
		}
		if p.Valid() {
			rt.Error("Valid() should be false when a skill Name is empty")
		}
	})
}

func TestProp_SkillsProvisionedPayload_EmptySkillSourcePathRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SkillsProvisionedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			Skills: []ProvisionedSkill{
				{Name: drawNonEmptyString(rt, "name"), SourcePath: ""},
			},
		}
		if p.Valid() {
			rt.Error("Valid() should be false when a skill SourcePath is empty")
		}
	})
}

func TestProp_SkillsProvisionedPayload_EmptySkillVersionRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		empty := ""
		p := SkillsProvisionedPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			Skills: []ProvisionedSkill{
				{Name: drawNonEmptyString(rt, "name"), SourcePath: drawNonEmptyString(rt, "sp"), Version: &empty},
			},
		}
		if p.Valid() {
			rt.Error("Valid() should be false when a skill Version is non-nil but empty")
		}
	})
}

// ============================================================
// SessionLogLocationPayload
// ============================================================

func TestProp_SessionLogLocationPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionLogLocationPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			LogPath:   drawNonEmptyString(rt, "log_path"),
			LogFormat: drawNonEmptyString(rt, "log_format"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed SessionLogLocationPayload")
		}
	})
}

func TestProp_SessionLogLocationPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionLogLocationPayload{
			RunID:     RunID(uuid.Nil),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			LogPath:   drawNonEmptyString(rt, "log_path"),
			LogFormat: drawNonEmptyString(rt, "log_format"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_SessionLogLocationPayload_EmptySessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionLogLocationPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: "",
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			LogPath:   drawNonEmptyString(rt, "log_path"),
			LogFormat: drawNonEmptyString(rt, "log_format"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty SessionID")
		}
	})
}

func TestProp_SessionLogLocationPayload_EmptyNodeIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionLogLocationPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    "",
			AgentType: AgentTypeClaudeCode,
			LogPath:   drawNonEmptyString(rt, "log_path"),
			LogFormat: drawNonEmptyString(rt, "log_format"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty NodeID")
		}
	})
}

func TestProp_SessionLogLocationPayload_EmptyLogPathRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionLogLocationPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			LogPath:   "",
			LogFormat: drawNonEmptyString(rt, "log_format"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty LogPath")
		}
	})
}

func TestProp_SessionLogLocationPayload_EmptyLogFormatRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := SessionLogLocationPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			LogPath:   drawNonEmptyString(rt, "log_path"),
			LogFormat: "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty LogFormat")
		}
	})
}

func TestProp_SessionLogLocationPayload_EmptyBeadIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		emptyBeadID := BeadID("")
		p := SessionLogLocationPayload{
			RunID:     RunID(drawNonNilUUID(rt, "run_id")),
			SessionID: SessionID(drawNonEmptyString(rt, "session_id")),
			NodeID:    NodeID(drawNonEmptyString(rt, "node_id")),
			AgentType: AgentTypeClaudeCode,
			LogPath:   drawNonEmptyString(rt, "log_path"),
			LogFormat: drawNonEmptyString(rt, "log_format"),
			BeadID:    &emptyBeadID, // non-nil but empty
		}
		if p.Valid() {
			rt.Error("Valid() should be false when BeadID is non-nil but empty")
		}
	})
}

// ============================================================
// HandlerCapabilitiesPayload
// ============================================================

func TestProp_HandlerCapabilitiesPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerCapabilitiesPayload{
			RunID:                    RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:                SessionID(drawNonEmptyString(rt, "session_id")),
			ProtocolVersionsSupported: []string{drawNonEmptyString(rt, "proto_ver")},
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed HandlerCapabilitiesPayload")
		}
	})
}

func TestProp_HandlerCapabilitiesPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerCapabilitiesPayload{
			RunID:                    RunID(uuid.Nil),
			SessionID:                SessionID(drawNonEmptyString(rt, "session_id")),
			ProtocolVersionsSupported: []string{"v1"},
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_HandlerCapabilitiesPayload_EmptySessionIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerCapabilitiesPayload{
			RunID:                    RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:                "",
			ProtocolVersionsSupported: []string{"v1"},
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty SessionID")
		}
	})
}

func TestProp_HandlerCapabilitiesPayload_EmptyProtocolVersionsRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HandlerCapabilitiesPayload{
			RunID:                    RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:                SessionID(drawNonEmptyString(rt, "session_id")),
			ProtocolVersionsSupported: []string{},
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ProtocolVersionsSupported")
		}
	})
}
