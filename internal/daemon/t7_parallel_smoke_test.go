package daemon_test

// t7_parallel_smoke_test.go — N=2 parallelism smoke test (hk-e61c3.4).
//
// TestParallelSmoke_TwoBeadsConcurrent is the roadmap row 7 integration test:
// two ready beads, MaxConcurrent=2, both close before daemon.Start returns.
// Uses daemon.Start at the binary-entrypoint level (real br adapter, real git
// worktrees, real handler subprocess via the twin /bin/sh exit-0 pattern).
//
// Acceptance criteria (hk-e61c3.4 bead body):
//   - Both beads close before Start returns.
//   - Both runs emit run_started AND run_completed in JSONL with distinct
//     run_id values in the payload.
//   - Worktree paths (workspace_path in run_started payload) are distinct.
//   - go test -race is clean for internal/daemon/, internal/eventbus/,
//     internal/handlercontract/.
//
// Design:
//   - Two beads seeded via `br create` with status=open.
//   - daemon.Start called with MaxConcurrent=2 and a baked handler.sh that
//     sleeps briefly (0.2 s) so both goroutines are simultaneously in-flight.
//   - Context cancelled once both beads are confirmed closed in SQLite AND
//     both run_completed events appear in JSONL.
//   - JSONL is parsed to extract run_id (from payload) and workspace_path
//     (from run_started payload) for the distinct-values assertions.
//
// Helper prefix: parallelSmokeFixture (per implementer-protocol.md
// §Helper-prefix discipline; bead hk-e61c3.4).

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// parallelSmokeFixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// parallelSmokeFixtureLocateBr finds the real `br` binary via exec.LookPath.
// Skips the test when br is not available (same pattern as smokeFixtureBrPath).
func parallelSmokeFixtureLocateBr(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for N=2 parallel smoke test (not on PATH); CI sets br on PATH")
	}
	return brPath
}

// parallelSmokeFixtureSleepHandlerScript writes a /bin/sh script to t.TempDir()
// that sleeps briefly then exits 0.  The sleep ensures both goroutines are
// simultaneously in-flight before either closes its bead.
func parallelSmokeFixtureSleepHandlerScript(t *testing.T) string {
	t.Helper()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "handler.sh")
	content := "#!/bin/sh\nsleep 0.3\nexit 0\n"
	//nolint:gosec // G306: script is test-only, chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("parallelSmokeFixtureSleepHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// parallelSmokeFixtureSetup initialises the full fixture for the parallel smoke
// test: git repo, .harmonik dirs, br init, two seeded beads, br wrapper script.
// Returns projectDir, jsonlPath, brWrapperPath, and the two bead IDs.
func parallelSmokeFixtureSetup(t *testing.T) (projectDir, jsonlPath, brWrapper, beadID1, beadID2 string) {
	t.Helper()

	realBrPath := parallelSmokeFixtureLocateBr(t)

	// Create project directory and standard sub-trees.
	projectDir, jsonlPath = smokeFixtureProjectDir(t)
	smokeFixtureGitRepo(t, projectDir)

	// br init — run with cmd.Dir = projectDir so br creates .beads/ there.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "ps")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("parallelSmokeFixtureSetup: br init: %v\n%s", initErr, initOut)
	}

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper = smokeFixtureBrWrapperScript(t, realBrPath, dbPath)

	// Seed two ready beads.
	beadID1 = parallelSmokeFixtureCreateBead(t, brWrapper, "parallel smoke bead 1")
	beadID2 = parallelSmokeFixtureCreateBead(t, brWrapper, "parallel smoke bead 2")
	t.Logf("parallelSmoke: seeded bead1=%s bead2=%s", beadID1, beadID2)

	return projectDir, jsonlPath, brWrapper, beadID1, beadID2
}

// parallelSmokeFixtureCreateBead creates a single bead via brWrapper and
// returns its ID.
func parallelSmokeFixtureCreateBead(t *testing.T, brWrapper, title string) string {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), brWrapper, "create", title, "--status", "open", "--silent")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("parallelSmokeFixtureCreateBead: br create %q: %v\n%s", title, err, out)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		t.Fatalf("parallelSmokeFixtureCreateBead: br create %q returned empty ID", title)
	}
	return id
}

// parallelSmokeFixturePollBeadClosed polls `br show <id>` for up to budget and
// returns true if the bead reaches "closed" status.
func parallelSmokeFixturePollBeadClosed(t *testing.T, brWrapper, beadID string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		//nolint:gosec // G204: br args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", beadID, "--format", "json")
		out, err := cmd.Output()
		if err == nil {
			var items []struct {
				Status string `json:"status"`
			}
			if jsonErr := json.Unmarshal(out, &items); jsonErr == nil && len(items) == 1 {
				if items[0].Status == "closed" {
					return true
				}
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	return false
}

// parallelSmokeFixtureCountRunTerminalEvents polls the JSONL file for
// run_completed or run_failed events and returns the count found.  Polling
// stops when count >= target or budget expires.
func parallelSmokeFixtureCountRunTerminalEvents(t *testing.T, jsonlPath string, target int, budget time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		count := parallelSmokeFixtureCountTerminalInJSONL(t, jsonlPath)
		if count >= target {
			return count
		}
		time.Sleep(15 * time.Millisecond)
	}
	return parallelSmokeFixtureCountTerminalInJSONL(t, jsonlPath)
}

// parallelSmokeFixtureCountTerminalInJSONL reads the JSONL file and counts
// run_completed and run_failed events (both are terminal events).
func parallelSmokeFixtureCountTerminalInJSONL(t *testing.T, jsonlPath string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		return 0 // file may not exist yet
	}
	defer func() { _ = f.Close() }()
	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `"run_completed"`) || strings.Contains(line, `"run_failed"`) {
			count++
		}
	}
	return count
}

// parallelSmokeRunStartedEntry holds the run_id and workspace_path extracted
// from a run_started JSONL line.
type parallelSmokeRunStartedEntry struct {
	runID         string
	workspacePath string
}

// parallelSmokeFixtureExtractRunStarted reads the JSONL file and extracts
// run_id and workspace_path from every run_started event's payload.
func parallelSmokeFixtureExtractRunStarted(t *testing.T, jsonlPath string) []parallelSmokeRunStartedEntry {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("parallelSmokeFixtureExtractRunStarted: open %s: %v", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()

	// Each JSONL line is a core.Event envelope. The payload is a JSON object
	// containing run_id and workspace_path (workloopRunStartedPayload).
	type envelope struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	type startedPayload struct {
		RunID         string `json:"run_id"`
		WorkspacePath string `json:"workspace_path"`
	}

	var entries []parallelSmokeRunStartedEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"run_started"`) {
			continue
		}
		var env envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Type != "run_started" {
			continue
		}
		var pl startedPayload
		if err := json.Unmarshal(env.Payload, &pl); err != nil {
			continue
		}
		entries = append(entries, parallelSmokeRunStartedEntry{
			runID:         pl.RunID,
			workspacePath: pl.WorkspacePath,
		})
	}
	return entries
}

// parallelSmokeFixtureExtractRunCompletedRunIDs reads the JSONL file and
// extracts run_id from every run_completed or run_failed event payload.
func parallelSmokeFixtureExtractRunCompletedRunIDs(t *testing.T, jsonlPath string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("parallelSmokeFixtureExtractRunCompletedRunIDs: open %s: %v", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()

	type envelope struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	type completedPayload struct {
		RunID string `json:"run_id"`
	}

	var runIDs []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"run_completed"`) && !strings.Contains(line, `"run_failed"`) {
			continue
		}
		var env envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Type != "run_completed" && env.Type != "run_failed" {
			continue
		}
		var pl completedPayload
		if err := json.Unmarshal(env.Payload, &pl); err != nil {
			continue
		}
		runIDs = append(runIDs, pl.RunID)
	}
	return runIDs
}

// ─────────────────────────────────────────────────────────────────────────────
// TestParallelSmoke_TwoBeadsConcurrent
// ─────────────────────────────────────────────────────────────────────────────

// TestParallelSmoke_TwoBeadsConcurrent is the row 7 N=2 smoke test.
//
// It exercises daemon.Start with MaxConcurrent=2 and two ready beads, asserting
// that:
//   - Both beads close before Start returns.
//   - Both run_started events appear in JSONL with distinct run_id values.
//   - Both run_completed (or run_failed) events appear with the same two distinct
//     run_id values observed in run_started.
//   - The workspace_path in each run_started payload is distinct.
//
// The handler sleeps 0.3 s before exiting 0, ensuring both goroutines are
// simultaneously in-flight when the work loop is at capacity.
//
// Spec ref: POST_MVH_PARALLELISM_ROADMAP.md row 7.
// Bead ref: hk-e61c3.4.
func TestParallelSmoke_TwoBeadsConcurrent(t *testing.T) {
	t.Parallel()

	projectDir, jsonlPath, brWrapper, beadID1, beadID2 :=
		parallelSmokeFixtureSetup(t)

	handlerScript := parallelSmokeFixtureSleepHandlerScript(t)

	cfg := daemon.Config{
		ProjectDir:    projectDir,
		JSONLLogPath:  jsonlPath,
		BrPath:        brWrapper,
		HandlerBinary: handlerScript,
		HandlerEnv:    nil,
		MaxConcurrent: 2,
	}

	// Launch daemon.Start in a goroutine.  It blocks until loopCtx is cancelled.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Phase 1: wait for both beads to be closed in SQLite (30 s budget).
	const closeBudget = 30 * time.Second
	closed1 := parallelSmokeFixturePollBeadClosed(t, brWrapper, beadID1, closeBudget)
	closed2 := parallelSmokeFixturePollBeadClosed(t, brWrapper, beadID2, closeBudget)

	// Phase 2: after both beads are confirmed closed in SQLite, wait for both
	// run_completed/run_failed events to appear in JSONL before cancelling.
	// (Same rationale as smokeFixturePollRunTerminal: avoids race between bead
	// close and event emission hk-c1ln2.)
	if closed1 && closed2 {
		_ = parallelSmokeFixtureCountRunTerminalEvents(t, jsonlPath, 2, 5*time.Second)
	}

	// Cancel the work loop.
	loopCancel()

	// Wait for daemon.Start to return.
	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("daemon.Start did not return within 5 s after context cancel")
	}

	// ── Assert both beads closed ───────────────────────────────────────────────
	if !closed1 {
		t.Errorf("bead1 %s was not closed within %s", beadID1, closeBudget)
	}
	if !closed2 {
		t.Errorf("bead2 %s was not closed within %s", beadID2, closeBudget)
	}

	// ── Assert JSONL contains two run_started events with distinct run_ids ─────
	startedEntries := parallelSmokeFixtureExtractRunStarted(t, jsonlPath)
	if len(startedEntries) < 2 {
		t.Fatalf("expected >= 2 run_started events in JSONL; got %d; lines: %v",
			len(startedEntries), parallelSmokeFixtureReadAllJSONLLines(t, jsonlPath))
	}

	// Collect distinct run_ids from run_started events.
	startedRunIDs := make(map[string]struct{})
	for _, entry := range startedEntries {
		if entry.runID == "" {
			t.Errorf("run_started event has empty run_id; entry: %+v", entry)
		}
		startedRunIDs[entry.runID] = struct{}{}
	}
	if len(startedRunIDs) < 2 {
		t.Errorf("run_started events do not have distinct run_id values; got run_ids: %v", startedRunIDs)
	}

	// ── Assert distinct workspace_path values ──────────────────────────────────
	startedPaths := make(map[string]struct{})
	for _, entry := range startedEntries {
		if entry.workspacePath == "" {
			t.Errorf("run_started event has empty workspace_path; entry: %+v", entry)
		}
		startedPaths[entry.workspacePath] = struct{}{}
	}
	if len(startedPaths) < 2 {
		t.Errorf("run_started events do not have distinct workspace_path values; got paths: %v", startedPaths)
	}

	// ── Assert two run_completed/run_failed events with matching run_ids ───────
	completedRunIDs := parallelSmokeFixtureExtractRunCompletedRunIDs(t, jsonlPath)
	if len(completedRunIDs) < 2 {
		t.Fatalf("expected >= 2 run_completed/run_failed events in JSONL; got %d", len(completedRunIDs))
	}

	completedRunIDSet := make(map[string]struct{})
	for _, id := range completedRunIDs {
		if id == "" {
			t.Errorf("run_completed/run_failed event has empty run_id")
		}
		completedRunIDSet[id] = struct{}{}
	}
	if len(completedRunIDSet) < 2 {
		t.Errorf("run_completed/run_failed events do not have distinct run_id values; got: %v", completedRunIDSet)
	}

	// Every run_id in run_completed must also appear in run_started.
	for id := range completedRunIDSet {
		if _, ok := startedRunIDs[id]; !ok {
			t.Errorf("run_completed run_id %q does not appear in any run_started event; started IDs: %v", id, startedRunIDs)
		}
	}

	t.Logf("parallelSmoke: bead1_closed=%v bead2_closed=%v run_started_count=%d "+
		"distinct_run_ids=%d distinct_workspace_paths=%d run_terminal_count=%d",
		closed1, closed2, len(startedEntries), len(startedRunIDs),
		len(startedPaths), len(completedRunIDs))
}

// parallelSmokeFixtureReadAllJSONLLines reads all non-empty lines from jsonlPath
// for diagnostic logging in test failures.
func parallelSmokeFixtureReadAllJSONLLines(t *testing.T, jsonlPath string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
