package main

import (
	"context"
	"encoding/json"
	"errors"
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

func runEvalMetrics(args []string, stdout, stderr io.Writer, getwd func() (string, error)) int {
	fs := flag.NewFlagSet("eval metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workdirFlag := fs.String("workdir", "", "Eval worktree root (default: cwd)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if _, writeErr := fmt.Fprint(stdout, evalMetricsHelp); writeErr != nil {
				return 1
			}
			return 0
		}
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval metrics: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	workdir := *workdirFlag
	if workdir == "" {
		wd, err := getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik eval metrics: cwd: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		workdir = wd
	}
	abs, err := filepath.Abs(workdir)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval metrics: abs: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	rec, err := evalComputeMetrics(abs)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval metrics: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	out, err := json.Marshal(rec)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval metrics: marshal: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	outPath := filepath.Join(abs, ".harmonik", "metrics.json")
	if err := os.WriteFile(outPath, append(out, '\n'), 0o600); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval metrics: write %s: %v\n", outPath, err); writeErr != nil {
			return 1
		}
		return 1
	}

	if _, err := fmt.Fprintf(stdout, "harmonik eval metrics: wrote %s\n", outPath); err != nil {
		return 1
	}
	return 0
}

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

	labels, labelsErr := evalFetchBeadLabels(beadID, workdir)
	if labelsErr != nil {
		labels = nil
	}
	if v := evalLabelValue(labels, "expected_big_o"); v != "" {
		rec.ExpectedBigO = &v
	}
	if v := evalLabelValue(labels, "reference_line_budget"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rec.ReferenceLineBudget = &n
		}
	}

	changedFiles, changedFilesErr := evalChangedGoFiles(workdir)
	if changedFilesErr != nil {
		changedFiles = []string{}
	}

	rec.GofmtClean, rec.GofmtUnformatted = evalGofmtCheck(workdir, changedFiles)
	rec.VetClean, rec.VetIssues = evalVetCheck(workdir, evaltaskDir)
	rec.GocycloMax = evalGocycloMax(workdir, changedFiles)

	diff, diffErr := evalGetHeadDiff(workdir)
	if diffErr != nil {
		diff = ""
	}
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

func evalReadBeadIDFromTask(workdir string) (string, error) {
	// #nosec G304 -- task metadata is read from the eval worktree selected by the local operator.
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

func evalDeriveTaskID(beadID string) string {
	s := strings.TrimPrefix(beadID, "hk-")
	if idx := strings.LastIndex(s, "-"); idx != -1 {
		s = s[:idx]
	}
	return s
}

func evalChangedGoFiles(workdir string) ([]string, error) {
	// #nosec G204 -- git arguments are fixed; workdir is the locally selected eval worktree.
	cmd := exec.CommandContext(context.Background(), "git", "diff-tree", "--no-commit-id", "-r", "--name-only", "HEAD")
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

func evalGofmtCheck(workdir string, files []string) (clean bool, unformatted []string) {
	if len(files) == 0 {
		return true, []string{}
	}
	args := append([]string{"-l"}, files...)
	// #nosec G204 -- file paths come from git's changed-file listing in the selected worktree.
	cmd := exec.CommandContext(context.Background(), "gofmt", args...)
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return false, []string{}
	}
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

func evalVetCheck(workdir, evaltaskDir string) (clean bool, issues []string) {
	// #nosec G204 -- evaltaskDir is derived from the bead ID read from local task metadata.
	cmd := exec.CommandContext(context.Background(), "go", "vet", "./"+evaltaskDir+"/...")
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

func evalGocycloMax(workdir string, files []string) *int {
	if len(files) == 0 {
		return nil
	}
	if _, err := exec.LookPath("gocyclo"); err != nil {
		return nil
	}
	args := append([]string{}, files...)
	// #nosec G204 -- file paths come from git's changed-file listing in the selected worktree.
	cmd := exec.CommandContext(context.Background(), "gocyclo", args...)
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	maxSeen := 0
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 1 {
			if n, err := strconv.Atoi(parts[0]); err == nil {
				found = true
				if n > maxSeen {
					maxSeen = n
				}
			}
		}
	}
	if !found {
		return nil
	}
	return &maxSeen
}

func evalGetHeadDiff(workdir string) (string, error) {
	// #nosec G204 -- git arguments are fixed; workdir is the locally selected eval worktree.
	cmd := exec.CommandContext(context.Background(), "git", "show", "HEAD")
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

func evalRunHiddenTest(workdir, evaltaskDir string) (pass *bool, count *int) {
	// #nosec G204 -- evaltaskDir is derived from the bead ID read from local task metadata.
	cmd := exec.CommandContext(context.Background(), "go", "test", "./"+evaltaskDir+"/...", "-run", "Hidden", "-timeout", "60s", "-v")
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

func evalDeadcodeCheck(workdir, evaltaskDir string) []string {
	if _, err := exec.LookPath("deadcode"); err != nil {
		return []string{}
	}
	// #nosec G204 -- evaltaskDir is derived from the bead ID read from local task metadata.
	cmd := exec.CommandContext(context.Background(), "deadcode", "./"+evaltaskDir+"/...")
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
