package daemon_test

// codexjsonlparser_test.go — unit tests for the codex `exec --json` JSONL parser
// (codex-harness C2/T8, hk-m57va).
//
// Coverage:
//   - parseCodexJSONLEvent classifies thread.started / turn.started /
//     turn.completed / turn.failed / unmodelled types (table-driven).
//   - thread.started captures the thread_id.
//   - turn.failed carries the error message.
//   - malformed / empty lines return an error.
//   - captureCodexThreadID folds a realistic JSONL sequence into run artifacts:
//     first thread.started wins; turn.completed / turn.failed set flags.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// TestParseCodexJSONLEvent_Table — per-line classification.
// ─────────────────────────────────────────────────────────────────────────────

func TestParseCodexJSONLEvent_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		line          string
		wantKind      daemon.ExportedCodexEventKind
		wantRawType   string
		wantThreadID  string
		wantTurnID    string
		wantErrMsg    string
		wantInTokens  int
		wantOutTokens int
	}{
		{
			name:         "thread.started captures thread_id",
			line:         `{"type":"thread.started","thread_id":"th_abc123"}`,
			wantKind:     daemon.ExportedCodexEventKindThreadStarted,
			wantRawType:  "thread.started",
			wantThreadID: "th_abc123",
		},
		{
			name:        "turn.started carries turn_id",
			line:        `{"type":"turn.started","turn_id":"tr_1"}`,
			wantKind:    daemon.ExportedCodexEventKindTurnStarted,
			wantRawType: "turn.started",
			wantTurnID:  "tr_1",
		},
		{
			name:         "turn.completed carries turn_id and input tokens",
			line:         `{"type":"turn.completed","turn_id":"tr_1","usage":{"input_tokens":10}}`,
			wantKind:     daemon.ExportedCodexEventKindTurnCompleted,
			wantRawType:  "turn.completed",
			wantTurnID:   "tr_1",
			wantInTokens: 10,
		},
		{
			name:          "turn.completed carries both input and output tokens",
			line:          `{"type":"turn.completed","turn_id":"tr_2","usage":{"input_tokens":24763,"output_tokens":122}}`,
			wantKind:      daemon.ExportedCodexEventKindTurnCompleted,
			wantRawType:   "turn.completed",
			wantTurnID:    "tr_2",
			wantInTokens:  24763,
			wantOutTokens: 122,
		},
		{
			name:        "turn.completed without usage object has zero token counts",
			line:        `{"type":"turn.completed","turn_id":"tr_3"}`,
			wantKind:    daemon.ExportedCodexEventKindTurnCompleted,
			wantRawType: "turn.completed",
			wantTurnID:  "tr_3",
		},
		{
			name:        "turn.failed carries error message",
			line:        `{"type":"turn.failed","turn_id":"tr_2","error":{"message":"sandbox denied write"}}`,
			wantKind:    daemon.ExportedCodexEventKindTurnFailed,
			wantRawType: "turn.failed",
			wantTurnID:  "tr_2",
			wantErrMsg:  "sandbox denied write",
		},
		{
			name:        "turn.failed without error object is still classified",
			line:        `{"type":"turn.failed","turn_id":"tr_3"}`,
			wantKind:    daemon.ExportedCodexEventKindTurnFailed,
			wantRawType: "turn.failed",
			wantTurnID:  "tr_3",
			wantErrMsg:  "",
		},
		{
			name:        "unmodelled item event maps to Other with RawType preserved",
			line:        `{"type":"item.completed","item":{"id":"i_1","type":"agent_message"}}`,
			wantKind:    daemon.ExportedCodexEventKindOther,
			wantRawType: "item.completed",
		},
		{
			name:        "token count event maps to Other",
			line:        `{"type":"token_count","input_tokens":42}`,
			wantKind:    daemon.ExportedCodexEventKindOther,
			wantRawType: "token_count",
		},
		{
			name:         "leading/trailing whitespace tolerated",
			line:         "  \t" + `{"type":"thread.started","thread_id":"th_ws"}` + "  ",
			wantKind:     daemon.ExportedCodexEventKindThreadStarted,
			wantRawType:  "thread.started",
			wantThreadID: "th_ws",
		},
		{
			name:        "thread.started with empty id still classifies as thread.started",
			line:        `{"type":"thread.started","thread_id":""}`,
			wantKind:    daemon.ExportedCodexEventKindThreadStarted,
			wantRawType: "thread.started",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ev, err := daemon.ExportedParseCodexJSONLEvent([]byte(tc.line))
			if err != nil {
				t.Fatalf("parseCodexJSONLEvent(%q): unexpected error: %v", tc.line, err)
			}
			if ev.Kind != tc.wantKind {
				t.Errorf("Kind = %v; want %v", ev.Kind, tc.wantKind)
			}
			if ev.RawType != tc.wantRawType {
				t.Errorf("RawType = %q; want %q", ev.RawType, tc.wantRawType)
			}
			if ev.ThreadID != tc.wantThreadID {
				t.Errorf("ThreadID = %q; want %q", ev.ThreadID, tc.wantThreadID)
			}
			if ev.TurnID != tc.wantTurnID {
				t.Errorf("TurnID = %q; want %q", ev.TurnID, tc.wantTurnID)
			}
			if ev.ErrorMessage != tc.wantErrMsg {
				t.Errorf("ErrorMessage = %q; want %q", ev.ErrorMessage, tc.wantErrMsg)
			}
			if ev.InputTokens != tc.wantInTokens {
				t.Errorf("InputTokens = %d; want %d", ev.InputTokens, tc.wantInTokens)
			}
			if ev.OutputTokens != tc.wantOutTokens {
				t.Errorf("OutputTokens = %d; want %d", ev.OutputTokens, tc.wantOutTokens)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestParseCodexJSONLEvent_Errors — malformed input.
// ─────────────────────────────────────────────────────────────────────────────

func TestParseCodexJSONLEvent_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
	}{
		{name: "empty line", line: ""},
		{name: "whitespace only", line: "   \t  "},
		{name: "not JSON", line: "this is not json"},
		{name: "JSON array not object", line: `["type","thread.started"]`},
		{name: "truncated object", line: `{"type":"thread.started"`},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := daemon.ExportedParseCodexJSONLEvent([]byte(tc.line))
			if err == nil {
				t.Errorf("parseCodexJSONLEvent(%q): want error, got nil", tc.line)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCaptureCodexThreadStream — folding a realistic sequence into run state.
// ─────────────────────────────────────────────────────────────────────────────

// TestCaptureCodexThreadStream_HappyPath verifies that a normal initial-turn
// JSONL stream captures the thread_id from thread.started and records turn
// completion including token counts.
func TestCaptureCodexThreadStream_HappyPath(t *testing.T) {
	t.Parallel()

	lines := codexStreamLines(
		`{"type":"thread.started","thread_id":"th_run1"}`,
		`{"type":"turn.started","turn_id":"tr_1"}`,
		`{"type":"item.completed","item":{"type":"command_execution"}}`,
		`{"type":"turn.completed","turn_id":"tr_1","usage":{"input_tokens":100}}`,
	)

	arts, err := daemon.ExportedCaptureCodexThreadStream(lines)
	if err != nil {
		t.Fatalf("ExportedCaptureCodexThreadStream: unexpected error: %v", err)
	}
	if arts.CapturedThreadID != "th_run1" {
		t.Errorf("CapturedThreadID = %q; want %q", arts.CapturedThreadID, "th_run1")
	}
	if !arts.TurnCompleted {
		t.Error("TurnCompleted = false; want true after a turn.completed event")
	}
	if arts.TurnFailed {
		t.Error("TurnFailed = true; want false on the happy path")
	}
	if arts.InputTokens != 100 {
		t.Errorf("InputTokens = %d; want 100", arts.InputTokens)
	}
	if arts.OutputTokens != 0 {
		t.Errorf("OutputTokens = %d; want 0 (not present in usage)", arts.OutputTokens)
	}
}

// TestCaptureCodexThreadStream_TokensFullUsage verifies that both input and
// output token counts are captured from a turn.completed usage object.
func TestCaptureCodexThreadStream_TokensFullUsage(t *testing.T) {
	t.Parallel()

	lines := codexStreamLines(
		`{"type":"thread.started","thread_id":"th_usage"}`,
		`{"type":"turn.started","turn_id":"tr_1"}`,
		`{"type":"turn.completed","turn_id":"tr_1","usage":{"input_tokens":24763,"output_tokens":122}}`,
	)

	arts, err := daemon.ExportedCaptureCodexThreadStream(lines)
	if err != nil {
		t.Fatalf("ExportedCaptureCodexThreadStream: unexpected error: %v", err)
	}
	if !arts.TurnCompleted {
		t.Error("TurnCompleted = false; want true")
	}
	if arts.InputTokens != 24763 {
		t.Errorf("InputTokens = %d; want 24763", arts.InputTokens)
	}
	if arts.OutputTokens != 122 {
		t.Errorf("OutputTokens = %d; want 122", arts.OutputTokens)
	}
}

// TestCaptureCodexThreadStream_NoUsageOnCompletion verifies that a
// turn.completed event without a usage object leaves token counts at zero.
func TestCaptureCodexThreadStream_NoUsageOnCompletion(t *testing.T) {
	t.Parallel()

	lines := codexStreamLines(
		`{"type":"thread.started","thread_id":"th_nousage"}`,
		`{"type":"turn.completed","turn_id":"tr_1"}`,
	)

	arts, err := daemon.ExportedCaptureCodexThreadStream(lines)
	if err != nil {
		t.Fatalf("ExportedCaptureCodexThreadStream: unexpected error: %v", err)
	}
	if !arts.TurnCompleted {
		t.Error("TurnCompleted = false; want true")
	}
	if arts.InputTokens != 0 {
		t.Errorf("InputTokens = %d; want 0 when usage absent", arts.InputTokens)
	}
	if arts.OutputTokens != 0 {
		t.Errorf("OutputTokens = %d; want 0 when usage absent", arts.OutputTokens)
	}
}

// TestCaptureCodexThreadStream_FirstThreadStartedWins verifies that a resumed
// stream re-emitting thread.started does NOT clobber the originally-captured
// thread_id (first wins).
func TestCaptureCodexThreadStream_FirstThreadStartedWins(t *testing.T) {
	t.Parallel()

	lines := codexStreamLines(
		`{"type":"thread.started","thread_id":"th_original"}`,
		`{"type":"turn.started","turn_id":"tr_1"}`,
		`{"type":"thread.started","thread_id":"th_should_be_ignored"}`,
		`{"type":"turn.completed","turn_id":"tr_1"}`,
	)

	arts, err := daemon.ExportedCaptureCodexThreadStream(lines)
	if err != nil {
		t.Fatalf("ExportedCaptureCodexThreadStream: unexpected error: %v", err)
	}
	if arts.CapturedThreadID != "th_original" {
		t.Errorf("CapturedThreadID = %q; want %q (first thread.started must win)",
			arts.CapturedThreadID, "th_original")
	}
}

// TestCaptureCodexThreadStream_TurnFailed verifies that turn.failed sets the
// failed flag and captures the error message while still capturing the thread_id.
func TestCaptureCodexThreadStream_TurnFailed(t *testing.T) {
	t.Parallel()

	lines := codexStreamLines(
		`{"type":"thread.started","thread_id":"th_fail"}`,
		`{"type":"turn.started","turn_id":"tr_1"}`,
		`{"type":"turn.failed","turn_id":"tr_1","error":{"message":"model error: rate limited"}}`,
	)

	arts, err := daemon.ExportedCaptureCodexThreadStream(lines)
	if err != nil {
		t.Fatalf("ExportedCaptureCodexThreadStream: unexpected error: %v", err)
	}
	if arts.CapturedThreadID != "th_fail" {
		t.Errorf("CapturedThreadID = %q; want %q", arts.CapturedThreadID, "th_fail")
	}
	if !arts.TurnFailed {
		t.Error("TurnFailed = false; want true after a turn.failed event")
	}
	if arts.TurnFailureMessage != "model error: rate limited" {
		t.Errorf("TurnFailureMessage = %q; want %q",
			arts.TurnFailureMessage, "model error: rate limited")
	}
	if arts.TurnCompleted {
		t.Error("TurnCompleted = true; want false when the turn failed")
	}
}

// TestCaptureCodexThreadStream_NoThreadStarted verifies that a stream missing
// thread.started leaves CapturedThreadID empty (no spurious capture from other
// event types).
func TestCaptureCodexThreadStream_NoThreadStarted(t *testing.T) {
	t.Parallel()

	lines := codexStreamLines(
		`{"type":"turn.started","turn_id":"tr_1"}`,
		`{"type":"item.completed","item":{"type":"agent_message"}}`,
		`{"type":"turn.completed","turn_id":"tr_1"}`,
	)

	arts, err := daemon.ExportedCaptureCodexThreadStream(lines)
	if err != nil {
		t.Fatalf("ExportedCaptureCodexThreadStream: unexpected error: %v", err)
	}
	if arts.CapturedThreadID != "" {
		t.Errorf("CapturedThreadID = %q; want empty (no thread.started in stream)",
			arts.CapturedThreadID)
	}
	if !arts.TurnCompleted {
		t.Error("TurnCompleted = false; want true")
	}
}

// codexStreamLines is a small helper that turns variadic JSONL strings into a
// [][]byte for ExportedCaptureCodexThreadStream.
func codexStreamLines(lines ...string) [][]byte {
	out := make([][]byte, 0, len(lines))
	for _, l := range lines {
		out = append(out, []byte(l))
	}
	return out
}

// TestParseCodexJSONLEvent_ErrorMessageMentionsLine asserts the decode error is
// diagnostic (carries the offending line) so production stream-reader logs are
// actionable.
func TestParseCodexJSONLEvent_ErrorMessageMentionsLine(t *testing.T) {
	t.Parallel()

	_, err := daemon.ExportedParseCodexJSONLEvent([]byte(`{"type":}`))
	if err == nil {
		t.Fatal("want error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parseCodexJSONLEvent") {
		t.Errorf("error %q should be prefixed with the function name", err.Error())
	}
}
