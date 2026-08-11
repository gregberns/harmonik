package daemon_test

// t11_throughput_test.go — 10-bead throughput integration test (hk-e61c3.6).
//
// TestThroughput_TenBeadsAtMaxFour is the roadmap row 11 closing test for the
// parallelism epic.  It exercises daemon.Start with 10 ready beads and
// MaxConcurrent=4 and asserts:
//
//  1. All 10 beads close cleanly.
//  2. Per-bead wall-clock < 3× a per-bead sequential baseline (a separate
//     sub-run with MaxConcurrent=1 over a smaller N is timed; per-bead
//     averages are compared so the ratio is meaningful regardless of N).
//  3. go test -race is clean.
//  4. JSONL run accounting: every bead emitted at least one run_started, all
//     run_id values are distinct, and any extra start for a bead is justified by
//     a run_failed for that same bead (verified via eventbus.Filter per
//     hk-e61c3.5 / row 10).  This assertion used to demand exactly 10 starts.
//     That form asserted that a bead is never retried, which contradicts the
//     designed fail-closed reopen path in internal/runmerge/merge.go
//     resolveMergeTips — a real merge-preflight failure produced 11 starts over
//     10 beads and red-lit a healthy run.  The double-dispatch detector is kept:
//     an extra start with no run_failed behind it still fails hard.  The pure
//     accounting is covered by TestThroughputRunAccounting below.
//
// Perf (hk-l6dsb): sequential baseline uses N=3 (seqBeadCount) and runs
// concurrently with the parallel run in an independent project dir via
// sync.WaitGroup; net wall-clock ~17 s (down from ~57 s at N=10 sequential).
//
// Metrics emission (roadmap §4): sqlite_lock_retries, in_flight_count fields
// are OUT OF SCOPE for this bead.  Populating new event fields is separate work.
// A follow-up bead should be filed if those fields are desired.
//
// Handler design: each bead handler sleeps 0.3 s, makes a minimal git commit,
// then exits 0. The sleep keeps at most 4 goroutines simultaneously in-flight
// (MaxConcurrent=4) and ensures the parallel run is meaningfully faster than the
// sequential run (4 in-flight × 0.3 s = 0.3 s per batch vs 0.3 s per bead × 10 =
// 3 s sequential). The commit is required so the no-commit guard does not reopen
// the bead — both cfgs use the historical single default with a committing handler,
// so run planning selects the registered DOT graph and the
// beads actually reach "closed" (hk-6hzci).
//
// Helper prefix: throughputFixture (per implementer-protocol.md
// §Helper-prefix discipline; bead hk-e61c3.6).

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
// that sleeps 0.3 s, makes a minimal git commit, then exits 0.
//
// The sleep ensures ≤MaxConcurrent goroutines are simultaneously in-flight and
// the parallel run is measurably faster than the sequential run (that is the
// throughput invariant under test).  The commit is required because the
// no-commit guard fails any run where HEAD does not advance past parentSHA;
// without it the run reopens the bead and the beads never reach "closed"
// (hk-6hzci: review-loop + sleep-only handler mismatch caused a reopen storm
// and the test timed out at "not all beads closed within 1m0s").  The commit
// pattern mirrors smokeFixtureHandlerScript: read bead_id from
// .harmonik/agent-task.md so the commit carries a proper Refs trailer, and
// redirect git output to stderr so the daemon's NDJSON stdout parser is not
// confused by non-JSON output.
func throughputFixtureSleepHandlerScript(t *testing.T) string {
	t.Helper()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "handler.sh")
	content := `#!/bin/sh
set -e
sleep 0.3
bead_id=$(grep '^bead_id:' .harmonik/agent-task.md | awk '{print $2}') 2>/dev/null
echo "throughput: ${bead_id}" > "throughput-${bead_id}.txt"
git add "throughput-${bead_id}.txt" >&2
git commit -m "throughput: handler commit for ${bead_id}

Refs: ${bead_id}" >&2
exit 0
`
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
		cmd := exec.CommandContext(t.Context(), brWrapper,
			"create", "throughput test bead", "--status", "open", "--labels", "workflow:single", "--silent")
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

// throughputRunStartedEntry holds the run_id (from the EV-001 envelope) and the
// bead_id (from the payload) of one run_started JSONL line.
//
// bead_id is carried because the throughput property is PER-BEAD accounting, not
// a global run count: the product deliberately reopens and re-dispatches a bead
// whose merge preflight fails (internal/runmerge/merge.go resolveMergeTips,
// covered by mergetomain_runbranchmissing_hc1jr_test.go), so "10 beads" and
// "10 runs" are different numbers by design.  Same shape as
// parallelSmokeRunStartedEntry in t7_parallel_smoke_test.go.
type throughputRunStartedEntry struct {
	runID  core.RunID
	beadID string
}

// throughputFixtureExtractRunStarted reads the JSONL file and extracts run_id
// (from the EV-001 envelope, stamped by EmitWithRunID per hk-a6nob) and bead_id
// (from the payload, always set by emitRunStarted) for every run_started event.
func throughputFixtureExtractRunStarted(t *testing.T, jsonlPath string) []throughputRunStartedEntry {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("throughputFixtureExtractRunStarted: open %s: %v", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()

	// bead_id is a payload field; run_id is on the envelope (hk-a6nob).
	type startedPayload struct {
		BeadID string `json:"bead_id"`
	}

	var entries []throughputRunStartedEntry
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
		var pl startedPayload
		if unmarshalErr := json.Unmarshal(ev.Payload, &pl); unmarshalErr != nil {
			continue
		}
		entries = append(entries, throughputRunStartedEntry{runID: *ev.RunID, beadID: pl.BeadID})
	}
	return entries
}

// throughputRunFailedEntry holds the bead_id and the human-readable failure text
// of one run_failed JSONL line.
//
// The daemon work loop writes that text to `summary` (workloopRunCompletedPayload
// in workloop.go); the spec payload core.RunFailedPayload names the same thing
// `reason`.  Both spellings are read so a tolerated retry always reports WHY.
type throughputRunFailedEntry struct {
	beadID string
	reason string
}

// throughputFixtureExtractRunFailed reads the JSONL file and extracts bead_id
// and the failure text for every run_failed event.
func throughputFixtureExtractRunFailed(t *testing.T, jsonlPath string) []throughputRunFailedEntry {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("throughputFixtureExtractRunFailed: open %s: %v", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()

	type failedPayload struct {
		BeadID  string `json:"bead_id"`
		Reason  string `json:"reason"`
		Summary string `json:"summary"`
	}

	var entries []throughputRunFailedEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"run_failed"`) {
			continue
		}
		var ev core.Event
		if unmarshalErr := json.Unmarshal([]byte(line), &ev); unmarshalErr != nil {
			continue
		}
		if ev.Type != "run_failed" {
			continue
		}
		var pl failedPayload
		if unmarshalErr := json.Unmarshal(ev.Payload, &pl); unmarshalErr != nil {
			continue
		}
		reason := pl.Reason
		if reason == "" {
			reason = pl.Summary
		}
		entries = append(entries, throughputRunFailedEntry{beadID: pl.BeadID, reason: reason})
	}
	return entries
}

// throughputRunAccounting is the run_started / run_failed tally the throughput
// assertion is built on.
type throughputRunAccounting struct {
	totalStarts    int
	distinctRunIDs int
	startsByBead   map[string]int
	failuresByBead map[string]int
	failureReasons map[string][]string
}

// throughputAccountRuns tallies run_started and run_failed events per bead.
func throughputAccountRuns(
	started []throughputRunStartedEntry,
	failed []throughputRunFailedEntry,
) throughputRunAccounting {
	acct := throughputRunAccounting{
		startsByBead:   make(map[string]int),
		failuresByBead: make(map[string]int),
		failureReasons: make(map[string][]string),
	}
	seenRunIDs := make(map[core.RunID]struct{}, len(started))
	for _, entry := range started {
		acct.totalStarts++
		seenRunIDs[entry.runID] = struct{}{}
		acct.startsByBead[entry.beadID]++
	}
	acct.distinctRunIDs = len(seenRunIDs)
	for _, entry := range failed {
		acct.failuresByBead[entry.beadID]++
		acct.failureReasons[entry.beadID] = append(acct.failureReasons[entry.beadID], entry.reason)
	}
	return acct
}

// throughputAccountingProblems checks the three properties the throughput test
// is really about and returns the violations plus the retries it tolerated.
//
// Properties:
//  1. Every seeded bead emitted at least one run_started, and at least
//     wantBeadCount distinct beads did.
//  2. All run_id values are distinct, compared against the TOTAL number of
//     run_started events seen — not against the bead count, because a retried
//     bead legitimately produces more starts than there are beads.
//  3. For each bead, starts-1 <= run_failed events for that bead.  An extra
//     start with no failure behind it is a double dispatch and a hard failure.
//     This is the anti-double-dispatch property.
func throughputAccountingProblems(
	acct throughputRunAccounting,
	wantBeadIDs []string,
	wantBeadCount int,
) (problems, tolerated []string) {
	for _, beadID := range wantBeadIDs {
		if acct.startsByBead[beadID] == 0 {
			problems = append(problems, fmt.Sprintf("bead %s never emitted run_started", beadID))
		}
	}
	if len(acct.startsByBead) < wantBeadCount {
		problems = append(problems, fmt.Sprintf(
			"only %d distinct beads emitted run_started, want at least %d; a bead never started",
			len(acct.startsByBead), wantBeadCount))
	}

	if acct.distinctRunIDs != acct.totalStarts {
		problems = append(problems, fmt.Sprintf(
			"run_id values are not distinct: %d run_started events carried only %d distinct run_ids",
			acct.totalStarts, acct.distinctRunIDs))
	}

	for _, beadID := range slices.Sorted(maps.Keys(acct.startsByBead)) {
		starts := acct.startsByBead[beadID]
		if starts <= 1 {
			continue
		}
		failures := acct.failuresByBead[beadID]
		if starts-1 > failures {
			problems = append(problems, fmt.Sprintf(
				"bead %s started %d times with only %d run_failed; %d extra start(s) have no failure to "+
					"justify a retry — this is a double dispatch",
				beadID, starts, failures, starts-1-failures))
			continue
		}
		tolerated = append(tolerated, fmt.Sprintf(
			"bead %s started %d times after %d run_failed; reasons: %v",
			beadID, starts, failures, acct.failureReasons[beadID]))
	}
	return problems, tolerated
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

	if err := awaitLoopTeardownErr(t, startDone, "daemon.Start"); err != nil {
		t.Errorf("daemon.Start returned error: %v", err)
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
//   - JSONL run accounting holds: every bead started, run_ids are all distinct,
//     and every extra start is justified by a run_failed for the same bead
//     (verified via eventbus.Filter per row 10 / hk-e61c3.5).
//
// Spec ref: POST_OPERATIONAL_PARALLELISM_ROADMAP.md row 11.
// Bead ref: hk-e61c3.6.
func TestThroughput_TenBeadsAtMaxFour(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	realBrPath := throughputFixtureLocateBr(t)
	handlerScript := throughputFixtureSleepHandlerScript(t)

	// Bead body invariant: 10 beads at MaxConcurrent=4 close cleanly with no
	// starvation, and the parallel run is < 3× a sequential baseline.
	//
	// Perf note (hk-l6dsb): the parallel run is the invariant — it must use
	// beadCount=10.  The sequential baseline only provides a ratio reference;
	// running it at the same N=10 doubled wall-clock for no extra coverage
	// (the ratio is dominated by per-bead claim/close overhead, ~4 s/bead).
	// Use seqBeadCount=3 for the baseline (still measures per-bead overhead at
	// MaxConcurrent=1) and run sequential + parallel sub-runs concurrently in
	// independent project dirs.  Net: ~57 s → ~17 s with the same assertions.
	const beadCount = 10
	const seqBeadCount = 3

	// ── Setup both project fixtures up-front ──────────────────────────────────
	//
	// Expected sequential time:  3 × 0.3 s = 0.9 s of handler work + ~3 × claim/close overhead.
	// Expected parallel time:    ceil(10/4) × 0.3 s ≈ 0.9 s of handler work + parallel claim/close.
	// Ratio budget: < 3× (bead body requirement).
	seqProjectDir, seqJSONLPath, seqBrWrapper := throughputFixtureSetupProject(t, realBrPath, "t11b")
	seqBeadIDs := throughputFixtureCreateBeads(t, seqBrWrapper, seqBeadCount)

	parProjectDir, parJSONLPath, parBrWrapper := throughputFixtureSetupProject(t, realBrPath, "t11p")
	parBeadIDs := throughputFixtureCreateBeads(t, parBrWrapper, beadCount)

	seqCfg := daemon.Config{
		ProjectDir:          seqProjectDir,
		JSONLLogPath:        seqJSONLPath,
		BrPath:              seqBrWrapper,
		HandlerBinary:       handlerScript,
		HandlerEnv:          nil,
		MaxConcurrent:       1,
		WorkflowModeDefault: core.WorkflowModeDot,
	}
	parCfg := daemon.Config{
		ProjectDir:          parProjectDir,
		JSONLLogPath:        parJSONLPath,
		BrPath:              parBrWrapper,
		HandlerBinary:       handlerScript,
		HandlerEnv:          nil,
		MaxConcurrent:       4,
		WorkflowModeDefault: core.WorkflowModeDot,
	}

	// ── Run sequential baseline and parallel run concurrently ─────────────────
	//
	// The two runs use independent project dirs / JSONL paths / wrapper scripts,
	// so they share no state.  Running them concurrently cuts wall-clock from
	// (seq + par) to max(seq, par).
	var (
		seqElapsed time.Duration
		parElapsed time.Duration
		wg         sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		seqElapsed = throughputFixtureRunDaemon(t, seqCfg, seqBrWrapper, seqBeadIDs)
	}()
	go func() {
		defer wg.Done()
		parElapsed = throughputFixtureRunDaemon(t, parCfg, parBrWrapper, parBeadIDs)
	}()
	wg.Wait()

	t.Logf("sequential: %d beads MaxConcurrent=1 wall_clock=%v", seqBeadCount, seqElapsed)
	t.Logf("parallel: %d beads MaxConcurrent=4 wall_clock=%v", beadCount, parElapsed)

	// ── Assert wall-clock ratio < 3× sequential per-bead baseline ─────────────
	//
	// Normalize: the parallel run does beadCount beads; the sequential baseline
	// does seqBeadCount beads.  Compare per-bead averages so the ratio is
	// meaningful across different N values.
	const ratioLimit = 3.0
	if seqElapsed > 0 {
		seqPerBead := float64(seqElapsed) / float64(seqBeadCount)
		parPerBead := float64(parElapsed) / float64(beadCount)
		ratio := parPerBead / seqPerBead
		t.Logf("throughput ratio: parallel/sequential per-bead = %.2f (limit %.1f×)", ratio, ratioLimit)
		if ratio >= ratioLimit {
			t.Errorf("parallel per-bead took %.2f× the sequential per-bead baseline (limit %.1f×); "+
				"parallel=%v (N=%d) sequential=%v (N=%d); concurrent dispatch not providing speedup",
				ratio, ratioLimit, parElapsed, beadCount, seqElapsed, seqBeadCount)
		}
	}

	// ── Assert JSONL run accounting: every bead ran, run_ids distinct, no ──────
	//    unjustified extra start.
	//
	// Deliberately NOT `run_started count == beadCount`.  That form asserts that a
	// bead is never retried, which contradicts designed fail-closed behaviour: when
	// the merge preflight cannot resolve a run branch the daemon reopens the bead
	// and re-dispatches it (internal/runmerge/merge.go resolveMergeTips, covered by
	// mergetomain_runbranchmissing_hc1jr_test.go), so 10 beads can legitimately
	// produce 11 starts.  What must NOT happen is an extra start with no run_failed
	// behind it — that is a double dispatch, and it stays a hard failure here.
	// The sibling row-7 test t7_parallel_smoke_test.go already uses >= bounds for
	// these same properties.
	//
	// emitRunStarted uses EmitWithRunID (hk-a6nob), so run_id is stamped on the
	// EV-001 envelope; bead_id comes from the payload.
	parStarted := throughputFixtureExtractRunStarted(t, parJSONLPath)
	parFailed := throughputFixtureExtractRunFailed(t, parJSONLPath)
	parAcct := throughputAccountRuns(parStarted, parFailed)
	parProblems, parTolerated := throughputAccountingProblems(parAcct, parBeadIDs, beadCount)
	for _, note := range parTolerated {
		t.Logf("throughput: tolerated retry: %s", note)
	}
	for _, problem := range parProblems {
		// No whole-JSONL dump: it is ~90 KB and unreadable.  Print the accounting
		// that explains the failure instead.
		t.Errorf("throughput run accounting: %s "+
			"(run_started=%d distinct_run_ids=%d run_failed=%d starts_by_bead=%v failures_by_bead=%v)",
			problem, parAcct.totalStarts, parAcct.distinctRunIDs, len(parFailed),
			parAcct.startsByBead, parAcct.failuresByBead)
	}

	// Collect the distinct envelope run_ids for the eventbus.Filter check below.
	distinctRunIDs := make(map[core.RunID]struct{}, len(parStarted))
	for _, entry := range parStarted {
		distinctRunIDs[entry.runID] = struct{}{}
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

	t.Logf("throughput: %d beads closed; %d run_started over %d beads; %d run_failed; "+
		"%d distinct run_ids verified via eventbus.Filter; parallel=%v sequential=%v",
		beadCount, parAcct.totalStarts, len(parAcct.startsByBead), len(parFailed),
		len(distinctRunIDs), parElapsed, seqElapsed)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestThroughputRunAccounting — deterministic cover for the accounting above
// ─────────────────────────────────────────────────────────────────────────────

// throughputFixtureSyntheticRunID returns a deterministic UUIDv7-shaped RunID
// for slot n, so synthetic JSONL has distinct (or deliberately repeated) run_ids
// without a random source.
func throughputFixtureSyntheticRunID(t *testing.T, n int) core.RunID {
	t.Helper()
	var rid core.RunID
	if err := rid.UnmarshalText([]byte(fmt.Sprintf("0189abcd-0000-7000-8000-%012d", n))); err != nil {
		t.Fatalf("throughputFixtureSyntheticRunID(%d): %v", n, err)
	}
	return rid
}

// throughputFixtureSyntheticStart builds a run_started event for beadID with the
// run_id in slot runSlot.  The payload carries the subset of
// core.RunStartedPayload the extractor reads; the full payload type refuses to
// marshal from a zero value (WorkflowID validates on MarshalText), and the other
// fields are not what this test is about.
func throughputFixtureSyntheticStart(t *testing.T, runSlot int, beadID string) core.Event {
	t.Helper()
	runID := throughputFixtureSyntheticRunID(t, runSlot)
	payload, err := json.Marshal(struct {
		RunID         string `json:"run_id"`
		BeadID        string `json:"bead_id"`
		WorkspacePath string `json:"workspace_path"`
	}{RunID: runID.String(), BeadID: beadID, WorkspacePath: "/synthetic/" + beadID})
	if err != nil {
		t.Fatalf("throughputFixtureSyntheticStart: marshal payload: %v", err)
	}
	return core.Event{Type: core.EventTypeRunStarted, RunID: &runID, Payload: payload}
}

// throughputFixtureSyntheticFailure builds a run_failed event for beadID.  The
// payload mirrors workloopRunCompletedPayload — the shape the daemon work loop
// actually writes, which names the failure text `summary`, not `reason`.
func throughputFixtureSyntheticFailure(t *testing.T, runSlot int, beadID, summary string) core.Event {
	t.Helper()
	runID := throughputFixtureSyntheticRunID(t, runSlot)
	payload, err := json.Marshal(struct {
		RunID   string `json:"run_id"`
		BeadID  string `json:"bead_id"`
		Success bool   `json:"success"`
		Summary string `json:"summary"`
	}{RunID: runID.String(), BeadID: beadID, Success: false, Summary: summary})
	if err != nil {
		t.Fatalf("throughputFixtureSyntheticFailure: marshal payload: %v", err)
	}
	return core.Event{Type: core.EventTypeRunFailed, RunID: &runID, Payload: payload}
}

// throughputFixtureWriteSyntheticJSONL writes one JSON line per event to a fresh
// file under t.TempDir() and returns its path.
func throughputFixtureWriteSyntheticJSONL(t *testing.T, events []core.Event) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var buf strings.Builder
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("throughputFixtureWriteSyntheticJSONL: marshal event: %v", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(buf.String()), 0o600); err != nil {
		t.Fatalf("throughputFixtureWriteSyntheticJSONL: WriteFile: %v", err)
	}
	return path
}

// TestThroughputRunAccounting is the deterministic cover for the run accounting
// TestThroughput_TenBeadsAtMaxFour asserts.  It needs no daemon, no br, and no
// git: it writes synthetic JSONL and runs the same extractors and checker.
//
// The case that earns this test its place is "extra start, no failure": that is
// the double-dispatch detector, and it is the property the widened assertion had
// to keep. Deleting the starts-1 <= failures clause in
// throughputAccountingProblems turns that case red, and nothing else.
func TestThroughputRunAccounting(t *testing.T) {
	t.Parallel()

	// Ten beads, named the way br names them.
	beads := make([]string, 10)
	for i := range beads {
		beads[i] = fmt.Sprintf("t11p-%d", i+1)
	}

	cases := []struct {
		name        string
		events      []core.Event
		wantBeads   []string
		wantCount   int
		wantProblem bool
		// wantSubstr, when non-empty, must appear in the first problem reported.
		wantSubstr    string
		wantTolerated int
	}{
		{
			// (a) The healthy run: one start per bead, no failures.
			name: "ten starts no failures",
			events: func() []core.Event {
				evs := make([]core.Event, 0, len(beads))
				for i, bead := range beads {
					evs = append(evs, throughputFixtureSyntheticStart(t, i+1, bead))
				}
				return evs
			}(),
			wantBeads: beads,
			wantCount: 10,
		},
		{
			// (b) The run that used to red-light the branch: 11 starts over 10
			// beads, where the extra start follows a run_failed for that bead.
			name: "eleven starts with a justifying failure",
			events: func() []core.Event {
				evs := make([]core.Event, 0, len(beads)+2)
				for i, bead := range beads {
					evs = append(evs, throughputFixtureSyntheticStart(t, i+1, bead))
				}
				evs = append(evs,
					throughputFixtureSyntheticFailure(t, 3, beads[2], "merge preflight: git rev-parse died on signal 11"),
					throughputFixtureSyntheticStart(t, 11, beads[2]),
				)
				return evs
			}(),
			wantBeads:     beads,
			wantCount:     10,
			wantTolerated: 1,
		},
		{
			// (c) The double-dispatch detector: 11 starts over 10 beads with NO
			// run_failed anywhere.  Nothing justifies the extra start.
			name: "eleven starts with no failure is a double dispatch",
			events: func() []core.Event {
				evs := make([]core.Event, 0, len(beads)+1)
				for i, bead := range beads {
					evs = append(evs, throughputFixtureSyntheticStart(t, i+1, bead))
				}
				evs = append(evs, throughputFixtureSyntheticStart(t, 11, beads[2]))
				return evs
			}(),
			wantBeads:   beads,
			wantCount:   10,
			wantProblem: true,
			wantSubstr:  "double dispatch",
		},
		{
			// A bead that never ran must still fail — this is the property the
			// widened assertion could most easily have lost.
			name: "a bead that never started",
			events: func() []core.Event {
				evs := make([]core.Event, 0, len(beads)-1)
				for i, bead := range beads[:len(beads)-1] {
					evs = append(evs, throughputFixtureSyntheticStart(t, i+1, bead))
				}
				return evs
			}(),
			wantBeads:   beads,
			wantCount:   10,
			wantProblem: true,
			wantSubstr:  "never emitted run_started",
		},
		{
			// Distinctness is compared against the TOTAL starts, not the bead
			// count, so a repeated run_id is caught even at 10 starts.
			name: "a repeated run_id",
			events: func() []core.Event {
				evs := make([]core.Event, 0, len(beads))
				for i, bead := range beads {
					slot := i + 1
					if slot == 10 {
						slot = 9 // reuse bead 9's run_id
					}
					evs = append(evs, throughputFixtureSyntheticStart(t, slot, bead))
				}
				return evs
			}(),
			wantBeads:   beads,
			wantCount:   10,
			wantProblem: true,
			wantSubstr:  "not distinct",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			jsonlPath := throughputFixtureWriteSyntheticJSONL(t, tc.events)
			acct := throughputAccountRuns(
				throughputFixtureExtractRunStarted(t, jsonlPath),
				throughputFixtureExtractRunFailed(t, jsonlPath),
			)
			problems, tolerated := throughputAccountingProblems(acct, tc.wantBeads, tc.wantCount)

			if tc.wantProblem && len(problems) == 0 {
				t.Errorf("want a reported problem, got none; accounting: starts=%d distinct=%d "+
					"starts_by_bead=%v failures_by_bead=%v",
					acct.totalStarts, acct.distinctRunIDs, acct.startsByBead, acct.failuresByBead)
			}
			if !tc.wantProblem && len(problems) != 0 {
				t.Errorf("want no problem, got %d: %v", len(problems), problems)
			}
			if tc.wantSubstr != "" {
				found := false
				for _, problem := range problems {
					if strings.Contains(problem, tc.wantSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no reported problem contains %q; problems: %v", tc.wantSubstr, problems)
				}
			}
			if len(tolerated) != tc.wantTolerated {
				t.Errorf("tolerated %d retries, want %d; tolerated: %v", len(tolerated), tc.wantTolerated, tolerated)
			}
		})
	}
}
