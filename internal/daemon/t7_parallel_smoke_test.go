package daemon_test

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

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

func parallelSmokeFixtureLocateBr(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for N=2 parallel smoke test (not on PATH); CI sets br on PATH")
	}
	return brPath
}

func parallelSmokeFixtureSleepHandlerScript(t *testing.T) string {
	t.Helper()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "handler.sh")
	content := `#!/bin/sh
set -e
sleep 0.3
bead_id=$(grep '^bead_id:' .harmonik/agent-task.md | awk '{print $2}') 2>/dev/null
echo "smoke: ${bead_id}" > "smoke-${bead_id}.txt"
git add "smoke-${bead_id}.txt" >&2
git commit -m "smoke: handler commit for ${bead_id}

Refs: ${bead_id}" >&2
exit 0
`
	//nolint:gosec // G306: script is test-only, chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("parallelSmokeFixtureSleepHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

func parallelSmokeFixtureSetup(t *testing.T) (projectDir, jsonlPath, brWrapper, beadID1, beadID2 string) {
	t.Helper()

	realBrPath := parallelSmokeFixtureLocateBr(t)

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

	beadID1 = parallelSmokeFixtureCreateBead(t, brWrapper, "parallel smoke bead 1")
	beadID2 = parallelSmokeFixtureCreateBead(t, brWrapper, "parallel smoke bead 2")
	t.Logf("parallelSmoke: seeded bead1=%s bead2=%s", beadID1, beadID2)

	return projectDir, jsonlPath, brWrapper, beadID1, beadID2
}

func parallelSmokeFixtureCreateBead(t *testing.T, brWrapper, title string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), brWrapper,
		"create", title, "--status", "open", "--labels", "workflow:single", "--silent")
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

func parallelSmokeFixturePollBeadClosed(t *testing.T, brWrapper, beadID string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
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

type parallelSmokeRunStartedEntry struct {
	runID         string
	workspacePath string
}

func parallelSmokeFixtureExtractRunStarted(t *testing.T, jsonlPath string) []parallelSmokeRunStartedEntry {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("parallelSmokeFixtureExtractRunStarted: open %s: %v", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()

	type startedPayload struct {
		WorkspacePath string `json:"workspace_path"`
	}

	var entries []parallelSmokeRunStartedEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"run_started"`) {
			continue
		}
		var ev core.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type != "run_started" {
			continue
		}
		if ev.RunID == nil {
			continue
		}
		var pl startedPayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			continue
		}
		entries = append(entries, parallelSmokeRunStartedEntry{
			runID:         ev.RunID.String(),
			workspacePath: pl.WorkspacePath,
		})
	}
	return entries
}

func parallelSmokeFixtureExtractRunCompletedRunIDs(t *testing.T, jsonlPath string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("parallelSmokeFixtureExtractRunCompletedRunIDs: open %s: %v", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()

	var runIDs []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"run_completed"`) && !strings.Contains(line, `"run_failed"`) {
			continue
		}
		var ev core.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type != "run_completed" && ev.Type != "run_failed" {
			continue
		}
		if ev.RunID == nil {
			continue
		}
		runIDs = append(runIDs, ev.RunID.String())
	}
	return runIDs
}

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
// Spec ref: POST_OPERATIONAL_PARALLELISM_ROADMAP.md row 7.
// Bead ref: hk-e61c3.4.
func TestParallelSmoke_TwoBeadsConcurrent(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, jsonlPath, brWrapper, beadID1, beadID2 := parallelSmokeFixtureSetup(t)

	handlerScript := parallelSmokeFixtureSleepHandlerScript(t)

	cfg := daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		HandlerEnv:          nil,
		MaxConcurrent:       2,
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	const closeBudget = 30 * time.Second
	closed1 := parallelSmokeFixturePollBeadClosed(t, brWrapper, beadID1, closeBudget)
	closed2 := parallelSmokeFixturePollBeadClosed(t, brWrapper, beadID2, closeBudget)

	if closed1 && closed2 {
		_ = parallelSmokeFixtureCountRunTerminalEvents(t, jsonlPath, 2, 5*time.Second)
	}

	loopCancel()

	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error after context cancel: %v", err)
		}
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Errorf("daemon.Start did not return within %s after context cancel", daemon.ExportedDaemonExitHangBudget)
	}

	if !closed1 {
		t.Errorf("bead1 %s was not closed within %s", beadID1, closeBudget)
	}
	if !closed2 {
		t.Errorf("bead2 %s was not closed within %s", beadID2, closeBudget)
	}

	startedEntries := parallelSmokeFixtureExtractRunStarted(t, jsonlPath)
	if len(startedEntries) < 2 {
		t.Fatalf("expected >= 2 run_started events in JSONL; got %d; lines: %v",
			len(startedEntries), parallelSmokeFixtureReadAllJSONLLines(t, jsonlPath))
	}

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

	for id := range completedRunIDSet {
		if _, ok := startedRunIDs[id]; !ok {
			t.Errorf("run_completed run_id %q does not appear in any run_started event; started IDs: %v", id, startedRunIDs)
		}
	}

	for idStr := range startedRunIDs {
		var rid core.RunID
		if err := rid.UnmarshalText([]byte(idStr)); err != nil {
			t.Errorf("parallelSmoke: run_id %q is not a valid RunID: %v", idStr, err)
			continue
		}
		var filteredCount int
		for range eventbus.Filter(jsonlPath, rid) {
			filteredCount++
		}
		if filteredCount < 2 {
			t.Errorf("eventbus.Filter: run_id %q yielded %d events, want >= 2 "+
				"(run_started + terminal); envelope run_id must be populated (hk-a6nob)", idStr, filteredCount)
		}
	}

	t.Logf("parallelSmoke: bead1_closed=%v bead2_closed=%v run_started_count=%d "+
		"distinct_run_ids=%d distinct_workspace_paths=%d run_terminal_count=%d",
		closed1, closed2, len(startedEntries), len(startedRunIDs),
		len(startedPaths), len(completedRunIDs))
}

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
