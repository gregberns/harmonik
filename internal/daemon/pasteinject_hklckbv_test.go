package daemon_test

// pasteinject_hklckbv_test.go — regression tests for hk-lckbv.
//
// Root cause: rlSynthesiseClaudeSessionID() produced IDs like
// "synthetic-claude-session-20260528T150405Z" whose uppercase 'T' and 'Z'
// violated the tmux bufferNameRe ([a-z0-9-]+), causing WriteLastPane to return
// ErrStructural on every iter-2 implementer-resume launch.  The run then went
// run_stale (no run_completed / run_failed emitted), wedging the daemon.
//
// Fix: rlSynthesiseClaudeSessionID now returns
// "syntheticclaudesession<YYYYMMDDHHmmss>" — all lowercase ASCII, no uppercase,
// no dashes in the ID body — which always satisfies the regex.
//
// Tests in this file verify:
//  1. The live rlSynthesiseClaudeSessionID output matches [a-z0-9-]+ (regression
//     guard: would fail with the old uppercase format).
//  2. bufferName(syntheticID, "task") and bufferName(syntheticID, "feedback")
//     match the expected harmonik-<id>-<purpose> pattern.
//  3. pasteInjectOnLaunch with ReviewLoopPhaseImplementerResume and a synthetic
//     session ID produces exactly ONE WriteToPane call (combined task+feedback
//     per hk-poy7k) containing both messages, without skipping due to buffer-name
//     validation failure.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// bufferNameRePattern is the same regex used by tmux.bufferNameRe.
// Duplicated here to make the daemon test self-contained.
var bufferNameRePattern = regexp.MustCompile(`^harmonik-[a-z0-9-]+-[a-z0-9-]+$`)

// TestRlSynthesiseClaudeSessionID_RegexSafe verifies that the synthetic session
// ID produced by rlSynthesiseClaudeSessionID satisfies the tmux buffer-name
// character class ([a-z0-9-]+).  The old format used uppercase 'T' and 'Z' in
// the timestamp which violated the regex and caused ErrStructural on iter-2
// resume (hk-lckbv).
func TestRlSynthesiseClaudeSessionID_RegexSafe(t *testing.T) {
	t.Parallel()
	id := daemon.ExportedSynthesiseClaudeSessionID()
	if id == "" {
		t.Fatal("ExportedSynthesiseClaudeSessionID returned empty string")
	}
	// The ID itself must contain only [a-z0-9-] (the character class allowed
	// between "harmonik-" and "-<purpose>" in the buffer name).
	idRe := regexp.MustCompile(`^[a-z0-9-]+$`)
	if !idRe.MatchString(id) {
		t.Errorf("synthetic session ID %q contains characters outside [a-z0-9-]; "+
			"buffer name would fail tmux bufferNameRe validation (hk-lckbv)", id)
	}

	for _, purpose := range []string{"task", "feedback", "review"} {
		bufName := daemon.ExportedBufferName(id, purpose)
		if !bufferNameRePattern.MatchString(bufName) {
			t.Errorf("bufferName(%q, %q) = %q; does not match harmonik buffer-name regex (hk-lckbv)",
				id, purpose, bufName)
		}
	}
}

// TestPasteInjectOnLaunch_ImplementerResume_SyntheticSessionID verifies that
// pasteInjectOnLaunch with ReviewLoopPhaseImplementerResume and a synthetic
// session ID (hk-lckbv format) produces exactly ONE WriteToPane call (task +
// feedback combined per hk-poy7k) and does not skip due to buffer-name
// validation failure.
func TestPasteInjectOnLaunch_ImplementerResume_SyntheticSessionID(t *testing.T) {
	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\nDo something.\n")
	// iterCount=2 → prior iter=1 → "reviewer-feedback.iter-1.md"
	pasteInjectFixtureTaskFile(t, wtPath, "reviewer-feedback.iter-1.md", "# Feedback\nFix something.\n")

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	// Use the live synthetic session ID (same function the daemon calls on iter-2).
	syntheticID := daemon.ExportedSynthesiseClaudeSessionID()

	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, syntheticID,
		handlercontract.ReviewLoopPhaseImplementerResume,
		2, wtPath,
	)
	<-briefDelivered

	calls := adapter.calls()
	// hk-poy7k: task + feedback are combined into ONE paste to eliminate the
	// inter-message race. 0 calls would indicate the inject was skipped (e.g.
	// ErrStructural on buffer name from uppercase chars — hk-lckbv); 2+ calls
	// would indicate the race was re-introduced.
	if len(calls) != 1 {
		t.Fatalf("implementer-resume synthetic ID: expected 1 WriteToPane call (combined task+feedback), got %d "+
			"(0 calls = inject skipped, likely ErrStructural on buffer name; hk-lckbv)",
			len(calls))
	}

	// T8: daemon-run delivery routes through the AIS InputPort.SubmitInput, whose
	// interim tmux driver uses the single AIS input buffer (the per-phase
	// "harmonik-<sessionID>-task" name is now keeper/CLI-only per PL-021d).
	wantTaskBuf := daemon.ExportedInputBufferName(sub)
	if calls[0].bufferName != wantTaskBuf {
		t.Errorf("call[0] bufferName = %q, want %q", calls[0].bufferName, wantTaskBuf)
	}
	// Both task and feedback content must be present in the single payload.
	if !strings.Contains(calls[0].payload, "agent-task.md") {
		t.Errorf("call[0] payload = %q, want mention of agent-task.md", calls[0].payload)
	}
	if !strings.Contains(calls[0].payload, "reviewer-feedback.iter-1.md") {
		t.Errorf("call[0] payload = %q, want mention of reviewer-feedback.iter-1.md", calls[0].payload)
	}
}
