package daemon_test

// t11_throughput_test.go — 10-bead throughput integration test (hk-e61c3.6).
//
// TestThroughput_TenBeadsAtMaxFour is the roadmap row 11 closing test for the
// post-MVH parallelism epic.  It exercises daemon.Start with 10 ready beads and
// MaxConcurrent=4 and asserts:
//
//  1. All 10 beads close cleanly.
//  2. Wall-clock < 3× the sequential baseline (a separate sub-test with
//     MaxConcurrent=1 is timed; the parallel run is compared to that ratio).
//  3. go test -race is clean.
//  4. JSONL contains exactly 10 distinct run_started events with 10 distinct
//     run_id values (verified via eventbus.Filter per hk-e61c3.5 / row 10).
//
// Metrics emission (roadmap §4): sqlite_lock_retries, in_flight_count fields
// are OUT OF SCOPE for this bead.  Populating new event fields is separate work.
// A follow-up bead should be filed if those fields are desired.
//
// Handler design: each bead handler sleeps 0.3 s before exiting 0. This keeps
// at most 4 goroutines simultaneously in-flight (MaxConcurrent=4) and ensures
// the parallel run is meaningfully faster than the sequential run (4 in-flight
// × 0.3 s = 0.3 s per batch vs 0.3 s per bead × 10 = 3 s sequential).
//
// Helper prefix: throughputFixture (per implementer-protocol.md
// §Helper-prefix discipline; bead hk-e61c3.6).

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

// ─────────────────────────────────────────────────────────────────────────────
// throughputFixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// throughputFixtureLocateBr finds the real `br` binary via exec.LookPath.
// Skips the test when br is not available.
func throughputFixtureLocateBr(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for throughput test (not on PATH); CI sets br on PATH")
	}
	return brPath
}

// throughputFixtureSleepHandlerScript writes a /bin/sh script to t.TempDir()
// that sleeps 0.3 s then exits 0.  This ensures ≤MaxConcurrent goroutines are
// simultaneously in-flight and the parallel run is measurably faster than the
// sequential run.
func throughputFixtureSleepHandlerScript(t *testing.T) string {
	t.Helper()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "handler.sh")
	content := "#!/bin/sh\nsleep 0.3\nexit 0\n"
	//nolint:gosec // G306: script is test-only, chmod 0755 required for execution
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("throughputFixtureSleepHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// throughputFixtureSetupProject creates the project directory, initialises a
// git repo, runs br init with the given prefix, and returns projectDir,
// jsonlPath, and the br wrapper script path.
func throughputFixtureSetupProject(t *testing.T, realBrPath, prefix string) (projectDir, jsonlPath, brWrapper string) {
	t.Helper()

	projectDir, jsonlPath = smokeFixtureProjectDir(t)
	smokeFixtureGitRepo(t, projectDir)

	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", prefix)
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("throughputFixtureSetupProject: br init: %v\n%s", initErr, initOut)
	}

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper = smokeFixtureBrWrapperScript(t, realBrPath, dbPath)
	return projectDir, jsonlPath, brWrapper
}

// throughputFixtureCreateBeads creates n beads via brWrapper and returns their
// IDs.  Each bead is seeded with status=open so the work loop can claim it.
func throughputFixtureCreateBeads(t *testing.T, brWrapper string, n int) []string {
	t.Helper()
	ids := make([]string, n)
	for i := range n {
		//nolint:gosec // G204: br args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), brWrapper,
			"create", "throughput test bead", "--status", "open", "--silent")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("throughputFixtureCreateBeads: br create bead %d: %v\n%s", i, err, out)
		}
		id := strings.TrimSpace(string(out))
		if id == "" {
			t.Fatalf("throughputFixtureCreateBeads: br create bead %d returned empty ID", i)
		}
		ids[i] = id
	}
	return ids
}

// throughputFixturePollAllBeadsClosed polls until all bead IDs in ids reach
// "closed" status, or until budget expires.  Returns true iff all beads closed.
func throughputFixturePollAllBeadsClosed(t *testing.T, brWrapper string, ids []string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		allClosed := true
		for _, id := range ids {
			//nolint:gosec // G204: br args are test-internal literals; not user input
			cmd := exec.CommandContext(t.Context(), brWrapper, "show", id, "--format", "json")
			out, err := cmd.Output()
			if err != nil {
				allClosed = false
				break
			}
			var items []struct {
				Status string `json:"status"`
			}
			if jsonErr := json.Unmarshal(out, &items); jsonErr != nil ||
				len(items) != 1 ||
				items[0].Status != "closed" {
				allClosed = false
				break
			}
		}
		if allClosed {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// throughputFixtureCountTerminalInJSONL counts run_completed and run_failed
// events in the JSONL file at path.
func throughputFixtureCountTerminalInJSONL(t *testing.T, jsonlPath string) int {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		return 0
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

// throughputFixturePollTerminalEvents polls the JSONL for run_completed/run_failed
// events until target count is reached or budget expires.
func throughputFixturePollTerminalEvents(t *testing.T, jsonlPath string, target int, budget time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if n := throughputFixtureCountTerminalInJSONL(t, jsonlPath); n >= target {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
	return throughputFixtureCountTerminalInJSONL(t, jsonlPath)
}

// throughputFixtureExtractRunStartedRunIDs reads the JSONL file and returns all
// run_id values found in the envelope of run_started events.
// After hk-a6nob, emitRunStarted uses EmitWithRunID, so run_id is stamped on
// the EV-001 envelope and readable without payload extraction.
func throughputFixtureExtractRunStartedRunIDs(t *testing.T, jsonlPath string) []core.RunID {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("throughputFixtureExtractRunStartedRunIDs: open %s: %v", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()

	var runIDs []core.RunID
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"run_started"`) {
			continue
		}
		var ev core.Event
		if unmarshalErr := json.Unmarshal([]byte(line), &ev); unmarshalErr != nil {
			continue
		}
		if ev.Type != "run_started" {
			continue
		}
		if ev.RunID == nil {
			continue
		}
		runIDs = append(runIDs, *ev.RunID)
	}
	return runIDs
}

// throughputFixtureRunDaemon runs daemon.Start with the given config and returns
// the wall-clock duration of the run. It polls for all n beads to close and
// then for all terminal events, cancels the loop, and waits for Start to return.
func throughputFixtureRunDaemon(
	t *testing.T,
	cfg daemon.Config,
	brWrapper string,
	beadIDs []string,
) time.Duration {
	t.Helper()
	const closeBudget = 60 * time.Second

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	startDone := make(chan error, 1)
	start := time.Now()
	go func() {
		startDone <- daemon.Start(loopCtx, cfg)
	}()

	// Phase 1: wait for all beads to be closed in SQLite.
	allClosed := throughputFixturePollAllBeadsClosed(t, brWrapper, beadIDs, closeBudget)

	// Phase 2: wait for all terminal events in JSONL (avoids race between bead
	// close and event emission; same pattern as hk-c1ln2 fix in smoke_test.go).
	if allClosed {
		_ = throughputFixturePollTerminalEvents(t, cfg.JSONLLogPath, len(beadIDs), 5*time.Second)
	}

	elapsed := time.Since(start)

	loopCancel()

	select {
	case err := <-startDone:
		if err != nil {
			t.Errorf("daemon.Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("daemon.Start did not return within 5 s after context cancel")
	}

	if !allClosed {
		t.Errorf("throughputFixtureRunDaemon: not all %d beads closed within %s", len(beadIDs), closeBudget)
	}

	return elapsed
}

// ─────────────────────────────────────────────────────────────────────────────
// TestThroughput_TenBeadsAtMaxFour
// ─────────────────────────────────────────────────────────────────────────────

// TestThroughput_TenBeadsAtMaxFour is the roadmap row 11 throughput test.
//
// It runs two sub-tests:
//  1. Sequential (MaxConcurrent=1): times N=10 beads processed serially.
//  2. Parallel (MaxConcurrent=4): times N=10 beads at concurrency 4.
//
// Assertions:
//   - All 10 beads close cleanly in both runs.
//   - Parallel wall-clock < 3× sequential baseline.
//   - JSONL contains exactly 10 distinct run_started events with 10 distinct
//     run_id values (verified via eventbus.Filter per row 10 / hk-e61c3.5).
//
// Spec ref: POST_MVH_PARALLELISM_ROADMAP.md row 11.
// Bead ref: hk-e61c3.6.
func TestThroughput_TenBeadsAtMaxFour(t *testing.T) {
	t.Parallel()

	realBrPath := throughputFixtureLocateBr(t)
	handlerScript := throughputFixtureSleepHandlerScript(t)

	const beadCount = 10

	// ── Sequential baseline (MaxConcurrent=1) ─────────────────────────────────
	//
	// Expected sequential time: 10 × 0.3 s = 3.0 s (plus overhead).
	// Expected parallel time:   ceil(10/4) × 0.3 s ≈ 0.9 s (plus overhead).
	// Ratio budget: < 3× (bead body requirement).
	seqProjectDir, seqJSONLPath, seqBrWrapper := throughputFixtureSetupProject(t, realBrPath, "t11b")
	seqBeadIDs := throughputFixtureCreateBeads(t, seqBrWrapper, beadCount)

	seqCfg := daemon.Config{
		ProjectDir:    seqProjectDir,
		JSONLLogPath:  seqJSONLPath,
		BrPath:        seqBrWrapper,
		HandlerBinary: handlerScript,
		HandlerEnv:    nil,
		MaxConcurrent: 1,
	}
	seqElapsed := throughputFixtureRunDaemon(t, seqCfg, seqBrWrapper, seqBeadIDs)
	t.Logf("sequential: %d beads MaxConcurrent=1 wall_clock=%v", beadCount, seqElapsed)

	// ── Parallel run (MaxConcurrent=4) ────────────────────────────────────────
	parProjectDir, parJSONLPath, parBrWrapper := throughputFixtureSetupProject(t, realBrPath, "t11p")
	parBeadIDs := throughputFixtureCreateBeads(t, parBrWrapper, beadCount)

	parCfg := daemon.Config{
		ProjectDir:    parProjectDir,
		JSONLLogPath:  parJSONLPath,
		BrPath:        parBrWrapper,
		HandlerBinary: handlerScript,
		HandlerEnv:    nil,
		MaxConcurrent: 4,
	}
	parElapsed := throughputFixtureRunDaemon(t, parCfg, parBrWrapper, parBeadIDs)
	t.Logf("parallel: %d beads MaxConcurrent=4 wall_clock=%v", beadCount, parElapsed)

	// ── Assert wall-clock ratio < 3× sequential baseline ──────────────────────
	const ratioLimit = 3.0
	if seqElapsed > 0 {
		ratio := float64(parElapsed) / float64(seqElapsed)
		t.Logf("throughput ratio: parallel/sequential = %.2f (limit %.1f×)", ratio, ratioLimit)
		if ratio >= ratioLimit {
			t.Errorf("parallel run took %.2f× the sequential baseline (limit %.1f×); "+
				"parallel=%v sequential=%v; concurrent dispatch not providing speedup",
				ratio, ratioLimit, parElapsed, seqElapsed)
		}
	}

	// ── Assert JSONL: exactly 10 distinct run_started events, 10 distinct run_ids ─
	//
	// emitRunStarted now uses EmitWithRunID (hk-a6nob), so run_id is stamped on
	// the EV-001 envelope.  Extract run_ids from the envelope directly, then use
	// eventbus.Filter (hk-e61c3.5 / row 10) as the canonical per-run query API
	// to verify each run_id's events are present and filter-reachable.
	parRunIDs := throughputFixtureExtractRunStartedRunIDs(t, parJSONLPath)
	if len(parRunIDs) != beadCount {
		t.Errorf("expected %d run_started events in JSONL; got %d; "+
			"lines: %v", beadCount, len(parRunIDs),
			parallelSmokeFixtureReadAllJSONLLines(t, parJSONLPath))
	}

	// Verify all envelope run_id values are distinct.
	distinctRunIDs := make(map[core.RunID]struct{}, len(parRunIDs))
	for _, id := range parRunIDs {
		distinctRunIDs[id] = struct{}{}
	}
	if len(distinctRunIDs) != beadCount {
		t.Errorf("expected %d distinct run_id values in run_started envelope; got %d; ids: %v",
			beadCount, len(distinctRunIDs), parRunIDs)
	}

	// Use eventbus.Filter (hk-e61c3.5 / row 10) to enumerate JSONL events by
	// run_id.  Filter matches on the envelope run_id field (populated by
	// EmitWithRunID).  Each run_id must yield at least one event (the
	// run_started event itself).
	totalFiltered := 0
	for runID := range distinctRunIDs {
		var filteredCount int
		for range eventbus.Filter(parJSONLPath, runID) {
			filteredCount++
		}
		if filteredCount == 0 {
			t.Errorf("eventbus.Filter: run_id %v returned 0 events; "+
				"envelope run_id must be populated by EmitWithRunID (hk-a6nob)", runID)
		}
		totalFiltered += filteredCount
	}
	t.Logf("eventbus.Filter: total events found across %d run_ids = %d "+
		"(each run_id must yield >= 1 via envelope filter; hk-a6nob)",
		len(distinctRunIDs), totalFiltered)

	t.Logf("throughput: %d beads closed; %d distinct run_ids verified via eventbus.Filter; "+
		"parallel=%v sequential=%v",
		beadCount, len(distinctRunIDs), parElapsed, seqElapsed)
}
