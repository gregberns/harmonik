package daemon_test

// crossharness_seedprompt_test.go — pi/codex resume-seed-prompt PARITY, asserted
// through both harnesses' launch specs in one table.
//
// Was agentseedprompt_test.go. Its production half (ImplementerResumeSeedPrompt)
// moved to internal/harness/shared/seedprompt.go in P2 unit E1a, leaving a test
// file named after a file that no longer exists here. The direct unit tests for
// the builder — including the iteration clamp — now live next to it in
// internal/harness/shared/seedprompt_test.go. What is left here is the thing
// that could never move: an INTEGRATION table proving the two harnesses deliver
// the same reviewer-feedback pointer through their own launch-spec builders.
// Same shape as crossharness_empty_model_test.go, hence the same name.
//
// It stays in package daemon_test until BOTH harnesses are extracted (E1a-1 for
// codex, E1c for pi); until then it is the only place the parity is pinned, and
// it reaches each harness through the daemon's Exported* seams. Once pi is out
// too it moves to a cross-harness test package.
//
// Ref: plans/2026-07-21-p2-extraction/E1a-codex-harness.md §1.3.

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
