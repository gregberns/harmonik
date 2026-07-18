package main

// eval_metrics_cmd.go — harmonik eval metrics (WS3b)
//
// Computes deterministic objective quality feeders and writes
// .harmonik/metrics.json in the eval run's working directory.
//
// Called from scripts/eval-grade.sh on grade SUCCESS so the metrics file is
// present when the judge node reads it.  Each feeder is non-fatal: if a tool
// is absent or fails, the corresponding field is null / empty.
//
// Feeders (specs/02-quality-assessment.md §Part 1):
//   gofmt -l      → gofmt_clean, gofmt_unformatted
//   go vet        → vet_clean, vet_issues
//   gocyclo       → gocyclo_max (if tool installed)
//   grep patterns → todo_count, fixme_count, stub_count
//   git show HEAD → diff_added_lines vs reference_line_budget label
//   bead labels   → expected_big_o, reference_line_budget
//   hidden_test.go → hidden_test_pass, hidden_test_pass_count
//   deadcode      → unused_symbols (if tool installed)
//
// Guardrail feeders (specs/02-quality-assessment.md §Part 3; WS3e, see
// eval_guardrails_lygpp.go):
//   rubric_version  → G6 rubric weight-set stamp
//   diff self-ID scan → self_id_matches (G2)
//   diff test-file scan → test_file_touched (G5)
//   bead-id sample hash → cross_check_sample (O-Q4)
//
// Bead: hk-eval-prog-quality-feeders-k5bxl (WS3b).

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const evalMetricsHelp = `harmonik eval metrics — compute objective quality feeders and write metrics.json

USAGE
  harmonik eval metrics [flags]

FLAGS
  --workdir DIR    Eval worktree root (default: current working directory)

DESCRIPTION
  Runs deterministic quality checks against the HEAD commit of the eval
  worktree and writes .harmonik/metrics.json.  Must be called after the
  implementer has committed its solution (i.e. after grade SUCCESS).

  Tool-dependent feeders (gocyclo, deadcode) emit null when the tool is
  not on PATH — the judge handles absence gracefully.

OUTPUT
  .harmonik/metrics.json (compact JSON, schema_version 1)
`

// evalMetricsRecord is the metrics.json schema (schema_version 1).
type evalMetricsRecord struct {
	SchemaVersion       int      `json:"schema_version"`
	RubricVersion       int      `json:"rubric_version"`
	GofmtClean          bool     `json:"gofmt_clean"`
	GofmtUnformatted    []string `json:"gofmt_unformatted"`
	VetClean            bool     `json:"vet_clean"`
	VetIssues           []string `json:"vet_issues"`
	GocycloMax          *int     `json:"gocyclo_max"`
	TodoCount           int      `json:"todo_count"`
	FixmeCount          int      `json:"fixme_count"`
	StubCount           int      `json:"stub_count"`
	DiffAddedLines      int      `json:"diff_added_lines"`
	ReferenceLineBudget *int     `json:"reference_line_budget"`
	WithinBudget        *bool    `json:"within_budget"`
	ExpectedBigO        *string  `json:"expected_big_o"`
	HiddenTestPass      *bool    `json:"hidden_test_pass"`
	HiddenTestPassCount *int     `json:"hidden_test_pass_count"`
	UnusedSymbols       []string `json:"unused_symbols"`
	SelfIDMatches       []string `json:"self_id_matches"`
	TestFileTouched     bool     `json:"test_file_touched"`
	CrossCheckSample    bool     `json:"cross_check_sample"`
}

// runEvalMetrics implements `harmonik eval metrics`.
func runEvalMetrics(args []string, stdout, stderr io.Writer, getwd func() (string, error)) int {
	fs := flag.NewFlagSet("eval metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workdirFlag := fs.String("workdir", "", "Eval worktree root (default: cwd)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(stdout, evalMetricsHelp)
			return 0
		}
		fmt.Fprintf(stderr, "harmonik eval metrics: %v\n", err)
		return 1
	}

	workdir := *workdirFlag
	if workdir == "" {
		wd, err := getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik eval metrics: cwd: %v\n", err)
			return 1
		}
		workdir = wd
	}
	abs, err := filepath.Abs(workdir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik eval metrics: abs: %v\n", err)
		return 1
	}

	rec, err := evalComputeMetrics(abs)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik eval metrics: %v\n", err)
		return 1
	}

	out, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik eval metrics: marshal: %v\n", err)
		return 1
	}

	outPath := filepath.Join(abs, ".harmonik", "metrics.json")
	if err := os.WriteFile(outPath, append(out, '\n'), 0o644); err != nil {
		fmt.Fprintf(stderr, "harmonik eval metrics: write %s: %v\n", outPath, err)
		return 1
	}

	fmt.Fprintf(stdout, "harmonik eval metrics: wrote %s\n", outPath)
	return 0
}

// evalComputeMetrics runs all feeders and assembles the metrics record.
func evalComputeMetrics(workdir string) (evalMetricsRecord, error) {
	rec := evalMetricsRecord{
		SchemaVersion:    1,
		RubricVersion:    evalRubricVersion,
		GofmtUnformatted: []string{},
		VetIssues:        []string{},
		UnusedSymbols:    []string{},
		SelfIDMatches:    []string{},
	}

	beadID, err := evalReadBeadIDFromTask(workdir)
	if err != nil {
		return rec, fmt.Errorf("reading agent-task.md: %w", err)
	}

	taskID := evalDeriveTaskID(beadID)
	evaltaskDir := "evaltasks/" + taskID
	rec.CrossCheckSample = evalShouldCrossCheckSample(beadID)

	labels, _ := evalFetchBeadLabels(beadID, workdir)
	if v := evalLabelValue(labels, "expected_big_o"); v != "" {
		rec.ExpectedBigO = &v
	}
	if v := evalLabelValue(labels, "reference_line_budget"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rec.ReferenceLineBudget = &n
		}
	}

	changedFiles, _ := evalChangedGoFiles(workdir)

	rec.GofmtClean, rec.GofmtUnformatted = evalGofmtCheck(workdir, changedFiles)
	rec.VetClean, rec.VetIssues = evalVetCheck(workdir, evaltaskDir)
	rec.GocycloMax = evalGocycloMax(workdir, changedFiles)

	diff, _ := evalGetHeadDiff(workdir)
	rec.TodoCount, rec.FixmeCount, rec.StubCount = evalCountDiffMarkers(diff)
	rec.DiffAddedLines = evalCountDiffAddedLines(diff)
	rec.SelfIDMatches = evalScrubSelfID(diff)
	rec.TestFileTouched = evalDiffTouchesTestFile(diff)

	if rec.ReferenceLineBudget != nil {
		within := rec.DiffAddedLines <= *rec.ReferenceLineBudget
		rec.WithinBudget = &within
	}

	hiddenTestPath := filepath.Join(workdir, evaltaskDir, "hidden_test.go")
	if _, err := os.Stat(hiddenTestPath); err == nil {
		rec.HiddenTestPass, rec.HiddenTestPassCount = evalRunHiddenTest(workdir, evaltaskDir)
	}

	rec.UnusedSymbols = evalDeadcodeCheck(workdir, evaltaskDir)

	return rec, nil
}

// evalReadBeadIDFromTask reads the bead_id field from .harmonik/agent-task.md.
func evalReadBeadIDFromTask(workdir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(workdir, ".harmonik", "agent-task.md"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "bead_id:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "bead_id:")), nil
		}
	}
	return "", fmt.Errorf("bead_id not found in agent-task.md")
}

// evalDeriveTaskID strips the hk- prefix and the trailing random suffix.
// hk-eval-fizzbuzz-avjjr → eval-fizzbuzz
func evalDeriveTaskID(beadID string) string {
	s := strings.TrimPrefix(beadID, "hk-")
	if idx := strings.LastIndex(s, "-"); idx != -1 {
		s = s[:idx]
	}
	return s
}

// evalChangedGoFiles returns non-test .go files changed in HEAD.
func evalChangedGoFiles(workdir string) ([]string, error) {
	cmd := exec.Command("git", "diff-tree", "--no-commit-id", "-r", "--name-only", "HEAD")
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasSuffix(line, ".go") && !strings.HasSuffix(line, "_test.go") {
			files = append(files, line)
		}
	}
	return files, nil
}

// evalGofmtCheck runs gofmt -l on changed files and returns clean status.
func evalGofmtCheck(workdir string, files []string) (clean bool, unformatted []string) {
	if len(files) == 0 {
		return true, []string{}
	}
	args := append([]string{"-l"}, files...)
	cmd := exec.Command("gofmt", args...)
	cmd.Dir = workdir
	out, _ := cmd.Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			unformatted = append(unformatted, line)
		}
	}
	if unformatted == nil {
		unformatted = []string{}
	}
	return len(unformatted) == 0, unformatted
}

// evalVetCheck runs go vet and returns clean status and any issues.
func evalVetCheck(workdir, evaltaskDir string) (clean bool, issues []string) {
	cmd := exec.Command("go", "vet", "./"+evaltaskDir+"/...")
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			issues = append(issues, line)
		}
	}
	if issues == nil {
		issues = []string{}
	}
	return err == nil, issues
}

// evalGocycloMax runs gocyclo and returns the max complexity, or nil if unavailable.
func evalGocycloMax(workdir string, files []string) *int {
	if len(files) == 0 {
		return nil
	}
	if _, err := exec.LookPath("gocyclo"); err != nil {
		return nil
	}
	args := append([]string{}, files...)
	cmd := exec.Command("gocyclo", args...)
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	max := 0
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 1 {
			if n, err := strconv.Atoi(parts[0]); err == nil {
				found = true
				if n > max {
					max = n
				}
			}
		}
	}
	if !found {
		// No parseable gocyclo output — distinct from a real max of 0.
		return nil
	}
	return &max
}

// evalGetHeadDiff returns the unified diff of the HEAD commit.
func evalGetHeadDiff(workdir string) (string, error) {
	cmd := exec.Command("git", "show", "HEAD")
	cmd.Dir = workdir
	out, err := cmd.Output()
	return string(out), err
}

// evalCountDiffMarkers counts TODO/FIXME/stub markers in added diff lines.
func evalCountDiffMarkers(diff string) (todo, fixme, stub int) {
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		if strings.Contains(line, "TODO") {
			todo++
		}
		if strings.Contains(line, "FIXME") {
			fixme++
		}
		if strings.Contains(line, `panic("not implemented")`) {
			stub++
		}
	}
	return
}

// evalCountDiffAddedLines counts added non-test, non-metadata lines in a diff.
func evalCountDiffAddedLines(diff string) int {
	inTestFile := false
	count := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			inTestFile = strings.Contains(line, "_test.go")
		}
		if inTestFile {
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			count++
		}
	}
	return count
}

// evalRunHiddenTest runs tests whose name contains "Hidden" (e.g. TestHiddenFoo)
// and returns pass status and count. -run matches the full "TestXxx" name, so
// the pattern must not anchor past the mandatory "Test" prefix.
func evalRunHiddenTest(workdir, evaltaskDir string) (pass *bool, count *int) {
	cmd := exec.Command("go", "test", "./"+evaltaskDir+"/...", "-run", "Hidden", "-timeout", "60s", "-v")
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	p := err == nil
	pass = &p
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "--- PASS") {
			n++
		}
	}
	count = &n
	return
}

// evalDeadcodeCheck runs deadcode if available and returns unused symbol lines.
func evalDeadcodeCheck(workdir, evaltaskDir string) []string {
	if _, err := exec.LookPath("deadcode"); err != nil {
		return []string{}
	}
	cmd := exec.Command("deadcode", "./"+evaltaskDir+"/...")
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return []string{}
	}
	var symbols []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			symbols = append(symbols, line)
		}
	}
	if symbols == nil {
		return []string{}
	}
	return symbols
}
