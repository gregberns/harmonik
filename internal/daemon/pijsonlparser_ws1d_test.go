package daemon_test

// pijsonlparser_ws1d_test.go — Pi NDJSON usage-extraction tests (WS1d).
//
// Coverage:
//   - parsePiNDJSONEvent: message_start decodes usage from nested "message.usage".
//   - parsePiNDJSONEvent: message_end decodes usage from top-level "usage".
//   - parsePiNDJSONEvent: agent_end sums usage across messages array.
//   - parsePiNDJSONEvent: events without usage fields produce zero Usage.
//   - capturePiUsage: accumulates InputTokens from message_start events.
//   - capturePiUsage: accumulates OutputTokens from message_end events.
//   - capturePiUsage: ignores session, agent_end, and other events.
//   - capturePiUsage: accumulates correctly across a multi-turn stream.
//
// Bead: hk-eval-prog-pi-tokens-sr316 (WS1d).

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// parsePiNDJSONEvent — usage field extraction
// ─────────────────────────────────────────────────────────────────────────────

func TestParsePiNDJSONEvent_MessageStart_Usage(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"message_start","message":{"id":"msg_01","usage":{"input_tokens":150,"output_tokens":2}}}`)
	kind, rawType, _, usage, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindMessageStart {
		t.Errorf("Kind = %v; want piEventKindMessageStart", kind)
	}
	if rawType != "message_start" {
		t.Errorf("RawType = %q; want %q", rawType, "message_start")
	}
	if usage.InputTokens != 150 {
		t.Errorf("Usage.InputTokens = %d; want 150", usage.InputTokens)
	}
	if usage.OutputTokens != 2 {
		t.Errorf("Usage.OutputTokens = %d; want 2", usage.OutputTokens)
	}
}

func TestParsePiNDJSONEvent_MessageStart_NoUsage(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"message_start","message":{"id":"msg_01"}}`)
	kind, _, _, usage, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindMessageStart {
		t.Errorf("Kind = %v; want piEventKindMessageStart", kind)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("Usage = {%d, %d}; want {0, 0} when no usage field present",
			usage.InputTokens, usage.OutputTokens)
	}
}

func TestParsePiNDJSONEvent_MessageEnd_Usage(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"message_end","usage":{"output_tokens":87}}`)
	kind, rawType, _, usage, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindMessageEnd {
		t.Errorf("Kind = %v; want piEventKindMessageEnd", kind)
	}
	if rawType != "message_end" {
		t.Errorf("RawType = %q; want %q", rawType, "message_end")
	}
	if usage.OutputTokens != 87 {
		t.Errorf("Usage.OutputTokens = %d; want 87", usage.OutputTokens)
	}
	if usage.InputTokens != 0 {
		t.Errorf("Usage.InputTokens = %d; want 0 for message_end", usage.InputTokens)
	}
}

func TestParsePiNDJSONEvent_MessageEnd_NoUsage(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"message_end"}`)
	kind, _, _, usage, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindMessageEnd {
		t.Errorf("Kind = %v; want piEventKindMessageEnd", kind)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("Usage = {%d, %d}; want {0, 0} when no usage field present",
			usage.InputTokens, usage.OutputTokens)
	}
}

func TestParsePiNDJSONEvent_AgentEnd_UsageFromMessages(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"agent_end","messages":[` +
		`{"role":"user","content":"hello"},` +
		`{"role":"assistant","content":"hi","usage":{"input_tokens":10,"output_tokens":5}},` +
		`{"role":"user","content":"do it"},` +
		`{"role":"assistant","content":"done","usage":{"input_tokens":20,"output_tokens":15}}` +
		`]}`)
	kind, _, _, usage, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindAgentEnd {
		t.Errorf("Kind = %v; want piEventKindAgentEnd", kind)
	}
	if usage.InputTokens != 30 {
		t.Errorf("Usage.InputTokens = %d; want 30 (10+20)", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Errorf("Usage.OutputTokens = %d; want 20 (5+15)", usage.OutputTokens)
	}
}

func TestParsePiNDJSONEvent_AgentEnd_EmptyMessages(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"agent_end","messages":[]}`)
	kind, _, _, usage, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindAgentEnd {
		t.Errorf("Kind = %v; want piEventKindAgentEnd", kind)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("Usage = {%d, %d}; want {0, 0} for empty messages", usage.InputTokens, usage.OutputTokens)
	}
}

func TestParsePiNDJSONEvent_AgentEnd_MessagesWithoutUsage(t *testing.T) {
	t.Parallel()

	// Messages that don't carry a usage field should contribute zero.
	line := []byte(`{"type":"agent_end","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hey"}]}`)
	kind, _, _, usage, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != daemon.ExportedPiEventKindAgentEnd {
		t.Errorf("Kind = %v; want piEventKindAgentEnd", kind)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("Usage = {%d, %d}; want {0, 0} when messages have no usage", usage.InputTokens, usage.OutputTokens)
	}
}

func TestParsePiNDJSONEvent_Session_ZeroUsage(t *testing.T) {
	t.Parallel()

	// Session events never carry usage; Usage must be zero.
	line := []byte(`{"type":"session","version":3,"id":"abc-123","cwd":"/tmp/wt"}`)
	_, _, _, usage, err := daemon.ExportedParsePiNDJSONEvent(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("Usage = {%d, %d}; want {0, 0} for session events", usage.InputTokens, usage.OutputTokens)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// capturePiUsage — accumulation
// ─────────────────────────────────────────────────────────────────────────────

func TestCapturePiUsage_MessageStart_AccumulatesInputOnly(t *testing.T) {
	t.Parallel()

	var arts daemon.ExportedPiRunArtifacts
	// message_start carries the prompt cost in input_tokens; output_tokens is an
	// initial draft count that must NOT be accumulated (message_end owns outputs).
	line := []byte(`{"type":"message_start","message":{"usage":{"input_tokens":200,"output_tokens":1}}}`)
	if !daemon.ExportedCapturePiUsage(&arts, line) {
		t.Error("capturePiUsage returned false; want true for message_start with non-zero input_tokens")
	}
	if arts.TotalUsage.InputTokens != 200 {
		t.Errorf("TotalUsage.InputTokens = %d; want 200", arts.TotalUsage.InputTokens)
	}
	// output_tokens from message_start is NOT accumulated — message_end owns it.
	if arts.TotalUsage.OutputTokens != 0 {
		t.Errorf("TotalUsage.OutputTokens = %d; want 0 (message_start output_tokens must not be accumulated)", arts.TotalUsage.OutputTokens)
	}
}

func TestCapturePiUsage_MessageEnd_AccumulatesOutput(t *testing.T) {
	t.Parallel()

	var arts daemon.ExportedPiRunArtifacts
	line := []byte(`{"type":"message_end","usage":{"output_tokens":42}}`)
	if !daemon.ExportedCapturePiUsage(&arts, line) {
		t.Error("capturePiUsage returned false; want true for message_end with usage")
	}
	if arts.TotalUsage.OutputTokens != 42 {
		t.Errorf("TotalUsage.OutputTokens = %d; want 42", arts.TotalUsage.OutputTokens)
	}
	if arts.TotalUsage.InputTokens != 0 {
		t.Errorf("TotalUsage.InputTokens = %d; want 0 for message_end", arts.TotalUsage.InputTokens)
	}
}

func TestCapturePiUsage_IgnoresAgentEnd(t *testing.T) {
	t.Parallel()

	var arts daemon.ExportedPiRunArtifacts
	line := []byte(`{"type":"agent_end","messages":[{"role":"assistant","usage":{"input_tokens":100,"output_tokens":50}}]}`)
	if daemon.ExportedCapturePiUsage(&arts, line) {
		t.Error("capturePiUsage returned true for agent_end; want false (not accumulated)")
	}
	if arts.TotalUsage.InputTokens != 0 || arts.TotalUsage.OutputTokens != 0 {
		t.Errorf("TotalUsage = {%d, %d}; want {0, 0} — agent_end usage not accumulated via capturePiUsage",
			arts.TotalUsage.InputTokens, arts.TotalUsage.OutputTokens)
	}
}

func TestCapturePiUsage_IgnoresSession(t *testing.T) {
	t.Parallel()

	var arts daemon.ExportedPiRunArtifacts
	line := []byte(`{"type":"session","id":"abc-123"}`)
	if daemon.ExportedCapturePiUsage(&arts, line) {
		t.Error("capturePiUsage returned true for session; want false")
	}
	if arts.TotalUsage.InputTokens != 0 || arts.TotalUsage.OutputTokens != 0 {
		t.Errorf("TotalUsage = {%d, %d}; want {0, 0}", arts.TotalUsage.InputTokens, arts.TotalUsage.OutputTokens)
	}
}

func TestCapturePiUsage_IgnoresOtherEvents(t *testing.T) {
	t.Parallel()

	var arts daemon.ExportedPiRunArtifacts
	line := []byte(`{"type":"tool_execution_start","name":"bash"}`)
	if daemon.ExportedCapturePiUsage(&arts, line) {
		t.Error("capturePiUsage returned true for tool event; want false")
	}
	if arts.TotalUsage.InputTokens != 0 || arts.TotalUsage.OutputTokens != 0 {
		t.Errorf("TotalUsage = {%d, %d}; want {0, 0}", arts.TotalUsage.InputTokens, arts.TotalUsage.OutputTokens)
	}
}

func TestCapturePiUsage_ReturnsFalse_ZeroUsage(t *testing.T) {
	t.Parallel()

	var arts daemon.ExportedPiRunArtifacts
	// message_start with no usage sub-object → zero usage → returns false.
	line := []byte(`{"type":"message_start","message":{}}`)
	if daemon.ExportedCapturePiUsage(&arts, line) {
		t.Error("capturePiUsage returned true for message_start with zero usage; want false")
	}
}

// TestCapturePiUsage_MultiTurnAccumulation verifies that calling capturePiUsage
// across a full multi-turn stream yields the correct totals.
func TestCapturePiUsage_MultiTurnAccumulation(t *testing.T) {
	t.Parallel()

	var arts daemon.ExportedPiRunArtifacts
	stream := [][]byte{
		[]byte(`{"type":"session","id":"sess-1"}`),
		[]byte(`{"type":"turn_start"}`),
		// Turn 1: 100 input tokens, 50 output tokens.
		// message_start carries the prompt cost (input_tokens=100); output_tokens=1
		// is an initial draft and must NOT be accumulated.
		[]byte(`{"type":"message_start","message":{"usage":{"input_tokens":100,"output_tokens":1}}}`),
		[]byte(`{"type":"message_end","usage":{"output_tokens":50}}`),
		[]byte(`{"type":"turn_end"}`),
		[]byte(`{"type":"turn_start"}`),
		// Turn 2: 200 input tokens, 75 output tokens.
		[]byte(`{"type":"message_start","message":{"usage":{"input_tokens":200,"output_tokens":1}}}`),
		[]byte(`{"type":"message_end","usage":{"output_tokens":75}}`),
		[]byte(`{"type":"turn_end"}`),
		// agent_end is not accumulated via capturePiUsage.
		[]byte(`{"type":"agent_end","messages":[]}`),
	}
	for _, line := range stream {
		daemon.ExportedCapturePiUsage(&arts, line)
	}
	// input: 100 (turn 1 message_start) + 200 (turn 2 message_start) = 300
	// output: 50 (turn 1 message_end) + 75 (turn 2 message_end) = 125
	if arts.TotalUsage.InputTokens != 300 {
		t.Errorf("TotalUsage.InputTokens = %d; want 300", arts.TotalUsage.InputTokens)
	}
	if arts.TotalUsage.OutputTokens != 125 {
		t.Errorf("TotalUsage.OutputTokens = %d; want 125", arts.TotalUsage.OutputTokens)
	}
}
