package daemon_test

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestPiResumeSeedPrompt_DeliversReviewerFeedback verifies the c073 fix: a pi
// resume turn (priorSessionID != nil) delivers a seed prompt that points the
// implementer at the prior iteration's reviewer-feedback file and demands a new
// commit — instead of reusing the identical initial prompt it already satisfied.
func TestPiResumeSeedPrompt_DeliversReviewerFeedback(t *testing.T) {
	t.Parallel()

	sessionID := "pi-session-resume-xyz"
	rc := daemon.ExportedPiRunCtx{
		WorkspacePath:    "/tmp/wt-test-pi-resume-fb",
		BeadID:           "hk-fb001",
		Provider:         "openrouter",
		Model:            "openrouter/qwen/qwen3-coder",
		APIKeyEnv:        "OPENROUTER_API_KEY",
		PriorSessionID:   &sessionID,
		IterationCount:   2, // priorIter = 1 → reviewer-feedback.iter-1.md
		BaseEnv:          []string{"PATH=/usr/bin"},
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	seed := spec.Args[len(spec.Args)-1]

	if !strings.Contains(seed, "reviewer-feedback.iter-1.md") {
		t.Errorf("pi resume seed prompt must reference reviewer-feedback.iter-1.md; got:\n%s", seed)
	}
	if !strings.Contains(seed, rc.BeadID) || !strings.Contains(strings.ToLower(seed), "refs") {
		t.Errorf("pi resume seed prompt must keep the Refs:<bead> commit instruction; got:\n%s", seed)
	}
	if !strings.Contains(strings.ToLower(seed), "commit") {
		t.Errorf("pi resume seed prompt must demand a new commit; got:\n%s", seed)
	}
}

// TestPiInitialSeedPrompt_NoReviewerFeedback guards against the resume prompt
// leaking onto the initial turn: an initial launch (priorSessionID == nil) must
// NOT reference the reviewer-feedback file.
func TestPiInitialSeedPrompt_NoReviewerFeedback(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedPiRunCtx{
		WorkspacePath:    "/tmp/wt-test-pi-initial-fb",
		BeadID:           "hk-fb002",
		Provider:         "openrouter",
		Model:            "openrouter/qwen/qwen3-coder",
		APIKeyEnv:        "OPENROUTER_API_KEY",
		BaseEnv:          []string{"PATH=/usr/bin"},
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	seed := spec.Args[len(spec.Args)-1]

	if strings.Contains(seed, "reviewer-feedback") {
		t.Errorf("pi initial seed prompt must NOT reference reviewer-feedback; got:\n%s", seed)
	}
}

// TestCodexResumeSeedPrompt_DeliversReviewerFeedback is the codex peer of the pi
// test: a resume turn (priorThreadID != nil) delivers the reviewer-feedback
// pointer for the prior iteration.
func TestCodexResumeSeedPrompt_DeliversReviewerFeedback(t *testing.T) {
	t.Parallel()

	threadID := "codex-thread-resume-xyz"
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-resume-fb",
		BeadID:           "hk-fb003",
		PriorThreadID:    &threadID,
		IterationCount:   3, // priorIter = 2 → reviewer-feedback.iter-2.md
		BaseEnv:          []string{"PATH=/usr/bin"},
		CodexHome:        t.TempDir(),
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	seed := spec.Args[len(spec.Args)-1]

	if !strings.Contains(seed, "reviewer-feedback.iter-2.md") {
		t.Errorf("codex resume seed prompt must reference reviewer-feedback.iter-2.md; got:\n%s", seed)
	}
	if !strings.Contains(seed, rc.BeadID) || !strings.Contains(strings.ToLower(seed), "refs") {
		t.Errorf("codex resume seed prompt must keep the Refs:<bead> commit instruction; got:\n%s", seed)
	}
}

// TestCodexInitialSeedPrompt_NoReviewerFeedback guards the codex initial turn.
func TestCodexInitialSeedPrompt_NoReviewerFeedback(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-initial-fb",
		BeadID:           "hk-fb004",
		BaseEnv:          []string{"PATH=/usr/bin"},
		CodexHome:        t.TempDir(),
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	seed := spec.Args[len(spec.Args)-1]

	if strings.Contains(seed, "reviewer-feedback") {
		t.Errorf("codex initial seed prompt must NOT reference reviewer-feedback; got:\n%s", seed)
	}
}
