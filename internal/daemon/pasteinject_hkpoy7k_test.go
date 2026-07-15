package daemon_test

// pasteinject_hkpoy7k_test.go — regression tests for hk-poy7k.
//
// Root cause: pasteInjectImplementerResume sent two messages back-to-back with
// no synchronization between them:
//  1. Task instruction → WriteLastPane("task") → Enter
//  2. Reviewer feedback → WriteLastPane("feedback") → Enter
//
// The second Enter was sent while Claude was still processing the first message
// and had NOT returned to the REPL input prompt. The feedback message was
// buffered/dropped, so the resumed implementer never saw it → reproduced the
// identical diff → reviewloop.go no-progress detector fired → run_failed.
//
// Fix (option b from hk-poy7k spec): combine task+feedback into a SINGLE paste
// buffer (blank-line separated) submitted with ONE Enter, eliminating the
// inter-message race entirely.
//
// Tests in this file verify:
//  1. ImplementerResume_SinglePaste: implementer-resume produces exactly 1
//     WriteToPane call, not 2. This is the primary no-race regression guard.
//  2. ImplementerResume_BothMessagesReadable: the single payload contains both
//     "agent-task.md" and "reviewer-feedback.iter-N.md" as distinct readable
//     content (separated by a blank line).
//  3. ImplementerResume_FeedbackMissing_TaskOnlyFallback: when the feedback
//     file is absent, the inject degrades gracefully to the task-only form
//     (still 1 call, still mentions agent-task.md, does NOT mention feedback).
//  4. ImplementerResume_FeedbackIter: the feedback file path encodes the prior
//     iteration number correctly (iter-2 resume uses iter-1 feedback).
//
// Helper prefix: hkpoy7k.
// Bead: hk-poy7k.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// TestPasteInjectImplementerResume_SinglePaste is the primary regression guard
// for hk-poy7k. It verifies that pasteInjectOnLaunch with
// ReviewLoopPhaseImplementerResume produces exactly ONE WriteToPane call when
// both the task file and the prior-iteration feedback file are present.
//
// Two calls would indicate the pre-fix two-message approach was restored,
// reintroducing the inter-message race.
func TestPasteInjectImplementerResume_SinglePaste(t *testing.T) {
	t.Parallel()

	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\nDo the thing.\n")
	// iterCount=2 → prior iter=1 → "reviewer-feedback.iter-1.md"
	pasteInjectFixtureTaskFile(t, wtPath, "reviewer-feedback.iter-1.md", "# Feedback\nFix the race.\n")

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	const sessionID = "hkpoy7k-single-paste"
	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhaseImplementerResume,
		2, wtPath,
	)
	<-briefDelivered

	calls := adapter.calls()
	if len(calls) != 1 {
		t.Fatalf("implementer-resume: expected 1 WriteToPane call (combined task+feedback, hk-poy7k), got %d "+
			"(2 calls = inter-message race re-introduced; 0 calls = inject skipped)", len(calls))
	}
}

// TestPasteInjectImplementerResume_BothMessagesReadable verifies that the
// single combined paste payload contains both the task instruction and the
// reviewer feedback as distinct readable content. The implementer must be
// able to find references to both files in one message.
func TestPasteInjectImplementerResume_BothMessagesReadable(t *testing.T) {
	t.Parallel()

	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\nDo the thing.\n")
	pasteInjectFixtureTaskFile(t, wtPath, "reviewer-feedback.iter-1.md", "# Feedback\nFix the race.\n")

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	const sessionID = "hkpoy7k-both-readable"
	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhaseImplementerResume,
		2, wtPath,
	)
	<-briefDelivered

	calls := adapter.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	payload := calls[0].payload

	// Task content must be readable.
	if !strings.Contains(payload, "agent-task.md") {
		t.Errorf("payload missing task reference: %q", payload)
	}
	// Feedback content must be readable as distinct content in the same message.
	if !strings.Contains(payload, "reviewer-feedback.iter-1.md") {
		t.Errorf("payload missing feedback reference: %q", payload)
	}
	// The two sections must be separated by a blank line so they are visually
	// distinct to the implementer.
	if !strings.Contains(payload, "\n\n") {
		t.Errorf("payload %q: expected blank-line separator between task and feedback sections", payload)
	}

	// Buffer name must use "task" purpose (not "feedback" — that was the old
	// second buffer; combined paste uses the task buffer, hk-poy7k).
	// T8: daemon-run delivery routes through the AIS InputPort.SubmitInput → the
	// single AIS input buffer (per-phase name is now keeper/CLI-only per PL-021d).
	wantBuf := daemon.ExportedInputBufferName()
	if calls[0].bufferName != wantBuf {
		t.Errorf("bufferName = %q, want %q", calls[0].bufferName, wantBuf)
	}
}

// TestPasteInjectImplementerResume_FeedbackMissing_TaskOnlyFallback verifies
// that when the prior-iteration feedback file does not exist, the inject
// degrades gracefully to the task-only form: 1 call, mentions agent-task.md,
// does NOT mention a feedback file.
func TestPasteInjectImplementerResume_FeedbackMissing_TaskOnlyFallback(t *testing.T) {
	t.Parallel()

	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\nDo the thing.\n")
	// Intentionally do NOT create "reviewer-feedback.iter-1.md".

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	const sessionID = "hkpoy7k-feedback-missing"
	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhaseImplementerResume,
		2, wtPath,
	)
	<-briefDelivered

	calls := adapter.calls()
	if len(calls) != 1 {
		t.Fatalf("feedback missing: expected 1 WriteToPane call (task-only fallback), got %d", len(calls))
	}
	payload := calls[0].payload

	if !strings.Contains(payload, "agent-task.md") {
		t.Errorf("task-only fallback: payload missing task reference: %q", payload)
	}
	if strings.Contains(payload, "reviewer-feedback") {
		t.Errorf("task-only fallback: payload unexpectedly mentions reviewer-feedback: %q", payload)
	}
}

// TestPasteInjectImplementerResume_FeedbackIter verifies that the feedback
// section of the combined paste references the correct prior-iteration number.
// iterCount=3 → prior iter=2 → "reviewer-feedback.iter-2.md".
func TestPasteInjectImplementerResume_FeedbackIter(t *testing.T) {
	t.Parallel()

	wtPath := t.TempDir()
	pasteInjectFixtureTaskFile(t, wtPath, "agent-task.md", "# Task\nDo the thing.\n")
	// iterCount=3 → prior iter=2 → "reviewer-feedback.iter-2.md"
	pasteInjectFixtureTaskFile(t, wtPath, "reviewer-feedback.iter-2.md", "# Feedback iter 2\nFix more things.\n")

	adapter := &pasteInjectFixtureAdapter{}
	sub := pasteInjectFixtureSubstrate(t, adapter)

	const sessionID = "hkpoy7k-iter3"
	briefDelivered := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, sessionID,
		handlercontract.ReviewLoopPhaseImplementerResume,
		3, wtPath,
	)
	<-briefDelivered

	calls := adapter.calls()
	if len(calls) != 1 {
		t.Fatalf("iter-3 resume: expected 1 WriteToPane call, got %d", len(calls))
	}
	payload := calls[0].payload

	if !strings.Contains(payload, "reviewer-feedback.iter-2.md") {
		t.Errorf("iter-3 resume: payload does not mention reviewer-feedback.iter-2.md: %q", payload)
	}
	// Must NOT reference iter-1 or iter-3 (wrong iteration numbers).
	if strings.Contains(payload, "reviewer-feedback.iter-1.md") {
		t.Errorf("iter-3 resume: payload incorrectly mentions iter-1 feedback: %q", payload)
	}
	if strings.Contains(payload, "reviewer-feedback.iter-3.md") {
		t.Errorf("iter-3 resume: payload incorrectly mentions iter-3 feedback: %q", payload)
	}
}
