package replay_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/replay"
)

// --- run-payload constructors (mirror the keeper-cycle helpers above) --------

// runID builds a deterministic core.RunID from a small integer, matching the
// scheme scripts/gen-run-baseline.py uses (n in the low byte).
func runID(n byte) core.RunID {
	var b [16]byte
	b[15] = n
	b[6] = 0x70
	b[8] = 0x80
	return core.RunID(uuid.UUID(b))
}

func rStarted(r core.RunID, mode core.WorkflowMode) core.EventPayload {
	return &core.RunStartedPayload{
		RunID: r, WorkflowID: core.WorkflowID(runID(200)), WorkflowVersion: "v1",
		WorkspacePath: "/w", InputRef: "ref", WorkflowMode: &mode,
	}
}

func rLaunch(r core.RunID) core.EventPayload {
	return &core.LaunchInitiatedPayload{RunID: r, ClaudeSessionID: "sess"}
}

func rReady(r core.RunID) core.EventPayload {
	return &core.AgentReadyPayload{RunID: r, ClaudeSessionID: "sess"}
}

func rResumed(r core.RunID, it int) core.EventPayload {
	return &core.ImplementerResumedPayload{
		RunID: r, WorkflowMode: core.WorkflowModeReviewLoop, SessionID: "sess-uuid",
		ClaudeSessionID: "sess", IterationCount: it, PriorVerdictSummary: "prior",
	}
}

func rRLComplete(r core.RunID, it int) core.EventPayload {
	return &core.ReviewLoopCycleCompletePayload{
		RunID: r, WorkflowMode: core.WorkflowModeReviewLoop,
		FinalIterationCount: it, CompletionReason: core.ReviewLoopCompletionReasonApproved,
	}
}

func rCompleted(r core.RunID) core.EventPayload {
	return &core.RunCompletedPayload{RunID: r, TerminalStateID: core.StateID(runID(220)), EndedAt: "2026-07-14T12:00:00Z"}
}

func rFailed(r core.RunID) core.EventPayload {
	return &core.RunFailedPayload{
		RunID: r, FailureClass: core.FailureClassStructural,
		EndedAt: "2026-07-14T12:00:00Z", Reason: "boom", LastCheckpoint: "",
	}
}

func rReadyTimeout(r core.RunID) core.EventPayload {
	return &core.AgentReadyTimeoutPayload{RunID: r, ClaudeSessionID: "sess", TimeoutMs: 150000}
}

// runKeys returns the sorted (Rule/RunID) keys of a report's violations. RunID
// is folded into Violation.CycleID for the run track.
func runKeys(vs []replay.Violation) []string { return violationKeys(vs) }

// --- RSM9 liveness + exclusivity -------------------------------------------

func TestCheckRuns_RSM9_FlagsHungRun(t *testing.T) {
	r := runID(1)
	// resumed, then SILENCE — no terminal, no failure-class event.
	lines := []line{
		{seq: 1, evType: core.EventTypeRunStarted, payload: rStarted(r, core.WorkflowModeReviewLoop)},
		{seq: 2, evType: core.EventTypeLaunchInitiated, payload: rLaunch(r)},
		{seq: 3, evType: core.EventTypeAgentReady, payload: rReady(r)},
		{seq: 4, evType: core.EventTypeImplementerResumed, payload: rResumed(r, 2)},
	}
	rep, err := replay.CheckRuns(writeLog(t, lines), core.EventID{}, false, replay.DefaultRunCheckers())
	if err != nil {
		t.Fatalf("CheckRuns: %v", err)
	}
	if got, want := runKeys(rep.Violations), []string{"RSM9/" + r.String()}; !equalStrings(got, want) {
		t.Fatalf("violations = %v, want %v", got, want)
	}
}

func TestCheckRuns_RSM9_PassesCleanRun(t *testing.T) {
	r := runID(2)
	lines := []line{
		{seq: 1, evType: core.EventTypeRunStarted, payload: rStarted(r, core.WorkflowModeReviewLoop)},
		{seq: 2, evType: core.EventTypeLaunchInitiated, payload: rLaunch(r)},
		{seq: 3, evType: core.EventTypeAgentReady, payload: rReady(r)},
		{seq: 4, evType: core.EventTypeImplementerResumed, payload: rResumed(r, 2)},
		{seq: 5, evType: core.EventTypeReviewLoopCycleComplete, payload: rRLComplete(r, 2)},
	}
	rep, err := replay.CheckRuns(writeLog(t, lines), core.EventID{}, false, replay.DefaultRunCheckers())
	if err != nil {
		t.Fatalf("CheckRuns: %v", err)
	}
	if len(rep.Violations) != 0 {
		t.Fatalf("clean run flagged: %v", rep.Violations)
	}
}

func TestCheckRuns_RSM9_FailureClassDischarges(t *testing.T) {
	r := runID(3)
	// resumed, then agent_ready_timeout (fail-closed) — NOT silence, so clean.
	lines := []line{
		{seq: 1, evType: core.EventTypeRunStarted, payload: rStarted(r, core.WorkflowModeReviewLoop)},
		{seq: 2, evType: core.EventTypeLaunchInitiated, payload: rLaunch(r)},
		{seq: 3, evType: core.EventTypeImplementerResumed, payload: rResumed(r, 2)},
		{seq: 4, evType: core.EventTypeAgentReadyTimeout, payload: rReadyTimeout(r)},
	}
	rep, err := replay.CheckRuns(writeLog(t, lines), core.EventID{}, false, replay.DefaultRunCheckers())
	if err != nil {
		t.Fatalf("CheckRuns: %v", err)
	}
	if len(rep.Violations) != 0 {
		t.Fatalf("failure-class run flagged: %v", rep.Violations)
	}
}

func TestCheckRuns_RSM9_TerminalExclusivity(t *testing.T) {
	r := runID(4)
	lines := []line{
		{seq: 1, evType: core.EventTypeRunStarted, payload: rStarted(r, core.WorkflowModeSingle)},
		{seq: 2, evType: core.EventTypeLaunchInitiated, payload: rLaunch(r)},
		{seq: 3, evType: core.EventTypeAgentReady, payload: rReady(r)},
		{seq: 4, evType: core.EventTypeRunCompleted, payload: rCompleted(r)},
		{seq: 5, evType: core.EventTypeRunFailed, payload: rFailed(r)},
	}
	rep, err := replay.CheckRuns(writeLog(t, lines), core.EventID{}, false, replay.DefaultRunCheckers())
	if err != nil {
		t.Fatalf("CheckRuns: %v", err)
	}
	if got, want := runKeys(rep.Violations), []string{"RSM9/" + r.String()}; !equalStrings(got, want) {
		t.Fatalf("violations = %v, want %v", got, want)
	}
}

// --- RSM4 ordering ---------------------------------------------------------

func TestCheckRuns_RSM4_ReadyBeforeLaunch(t *testing.T) {
	r := runID(5)
	// agent_ready with launch_initiated NEVER seen ⇒ out of order.
	lines := []line{
		{seq: 1, evType: core.EventTypeRunStarted, payload: rStarted(r, core.WorkflowModeSingle)},
		{seq: 2, evType: core.EventTypeAgentReady, payload: rReady(r)},
		{seq: 3, evType: core.EventTypeRunCompleted, payload: rCompleted(r)},
	}
	rep, err := replay.CheckRuns(writeLog(t, lines), core.EventID{}, false, replay.DefaultRunCheckers())
	if err != nil {
		t.Fatalf("CheckRuns: %v", err)
	}
	if got, want := runKeys(rep.Violations), []string{"RSM4/" + r.String()}; !equalStrings(got, want) {
		t.Fatalf("violations = %v, want %v", got, want)
	}
}

func TestCheckRuns_RSM4_LaunchBeforeReadyClean(t *testing.T) {
	r := runID(6)
	lines := []line{
		{seq: 1, evType: core.EventTypeLaunchInitiated, payload: rLaunch(r)},
		{seq: 2, evType: core.EventTypeAgentReady, payload: rReady(r)},
		{seq: 3, evType: core.EventTypeRunCompleted, payload: rCompleted(r)},
	}
	rep, err := replay.CheckRuns(writeLog(t, lines), core.EventID{}, false, replay.DefaultRunCheckers())
	if err != nil {
		t.Fatalf("CheckRuns: %v", err)
	}
	if len(rep.Violations) != 0 {
		t.Fatalf("clean ordering flagged: %v", rep.Violations)
	}
}

// --- committed fixtures (write both; the RT10 acceptance) ------------------

func TestCheckRuns_CommittedFixtures(t *testing.T) {
	root := repoRoot(t)
	hung := filepath.Join(root, "testdata", "daemon-runs", "fixtures", "hung-run.jsonl")
	clean := filepath.Join(root, "testdata", "daemon-runs", "fixtures", "clean-run.jsonl")

	repHung, err := replay.CheckRuns(hung, core.EventID{}, false, replay.DefaultRunCheckers())
	if err != nil {
		t.Fatalf("CheckRuns(hung): %v", err)
	}
	if len(repHung.Violations) == 0 {
		t.Fatalf("hung-run fixture NOT flagged by RSM9")
	}
	sawRSM9 := false
	for _, v := range repHung.Violations {
		if v.Rule == "RSM9" {
			sawRSM9 = true
		}
	}
	if !sawRSM9 {
		t.Fatalf("hung-run flagged but not by RSM9: %v", repHung.Violations)
	}

	repClean, err := replay.CheckRuns(clean, core.EventID{}, false, replay.DefaultRunCheckers())
	if err != nil {
		t.Fatalf("CheckRuns(clean): %v", err)
	}
	if len(repClean.Violations) != 0 {
		t.Fatalf("clean-run fixture flagged: %v", repClean.Violations)
	}
}

// --- the extracted baseline corpus is clean + reproduces the manifest ------

func TestExtractRunCorpus_ManifestAndCleanReplay(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "testdata", "daemon-runs", "baseline-2026-07-14")

	// Pinned manifest counts (P1 anchor-pinning, D13). Regenerated by
	// scripts/extract-run-corpus.py over the committed source log.
	var man struct {
		Count        int            `json:"count"`
		Resumed      int            `json:"resumed"`
		Terminated   int            `json:"terminated"`
		Unterminated int            `json:"unterminated"`
		FailureClass int            `json:"failure_class"`
		Strata       map[string]int `json:"strata"`
	}
	readJSON(t, filepath.Join(dir, "manifest.json"), &man)
	if man.Count != 6 || man.Resumed != 2 || man.Terminated != 4 ||
		man.Unterminated != 0 || man.FailureClass != 2 {
		t.Fatalf("manifest aggregate drift: %+v", man)
	}
	wantStrata := map[string]int{
		"single": 1, "review-loop-resume": 1, "dot": 1,
		"merge-failure": 1, "run-stale": 1, "hung-relaunch": 1,
	}
	for k, v := range wantStrata {
		if man.Strata[k] != v {
			t.Fatalf("stratum %q = %d, want %d (full: %+v)", k, man.Strata[k], v, man.Strata)
		}
	}
	if len(man.Strata) != len(wantStrata) {
		t.Fatalf("unexpected strata: %+v", man.Strata)
	}

	// Every per-run stream in the CLEAN baseline replays with zero violations.
	runsDir := filepath.Join(dir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Fatalf("read runs dir: %v", err)
	}
	streams := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		streams++
		rep, err := replay.CheckRuns(filepath.Join(runsDir, e.Name()), core.EventID{}, false, replay.DefaultRunCheckers())
		if err != nil {
			t.Fatalf("CheckRuns(%s): %v", e.Name(), err)
		}
		if len(rep.Violations) != 0 {
			t.Fatalf("clean baseline stream %s flagged: %v", e.Name(), rep.Violations)
		}
	}
	if streams != man.Count {
		t.Fatalf("per-run streams = %d, manifest count = %d", streams, man.Count)
	}
}

// --- the StimulusSynthesizer -----------------------------------------------

func TestSynthesizeSchedule(t *testing.T) {
	cases := []struct {
		name    string
		summary replay.RunSummary
		want    []string // ordered step kinds
	}{
		{
			name:    "single clean",
			summary: replay.RunSummary{RunID: "a", Stratum: "single", TerminalType: "run_completed", Mode: "single"},
			want:    []string{"start_dispatch", "launched", "agent_ready", "agent_completed"},
		},
		{
			name:    "review-loop resumed",
			summary: replay.RunSummary{RunID: "b", Stratum: "review-loop-resume", TerminalType: "review_loop_cycle_complete", Mode: "review-loop", Resumed: true},
			want:    []string{"start_dispatch", "launched", "agent_ready", "input_ack", "outcome_received"},
		},
		{
			name:    "hung relaunch",
			summary: replay.RunSummary{RunID: "c", Stratum: "hung-relaunch", TerminalType: "agent_ready_timeout", Resumed: true},
			want:    []string{"start_dispatch", "launched", "timer_fired"},
		},
		{
			name:    "run stale",
			summary: replay.RunSummary{RunID: "d", Stratum: "run-stale", TerminalType: "run_stale", Mode: "single"},
			want:    []string{"start_dispatch", "launched", "agent_ready", "heartbeat_stale"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sched := replay.SynthesizeSchedule(tc.summary)
			if sched.RunID != tc.summary.RunID {
				t.Fatalf("run_id = %q, want %q", sched.RunID, tc.summary.RunID)
			}
			got := make([]string, len(sched.Steps))
			for i, s := range sched.Steps {
				got[i] = s.Kind
			}
			if !equalStrings(got, tc.want) {
				t.Fatalf("steps = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- test helpers ----------------------------------------------------------

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- test reads a fixed repo-relative testdata path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}

// repoRoot walks up from the package dir to the module root (go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
