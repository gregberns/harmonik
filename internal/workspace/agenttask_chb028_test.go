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

// TestCHB028_CollisionRejection verifies that a pre-existing agent-task.md on a
// fresh launch (ReAttach=false) returns ErrTaskFileCollision.
func TestCHB028_CollisionRejection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	payload := AgentTaskPayload{
		BeadID:        "hk-abc02",
		Title:         "Task",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-000000000002",
		WorkspacePath: workspacePath,
		Body:          "Some body.",
		ReAttach:      false,
	}

	// First write — must succeed.
	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask first write: %v", err)
	}

	// Second write on same path without ReAttach — must return ErrTaskFileCollision.
	err := WriteAgentTask(workspacePath, payload)
	if err == nil {
		t.Fatal("CHB-028 collision: expected ErrTaskFileCollision, got nil")
	}
	if !errors.Is(err, ErrTaskFileCollision) {
		t.Errorf("CHB-028 collision: want ErrTaskFileCollision, got %v", err)
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

// TestCHB028_GitignoreUpdated verifies that WriteAgentTask adds the
// .harmonik/agent-task* gitignore line to the worktree .gitignore.
func TestCHB028_GitignoreUpdated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workspacePath := filepath.Join(dir, "workspace")
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
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
		t.Fatalf("WriteAgentTask (gitignore): %v", err)
	}

	gitignorePath := filepath.Join(workspacePath, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("ReadFile .gitignore: %v", err)
	}
	if !strings.Contains(string(data), AgentTaskGitignoreLine) {
		t.Errorf("CHB-028 gitignore: missing %q in .gitignore:\n%s", AgentTaskGitignoreLine, string(data))
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
