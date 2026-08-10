package workspace

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// TestSessionCompletionFollowsTheHarness verifies that the ## Session Completion
// section is written in the terms of the harness that will read it.
//
// The claude case is the older half and still load-bearing: claude is a REPL
// that outlives the work, its Stop hook fires on session exit, and without an
// explicit /quit the daemon's workloop waits at sess.Wait() forever (hk-cmybm).
// The one-shot case is the newer half: a pi agent that had already committed
// obeyed the claude instruction with `echo "/quit" | pbcopy`, outlived its
// budget, and was scored as a crash (hk-quit-instruction-not-portable-ms55w).
//
// The wiring that decides WHICH of the two a real pi or codex launch gets is
// proved separately, against the daemon's own launch-spec builder.
func TestSessionCompletionFollowsTheHarness(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		completion handlercontract.CompletionMode
		wantQuit   bool
	}{
		{"claude_repl", handlercontract.CompletionEventStreamThenQuit, true},
		{"one_shot", handlercontract.CompletionProcessExit, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workspacePath := t.TempDir()
			payload := AgentTaskPayload{
				BeadID:        "hk-abc11",
				Title:         "Task",
				Phase:         "implementer-initial",
				Iteration:     1,
				RunID:         "018e1234-0000-7000-8000-00000000000b",
				WorkspacePath: workspacePath,
				Body:          "Do the work.",
				Completion:    tc.completion,
			}
			if err := WriteAgentTask(workspacePath, payload); err != nil {
				t.Fatalf("WriteAgentTask: %v", err)
			}

			content := string(mustReadFile(t, AgentTaskPath(workspacePath)))
			if !strings.Contains(content, "## Session Completion") {
				t.Fatal("no ## Session Completion section")
			}
			if got := strings.Contains(content, "/quit"); got != tc.wantQuit {
				t.Errorf("/quit present = %v, want %v", got, tc.wantQuit)
			}
		})
	}
}

// TestAgentTaskGuidanceIsImplementerOnly verifies that the generated
// agent-task.md carries the Tests and Structure guidance for every implementer
// phase and for none of the reviewer's.
//
// The guidance is the instruction surface every dispatched implementer reads,
// so a regression here is silent: nothing fails to build, and the only symptom
// is agents behaving differently weeks later. The reviewer's quality sections
// live in review-target.md, so leaking implementer test guidance into a
// read-only reviewer is instruction noise, not a harmless extra.
//
// The assertions quote the load-bearing clauses rather than the headings: the
// prohibition on bead-named test files (whose absence produced 885 of them) and
// the delete-a-bad-test-in-your-path default.
func TestAgentTaskGuidanceIsImplementerOnly(t *testing.T) {
	t.Parallel()

	clauses := []string{
		"## Tests",
		"## Structure",
		"never after a bead or ticket ID",
		"the default is to delete it",
	}

	for _, tc := range []struct {
		phase string
		want  bool
	}{
		{"implementer-initial", true},
		{"implementer-resume", true},
		{"", true}, // single-mode dispatch
		{"reviewer", false},
	} {
		t.Run("phase="+tc.phase, func(t *testing.T) {
			t.Parallel()

			workspacePath := t.TempDir()
			payload := AgentTaskPayload{
				BeadID:        "hk-abc10",
				Title:         "Task",
				Phase:         tc.phase,
				Iteration:     2,
				RunID:         "018e1234-0000-7000-8000-00000000000a",
				WorkspacePath: workspacePath,
				Body:          "Do the work.",
			}

			if err := WriteAgentTask(workspacePath, payload); err != nil {
				t.Fatalf("WriteAgentTask phase=%q: %v", tc.phase, err)
			}

			content := string(mustReadFile(t, AgentTaskPath(workspacePath)))
			for _, clause := range clauses {
				if got := strings.Contains(content, clause); got != tc.want {
					t.Errorf("phase=%q: %q present = %v, want %v", tc.phase, clause, got, tc.want)
				}
			}
		})
	}
}
