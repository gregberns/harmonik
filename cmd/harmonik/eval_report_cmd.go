package main

// eval_report_cmd.go — harmonik eval report (EH4)
//
// Aggregates .harmonik/eval-results.jsonl (the EH1 collector's record
// format) grouped by (model, difficulty): pass-rate, median wall_time_s,
// mean judge_grade. This is the comparison table referenced by DESIGN.md
// §1.3 — the router's training set.
//
// Read-only over the collector output. Deterministic.
// Bead: hk-eval-harness-report-346ta (EH4).

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const evalReportHelp = `harmonik eval report — aggregate eval-results.jsonl by (model, difficulty)

USAGE
  harmonik eval report [flags]

FLAGS
  --project DIR   Project directory (default: current working directory)
  --input FILE    Path to eval-results.jsonl (default: <project>/.harmonik/eval-results.jsonl)
  --format FORM   Output format: table (default) or json

DESCRIPTION
  Reads eval-results.jsonl (the "harmonik eval collect" output) and groups
  records by (model, difficulty), computing:
    - n            number of records in the group
    - pass_rate    fraction of records with pass=true
    - median_wall_time_s   median of wall_time_s
    - mean_judge_grade     mean of judge_grade (nulls excluded; null if none)

EXIT CODES
  0   Report printed (or zero records found)
  1   Error reading input
`

// evalReportRecord is the subset of the EH1 record schema this report needs.
type evalReportRecord struct {
	Model      string   `json:"model"`
	Difficulty string   `json:"difficulty"`
	Pass       bool     `json:"pass"`
	WallTimeS  float64  `json:"wall_time_s"`
	JudgeGrade *float64 `json:"judge_grade"`
}

// evalReportGroup accumulates one (model, difficulty) bucket.
type evalReportGroup struct {
	Model           string    `json:"model"`
	Difficulty      string    `json:"difficulty"`
	N               int       `json:"n"`
	PassRate        float64   `json:"pass_rate"`
	MedianWallTimeS float64   `json:"median_wall_time_s"`
	MeanJudgeGrade  *float64  `json:"mean_judge_grade"`
	wallTimes       []float64 `json:"-"`
	passCount       int       `json:"-"`
	judgeSum        float64   `json:"-"`
	judgeCount      int       `json:"-"`
}

// runEvalReport is the testable entry-point for `harmonik eval report`.
func runEvalReport(args []string, stdout, stderr io.Writer, getwd func() (string, error)) int {
	fs := flag.NewFlagSet("eval report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectDir := fs.String("project", "", "Project directory (default: cwd)")
	inputFile := fs.String("input", "", "Path to eval-results.jsonl")
	format := fs.String("format", "table", "Output format: table or json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(stdout, evalReportHelp)
			return 0
		}
		fmt.Fprintf(stderr, "harmonik eval report: %v\n", err)
		return 1
	}

	if *format != "table" && *format != "json" {
		fmt.Fprintf(stderr, "harmonik eval report: unknown --format %q (want table or json)\n", *format)
		return 1
	}

	if *projectDir == "" {
		wd, err := getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik eval report: cannot determine working directory: %v\n", err)
			return 1
		}
		*projectDir = wd
	}
	absProject, err := filepath.Abs(*projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik eval report: cannot resolve project path: %v\n", err)
		return 1
	}

	if *inputFile == "" {
		*inputFile = filepath.Join(absProject, ".harmonik", "eval-results.jsonl")
	}

	records, err := evalReadReportRecords(*inputFile)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik eval report: reading input: %v\n", err)
		return 1
	}

	groups := evalAggregateReport(records)

	if *format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(groups); err != nil {
			fmt.Fprintf(stderr, "harmonik eval report: encoding output: %v\n", err)
			return 1
		}
		return 0
	}

	evalPrintReportTable(stdout, groups)
	return 0
}

// evalReadReportRecords reads eval-results.jsonl, skipping malformed lines.
func evalReadReportRecords(path string) ([]evalReportRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var records []evalReportRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec evalReportRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // skip malformed lines
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// evalAggregateReport groups records by (model, difficulty) and computes
// pass-rate, median wall_time_s, and mean judge_grade per group. Groups are
// returned sorted by (model, difficulty) for stable output.
func evalAggregateReport(records []evalReportRecord) []evalReportGroup {
	index := map[string]*evalReportGroup{}
	var order []string

	for _, rec := range records {
		key := rec.Model + "\x00" + rec.Difficulty
		g := index[key]
		if g == nil {
			g = &evalReportGroup{Model: rec.Model, Difficulty: rec.Difficulty}
			index[key] = g
			order = append(order, key)
		}
		g.N++
		if rec.Pass {
			g.passCount++
		}
		g.wallTimes = append(g.wallTimes, rec.WallTimeS)
		if rec.JudgeGrade != nil {
			g.judgeSum += *rec.JudgeGrade
			g.judgeCount++
		}
	}

	sort.Strings(order)

	groups := make([]evalReportGroup, 0, len(order))
	for _, key := range order {
		g := *index[key]
		if g.N > 0 {
			g.PassRate = float64(g.passCount) / float64(g.N)
		}
		g.MedianWallTimeS = evalMedian(g.wallTimes)
		if g.judgeCount > 0 {
			mean := g.judgeSum / float64(g.judgeCount)
			g.MeanJudgeGrade = &mean
		}
		groups = append(groups, g)
	}
	return groups
}

// evalMedian returns the median of a slice of float64 (does not mutate the input).
func evalMedian(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// evalPrintReportTable renders the aggregation as a plain-text table.
func evalPrintReportTable(w io.Writer, groups []evalReportGroup) {
	if len(groups) == 0 {
		fmt.Fprintln(w, "harmonik eval report: no records found")
		return
	}
	fmt.Fprintf(w, "%-30s %-12s %5s %10s %14s %10s\n",
		"MODEL", "DIFFICULTY", "N", "PASS_RATE", "MEDIAN_WALL_S", "MEAN_GRADE")
	for _, g := range groups {
		gradeStr := "-"
		if g.MeanJudgeGrade != nil {
			gradeStr = fmt.Sprintf("%.2f", *g.MeanJudgeGrade)
		}
		fmt.Fprintf(w, "%-30s %-12s %5d %10.2f %14.1f %10s\n",
			g.Model, g.Difficulty, g.N, g.PassRate, g.MedianWallTimeS, gradeStr)
	}
}
