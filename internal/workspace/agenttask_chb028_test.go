package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// WriteAgentTask tests (CHB-028)
// ---------------------------------------------------------------------------

// TestCHB028_FreshWrite verifies that WriteAgentTask creates agent-task.md
// with the expected content on a fresh workspace.
func TestCHB028_FreshWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:        "hk-abc01",
		Title:         "Implement feature X",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000001",
		WorkspacePath: workspacePath,
		Body:          "Implement the X subsystem per the spec.",
	}

	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask (fresh): %v", err)
	}

	target := AgentTaskPath(workspacePath)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile agent-task.md: %v", err)
	}
	content := string(data)

	checks := []string{
		"# Harmonik Task",
		"bead_id: hk-abc01",
		"title: Implement feature X",
		"phase: implementer-initial",
		"iteration: 1",
		"run_id: 018e1234-0000-7000-8000-000000000001",
		"workspace_path: " + workspacePath,
		"## Task Description",
		"Implement the X subsystem per the spec.",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("CHB-028 fresh: content missing %q\ngot:\n%s", check, content)
		}
	}

	// Prior-Iteration Context section MUST NOT be present for implementer-initial.
	if strings.Contains(content, "## Prior-Iteration Context") {
		t.Errorf("CHB-028 fresh: unexpected Prior-Iteration Context section for implementer-initial")
	}
}

// TestCHB028_LaunchOverwrites verifies that a second WriteAgentTask call on the
// same workspace overwrites the existing agent-task.md. Review-loop phase
// transitions (impl → reviewer → impl-resume) reuse the same worktree; each
// launch writes content for its own (run_id, phase, iteration) tuple.
func TestCHB028_LaunchOverwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	first := AgentTaskPayload{
		BeadID:        "hk-abc02",
		Title:         "First Task",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000002",
		WorkspacePath: workspacePath,
		Body:          "First body.",
	}
	if err := WriteAgentTask(workspacePath, first); err != nil {
		t.Fatalf("WriteAgentTask first write: %v", err)
	}

	second := first
	second.Phase = "reviewer"
	second.Body = "Reviewer body."
	if err := WriteAgentTask(workspacePath, second); err != nil {
		t.Fatalf("WriteAgentTask second (overwrite) write: %v", err)
	}

	got, err := os.ReadFile(AgentTaskPath(workspacePath))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "Reviewer body.") {
		t.Errorf("second launch should have overwritten file; got %q", string(got))
	}
	if strings.Contains(string(got), "First body.") {
		t.Errorf("first-launch body should no longer be present; got %q", string(got))
	}
}

// TestCHB028_ReAttachIdempotency verifies that WriteAgentTask with ReAttach=true
// is a no-op when agent-task.md already exists.
func TestCHB028_ReAttachIdempotency(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:        "hk-abc03",
		Title:         "Task",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000003",
		WorkspacePath: workspacePath,
		Body:          "Original body.",
		ReAttach:      false,
	}

	// First write.
	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask (initial): %v", err)
	}

	// Record original content.
	original, err := os.ReadFile(AgentTaskPath(workspacePath))
	if err != nil {
		t.Fatalf("ReadFile original: %v", err)
	}

	// Re-attach write with different body — must NOT overwrite.
	reAttachPayload := payload
	reAttachPayload.Body = "Different body that must NOT appear."
	reAttachPayload.ReAttach = true

	if err := WriteAgentTask(workspacePath, reAttachPayload); err != nil {
		t.Fatalf("WriteAgentTask (re-attach): %v", err)
	}

	after, err := os.ReadFile(AgentTaskPath(workspacePath))
	if err != nil {
		t.Fatalf("ReadFile after re-attach: %v", err)
	}

	if string(after) != string(original) {
		t.Errorf("CHB-028 re-attach: file was overwritten; original:\n%s\nafter:\n%s",
			string(original), string(after))
	}
}

// TestCHB028_EmptyBodyRejection verifies that WriteAgentTask returns
// ErrTaskFileEmpty when Body is empty.
func TestCHB028_EmptyBodyRejection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:        "hk-abc04",
		Title:         "Task",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000004",
		WorkspacePath: workspacePath,
		Body:          "", // empty — must be rejected
	}

	err := WriteAgentTask(workspacePath, payload)
	if err == nil {
		t.Fatal("CHB-028 empty body: expected ErrTaskFileEmpty, got nil")
	}
	if !errors.Is(err, ErrTaskFileEmpty) {
		t.Errorf("CHB-028 empty body: want ErrTaskFileEmpty, got %v", err)
	}
}

// TestCHB028_AtomicTempCleanupOnFailure verifies that no orphan temp file is
// left behind when the target directory is not writable (simulating a write
// failure path).
func TestCHB028_AtomicTempCleanupOnFailure(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("CHB-028 atomic-temp-cleanup: running as root; permission test not meaningful")
	}

	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	// Make .harmonik read-only so the write will fail.
	if err := os.Chmod(harmonikDir, 0o555); err != nil {
		t.Fatalf("Chmod .harmonik read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(harmonikDir, 0o755) })

	payload := AgentTaskPayload{
		BeadID:        "hk-abc05",
		Title:         "Task",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000005",
		WorkspacePath: workspacePath,
		Body:          "Some body.",
	}

	err := WriteAgentTask(workspacePath, payload)
	if err == nil {
		t.Fatal("CHB-028 temp-cleanup: expected error on read-only dir, got nil")
	}

	// Verify no orphan .tmp-<pid> file was left in the .harmonik directory.
	entries, readErr := os.ReadDir(harmonikDir)
	if readErr != nil {
		// If ReadDir fails due to permissions, that's fine — no cleanup needed.
		return
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("CHB-028 temp-cleanup: orphan temp file left: %s", e.Name())
		}
	}
}

// TestCHB028_ImplementerResumePhase verifies that the Prior-Iteration Context
// section is present for implementer-resume, with the reviewer-feedback path.
func TestCHB028_ImplementerResumePhase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	priorFile := filepath.Join(workspacePath, ".harmonik", "review.iter-1.json")
	payload := AgentTaskPayload{
		BeadID:              "hk-abc06",
		Title:               "Resume task",
		Phase:               "implementer-resume",
		Iteration:           2,
		RunID:               "018e1234-0000-7000-8000-000000000006",
		WorkspacePath:       workspacePath,
		Body:                "Continue the work.",
		PriorVerdictFile:    priorFile,
		PriorVerdictSummary: "REQUEST_CHANGES — address flagged issues",
	}

	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask (implementer-resume): %v", err)
	}

	data, _ := os.ReadFile(AgentTaskPath(workspacePath))
	content := string(data)

	if !strings.Contains(content, "## Prior-Iteration Context") {
		t.Error("CHB-028 resume: missing Prior-Iteration Context section")
	}
	if !strings.Contains(content, "reviewer-feedback: "+priorFile) {
		t.Errorf("CHB-028 resume: missing reviewer-feedback path; content:\n%s", content)
	}
	if !strings.Contains(content, "REQUEST_CHANGES") {
		t.Errorf("CHB-028 resume: missing verdict summary; content:\n%s", content)
	}
}

// TestCHB028_ReviewerPhase verifies that the Prior-Iteration Context section
// contains review_base_sha and review_head_sha for the reviewer phase.
func TestCHB028_ReviewerPhase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:        "hk-abc07",
		Title:         "Review task",
		Phase:         "reviewer",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000007",
		WorkspacePath: workspacePath,
		Body:          "Review the implementation.",
		ReviewBaseSHA: "aabbccdd1111",
		ReviewHeadSHA: "eeff22223333",
	}

	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask (reviewer): %v", err)
	}

	data, _ := os.ReadFile(AgentTaskPath(workspacePath))
	content := string(data)

	if !strings.Contains(content, "## Prior-Iteration Context") {
		t.Error("CHB-028 reviewer: missing Prior-Iteration Context section")
	}
	if !strings.Contains(content, "review_base_sha: aabbccdd1111") {
		t.Errorf("CHB-028 reviewer: missing review_base_sha; content:\n%s", content)
	}
	if !strings.Contains(content, "review_head_sha: eeff22223333") {
		t.Errorf("CHB-028 reviewer: missing review_head_sha; content:\n%s", content)
	}
}

// TestHkJvzc2_WriteAgentTaskDoesNotTouchGitignore verifies that WriteAgentTask
// does NOT create or modify a .gitignore at the workspace path. Per hk-jvzc2,
// .harmonik/agent-task* exclusion is an operator-setup obligation (covered by
// the parent repo's root .gitignore which already lists /.harmonik/). Earlier
// revisions of WriteAgentTask appended the entry per-launch; that silent edit
// leaked into the parent repo's working tree across dogfood runs.
func TestHkJvzc2_WriteAgentTaskDoesNotTouchGitignore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	// Seed a pre-existing operator-style .gitignore so we can assert byte-equality.
	gitignorePath := filepath.Join(workspacePath, ".gitignore")
	seed := "# operator setup\n.harmonik/\n"
	if err := os.WriteFile(gitignorePath, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:        "hk-abc08",
		Title:         "Task",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000008",
		WorkspacePath: workspacePath,
		Body:          "Do work.",
	}

	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask: %v", err)
	}

	//nolint:gosec // G304: controlled test path
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("ReadFile .gitignore: %v", err)
	}
	if string(data) != seed {
		t.Errorf("hk-jvzc2: WriteAgentTask mutated .gitignore:\nwant:\n%q\ngot:\n%q", seed, string(data))
	}
}

// ---------------------------------------------------------------------------
// WriteReviewerFeedback tests (EM-015d-RFD)
// ---------------------------------------------------------------------------

// TestCHB028_SessionCompletionInstruction verifies that every agent-task.md
// includes the ## Session Completion section with the /quit instruction.
//
// This is the mechanism that unblocks the daemon's workloop (hk-cmybm): the
// Stop hook fires on /quit, which delivers outcome_emitted to the daemon socket.
func TestCHB028_SessionCompletionInstruction(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"implementer-initial", "implementer-resume", "reviewer"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			// Each subtest gets its own workspace dir to avoid rename conflicts.
			subDir := t.TempDir()
			subWorkspacePath := filepath.Join(subDir, "workspace")
			subHarmonikDir := filepath.Join(subWorkspacePath, ".harmonik")
			if err := os.MkdirAll(subHarmonikDir, 0o750); err != nil {
				t.Fatalf("MkdirAll .harmonik phase=%q: %v", phase, err)
			}

			payload := AgentTaskPayload{
				BeadID:        "hk-abc09",
				Title:         "Task",
				Phase:         phase,
				Iteration:     1,
				RunID:         "018e1234-0000-7000-8000-000000000009",
				WorkspacePath: subWorkspacePath,
				Body:          "Do the work.",
			}

			if err := WriteAgentTask(subWorkspacePath, payload); err != nil {
				t.Fatalf("WriteAgentTask phase=%q: %v", phase, err)
			}

			data, err := os.ReadFile(AgentTaskPath(subWorkspacePath))
			if err != nil {
				t.Fatalf("ReadFile phase=%q: %v", phase, err)
			}
			content := string(data)

			if !strings.Contains(content, "## Session Completion") {
				t.Errorf("phase=%q: missing ## Session Completion section; content:\n%s", phase, content)
			}
			if !strings.Contains(content, "/quit") {
				t.Errorf("phase=%q: missing /quit instruction; content:\n%s", phase, content)
			}
			// Verify the instruction is imperative (Claude must run /quit, not report it).
			if !strings.Contains(content, "You MUST run `/quit`") {
				t.Errorf("phase=%q: instruction not imperative (must contain 'You MUST run `/quit`'); content:\n%s", phase, content)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WriteReviewerFeedback tests (EM-015d-RFD)
// ---------------------------------------------------------------------------

// TestEM015dRFD_FreshWrite verifies that WriteReviewerFeedback creates the
// reviewer-feedback file with the correct content.
func TestEM015dRFD_FreshWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := ReviewerFeedbackPayload{
		WorkspacePath:  workspacePath,
		PriorIteration: 1,
		Verdict:        "REQUEST_CHANGES",
		Flags:          []string{"missing-tests", "wrong-abstraction"},
		Notes:          "The implementation is missing unit tests and adds unnecessary abstraction.",
		DiffHash:       "abcdef1234567890",
		DiffLines:      42,
	}

	if err := WriteReviewerFeedback(payload); err != nil {
		t.Fatalf("WriteReviewerFeedback: %v", err)
	}

	target := ReviewerFeedbackPath(workspacePath, 1)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile reviewer-feedback: %v", err)
	}
	content := string(data)

	checks := []string{
		"# Reviewer feedback — iteration 1",
		"verdict: REQUEST_CHANGES",
		"- missing-tests",
		"- wrong-abstraction",
		"The implementation is missing unit tests",
		"diff_hash: abcdef1234567890",
		"diff_lines: 42",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("EM-015d-RFD: missing %q in reviewer-feedback:\n%s", check, content)
		}
	}
}

// TestEM015dRFD_EmptyFlags verifies that empty flags are rendered as "(none)".
func TestEM015dRFD_EmptyFlags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := ReviewerFeedbackPayload{
		WorkspacePath:  workspacePath,
		PriorIteration: 2,
		Verdict:        "APPROVE",
		Flags:          nil,
		Notes:          "Looks good.",
	}

	if err := WriteReviewerFeedback(payload); err != nil {
		t.Fatalf("WriteReviewerFeedback (empty flags): %v", err)
	}

	data, _ := os.ReadFile(ReviewerFeedbackPath(workspacePath, 2))
	if !strings.Contains(string(data), "(none)") {
		t.Errorf("EM-015d-RFD empty flags: expected '(none)'; got:\n%s", string(data))
	}
}

// ---------------------------------------------------------------------------
// WriteReviewTarget tests (EM-015d-RIA)
// ---------------------------------------------------------------------------

// TestEM015dRIA_Iteration1 verifies that WriteReviewTarget for iteration 1
// omits the Prior verdicts section.
func TestEM015dRIA_Iteration1(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := ReviewTargetPayload{
		WorkspacePath: workspacePath,
		BeadID:        "hk-def01",
		Iteration:     1,
		BeadTitle:     "Implement foo",
		BeadBody:      "Implement the foo subsystem.",
		BaseSHA:       "basesha111",
		HeadSHA:       "headsha222",
		PriorVerdicts: nil,
	}

	if err := WriteReviewTarget(payload); err != nil {
		t.Fatalf("WriteReviewTarget (iter 1): %v", err)
	}

	target := ReviewTargetPath(workspacePath)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile review-target.md: %v", err)
	}
	content := string(data)

	checks := []string{
		"# Review target — bead hk-def01, iteration 1",
		"## Bead",
		"id: hk-def01",
		"title: Implement foo",
		"Implement the foo subsystem.",
		"## Diff range",
		"base: basesha111",
		"head: headsha222",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("EM-015d-RIA iter1: missing %q; content:\n%s", check, content)
		}
	}

	// Prior verdicts section MUST NOT be present for iteration 1.
	if strings.Contains(content, "## Prior verdicts") {
		t.Errorf("EM-015d-RIA iter1: unexpected Prior verdicts section")
	}
}

// TestEM015dRIA_Iteration2 verifies that WriteReviewTarget for iteration ≥ 2
// includes the Prior verdicts section.
func TestEM015dRIA_Iteration2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := ReviewTargetPayload{
		WorkspacePath: workspacePath,
		BeadID:        "hk-def02",
		Iteration:     2,
		BeadTitle:     "Implement bar",
		BeadBody:      "Implement the bar subsystem.",
		BaseSHA:       "basesha333",
		HeadSHA:       "headsha444",
		PriorVerdicts: []ReviewTargetPriorVerdict{
			{
				Iteration:    1,
				Verdict:      "REQUEST_CHANGES",
				Flags:        []string{"missing-tests"},
				NotesSummary: "Tests are missing.",
			},
		},
	}

	if err := WriteReviewTarget(payload); err != nil {
		t.Fatalf("WriteReviewTarget (iter 2): %v", err)
	}

	data, _ := os.ReadFile(ReviewTargetPath(workspacePath))
	content := string(data)

	if !strings.Contains(content, "## Prior verdicts") {
		t.Error("EM-015d-RIA iter2: missing Prior verdicts section")
	}
	if !strings.Contains(content, "### Iteration 1") {
		t.Error("EM-015d-RIA iter2: missing iteration 1 sub-heading")
	}
	if !strings.Contains(content, "verdict: REQUEST_CHANGES") {
		t.Errorf("EM-015d-RIA iter2: missing verdict; content:\n%s", content)
	}
	if !strings.Contains(content, "missing-tests") {
		t.Errorf("EM-015d-RIA iter2: missing flags; content:\n%s", content)
	}
}

// TestEM015dRIA_ReviewerHints verifies that the Hints section is included when
// ReviewerHints is non-empty and omitted when empty.
func TestEM015dRIA_ReviewerHints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := ReviewTargetPayload{
		WorkspacePath: workspacePath,
		BeadID:        "hk-def03",
		Iteration:     1,
		BeadTitle:     "Task with hints",
		BeadBody:      "Do the task.",
		BaseSHA:       "basesha555",
		HeadSHA:       "headsha666",
		ReviewerHints: "Pay special attention to error handling paths.",
	}

	if err := WriteReviewTarget(payload); err != nil {
		t.Fatalf("WriteReviewTarget (hints): %v", err)
	}

	data, _ := os.ReadFile(ReviewTargetPath(workspacePath))
	content := string(data)

	if !strings.Contains(content, "## Hints") {
		t.Error("EM-015d-RIA hints: missing ## Hints section")
	}
	if !strings.Contains(content, "Pay special attention to error handling paths.") {
		t.Errorf("EM-015d-RIA hints: missing hints text; content:\n%s", content)
	}

	// Write again without hints — overwrite path, section MUST be absent.
	payload2 := payload
	payload2.ReviewerHints = ""
	if err := WriteReviewTarget(payload2); err != nil {
		t.Fatalf("WriteReviewTarget (no hints): %v", err)
	}
	data2, _ := os.ReadFile(ReviewTargetPath(workspacePath))
	if strings.Contains(string(data2), "## Hints") {
		t.Error("EM-015d-RIA no-hints: unexpected ## Hints section when ReviewerHints is empty")
	}
}

// ---------------------------------------------------------------------------
// Additional unit tests for hk-rfda7 step (a) — coverage uplift
// ---------------------------------------------------------------------------

// TestCHB028_ExtraContextSection verifies that the ## Extra Context section is
// rendered when ExtraContext is non-empty and is omitted entirely when empty.
// Covers the hk-boiwe ExtraContext branch in buildAgentTaskContent.
func TestCHB028_ExtraContextSection(t *testing.T) {
	t.Parallel()

	t.Run("non-empty extra context included", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		workspacePath := filepath.Join(dir, "workspace")
		harmonikDir := filepath.Join(workspacePath, ".harmonik")
		if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		payload := AgentTaskPayload{
			BeadID:        "hk-ec01",
			Title:         "Task with extra context",
			Phase:         "implementer-initial",
			Iteration:     1,
			RunID:         "018e1234-0000-7000-8000-000000000ec1",
			WorkspacePath: workspacePath,
			Body:          "Implement the feature.",
			ExtraContext:  "predecessor: abc1234\nnote: dependency hk-xyz landed in main",
		}

		if err := WriteAgentTask(workspacePath, payload); err != nil {
			t.Fatalf("WriteAgentTask: %v", err)
		}

		data, _ := os.ReadFile(AgentTaskPath(workspacePath))
		content := string(data)

		if !strings.Contains(content, "## Extra Context") {
			t.Error("ExtraContext: missing ## Extra Context section")
		}
		if !strings.Contains(content, "predecessor: abc1234") {
			t.Errorf("ExtraContext: missing extra context body; content:\n%s", content)
		}
		// Verify Extra Context appears AFTER Task Description.
		descIdx := strings.Index(content, "## Task Description")
		extraIdx := strings.Index(content, "## Extra Context")
		if descIdx < 0 || extraIdx < 0 || extraIdx < descIdx {
			t.Errorf("ExtraContext: ## Extra Context must follow ## Task Description; desc@%d extra@%d", descIdx, extraIdx)
		}
	})

	t.Run("empty extra context omitted", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		workspacePath := filepath.Join(dir, "workspace")
		harmonikDir := filepath.Join(workspacePath, ".harmonik")
		if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		payload := AgentTaskPayload{
			BeadID:        "hk-ec02",
			Title:         "Task without extra context",
			Phase:         "implementer-initial",
			Iteration:     1,
			RunID:         "018e1234-0000-7000-8000-000000000ec2",
			WorkspacePath: workspacePath,
			Body:          "Do the work.",
			ExtraContext:  "", // omitted
		}

		if err := WriteAgentTask(workspacePath, payload); err != nil {
			t.Fatalf("WriteAgentTask: %v", err)
		}

		data, _ := os.ReadFile(AgentTaskPath(workspacePath))
		if strings.Contains(string(data), "## Extra Context") {
			t.Error("ExtraContext empty: unexpected ## Extra Context section when ExtraContext is empty")
		}
	})

	t.Run("whitespace-only extra context omitted", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		workspacePath := filepath.Join(dir, "workspace")
		harmonikDir := filepath.Join(workspacePath, ".harmonik")
		if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		payload := AgentTaskPayload{
			BeadID:        "hk-ec03",
			Title:         "Task with whitespace extra context",
			Phase:         "implementer-initial",
			Iteration:     1,
			RunID:         "018e1234-0000-7000-8000-000000000ec3",
			WorkspacePath: workspacePath,
			Body:          "Do the work.",
			ExtraContext:  "   \n\t  ", // whitespace-only — must be treated as empty
		}

		if err := WriteAgentTask(workspacePath, payload); err != nil {
			t.Fatalf("WriteAgentTask: %v", err)
		}

		data, _ := os.ReadFile(AgentTaskPath(workspacePath))
		if strings.Contains(string(data), "## Extra Context") {
			t.Error("ExtraContext whitespace: unexpected ## Extra Context section for whitespace-only value")
		}
	})
}

// TestCHB028_AutoCreatesDotHarmonikDir verifies that WriteAgentTask creates
// the .harmonik/ directory automatically when it does not already exist, per
// the MkdirAll call in WriteAgentTask.
//
// This is a regression guard: a workspace path that has no .harmonik/ dir
// must not fail — the function creates it.
func TestCHB028_AutoCreatesDotHarmonikDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// workspacePath exists but .harmonik/ is deliberately NOT pre-created.
	workspacePath := filepath.Join(dir, "workspace-no-harmonik")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:        "hk-ah01",
		Title:         "Auto-create test",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000ah1",
		WorkspacePath: workspacePath,
		Body:          "Body for auto-create test.",
	}

	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask (no pre-existing .harmonik): %v", err)
	}

	target := AgentTaskPath(workspacePath)
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatalf("agent-task.md not created: %v", err)
	}
	if fi.Size() == 0 {
		t.Error("agent-task.md is zero bytes after WriteAgentTask auto-create path")
	}
}

// TestCHB028_WhitespaceBodyRejected verifies that a body consisting entirely
// of whitespace (spaces, tabs, newlines) is rejected with ErrTaskFileEmpty.
// Covers the strings.TrimSpace guard in WriteAgentTask.
func TestCHB028_WhitespaceBodyRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:        "hk-wb01",
		Title:         "Whitespace body",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000wb1",
		WorkspacePath: workspacePath,
		Body:          "   \n\t\n   ", // whitespace-only body
	}

	err := WriteAgentTask(workspacePath, payload)
	if err == nil {
		t.Fatal("expected ErrTaskFileEmpty for whitespace-only body, got nil")
	}
	if !errors.Is(err, ErrTaskFileEmpty) {
		t.Errorf("want ErrTaskFileEmpty, got %v", err)
	}
}

// TestCHB028_ReAttachWritesWhenFileAbsent verifies that WriteAgentTask with
// ReAttach=true writes the file normally when agent-task.md does NOT yet exist.
// This covers the "re-attach but file absent" branch — the first-time write
// must succeed even when ReAttach is set.
func TestCHB028_ReAttachWritesWhenFileAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:        "hk-ra01",
		Title:         "ReAttach-absent test",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000ra1",
		WorkspacePath: workspacePath,
		Body:          "Body that must be written on first re-attach.",
		ReAttach:      true, // file is absent — must write
	}

	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask (ReAttach=true, file absent): %v", err)
	}

	data, err := os.ReadFile(AgentTaskPath(workspacePath))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "Body that must be written on first re-attach.") {
		t.Errorf("ReAttach-absent: body not written; content:\n%s", string(data))
	}
}

// TestCHB028_ImplementerResumeWithDerivedVerdictPath verifies that when
// PriorVerdictFile is empty, buildAgentTaskContent derives the canonical path
// from WorkspacePath and priorN per the fallback branch.
func TestCHB028_ImplementerResumeWithDerivedVerdictPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:           "hk-dv01",
		Title:            "Resume — derived verdict path",
		Phase:            "implementer-resume",
		Iteration:        3,
		RunID:            "018e1234-0000-7000-8000-000000000dv1",
		WorkspacePath:    workspacePath,
		Body:             "Continue the work.",
		PriorVerdictFile: "", // empty — derived path should appear
	}

	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask (derived verdict path): %v", err)
	}

	data, _ := os.ReadFile(AgentTaskPath(workspacePath))
	content := string(data)

	// Derived path: .harmonik/review.iter-2.json (iteration 3 → prior = 2).
	expectedPath := filepath.Join(workspacePath, ".harmonik", "review.iter-2.json")
	if !strings.Contains(content, "reviewer-feedback: "+expectedPath) {
		t.Errorf("derived verdict path: missing derived reviewer-feedback path %q; content:\n%s",
			expectedPath, content)
	}
}

// TestCHB028_ImplementerResumeIterationClamp verifies that when Iteration ≤ 1,
// the priorN value is clamped to 1 (no negative or zero iteration files).
// Covers the `if priorN < 1 { priorN = 1 }` branch in buildAgentTaskContent.
func TestCHB028_ImplementerResumeIterationClamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Iteration=1 means priorN = 0, which should clamp to 1.
	payload := AgentTaskPayload{
		BeadID:           "hk-ic01",
		Title:            "Iteration clamp test",
		Phase:            "implementer-resume",
		Iteration:        1,
		RunID:            "018e1234-0000-7000-8000-000000000ic1",
		WorkspacePath:    workspacePath,
		Body:             "Clamped iteration body.",
		PriorVerdictFile: "", // trigger derived-path branch
	}

	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask (iteration clamp): %v", err)
	}

	data, _ := os.ReadFile(AgentTaskPath(workspacePath))
	content := string(data)

	// Clamped: priorN=1 → .harmonik/review.iter-1.json, NOT review.iter-0.json.
	expectedPath := filepath.Join(workspacePath, ".harmonik", "review.iter-1.json")
	if !strings.Contains(content, "reviewer-feedback: "+expectedPath) {
		t.Errorf("iteration clamp: expected derived path %q; content:\n%s", expectedPath, content)
	}
	// Paranoia: zero-index path must not appear.
	zeroPath := filepath.Join(workspacePath, ".harmonik", "review.iter-0.json")
	if strings.Contains(content, zeroPath) {
		t.Errorf("iteration clamp: unexpected zero-index path %q in content", zeroPath)
	}
}

// TestEM015dRFD_WritesMultipleIterations verifies that WriteReviewerFeedback
// can write feedback files for successive iterations (iter 1 and iter 2) to
// the same workspace, and that each file is independently addressable via
// ReviewerFeedbackPath. This exercises the iteration-indexed path logic.
func TestEM015dRFD_WritesMultipleIterations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	for _, iter := range []int{1, 2} {
		payload := ReviewerFeedbackPayload{
			WorkspacePath:  workspacePath,
			PriorIteration: iter,
			Verdict:        "APPROVE",
			Notes:          "Iteration notes.",
		}
		if err := WriteReviewerFeedback(payload); err != nil {
			t.Fatalf("WriteReviewerFeedback iter=%d: %v", iter, err)
		}
	}

	// Both files must exist independently.
	for _, iter := range []int{1, 2} {
		target := ReviewerFeedbackPath(workspacePath, iter)
		fi, err := os.Stat(target)
		if err != nil {
			t.Errorf("reviewer-feedback.iter-%d.md not found: %v", iter, err)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("reviewer-feedback.iter-%d.md is zero bytes", iter)
		}
	}

	// Sanity: iter-1 and iter-2 are distinct paths.
	if ReviewerFeedbackPath(workspacePath, 1) == ReviewerFeedbackPath(workspacePath, 2) {
		t.Error("ReviewerFeedbackPath must return distinct paths for different iterations")
	}
}

// TestEM015dRIA_WriteTargetOverwrite verifies that WriteReviewTarget overwrites
// review-target.md on each reviewer launch (not appended). The second write's
// content must fully replace the first write's content.
func TestEM015dRIA_WriteTargetOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	first := ReviewTargetPayload{
		WorkspacePath: workspacePath,
		BeadID:        "hk-ov01",
		Iteration:     1,
		BeadTitle:     "First reviewer launch",
		BeadBody:      "First body text.",
		BaseSHA:       "base111",
		HeadSHA:       "head111",
	}
	if err := WriteReviewTarget(first); err != nil {
		t.Fatalf("WriteReviewTarget (first): %v", err)
	}

	second := ReviewTargetPayload{
		WorkspacePath: workspacePath,
		BeadID:        "hk-ov01",
		Iteration:     2,
		BeadTitle:     "Second reviewer launch",
		BeadBody:      "Second body text.",
		BaseSHA:       "base222",
		HeadSHA:       "head222",
	}
	if err := WriteReviewTarget(second); err != nil {
		t.Fatalf("WriteReviewTarget (second): %v", err)
	}

	data, _ := os.ReadFile(ReviewTargetPath(workspacePath))
	content := string(data)

	if strings.Contains(content, "First body text.") {
		t.Error("WriteReviewTarget overwrite: first-launch body still present after second write")
	}
	if !strings.Contains(content, "Second body text.") {
		t.Errorf("WriteReviewTarget overwrite: second-launch body missing; content:\n%s", content)
	}
}

// TestEM015dRIA_MultiplePriorVerdictsWithNilFlags verifies that WriteReviewTarget
// renders multiple prior verdicts correctly, and that a nil Flags slice in a
// prior verdict renders as "(none)" rather than panicking or emitting empty.
func TestEM015dRIA_MultiplePriorVerdictsWithNilFlags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	payload := ReviewTargetPayload{
		WorkspacePath: workspacePath,
		BeadID:        "hk-mv01",
		Iteration:     3,
		BeadTitle:     "Three-iteration task",
		BeadBody:      "Implement the multi-iteration feature.",
		BaseSHA:       "base333",
		HeadSHA:       "head333",
		PriorVerdicts: []ReviewTargetPriorVerdict{
			{
				Iteration:    1,
				Verdict:      "REQUEST_CHANGES",
				Flags:        []string{"missing-tests", "spec-drift"},
				NotesSummary: "Tests are missing and spec is drifted.",
			},
			{
				Iteration:    2,
				Verdict:      "REQUEST_CHANGES",
				Flags:        nil, // nil flags — must render as "(none)"
				NotesSummary: "Still some issues.",
			},
		},
	}

	if err := WriteReviewTarget(payload); err != nil {
		t.Fatalf("WriteReviewTarget (multiple prior verdicts): %v", err)
	}

	data, _ := os.ReadFile(ReviewTargetPath(workspacePath))
	content := string(data)

	// Both iterations must appear.
	if !strings.Contains(content, "### Iteration 1") {
		t.Error("multi-verdict: missing ### Iteration 1")
	}
	if !strings.Contains(content, "### Iteration 2") {
		t.Error("multi-verdict: missing ### Iteration 2")
	}

	// Flags from iter 1.
	if !strings.Contains(content, "missing-tests") {
		t.Error("multi-verdict: missing 'missing-tests' flag from iteration 1")
	}
	if !strings.Contains(content, "spec-drift") {
		t.Error("multi-verdict: missing 'spec-drift' flag from iteration 1")
	}

	// Nil flags for iter 2 must render as "(none)".
	if !strings.Contains(content, "(none)") {
		t.Errorf("multi-verdict: nil flags for iteration 2 must render as '(none)'; content:\n%s", content)
	}

	// Verdict file paths must be present for both iterations.
	verdictPath1 := filepath.Join(workspacePath, ".harmonik", "review.iter-1.json")
	verdictPath2 := filepath.Join(workspacePath, ".harmonik", "review.iter-2.json")
	if !strings.Contains(content, verdictPath1) {
		t.Errorf("multi-verdict: missing verdict file path for iter 1 (%s)", verdictPath1)
	}
	if !strings.Contains(content, verdictPath2) {
		t.Errorf("multi-verdict: missing verdict file path for iter 2 (%s)", verdictPath2)
	}
}

// TestEM015dRIA_CoverageCheckSection verifies that WriteReviewTarget always
// includes the Coverage Check section instructing the reviewer to detect
// partial 'all-X' changes (hk-hay).
func TestEM015dRIA_CoverageCheckSection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	payload := ReviewTargetPayload{
		WorkspacePath: workspacePath,
		BeadID:        "hk-hay",
		Iteration:     1,
		BeadTitle:     "update all handlers for new interface",
		BeadBody:      "Update all call sites to use the new interface.",
		BaseSHA:       "base001",
		HeadSHA:       "head001",
	}

	if err := WriteReviewTarget(payload); err != nil {
		t.Fatalf("WriteReviewTarget: %v", err)
	}

	data, err := os.ReadFile(ReviewTargetPath(workspacePath))
	if err != nil {
		t.Fatalf("ReadFile review-target.md: %v", err)
	}
	content := string(data)

	// Coverage Check section must be present.
	checks := []string{
		"## Coverage Check",
		"all-inclusive language",
		"incomplete-coverage",
		"REQUEST_CHANGES",
		"grep",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("coverage-check section: missing %q; content:\n%s", check, content)
		}
	}

	// Coverage Check must appear before the Bead section so it is read first.
	coverageIdx := strings.Index(content, "## Coverage Check")
	beadIdx := strings.Index(content, "## Bead")
	if coverageIdx == -1 {
		t.Fatal("coverage-check section not found")
	}
	if beadIdx == -1 {
		t.Fatal("bead section not found")
	}
	if coverageIdx > beadIdx {
		t.Error("coverage-check section must appear before the Bead section")
	}
}
