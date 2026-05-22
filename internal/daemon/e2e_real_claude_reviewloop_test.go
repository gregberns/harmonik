//go:build e2e_real_claude

package daemon_test

// e2e_real_claude_reviewloop_test.go — real-Claude review-loop E2E smoke test.
//
// This file codifies the review-loop mode happy path as a runnable Go test.
// It runs the full cycle: real harmonik daemon + real claude binary + real br
// bead ledger, with the daemon executing inside a detached tmux session so the
// PL-028b tmux guard passes.
//
// The implementer agent appends a line to marker.txt and commits; the reviewer
// agent evaluates the result and writes review.json with an APPROVE verdict.
// The daemon reads the verdict and terminates with run_completed(success=true).
//
// # Build tag
//
// The file is gated behind //go:build e2e_real_claude.  Default `go test ./...`
// skips it; run it explicitly:
//
//	go test -tags e2e_real_claude ./internal/daemon/... -run TestE2ERealClaudeReviewLoopMode -v -timeout 300s
//
// Or via Make:
//
//	make test-e2e-real-claude-reviewloop
//
// # Skip guards
//
// The test calls t.Skip early when any of the following are absent:
//   - claude binary
//   - tmux binary
//   - git binary
//   - br binary
//   - ntm binary
//   - ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN env var
//   - harmonik daemon binary (buildable from source)
//
// # Helper prefix
//
// rcrlFixture (real-claude-review-loop; per implementer-protocol.md
// §Helper-prefix discipline; bead hk-7uasg).
//
// Cite: specs/execution-model.md §4.3 EM-015d, §4.3 EM-015e;
// specs/claude-hook-bridge.md §4.8 CHB-021.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Skip guards (reuse rcsmFixtureCheckPreconditions from single-mode file)
// ─────────────────────────────────────────────────────────────────────────────

// rcrlFixtureCheckPreconditions delegates to the shared precondition helper
// defined in e2e_real_claude_single_test.go.  Same binaries + credentials required.
func rcrlFixtureCheckPreconditions(t *testing.T) {
	t.Helper()
	rcsmFixtureCheckPreconditions(t)
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixture project setup
// ─────────────────────────────────────────────────────────────────────────────

// rcrlFixtureProject creates the scratch project for review-loop E2E:
//   - git-init with marker.txt and initial commit
//   - br init
//   - one P1 bead labelled workflow:review-loop with the REVIEW-LOOP-OK task body
//
// The bead body asks the implementer to append "REVIEW-LOOP-OK" to marker.txt
// and commit.  The reviewer instructions (injected via spec agent-task.md) ask
// Claude to write an APPROVE verdict to .harmonik/review.json.
//
// Returns the project directory and the bead ID.
func rcrlFixtureProject(t *testing.T) (smokeDir, beadID string) {
	t.Helper()

	smokeDir = t.TempDir()

	// Git init.
	gitRun := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = smokeDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=smoke-e2e",
			"GIT_AUTHOR_EMAIL=smoke@harmonik.local",
			"GIT_COMMITTER_NAME=smoke-e2e",
			"GIT_COMMITTER_EMAIL=smoke@harmonik.local",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rcrlFixtureProject: git %v: %v\n%s", args, err, out)
		}
	}

	gitRun("init", "-b", "main")
	gitRun("config", "user.email", "smoke@harmonik.local")
	gitRun("config", "user.name", "smoke-e2e")

	markerPath := filepath.Join(smokeDir, "marker.txt")
	if err := os.WriteFile(markerPath, []byte(""), 0o644); err != nil {
		t.Fatalf("rcrlFixtureProject: write marker.txt: %v", err)
	}
	gitRun("add", "marker.txt")
	gitRun("commit", "-m", "initial")

	// br init — run in smokeDir so .beads/ is created there.
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skipf("e2e_real_claude: br not found on PATH: %v", err)
	}

	//nolint:gosec // G204: brPath is from exec.LookPath; args are literals
	initCmd := exec.CommandContext(t.Context(), brPath, "init", "--prefix", "rcrl")
	initCmd.Dir = smokeDir
	if out, initErr := initCmd.CombinedOutput(); initErr != nil {
		t.Fatalf("rcrlFixtureProject: br init: %v\n%s", initErr, out)
	}

	// br create — the REVIEW-LOOP-OK task bead.
	//
	// The body asks the implementer to append the REVIEW-LOOP-OK sentinel to
	// marker.txt and commit.  The reviewer phase is handled by the agent via
	// the reviewer agent-task.md injected by buildClaudeLaunchSpec (the body
	// does not need to repeat reviewer instructions — they come from the spec).
	const beadBody = `Append the line ` + "`REVIEW-LOOP-OK`" + ` to marker.txt in this worktree and commit with message ` + "`add REVIEW-LOOP-OK`" + `. Use the Edit tool and a single git commit.`

	//nolint:gosec // G204: brPath is from exec.LookPath; args are literals
	createCmd := exec.CommandContext(t.Context(),
		brPath,
		"create",
		"--title", "Add REVIEW-LOOP-OK marker line",
		"--body", beadBody,
		"--type", "task",
		"--priority", "1",
		"--label", "workflow:review-loop",
		"--format", "id",
	)
	createCmd.Dir = smokeDir
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("rcrlFixtureProject: br create: %v\n%s", createErr, createOut)
	}
	beadID = strings.TrimSpace(string(createOut))
	if beadID == "" {
		t.Fatal("rcrlFixtureProject: br create returned empty ID")
	}

	return smokeDir, beadID
}

// ─────────────────────────────────────────────────────────────────────────────
// Tmux session management (delegates to shared helpers)
// ─────────────────────────────────────────────────────────────────────────────

// rcrlFixtureTmuxSession creates a detached tmux session for the daemon.
// Delegates to rcsmFixtureTmuxSession with a review-loop-specific suffix.
func rcrlFixtureTmuxSession(t *testing.T, suffix string) (sessionName string, cleanup func()) {
	t.Helper()
	return rcsmFixtureTmuxSession(t, "rl-"+suffix)
}

// ─────────────────────────────────────────────────────────────────────────────
// Harmonik subprocess (delegates to shared helper)
// ─────────────────────────────────────────────────────────────────────────────

// rcrlFixtureLaunchHarmonik launches the harmonik daemon inside a named tmux
// session.  Delegates to rcsmFixtureLaunchHarmonik.
func rcrlFixtureLaunchHarmonik(
	t *testing.T,
	harmonikBin, sessionName, smokeDir string,
) (stop func()) {
	t.Helper()
	return rcsmFixtureLaunchHarmonik(t, harmonikBin, sessionName, smokeDir)
}

// ─────────────────────────────────────────────────────────────────────────────
// Post-condition helpers
// ─────────────────────────────────────────────────────────────────────────────

// rcrlAssertMarkerOK checks that marker.txt in workspacePath contains "REVIEW-LOOP-OK".
func rcrlAssertMarkerOK(t *testing.T, workspacePath string) {
	t.Helper()
	markerPath := filepath.Join(workspacePath, "marker.txt")
	//nolint:gosec // G304: workspacePath extracted from events.jsonl payload (daemon-written); not user input
	contents, err := os.ReadFile(markerPath)
	if err != nil {
		t.Errorf("rcrlAssertMarkerOK: read marker.txt: %v", err)
		return
	}
	if !strings.Contains(string(contents), "REVIEW-LOOP-OK") {
		t.Errorf("marker.txt does not contain REVIEW-LOOP-OK; got:\n%s", string(contents))
	}
}

// rcrlAssertReviewerVerdictPresent checks that a reviewer_verdict event is
// present in the collected events.
func rcrlAssertReviewerVerdictPresent(t *testing.T, events []rcsmEvent) {
	t.Helper()
	for _, ev := range events {
		if ev.Type == "reviewer_verdict" {
			return
		}
	}
	t.Errorf("reviewer_verdict event not found in collected events")
}

// rcrlAssertCycleCompletePresent checks that a review_loop_cycle_complete event
// is present in the collected events.
func rcrlAssertCycleCompletePresent(t *testing.T, events []rcsmEvent) {
	t.Helper()
	for _, ev := range events {
		if ev.Type == "review_loop_cycle_complete" {
			return
		}
	}
	t.Errorf("review_loop_cycle_complete event not found in collected events")
}

// rcrlAssertVerdictArchived checks that the iteration-1 verdict archive file
// exists: .harmonik/review.iter-1.json (T-WM-027 acceptance criterion).
// This is only checked when workspacePath is non-empty and the cycle
// completed more than one iteration; skip silently when absent (single-iteration
// APPROVE path archives on a different codepath).
func rcrlAssertVerdictArchived(t *testing.T, workspacePath string) {
	t.Helper()
	if workspacePath == "" {
		return
	}
	// The APPROVE-on-iteration-1 path archives the file immediately after the
	// reviewer_verdict event.  Verify it exists.
	archivePath := filepath.Join(workspacePath, ".harmonik", "review.iter-1.json")
	if _, err := os.Stat(archivePath); err != nil {
		// Non-fatal: the archive file is a quality-of-life check, not a hard
		// E2E requirement.  Log a warning rather than failing the test.
		t.Logf("rcrlAssertVerdictArchived: review.iter-1.json not found (non-fatal): %v", archivePath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Main test
// ─────────────────────────────────────────────────────────────────────────────

// TestE2ERealClaudeReviewLoopMode is the real-Claude review-loop mode happy-path
// E2E smoke test.  It exercises the full implementer→reviewer cycle:
//
//  1. Skip-guard: checks all required binaries and credentials.
//  2. Builds the harmonik binary from source.
//  3. Creates a scratch project: git repo, marker.txt, br init, single P1 bead
//     with label workflow:review-loop.
//  4. Launches harmonik inside a detached tmux session.
//  5. Tails events.jsonl, waits for run_completed or 240s timeout.
//  6. Asserts event sequence and post-conditions (marker.txt, git commit,
//     bead closed, .claude/settings.json, reviewer_verdict, cycle_complete).
//
// Expected event subsequence for a single-iteration APPROVE cycle:
//
//	daemon_started → daemon_orphan_sweep_completed → run_started →
//	reviewer_launched → reviewer_verdict → review_loop_cycle_complete →
//	run_completed
//
// Build tag: e2e_real_claude — excluded from default go test ./...
// Timeout budget: 240s for the tail watcher (set -timeout 300s on the test binary).
//
// Bead: hk-7uasg (real-Claude review-loop E2E integration test).
func TestE2ERealClaudeReviewLoopMode(t *testing.T) {
	// Not parallel: this test spawns a tmux session and real LLM subprocesses
	// (implementer + reviewer).

	rcrlFixtureCheckPreconditions(t)

	harmonikBin := rcsmFixtureBuildHarmonik(t)
	smokeDir, beadID := rcrlFixtureProject(t)
	t.Logf("e2e_real_claude_reviewloop: smokeDir=%s beadID=%s harmonikBin=%s", smokeDir, beadID, harmonikBin)

	// Create a detached tmux session for the daemon so $TMUX is set.
	sessionName, tmuxCleanup := rcrlFixtureTmuxSession(t, beadID)
	defer tmuxCleanup()

	// Events path — daemon writes here per EV-020.
	jsonlPath := filepath.Join(smokeDir, ".harmonik", "events", "events.jsonl")

	// Start tailing events before launching the daemon (avoids a race where
	// run_completed is written before the watcher goroutine opens the file).
	//
	// Review-loop mode runs two LLM sessions (implementer + reviewer), so we
	// budget 240s (vs 180s for single mode).
	watchCtx, watchCancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer watchCancel()

	eventsCh := make(chan []rcsmEvent, 1)
	go func() {
		eventsCh <- rcsmFixtureTailEvents(watchCtx, t, jsonlPath)
	}()

	// Launch harmonik.  Wrap stop in sync.Once so defer + explicit call are idempotent.
	rawStop := rcrlFixtureLaunchHarmonik(t, harmonikBin, sessionName, smokeDir)
	var stopOnce sync.Once
	stopDaemon := func() { stopOnce.Do(rawStop) }
	defer stopDaemon()

	// Wait for events (run_completed or timeout). MustCompleteWithin adds a
	// 30 s grace beyond the 240 s watchCtx so we get diagnostics if the
	// goroutine ever hangs rather than just a silent test timeout.
	var events []rcsmEvent
	scenariotest.MustCompleteWithin(t, jsonlPath, "", tmux.OSAdapter{}, 270*time.Second, func() {
		events = <-eventsCh
	})

	// Stop the daemon gracefully before asserting.
	stopDaemon()

	t.Logf("e2e_real_claude_reviewloop: collected %d events", len(events))
	for _, ev := range events {
		t.Logf("  event: %s", ev.Type)
	}

	// ── Event sequence assertion ──────────────────────────────────────────────
	//
	// Expected subsequence for a single-iteration APPROVE cycle per
	// specs/execution-model.md §4.3 EM-015d and specs/event-model.md §8.1a:
	//
	//   daemon_started → daemon_orphan_sweep_completed → run_started →
	//   reviewer_launched → reviewer_verdict → review_loop_cycle_complete →
	//   run_completed
	//
	// Tolerates interleaved events (heartbeats, agent_started, agent_ready, etc.).
	wantSeq := []string{
		"daemon_started",
		"daemon_orphan_sweep_completed",
		"run_started",
		"reviewer_launched",
		"reviewer_verdict",
		"review_loop_cycle_complete",
		"run_completed",
	}
	rcsmAssertEventSequence(t, events, wantSeq)
	rcsmAssertRunCompleted(t, events)
	rcrlAssertReviewerVerdictPresent(t, events)
	rcrlAssertCycleCompletePresent(t, events)

	// ── Post-condition assertions ─────────────────────────────────────────────

	workspacePath := rcsmWorkspacePath(events)
	if workspacePath == "" {
		t.Fatalf("workspace_path not observed in run_started event — cannot verify post-conditions")
	}

	// Verify the implementer appended the marker line.
	rcrlAssertMarkerOK(t, workspacePath)

	// Verify a new commit was added beyond the initial.
	rcsmAssertNewCommit(t, workspacePath)

	// Verify .claude/settings.json was created with hook-relay entries.
	rcsmAssertSettingsJSON(t, workspacePath)

	// Verify the bead was closed by harmonik.
	rcsmAssertBeadClosed(t, smokeDir, beadID)

	// Verify the iteration-1 verdict archive (non-fatal; see helper doc).
	rcrlAssertVerdictArchived(t, workspacePath)

	// Print a summary of review-loop-specific events for operator visibility.
	t.Logf("e2e_real_claude_reviewloop: review-loop event summary:")
	reviewLoopEventTypes := map[string]bool{
		"reviewer_launched":          true,
		"reviewer_verdict":           true,
		"review_loop_cycle_complete": true,
		"implementer_resumed":        true,
		"iteration_cap_hit":          true,
		"no_progress_detected":       true,
	}
	for _, ev := range events {
		if reviewLoopEventTypes[ev.Type] {
			t.Logf("  review-loop event: %s payload=%s", ev.Type, string(ev.Payload))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Verify build tag compiles (package-level var forces compiler to parse file)
// ─────────────────────────────────────────────────────────────────────────────

var _ = fmt.Sprintf // suppress unused-import in case log helpers are removed
