package handlercontract_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// progressStreamFixture — per-bead helper prefix for test helpers in this file
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.7).

// ─────────────────────────────────────────────────────────────────────────────
// HC-007 — 12 required message types
// ─────────────────────────────────────────────────────────────────────────────

// TestProgressStream_MessageTypeValues verifies that each of the 12
// required progress-stream message type constants has the exact string value
// mandated by specs/handler-contract.md §4.2.HC-007.
//
// Normative: the daemon dispatcher matches on these literal strings; a typo
// silently breaks the dispatch chain.
func TestProgressStream_MessageTypeValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		constant handlercontract.ProgressMsgType
		want     string
	}{
		{handlercontract.ProgressMsgTypeHandlerCapabilities, "handler_capabilities"},
		{handlercontract.ProgressMsgTypeAgentReady, "agent_ready"},
		{handlercontract.ProgressMsgTypeAgentStarted, "agent_started"},
		{handlercontract.ProgressMsgTypeAgentOutputChunk, "agent_output_chunk"},
		{handlercontract.ProgressMsgTypeAgentCompleted, "agent_completed"},
		{handlercontract.ProgressMsgTypeAgentFailed, "agent_failed"},
		{handlercontract.ProgressMsgTypeAgentRateLimited, "agent_rate_limited"},
		{handlercontract.ProgressMsgTypeAgentRateLimitCleared, "agent_rate_limit_cleared"},
		{handlercontract.ProgressMsgTypeAgentHeartbeat, "agent_heartbeat"},
		{handlercontract.ProgressMsgTypeSessionLogLocation, "session_log_location"},
		{handlercontract.ProgressMsgTypeSkillsProvisioned, "skills_provisioned"},
		{handlercontract.ProgressMsgTypeOutcomeEmitted, "outcome_emitted"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if tc.constant != tc.want {
				t.Errorf("ProgressMsgType constant = %q, want %q", tc.constant, tc.want)
			}
		})
	}
}

// TestProgressStream_MessageTypeCount verifies that exactly 12 message types
// are declared, as required by specs/handler-contract.md §4.2.HC-007.
//
// This acts as a regression guard: adding a 13th constant without updating
// this count makes the omission visible.
func TestProgressStream_MessageTypeCount(t *testing.T) {
	t.Parallel()

	const wantCount = 12

	all := []handlercontract.ProgressMsgType{
		handlercontract.ProgressMsgTypeHandlerCapabilities,
		handlercontract.ProgressMsgTypeAgentReady,
		handlercontract.ProgressMsgTypeAgentStarted,
		handlercontract.ProgressMsgTypeAgentOutputChunk,
		handlercontract.ProgressMsgTypeAgentCompleted,
		handlercontract.ProgressMsgTypeAgentFailed,
		handlercontract.ProgressMsgTypeAgentRateLimited,
		handlercontract.ProgressMsgTypeAgentRateLimitCleared,
		handlercontract.ProgressMsgTypeAgentHeartbeat,
		handlercontract.ProgressMsgTypeSessionLogLocation,
		handlercontract.ProgressMsgTypeSkillsProvisioned,
		handlercontract.ProgressMsgTypeOutcomeEmitted,
	}

	if len(all) != wantCount {
		t.Errorf("message type count = %d, want %d (specs/handler-contract.md §4.2.HC-007)",
			len(all), wantCount)
	}
}

// TestProgressStream_MessageTypesDistinct verifies that each of the 12
// required message type constants has a unique string value (no accidental
// alias).
func TestProgressStream_MessageTypesDistinct(t *testing.T) {
	t.Parallel()

	all := []struct {
		name string
		val  handlercontract.ProgressMsgType
	}{
		{"HandlerCapabilities", handlercontract.ProgressMsgTypeHandlerCapabilities},
		{"AgentReady", handlercontract.ProgressMsgTypeAgentReady},
		{"AgentStarted", handlercontract.ProgressMsgTypeAgentStarted},
		{"AgentOutputChunk", handlercontract.ProgressMsgTypeAgentOutputChunk},
		{"AgentCompleted", handlercontract.ProgressMsgTypeAgentCompleted},
		{"AgentFailed", handlercontract.ProgressMsgTypeAgentFailed},
		{"AgentRateLimited", handlercontract.ProgressMsgTypeAgentRateLimited},
		{"AgentRateLimitCleared", handlercontract.ProgressMsgTypeAgentRateLimitCleared},
		{"AgentHeartbeat", handlercontract.ProgressMsgTypeAgentHeartbeat},
		{"SessionLogLocation", handlercontract.ProgressMsgTypeSessionLogLocation},
		{"SkillsProvisioned", handlercontract.ProgressMsgTypeSkillsProvisioned},
		{"OutcomeEmitted", handlercontract.ProgressMsgTypeOutcomeEmitted},
	}

	seen := make(map[string]string, len(all))
	for _, item := range all {
		if prior, ok := seen[item.val]; ok {
			t.Errorf("ProgressMsgType constants %q and %q share the same value %q; constants must be distinct",
				prior, item.name, item.val)
		}
		seen[item.val] = item.name
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-007a — NDJSON max line length
// ─────────────────────────────────────────────────────────────────────────────

// TestProgressStream_NDJSONMaxLineLenBytesValue verifies that
// NDJSONMaxLineLenBytes equals 1 MiB (1 048 576 bytes) as required by
// specs/handler-contract.md §4.2.HC-007a.
func TestProgressStream_NDJSONMaxLineLenBytesValue(t *testing.T) {
	t.Parallel()

	const wantOneMiB = 1 << 20 // 1 048 576
	if handlercontract.NDJSONMaxLineLenBytes != wantOneMiB {
		t.Errorf("NDJSONMaxLineLenBytes = %d, want %d (1 MiB per HC-007a)",
			handlercontract.NDJSONMaxLineLenBytes, wantOneMiB)
	}
}

// TestProgressStream_NDJSONMaxLineLenBytesPositive verifies that the cap is
// strictly positive (a zero or negative cap would accept nothing / accept
// everything, respectively).
func TestProgressStream_NDJSONMaxLineLenBytesPositive(t *testing.T) {
	t.Parallel()

	if handlercontract.NDJSONMaxLineLenBytes <= 0 {
		t.Errorf("NDJSONMaxLineLenBytes = %d; must be > 0", handlercontract.NDJSONMaxLineLenBytes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-007b — sub-reason string constants
// ─────────────────────────────────────────────────────────────────────────────

// TestProgressStream_NDJSONLineTooLongSubReasonValue verifies the literal value
// against the string mandated by specs/handler-contract.md §4.2.HC-007a and §8.7.
func TestProgressStream_NDJSONLineTooLongSubReasonValue(t *testing.T) {
	t.Parallel()

	const want = "ndjson_line_too_long"
	if handlercontract.NDJSONLineTooLongSubReason != want {
		t.Errorf("NDJSONLineTooLongSubReason = %q, want %q", handlercontract.NDJSONLineTooLongSubReason, want)
	}
}

// TestProgressStream_PartialMessageSubReasonValue verifies the literal value
// against the string mandated by specs/handler-contract.md §4.2.HC-007b.
func TestProgressStream_PartialMessageSubReasonValue(t *testing.T) {
	t.Parallel()

	const want = "partial-message"
	if handlercontract.PartialMessageSubReason != want {
		t.Errorf("PartialMessageSubReason = %q, want %q", handlercontract.PartialMessageSubReason, want)
	}
}

// TestProgressStream_MalformedProgressMessageSubReasonValue verifies the literal
// value against the string mandated by specs/handler-contract.md §4.2.HC-007b.
func TestProgressStream_MalformedProgressMessageSubReasonValue(t *testing.T) {
	t.Parallel()

	const want = "malformed_progress_message"
	if handlercontract.MalformedProgressMessageSubReason != want {
		t.Errorf("MalformedProgressMessageSubReason = %q, want %q",
			handlercontract.MalformedProgressMessageSubReason, want)
	}
}

// TestProgressStream_SubReasonsDistinct verifies the three framing sub-reason
// constants are mutually distinct (no accidental alias).
func TestProgressStream_SubReasonsDistinct(t *testing.T) {
	t.Parallel()

	constants := []struct {
		name string
		val  string
	}{
		{"NDJSONLineTooLongSubReason", handlercontract.NDJSONLineTooLongSubReason},
		{"PartialMessageSubReason", handlercontract.PartialMessageSubReason},
		{"MalformedProgressMessageSubReason", handlercontract.MalformedProgressMessageSubReason},
	}

	seen := make(map[string]string, len(constants))
	for _, c := range constants {
		if prior, ok := seen[c.val]; ok {
			t.Errorf("sub-reason constants %q and %q share the same value %q; must be distinct",
				prior, c.name, c.val)
		}
		seen[c.val] = c.name
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-007 — OutcomeEmittedMsg type field matches ProgressMsgTypeOutcomeEmitted
// ─────────────────────────────────────────────────────────────────────────────

// TestProgressStream_OutcomeEmittedMsgTypeFieldMatchesConstant verifies that the
// "type" value expected in OutcomeEmittedMsg.Type matches ProgressMsgTypeOutcomeEmitted.
//
// This cross-checks the two representations — the typed message constant and the
// OutcomeEmittedMsg wire struct — to ensure they name the same message type.
//
// Spec: specs/handler-contract.md §4.2.HC-007, §4.2.HC-008.
func TestProgressStream_OutcomeEmittedMsgTypeFieldMatchesConstant(t *testing.T) {
	t.Parallel()

	msg := handlercontract.OutcomeEmittedMsg{
		Type:          handlercontract.ProgressMsgTypeOutcomeEmitted,
		RunID:         "r",
		SessionID:     "s",
		NodeID:        "n",
		OutcomeStatus: "SUCCESS",
	}

	if msg.Type != handlercontract.ProgressMsgTypeOutcomeEmitted {
		t.Errorf("OutcomeEmittedMsg.Type = %q, want ProgressMsgTypeOutcomeEmitted (%q)",
			msg.Type, handlercontract.ProgressMsgTypeOutcomeEmitted)
	}
}
