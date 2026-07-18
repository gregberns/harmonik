package main

// eval_cmd.go — harmonik eval collect (EH1)
//
// Pure post-run collector: reads <project>/.harmonik/events/events.jsonl,
// groups by run_id, and for each eval run emits one flat JSON record
// (DESIGN.md §1.3 schema) to .harmonik/eval-results.jsonl.
//
// An eval run is identified by the presence of a node_dispatch_requested
// event for the "grade" node (dot_cascade.go emits this for ALL node types,
// including non-agentic shell nodes). Grade pass/fail is inferred from
// whether outcome_emitted for the "judge" node appears — judge only executes
// when grade succeeds per the eval DOT topology.
//
// Read-only over the log, off the daemon hot path, deterministic.
// Bead: hk-eval-harness-collector-uavgd (EH1).

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const evalCmdHelp = `harmonik eval — eval-harness tooling

USAGE
  harmonik eval <verb> [flags]

VERBS
  collect   Collect results from events.jsonl and write to eval-results.jsonl
  metrics   Compute objective quality feeders and write .harmonik/metrics.json
  report    Aggregate eval-results.jsonl by (model, difficulty)

EXAMPLES
  harmonik eval collect
  harmonik eval collect --project /path/to/project
  harmonik eval collect --output /tmp/results.jsonl
  harmonik eval metrics
  harmonik eval metrics --workdir /path/to/worktree
  harmonik eval report
  harmonik eval report --format json
`

const evalCollectHelp = `harmonik eval collect — collect eval run results from events.jsonl

USAGE
  harmonik eval collect [flags]

FLAGS
  --project DIR        Project directory (default: current working directory)
  --events-file FILE   Path to events.jsonl (default: <project>/.harmonik/events/events.jsonl)
  --output FILE        Output file (default: <project>/.harmonik/eval-results.jsonl)
  --run-id ID          Filter to a specific run_id (optional)

DESCRIPTION
  Reads events.jsonl, groups events by run_id, and for each eval run
  (a run that passed through a "grade" node) emits one flat JSON record to
  the output file. Appends to the output file (existing records are preserved);
  run_ids already present in the output file are skipped, so re-running collect
  does not double-count.

  Eval runs are identified by the presence of a node_dispatch_requested event
  for the "grade" node. Grade pass/fail is inferred from whether an
  outcome_emitted event for the "judge" node appears (judge only runs when
  grade succeeds). Bead labels (task_id, difficulty, check_kind) are fetched
  via "br show". The Pi model name is read from .harmonik/config.yaml.

OUTPUT SCHEMA
  {"schema_version":1,"run_id":"...","bead_id":"...","task_id":"eval-fizzbuzz",
   "difficulty":"simple","model":"openrouter/qwen/qwen3-coder","harness":"pi",
   "pass":true,"check_kind":"unit-test","wall_time_s":214.7,
   "implement_time_s":191.2,"judge_grade":null,"judge_notes":null,
   "commit_sha":"abcd123","timestamp":"2026-07-02T22:14:03Z"}

EXIT CODES
  0   Records written (or zero eval runs found)
  1   Error reading input or writing output
`

// evalEnvelope is the minimal event envelope we decode from each JSONL line.
type evalEnvelope struct {
	Type          string          `json:"type"`
	TimestampWall time.Time       `json:"timestamp_wall"`
	RunID         *string         `json:"run_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// evalRunState accumulates events for one run_id.
type evalRunState struct {
	beadID          string
	startedAt       string // from run_started payload
	endedAt         string // from run_completed or run_failed payload
	completedWall   time.Time
	harness         string  // from harness_selected.agent_type
	implSecs        float64 // from implementer_phase_complete.duration_seconds
	gradeDispatched bool    // node_dispatch_requested where node_id=="grade" → eval run
	judgeOutcome    bool    // outcome_emitted where node_id=="judge" → grade passed
	commitSHA       string  // last checkpoint_written.commit_hash
}

// evalResultRecord is the output schema (DESIGN.md §1.3).
type evalResultRecord struct {
	SchemaVersion  int     `json:"schema_version"`
	RunID          string  `json:"run_id"`
	BeadID         string  `json:"bead_id"`
	TaskID         string  `json:"task_id"`
	Difficulty     string  `json:"difficulty"`
	Model          string  `json:"model"`
	Harness        string  `json:"harness"`
	Pass           bool    `json:"pass"`
	CheckKind      string  `json:"check_kind"`
	WallTimeS      float64 `json:"wall_time_s"`
	ImplementTimeS float64 `json:"implement_time_s"`
	JudgeGrade     *int    `json:"judge_grade"`
	JudgeNotes     *string `json:"judge_notes"`
	CommitSHA      string  `json:"commit_sha"`
	Timestamp      string  `json:"timestamp"`
}

// runEvalCollect is the testable entry-point for `harmonik eval collect`.
func runEvalCollect(args []string, stdout, stderr io.Writer, getwd func() (string, error)) int {
	fs := flag.NewFlagSet("eval collect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectDir := fs.String("project", "", "Project directory (default: cwd)")
	eventsFile := fs.String("events-file", "", "Path to events.jsonl")
	outputFile := fs.String("output", "", "Output file")
	filterRunID := fs.String("run-id", "", "Filter to a specific run_id")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(stdout, evalCollectHelp)
			return 0
		}
		fmt.Fprintf(stderr, "harmonik eval collect: %v\n", err)
		return 1
	}

	if *projectDir == "" {
		wd, err := getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik eval collect: cannot determine working directory: %v\n", err)
			return 1
		}
		*projectDir = wd
	}
	absProject, err := filepath.Abs(*projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik eval collect: cannot resolve project path: %v\n", err)
		return 1
	}

	if *eventsFile == "" {
		*eventsFile = filepath.Join(absProject, ".harmonik", "events", "events.jsonl")
	}
	if *outputFile == "" {
		*outputFile = filepath.Join(absProject, ".harmonik", "eval-results.jsonl")
	}

	states, err := evalReadEvents(*eventsFile, *filterRunID)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik eval collect: reading events: %v\n", err)
		return 1
	}

	piModel := evalReadPiModel(absProject)

	// Dedup on re-run: records for run_ids already present in the output file
	// are skipped so re-collecting does not double-count the training set.
	existing, err := evalReadExistingRunIDs(*outputFile)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik eval collect: reading existing output: %v\n", err)
		return 1
	}

	f, err := os.OpenFile(*outputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik eval collect: opening output: %v\n", err)
		return 1
	}
	defer f.Close()

	// Emit in a deterministic run_id order so re-collecting the same events
	// produces byte-identical output (map iteration order is randomised).
	runIDs := make([]string, 0, len(states))
	for runID := range states {
		runIDs = append(runIDs, runID)
	}
	sort.Strings(runIDs)

	written := 0
	skipped := 0
	for _, runID := range runIDs {
		st := states[runID]
		if !st.gradeDispatched {
			continue // not an eval run
		}
		if _, dup := existing[runID]; dup {
			skipped++
			continue // already collected on a prior run
		}
		rec, err := evalBuildRecord(runID, st, absProject, piModel)
		if err != nil {
			fmt.Fprintf(stderr, "harmonik eval collect: building record for run %s: %v\n", runID, err)
			continue
		}
		line, err := json.Marshal(rec)
		if err != nil {
			fmt.Fprintf(stderr, "harmonik eval collect: marshalling record: %v\n", err)
			continue
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			fmt.Fprintf(stderr, "harmonik eval collect: writing record: %v\n", err)
			return 1
		}
		written++
	}

	fmt.Fprintf(stdout, "harmonik eval collect: wrote %d record(s) to %s (%d already present, skipped)\n",
		written, *outputFile, skipped)
	return 0
}

// evalReadExistingRunIDs returns the set of run_ids already present in the
// output file. A missing file yields an empty set. Malformed lines are skipped.
func evalReadExistingRunIDs(path string) (map[string]struct{}, error) {
	ids := map[string]struct{}{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ids, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	setLargeScanBuffer(scanner)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec struct {
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal(line, &rec); err != nil || rec.RunID == "" {
			continue
		}
		ids[rec.RunID] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// evalReadEvents scans events.jsonl and returns per-run accumulated state.
// If filterRunID is non-empty, only that run is tracked.
func evalReadEvents(path, filterRunID string) (map[string]*evalRunState, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	states := map[string]*evalRunState{}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var env evalEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue // skip malformed lines
		}
		if env.RunID == nil {
			continue
		}
		runID := *env.RunID
		if filterRunID != "" && runID != filterRunID {
			continue
		}

		st := states[runID]
		if st == nil {
			st = &evalRunState{}
			states[runID] = st
		}

		switch env.Type {
		case "run_started":
			var p struct {
				BeadID    string `json:"bead_id"`
				StartedAt string `json:"started_at"`
			}
			if err := json.Unmarshal(env.Payload, &p); err == nil {
				st.beadID = p.BeadID
				st.startedAt = p.StartedAt
			}

		case "run_completed":
			var p struct {
				EndedAt string `json:"ended_at"`
			}
			if err := json.Unmarshal(env.Payload, &p); err == nil {
				st.endedAt = p.EndedAt
				st.completedWall = env.TimestampWall
			}

		case "run_failed":
			var p struct {
				EndedAt string `json:"ended_at"`
			}
			if err := json.Unmarshal(env.Payload, &p); err == nil {
				st.endedAt = p.EndedAt
				st.completedWall = env.TimestampWall
			}

		case "harness_selected":
			var p struct {
				AgentType string `json:"agent_type"`
			}
			if err := json.Unmarshal(env.Payload, &p); err == nil && p.AgentType != "" {
				st.harness = p.AgentType
			}

		case "implementer_phase_complete":
			var p struct {
				DurationSeconds float64 `json:"duration_seconds"`
			}
			if err := json.Unmarshal(env.Payload, &p); err == nil {
				st.implSecs = p.DurationSeconds
			}

		case "node_dispatch_requested":
			var p struct {
				NodeID string `json:"node_id"`
			}
			if err := json.Unmarshal(env.Payload, &p); err == nil && p.NodeID == "grade" {
				st.gradeDispatched = true
			}

		case "outcome_emitted":
			// Judge is an agentic node — it emits outcome_emitted.
			// Judge only executes when grade succeeds (DOT topology), so
			// its outcome_emitted is the grade-pass signal.
			var p struct {
				NodeID string `json:"node_id"`
			}
			if err := json.Unmarshal(env.Payload, &p); err == nil && p.NodeID == "judge" {
				st.judgeOutcome = true
			}

		case "checkpoint_written":
			var p struct {
				CommitHash string `json:"commit_hash"`
			}
			if err := json.Unmarshal(env.Payload, &p); err == nil && p.CommitHash != "" {
				st.commitSHA = p.CommitHash // last checkpoint wins
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return states, nil
}

// evalBuildRecord constructs one output record for an eval run.
func evalBuildRecord(runID string, st *evalRunState, projectDir, piModel string) (evalResultRecord, error) {
	rec := evalResultRecord{
		SchemaVersion: 1,
		RunID:         runID,
		BeadID:        st.beadID,
		Harness:       st.harness,
		CommitSHA:     st.commitSHA,
	}

	rec.Pass = st.judgeOutcome

	// wall_time_s from run_started.started_at and run_completed.ended_at.
	if st.startedAt != "" && st.endedAt != "" {
		start, err1 := time.Parse(time.RFC3339, st.startedAt)
		end, err2 := time.Parse(time.RFC3339, st.endedAt)
		if err1 == nil && err2 == nil {
			rec.WallTimeS = end.Sub(start).Seconds()
		}
	}

	// implement_time_s from implementer_phase_complete.duration_seconds.
	rec.ImplementTimeS = st.implSecs

	// timestamp from the run_completed wall clock.
	if !st.completedWall.IsZero() {
		rec.Timestamp = st.completedWall.UTC().Format(time.RFC3339)
	}

	// model: pi harness → harnesses.pi.model from config; others → harness name.
	switch st.harness {
	case "pi":
		rec.Model = piModel
	default:
		rec.Model = st.harness
	}

	// task_id, difficulty, check_kind from bead labels.
	if st.beadID != "" {
		labels, err := evalFetchBeadLabels(st.beadID, projectDir)
		if err == nil {
			rec.TaskID = evalLabelValue(labels, "task_id")
			rec.Difficulty = evalLabelValue(labels, "difficulty")
			rec.CheckKind = evalLabelValue(labels, "check_kind")
			// override model if explicitly labelled
			if m := evalLabelValue(labels, "model"); m != "" {
				rec.Model = m
			}
		}
	}

	return rec, nil
}

// evalFetchBeadLabels invokes `br show --json <beadID>` and returns the labels slice.
func evalFetchBeadLabels(beadID, projectDir string) ([]string, error) {
	cmd := exec.Command("br", "show", "--json", beadID)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	// br show --json returns a JSON array with one object.
	var items []struct {
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items[0].Labels, nil
}

// evalLabelValue extracts the value after "key:" from a label list.
// E.g. evalLabelValue(labels, "task_id") on ["task_id:eval-fizzbuzz"] → "eval-fizzbuzz".
func evalLabelValue(labels []string, key string) string {
	prefix := key + ":"
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return ""
}

// rawMinimalConfig holds just the harnesses block needed by the collector.
type rawMinimalConfig struct {
	Harnesses struct {
		Pi struct {
			Model string `yaml:"model"`
		} `yaml:"pi"`
	} `yaml:"harnesses"`
}

// evalReadPiModel reads harnesses.pi.model from .harmonik/config.yaml.
// Returns empty string on any error (model label on bead is the fallback).
func evalReadPiModel(projectDir string) string {
	data, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "config.yaml"))
	if err != nil {
		return ""
	}
	var cfg rawMinimalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return cfg.Harnesses.Pi.Model
}

// runEvalCmd dispatches harmonik eval sub-verbs.
func runEvalCmd(subArgs []string, stdout, stderr io.Writer) int {
	if len(subArgs) == 0 || subArgs[0] == "--help" || subArgs[0] == "-h" {
		fmt.Fprint(stdout, evalCmdHelp)
		return 0
	}
	switch subArgs[0] {
	case "collect":
		return runEvalCollect(subArgs[1:], stdout, stderr, os.Getwd)
	case "metrics":
		return runEvalMetrics(subArgs[1:], stdout, stderr, os.Getwd)
	case "report":
		return runEvalReport(subArgs[1:], stdout, stderr, os.Getwd)
	default:
		fmt.Fprintf(stderr, "harmonik eval: unknown verb %q\n\n%s", subArgs[0], evalCmdHelp)
		return 2
	}
}
