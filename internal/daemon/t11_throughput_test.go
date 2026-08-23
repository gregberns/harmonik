package daemon_test

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

func throughputFixtureLocateBr(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for throughput test (not on PATH); CI sets br on PATH")
	}
	return brPath
}

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

type throughputRunStartedEntry struct {
	runID  core.RunID
	beadID string
	// line is the 1-based position of this event in the JSONL file.  File
	// order is emit order for any two events with a happens-before edge
	// between them: one drainer goroutine owns the file descriptor, the queue
	// in front of it is FIFO, and Append blocks until its own bytes are in a
	// completed write.  That order is what tells a retry apart from a double
	// dispatch.
	line int
}

func throughputFixtureExtractRunStarted(t *testing.T, jsonlPath string) []throughputRunStartedEntry {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		t.Fatalf("throughputFixtureExtractRunStarted: open %s: %v", jsonlPath, err)
	}
	defer func() { _ = f.Close() }()

	type startedPayload struct {
		BeadID string `json:"bead_id"`
	}

	var entries []throughputRunStartedEntry
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
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
		entries = append(entries, throughputRunStartedEntry{runID: *ev.RunID, beadID: pl.BeadID, line: lineNo})
	}
	return entries
}

type throughputRunFailedEntry struct {
	beadID string
	reason string
	// line is the 1-based position of this event in the JSONL file, read the
	// same way as throughputRunStartedEntry.line.
	line int
}

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
	lineNo := 0
	for scanner.Scan() {
		lineNo++
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
		entries = append(entries, throughputRunFailedEntry{beadID: pl.BeadID, reason: reason, line: lineNo})
	}
	return entries
}

type throughputRunAccounting struct {
	totalStarts    int
	distinctRunIDs int
	// startLinesByBead and failureLinesByBead hold the JSONL line of every
	// run_started and run_failed event for a bead, in file order.  Counts alone
	// cannot tell a retry from a double dispatch: both show one more start than
	// the bead has runs.  Order can.  A retry starts after its own run failed.
	// A double dispatch starts while the first run is still alive, so it comes
	// before any failure for that bead.
	//
	// One caution for whoever changes this.  The product does not guarantee
	// that order structurally: finalizeReopen in internal/runexec/run.go
	// reopens the bead BEFORE it emits the failure, so the bead is claimable
	// first.  What separates them is the gap.  The failure is written and
	// flushed in milliseconds, and the dispatcher only sees the reopened bead
	// on its next poll two seconds later.  If that poll ever gets faster than
	// the emit, this rule needs the emit moved ahead of the reopen, not a
	// weaker comparison here.
	startLinesByBead   map[string][]int
	failureLinesByBead map[string][]int
	failureReasons     map[string][]string
}

func throughputAccountRuns(
	started []throughputRunStartedEntry,
	failed []throughputRunFailedEntry,
) throughputRunAccounting {
	acct := throughputRunAccounting{
		startLinesByBead:   make(map[string][]int),
		failureLinesByBead: make(map[string][]int),
		failureReasons:     make(map[string][]string),
	}
	seenRunIDs := make(map[core.RunID]struct{}, len(started))
	for _, entry := range started {
		acct.totalStarts++
		seenRunIDs[entry.runID] = struct{}{}
		acct.startLinesByBead[entry.beadID] = append(acct.startLinesByBead[entry.beadID], entry.line)
	}
	acct.distinctRunIDs = len(seenRunIDs)
	for _, entry := range failed {
		acct.failureLinesByBead[entry.beadID] = append(acct.failureLinesByBead[entry.beadID], entry.line)
		acct.failureReasons[entry.beadID] = append(acct.failureReasons[entry.beadID], entry.reason)
	}
	return acct
}

func throughputUnjustifiedStarts(startLines, failureLines []int) int {
	unjustified := 0
	for extra, startLine := range startLines {
		if extra == 0 {
			continue // the first start of a bead never needs a justification
		}
		earlier := 0
		for _, failureLine := range failureLines {
			if failureLine < startLine {
				earlier++
			}
		}
		if earlier < extra {
			unjustified++
		}
	}
	return unjustified
}

func throughputAccountingProblems(
	acct throughputRunAccounting,
	wantBeadIDs []string,
	wantBeadCount int,
) (problems, tolerated []string) {
	seeded := make(map[string]struct{}, len(wantBeadIDs))
	for _, beadID := range wantBeadIDs {
		seeded[beadID] = struct{}{}
		if len(acct.startLinesByBead[beadID]) == 0 {
			problems = append(problems, fmt.Sprintf("bead %s never emitted run_started", beadID))
		}
	}
	for _, beadID := range slices.Sorted(maps.Keys(acct.startLinesByBead)) {
		if _, ok := seeded[beadID]; !ok {
			problems = append(problems, fmt.Sprintf(
				"bead %s emitted run_started but this test never seeded it; the log being read holds "+
					"another run's events, so this accounting is not about this test", beadID))
		}
	}
	if len(acct.startLinesByBead) < wantBeadCount {
		problems = append(problems, fmt.Sprintf(
			"only %d distinct beads emitted run_started, want at least %d; a bead never started",
			len(acct.startLinesByBead), wantBeadCount))
	}

	if acct.distinctRunIDs != acct.totalStarts {
		problems = append(problems, fmt.Sprintf(
			"run_id values are not distinct: %d run_started events carried only %d distinct run_ids",
			acct.totalStarts, acct.distinctRunIDs))
	}

	for _, beadID := range slices.Sorted(maps.Keys(acct.startLinesByBead)) {
		startLines := acct.startLinesByBead[beadID]
		if len(startLines) <= 1 {
			continue
		}
		failureLines := acct.failureLinesByBead[beadID]
		if unjustified := throughputUnjustifiedStarts(startLines, failureLines); unjustified > 0 {
			problems = append(problems, fmt.Sprintf(
				"bead %s started %d times with %d run_failed, and %d extra start(s) have no EARLIER "+
					"failure to justify a retry (start lines %v, failure lines %v) — this is a double dispatch",
				beadID, len(startLines), len(failureLines), unjustified, startLines, failureLines))
			continue
		}
		tolerated = append(tolerated, fmt.Sprintf(
			"bead %s started %d times after %d run_failed; reasons: %v",
			beadID, len(startLines), len(failureLines), acct.failureReasons[beadID]))
	}
	return problems, tolerated
}

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

	allClosed := throughputFixturePollAllBeadsClosed(t, brWrapper, beadIDs, closeBudget)

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

	const beadCount = 10
	const seqBeadCount = 3

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

	parStarted := throughputFixtureExtractRunStarted(t, parJSONLPath)
	parFailed := throughputFixtureExtractRunFailed(t, parJSONLPath)
	parAcct := throughputAccountRuns(parStarted, parFailed)
	parProblems, parTolerated := throughputAccountingProblems(parAcct, parBeadIDs, beadCount)
	for _, note := range parTolerated {
		t.Logf("throughput: tolerated retry: %s", note)
	}
	for _, problem := range parProblems {
		t.Errorf("throughput run accounting: %s "+
			"(run_started=%d distinct_run_ids=%d run_failed=%d start_lines_by_bead=%v failure_lines_by_bead=%v)",
			problem, parAcct.totalStarts, parAcct.distinctRunIDs, len(parFailed),
			parAcct.startLinesByBead, parAcct.failureLinesByBead)
	}

	distinctRunIDs := make(map[core.RunID]struct{}, len(parStarted))
	for _, entry := range parStarted {
		distinctRunIDs[entry.runID] = struct{}{}
	}

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
		beadCount, parAcct.totalStarts, len(parAcct.startLinesByBead), len(parFailed),
		len(distinctRunIDs), parElapsed, seqElapsed)
}

func throughputFixtureSyntheticRunID(t *testing.T, n int) core.RunID {
	t.Helper()
	var rid core.RunID
	if err := rid.UnmarshalText([]byte(fmt.Sprintf("0189abcd-0000-7000-8000-%012d", n))); err != nil {
		t.Fatalf("throughputFixtureSyntheticRunID(%d): %v", n, err)
	}
	return rid
}

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
// The cases that earn this test its place are the two double-dispatch shapes:
// an extra start with no failure at all, and an extra start that precedes the
// only failure. Both are the property the widened assertion had to keep.
// Deleting the justification check in throughputUnjustifiedStarts turns those two
// cases red, and nothing else.
func TestThroughputRunAccounting(t *testing.T) {
	t.Parallel()

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
			name: "an extra start before the failure is still a double dispatch",
			events: func() []core.Event {
				evs := make([]core.Event, 0, len(beads)+2)
				for i, bead := range beads {
					evs = append(evs, throughputFixtureSyntheticStart(t, i+1, bead))
				}
				evs = append(evs,
					throughputFixtureSyntheticStart(t, 11, beads[2]),
					throughputFixtureSyntheticFailure(t, 11, beads[2], "the duplicate run failed after it started"),
				)
				return evs
			}(),
			wantBeads:   beads,
			wantCount:   10,
			wantProblem: true,
			wantSubstr:  "double dispatch",
		},
		{
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
			name: "a start from a bead this test never seeded",
			events: func() []core.Event {
				evs := make([]core.Event, 0, len(beads)+1)
				for i, bead := range beads {
					evs = append(evs, throughputFixtureSyntheticStart(t, i+1, bead))
				}
				evs = append(evs, throughputFixtureSyntheticStart(t, 11, "t11b-1"))
				return evs
			}(),
			wantBeads:   beads,
			wantCount:   10,
			wantProblem: true,
			wantSubstr:  "another run's events",
		},
		{
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
					"start_lines_by_bead=%v failure_lines_by_bead=%v",
					acct.totalStarts, acct.distinctRunIDs, acct.startLinesByBead, acct.failureLinesByBead)
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
