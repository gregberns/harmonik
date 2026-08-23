package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/sessiondata"
)

func slugForTest(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	return strings.NewReplacer("/", "-", ".", "-").Replace(resolved)
}

func writeTranscript(t *testing.T, claudeProjectsDir, dirName, sessionID, branch string, inTok, outTok int) {
	t.Helper()
	dir := filepath.Join(claudeProjectsDir, dirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","timestamp":"2026-06-21T12:00:00Z","gitBranch":"` + branch +
		`","message":{"model":"claude-opus-4","usage":{"input_tokens":` + itoa(inTok) +
		`,"output_tokens":` + itoa(outTok) + `,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func newScopeCase(t *testing.T) (projectDir, claudeProjectsDir string, cfg Config) {
	t.Helper()
	root := t.TempDir()
	projectDir = filepath.Join(root, "named-project")
	claudeProjectsDir = filepath.Join(root, "claude-projects")
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claudeProjectsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfg = Config{
		Since:             "2026-06-21T00:00:00Z",
		Until:             "2026-06-22T00:00:00Z",
		ClaudeProjectsDir: claudeProjectsDir,
		ProjectDir:        projectDir,
	}
	return projectDir, claudeProjectsDir, cfg
}

// TestRunAnalysis_CountsTheNamedProjectAndExcludesTheNeighbour is the
// regression for the report that counted another project's sessions. The
// neighbour directory is the harmonik checkout that the scan used to read no
// matter which project the operator named.
func TestRunAnalysis_CountsTheNamedProjectAndExcludesTheNeighbour(t *testing.T) {
	t.Setenv("USER", "gb")
	projectDir, claudeProjectsDir, cfg := newScopeCase(t)

	writeTranscript(t, claudeProjectsDir, slugForTest(t, projectDir), "own-session", "work/alpha", 1_000_000, 100_000)
	writeTranscript(t, claudeProjectsDir, "-Users-gb-github-harmonik", "neighbour-session", "work/other", 2_000_000, 200_000)

	result, err := RunAnalysis(cfg)
	if err != nil {
		t.Fatalf("RunAnalysis: %v", err)
	}
	if result.OrchSessionCount != 1 {
		t.Errorf("OrchSessionCount = %d, want 1", result.OrchSessionCount)
	}
	for _, s := range result.TopOrchestrators {
		if s.SessionID == "neighbour-session" {
			t.Errorf("the report counted a session from another project: %+v", s)
		}
	}
	if len(result.TopOrchestrators) != 1 || result.TopOrchestrators[0].SessionID != "own-session" {
		t.Errorf("TopOrchestrators = %+v, want only own-session", result.TopOrchestrators)
	}
	wantCost := sessiondata.ComputeCost(
		sessiondata.TokenUsage{Input: 1_000_000, Output: 100_000}, "claude-opus-4")
	if diff := result.OrchestratorCostUSD - wantCost; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("OrchestratorCostUSD = %f, want %f", result.OrchestratorCostUSD, wantCost)
	}
	if result.TotalCostUSD <= 0 {
		t.Errorf("TotalCostUSD = %f, want the named project's spend", result.TotalCostUSD)
	}
}

// TestTranscriptSlug_MatchesTheNamesClaudeWrites pins the naming convention
// against directory names seen in a real transcript store.
func TestTranscriptSlug_MatchesTheNamesClaudeWrites(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/Users/gb/github/harmonik", "-Users-gb-github-harmonik"},
		{"/private/tmp/h/bravo-xt", "-private-tmp-h-bravo-xt"},
		{
			"/Users/gb/github/harmonik/.claude/worktrees/alpha-coldstart-diag",
			"-Users-gb-github-harmonik--claude-worktrees-alpha-coldstart-diag",
		},
		{
			"/private/tmp/h/bravo-xt/.harmonik/worktrees/019fe9de",
			"-private-tmp-h-bravo-xt--harmonik-worktrees-019fe9de",
		},
	}
	for _, c := range cases {
		if got := transcriptSlug(c.path); got != c.want {
			t.Errorf("transcriptSlug(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestRunAnalysis_CountsSessionsFromTheProjectsOwnWorktrees checks that work
// done in a worktree of the project still counts for the project.
func TestRunAnalysis_CountsSessionsFromTheProjectsOwnWorktrees(t *testing.T) {
	projectDir, claudeProjectsDir, cfg := newScopeCase(t)
	root := slugForTest(t, projectDir)

	writeTranscript(t, claudeProjectsDir, root+"--claude-worktrees-alpha", "crew-session", "work/alpha", 500_000, 50_000)
	writeTranscript(t, claudeProjectsDir, root+"--harmonik-worktrees-019f", "run-session", "work/beta", 500_000, 50_000)

	result, err := RunAnalysis(cfg)
	if err != nil {
		t.Fatalf("RunAnalysis: %v", err)
	}
	if result.OrchSessionCount != 2 {
		t.Errorf("OrchSessionCount = %d, want 2 worktree sessions", result.OrchSessionCount)
	}
	if result.UnattributedSessionCount != 0 {
		t.Errorf("UnattributedSessionCount = %d, want 0", result.UnattributedSessionCount)
	}
	if result.OrchestratorCostUSD <= 0 {
		t.Errorf("OrchestratorCostUSD = %f, want the worktree spend", result.OrchestratorCostUSD)
	}
}

// TestRunAnalysis_ReportsSessionsItCannotPlace checks the honest middle case. A
// directory name that starts with the project path but names no place inside it
// can come from a deleted worktree of this project or from a different project.
// The report must show it on its own line and keep it out of the total.
func TestRunAnalysis_ReportsSessionsItCannotPlace(t *testing.T) {
	projectDir, claudeProjectsDir, cfg := newScopeCase(t)
	root := slugForTest(t, projectDir)

	writeTranscript(t, claudeProjectsDir, root, "own-session", "work/alpha", 1_000_000, 0)
	writeTranscript(t, claudeProjectsDir, root+"-wt-bravo", "cannot-place", "work/beta", 1_000_000, 0)

	result, err := RunAnalysis(cfg)
	if err != nil {
		t.Fatalf("RunAnalysis: %v", err)
	}
	if result.OrchSessionCount != 1 {
		t.Errorf("OrchSessionCount = %d, want 1", result.OrchSessionCount)
	}
	if result.UnattributedSessionCount != 1 {
		t.Fatalf("UnattributedSessionCount = %d, want 1", result.UnattributedSessionCount)
	}
	if result.UnattributedCostUSD <= 0 {
		t.Errorf("UnattributedCostUSD = %f, want the spend of the session it cannot place", result.UnattributedCostUSD)
	}
	if result.TotalCostUSD >= result.UnattributedCostUSD*2 {
		t.Errorf("TotalCostUSD = %f, want the unattributed %f left out",
			result.TotalCostUSD, result.UnattributedCostUSD)
	}
	var sb strings.Builder
	if err := PrintSummary(result, &sb); err != nil {
		t.Fatalf("PrintSummary: %v", err)
	}
	if !strings.Contains(sb.String(), "Unattributed:") {
		t.Errorf("summary does not name the sessions it cannot place:\n%s", sb.String())
	}
}

// TestRunAnalysis_UnpricedRunIsNotReportedAsZeroDollars checks a run whose
// model carries no price. The collector writes no cost for it. The report must
// say the cost is not known instead of printing $0.0000, which reads as free
// work and made a real project look 0.0% productive.
func TestRunAnalysis_UnpricedRunIsNotReportedAsZeroDollars(t *testing.T) {
	projectDir, _, cfg := newScopeCase(t)

	priced := sessiondata.TokenUsage{Input: 100, Output: 50}
	cost := sessiondata.ComputeCost(priced, "claude-sonnet-4-6")
	if err := sessiondata.Append(projectDir, sessiondata.Record{
		SchemaVersion: 1, RunID: "run-priced", BeadID: "bx-priced",
		Harness: "claude-code", Model: "claude-sonnet-4-6", Success: true,
		StartedAt: "2026-06-21T12:00:00Z", EndedAt: "2026-06-21T12:30:00Z",
		TokensTotal: priced, CostUSD: &cost, TurnCount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessiondata.Append(projectDir, sessiondata.Record{
		SchemaVersion: 1, RunID: "run-unpriced", BeadID: "bx-unpriced",
		Harness: "codex", Success: true,
		StartedAt: "2026-06-21T12:00:00Z", EndedAt: "2026-06-21T12:30:00Z",
		TokensTotal: sessiondata.TokenUsage{Input: 900_000, Output: 40_000}, TurnCount: 7,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := RunAnalysis(cfg)
	if err != nil {
		t.Fatalf("RunAnalysis: %v", err)
	}
	if result.RunCount != 2 {
		t.Errorf("RunCount = %d, want 2", result.RunCount)
	}
	if result.UnpricedRunCount != 1 {
		t.Errorf("UnpricedRunCount = %d, want 1", result.UnpricedRunCount)
	}
	if result.UnpricedUsage.Total() != 940_000 {
		t.Errorf("UnpricedUsage.Total = %d, want 940000", result.UnpricedUsage.Total())
	}
	if diff := result.ProductiveCostUSD - cost; diff > 0.000001 || diff < -0.000001 {
		t.Errorf("ProductiveCostUSD = %f, want the priced run's %f", result.ProductiveCostUSD, cost)
	}
	if result.ProductiveCostUSD <= 0 {
		t.Errorf("ProductiveCostUSD = %f, want more than zero when spend exists", result.ProductiveCostUSD)
	}
	for _, b := range result.TopBeads {
		if b.BeadID == "bx-unpriced" && b.UnpricedRuns != 1 {
			t.Errorf("bead bx-unpriced UnpricedRuns = %d, want 1", b.UnpricedRuns)
		}
	}

	var sb strings.Builder
	if err := PrintSummary(result, &sb); err != nil {
		t.Fatalf("PrintSummary: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "Cost unknown:") {
		t.Errorf("summary does not count the runs it cannot price:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "bx-unpriced") && !strings.Contains(line, "cost unknown") {
			t.Errorf("summary states a cost for a run it cannot price: %q", line)
		}
	}
}

// TestRunAnalysis_FindsSessionsWhenTheProjectPathIsASymlink covers the shape of
// the project in the defect report. The operator names /tmp/h/bravo-xt, macOS
// resolves /tmp to /private/tmp, and Claude Code names the transcript directory
// after the resolved path. The report must still find the sessions.
func TestRunAnalysis_FindsSessionsWhenTheProjectPathIsASymlink(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real", "named-project")
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "real"), linkDir); err != nil {
		t.Skipf("this file system makes no symlink: %v", err)
	}
	viaLink := filepath.Join(linkDir, "named-project")

	claudeProjectsDir := filepath.Join(root, "claude-projects")
	if err := os.MkdirAll(claudeProjectsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, claudeProjectsDir, slugForTest(t, realDir), "own-session", "work/alpha", 1_000_000, 0)

	result, err := RunAnalysis(Config{
		Since:             "2026-06-21T00:00:00Z",
		Until:             "2026-06-22T00:00:00Z",
		ClaudeProjectsDir: claudeProjectsDir,
		ProjectDir:        viaLink, // the operator names the link
	})
	if err != nil {
		t.Fatalf("RunAnalysis: %v", err)
	}
	if result.OrchSessionCount != 1 {
		t.Errorf("OrchSessionCount = %d, want 1 through the symlinked path", result.OrchSessionCount)
	}
	if result.OrchestratorCostUSD <= 0 {
		t.Errorf("OrchestratorCostUSD = %f, want the project's spend", result.OrchestratorCostUSD)
	}
}
