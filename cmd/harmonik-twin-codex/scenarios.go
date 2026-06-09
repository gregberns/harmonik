// Scenario implementations for harmonik-twin-codex (codex-harness C6/T15, hk-of3h4).
//
// Each scenario simulates one codex exec outcome variant used in C2 acceptance
// criteria (C2-codex-adapter-spec.md §AC2.3–AC2.5) and the C6 twin spec
// (C6-migration-test-spec.md §Approach).
//
// # The four variants
//
//   - trailer-commit  — twin emits thread.started + turn.completed, then makes
//     a worktree commit with Refs:<beadID> in the message trailer.  Exercises
//     C2 AC2.3: codex committed with Refs; shared merge lands it on process exit.
//
//   - edits-no-commit — twin emits thread.started + turn.completed, writes a
//     file to the worktree but does NOT commit.  Exercises C2 AC2.4: adapter's
//     commit-after-exit fallback must create the Refs commit.
//
//   - no-edits        — twin emits thread.started + turn.completed, makes no
//     worktree changes.  Exercises C2 AC2.5: noChange path fires, bead reopened.
//
//   - turn-failed     — twin emits thread.started + turn.failed.  Exercises C2
//     edge case: turn.failed → run_failed.
//
// # Deterministic thread_id
//
// The twin uses a fixed, scenario-scoped thread_id (not random) so scenario
// tests produce byte-reproducible JSONL streams (C6-migration-test-spec.md §Error
// handling: "Twin must be deterministic").
//
// Cite: codex-harness C2-codex-adapter-spec.md §AC2.3–AC2.5;
// codex-harness C6-migration-test-spec.md §Approach, §Error handling.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Scenario name constants — use these in tests and --scenario flag values.
const (
	ScenarioTrailerCommit  = "trailer-commit"
	ScenarioEditsNoCommit  = "edits-no-commit"
	ScenarioNoEdits        = "no-edits"
	ScenarioTurnFailed     = "turn-failed"
)

// threadIDForScenario returns a deterministic thread_id for the given scenario
// name. The ID is fixed per scenario so JSONL streams are byte-reproducible.
func threadIDForScenario(scenario string) string {
	return "codex-twin-" + scenario
}

// scenarioConfig carries the runtime parameters for a scenario execution.
type scenarioConfig struct {
	// worktreePath is the -C / --cd working directory for git operations.
	// Required for trailer-commit and edits-no-commit; ignored for no-edits
	// and turn-failed.
	worktreePath string

	// beadID is the bead identifier inserted into the Refs: trailer for the
	// trailer-commit variant. If empty the twin emits a placeholder trailer.
	beadID string
}

// runScenario drives the named scenario, writing codex JSONL to w and
// optionally performing git operations in cfg.worktreePath.
//
// Returns an error if the scenario name is unrecognised or if a scenario step
// fails (e.g., git error for trailer-commit).
func runScenario(w io.Writer, name string, cfg scenarioConfig) error {
	e := newCodexEmitter(w)
	threadID := threadIDForScenario(name)

	switch name {
	case ScenarioTrailerCommit:
		return runTrailerCommit(e, threadID, cfg)
	case ScenarioEditsNoCommit:
		return runEditsNoCommit(e, threadID, cfg)
	case ScenarioNoEdits:
		return runNoEdits(e, threadID)
	case ScenarioTurnFailed:
		return runTurnFailed(e, threadID)
	default:
		return fmt.Errorf("unknown scenario %q: must be one of %s, %s, %s, %s",
			name, ScenarioTrailerCommit, ScenarioEditsNoCommit, ScenarioNoEdits, ScenarioTurnFailed)
	}
}

// runTrailerCommit implements the trailer-commit variant.
//
// Emits thread.started + turn.completed, then commits a sentinel file with a
// Refs:<beadID> trailer in cfg.worktreePath.  Returns nil even when git exits
// non-zero (the twin emits turn.completed before the commit step, mirroring
// real codex where commit is a model decision after the turn concludes).
func runTrailerCommit(e *codexEmitter, threadID string, cfg scenarioConfig) error {
	if err := e.emitThreadStarted(threadID); err != nil {
		return fmt.Errorf("trailer-commit: emit thread.started: %w", err)
	}
	if err := e.emitTurnCompleted(); err != nil {
		return fmt.Errorf("trailer-commit: emit turn.completed: %w", err)
	}

	if cfg.worktreePath == "" {
		// No worktree supplied — caller cannot assert git side-effects.
		// Twin still exits cleanly; scenario test that needs git must pass -C.
		return nil
	}

	return commitWithRefsTrailer(cfg.worktreePath, cfg.beadID)
}

// runEditsNoCommit implements the edits-no-commit variant.
//
// Emits thread.started + turn.completed, then writes a sentinel file to
// cfg.worktreePath without committing.  The adapter's commit-after-exit
// fallback (C2 AC2.4) must create the Refs commit.
func runEditsNoCommit(e *codexEmitter, threadID string, cfg scenarioConfig) error {
	if err := e.emitThreadStarted(threadID); err != nil {
		return fmt.Errorf("edits-no-commit: emit thread.started: %w", err)
	}
	if err := e.emitTurnCompleted(); err != nil {
		return fmt.Errorf("edits-no-commit: emit turn.completed: %w", err)
	}

	if cfg.worktreePath == "" {
		return nil
	}

	// Write a sentinel file but do NOT commit.
	ts := strconv.FormatInt(time.Now().UnixNano(), 10)
	name := ".harmonik-twin-codex-edit-" + ts
	path := filepath.Join(cfg.worktreePath, name)
	//nolint:gosec // G306: sentinel file is world-readable; not sensitive.
	if err := os.WriteFile(path, []byte("codex-twin edits-no-commit "+ts+"\n"), 0o644); err != nil {
		return fmt.Errorf("edits-no-commit: write sentinel: %w", err)
	}
	return nil
}

// runNoEdits implements the no-edits variant.
//
// Emits thread.started + turn.completed with no worktree changes.
// The noChange path in the shared loop fires (C2 AC2.5).
func runNoEdits(e *codexEmitter, threadID string) error {
	if err := e.emitThreadStarted(threadID); err != nil {
		return fmt.Errorf("no-edits: emit thread.started: %w", err)
	}
	return e.emitTurnCompleted()
}

// runTurnFailed implements the turn-failed variant.
//
// Emits thread.started + turn.failed.  The adapter maps turn.failed to
// run_failed (C2 edge case).
func runTurnFailed(e *codexEmitter, threadID string) error {
	if err := e.emitThreadStarted(threadID); err != nil {
		return fmt.Errorf("turn-failed: emit thread.started: %w", err)
	}
	return e.emitTurnFailed("codex-twin: turn.failed scenario simulation")
}

// ─────────────────────────────────────────────────────────────────────────────
// Git helper (trailer-commit)
// ─────────────────────────────────────────────────────────────────────────────

// commitWithRefsTrailer writes a sentinel file and commits it in worktreePath
// with a Refs:<beadID> trailer.  Git author/committer identity is set via env
// vars to avoid touching the project's git config (same approach as the claude
// twin's commit_on_cue step).
func commitWithRefsTrailer(worktreePath, beadID string) error {
	ts := strconv.FormatInt(time.Now().UnixNano(), 10)
	sentinelName := ".harmonik-twin-codex-commit-" + ts
	sentinelPath := filepath.Join(worktreePath, sentinelName)
	//nolint:gosec // G306: sentinel file is world-readable; not sensitive.
	if err := os.WriteFile(sentinelPath, []byte("codex-twin commit-on-cue "+ts+"\n"), 0o644); err != nil {
		return fmt.Errorf("commitWithRefsTrailer: write sentinel: %w", err)
	}

	gitEnv := append(os.Environ(), //nolint:gocritic // appendAssign: intentional new slice
		"GIT_AUTHOR_NAME=harmonik-twin-codex",
		"GIT_AUTHOR_EMAIL=codex-twin@harmonik.local",
		"GIT_COMMITTER_NAME=harmonik-twin-codex",
		"GIT_COMMITTER_EMAIL=codex-twin@harmonik.local",
	)

	addCmd := exec.Command("git", "add", sentinelName) //nolint:gosec // G204: sentinelName is a timestamp-derived literal
	addCmd.Dir = worktreePath
	addCmd.Env = gitEnv
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commitWithRefsTrailer: git add: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	refsLine := "hk-twin"
	if beadID != "" {
		refsLine = beadID
	}
	commitMsg := "codex twin commit\n\nRefs: " + refsLine

	commitCmd := exec.Command("git", "commit", "-m", commitMsg) //nolint:gosec // G204: commitMsg is a controlled literal
	commitCmd.Dir = worktreePath
	commitCmd.Env = gitEnv
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("commitWithRefsTrailer: git commit: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
