package main

// eval_report_hk_eval_harness_report_346ta_test.go
// Sensors for `harmonik eval report` (EH4, bead hk-eval-harness-report-346ta).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func evalReportWriteResults(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write results file: %v", err)
	}
}

func evalReportLine(model, difficulty string, pass bool, wallTimeS float64, judgeGrade *float64) string {
	rec := map[string]any{
		"model":       model,
		"difficulty":  difficulty,
		"pass":        pass,
		"wall_time_s": wallTimeS,
	}
	if judgeGrade != nil {
		rec["judge_grade"] = *judgeGrade
	} else {
		rec["judge_grade"] = nil
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

func floatPtr(f float64) *float64 { return &f }

func TestEvalReport_GroupsByModelAndDifficulty(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "eval-results.jsonl")
	evalReportWriteResults(t, input, []string{
		evalReportLine("claude-code", "simple", true, 100, floatPtr(5)),
		evalReportLine("claude-code", "simple", false, 200, floatPtr(3)),
		evalReportLine("claude-code", "hard", true, 300, nil),
		evalReportLine("pi", "simple", true, 50, floatPtr(4)),
	})

	var stdout, stderr bytes.Buffer
	code := runEvalReport([]string{"--input", input, "--format", "json"}, &stdout, &stderr, os.Getwd)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}

	var groups []evalReportGroup
	if err := json.Unmarshal(stdout.Bytes(), &groups); err != nil {
		t.Fatalf("unmarshal output: %v (output: %s)", err, stdout.String())
	}
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d: %+v", len(groups), groups)
	}

	byKey := map[string]evalReportGroup{}
	for _, g := range groups {
		byKey[g.Model+"/"+g.Difficulty] = g
	}

	cs := byKey["claude-code/simple"]
	if cs.N != 2 {
		t.Errorf("claude-code/simple N = %d, want 2", cs.N)
	}
	if cs.PassRate != 0.5 {
		t.Errorf("claude-code/simple pass_rate = %v, want 0.5", cs.PassRate)
	}
	if cs.MedianWallTimeS != 150 {
		t.Errorf("claude-code/simple median_wall_time_s = %v, want 150", cs.MedianWallTimeS)
	}
	if cs.MeanJudgeGrade == nil || *cs.MeanJudgeGrade != 4 {
		t.Errorf("claude-code/simple mean_judge_grade = %v, want 4", cs.MeanJudgeGrade)
	}

	ch := byKey["claude-code/hard"]
	if ch.MeanJudgeGrade != nil {
		t.Errorf("claude-code/hard mean_judge_grade = %v, want nil", *ch.MeanJudgeGrade)
	}

	pi := byKey["pi/simple"]
	if pi.PassRate != 1.0 {
		t.Errorf("pi/simple pass_rate = %v, want 1.0", pi.PassRate)
	}
}

func TestEvalReport_MissingInputIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runEvalReport([]string{"--input", filepath.Join(dir, "does-not-exist.jsonl")}, &stdout, &stderr, os.Getwd)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no records found") {
		t.Errorf("expected 'no records found' message, got: %s", stdout.String())
	}
}

func TestEvalReport_TableFormat(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "eval-results.jsonl")
	evalReportWriteResults(t, input, []string{
		evalReportLine("claude-code", "simple", true, 100, floatPtr(5)),
	})

	var stdout, stderr bytes.Buffer
	code := runEvalReport([]string{"--input", input}, &stdout, &stderr, os.Getwd)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "MODEL") || !strings.Contains(out, "claude-code") {
		t.Errorf("expected table header + row, got: %s", out)
	}
}
