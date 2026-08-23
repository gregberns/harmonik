package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

type evalEnvelope struct {
	Type          string          `json:"type"`
	TimestampWall time.Time       `json:"timestamp_wall"`
	RunID         *string         `json:"run_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

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

func runEvalCollect(args []string, stdout, stderr io.Writer, getwd func() (string, error)) int {
	fs := flag.NewFlagSet("eval collect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectDir := fs.String("project", "", "Project directory (default: cwd)")
	eventsFile := fs.String("events-file", "", "Path to events.jsonl")
	outputFile := fs.String("output", "", "Output file")
	filterRunID := fs.String("run-id", "", "Filter to a specific run_id")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if _, writeErr := fmt.Fprint(stdout, evalCollectHelp); writeErr != nil {
				return 1
			}
			return 0
		}
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval collect: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	if *projectDir == "" {
		wd, err := getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik eval collect: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		*projectDir = wd
	}
	absProject, err := filepath.Abs(*projectDir)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval collect: cannot resolve project path: %v\n", err); writeErr != nil {
			return 1
		}
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
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval collect: reading events: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	piModel := evalReadPiModel(absProject)

	existing, err := evalReadExistingRunIDs(*outputFile)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval collect: reading existing output: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	f, err := os.OpenFile(*outputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik eval collect: opening output: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	written, skipped, writeErr := evalWriteRecords(f, states, existing, absProject, piModel, stderr)
	if closeErr := f.Close(); closeErr != nil && writeErr == nil {
		writeErr = fmt.Errorf("close %s: %w", *outputFile, closeErr)
	}
	if writeErr != nil {
		if _, outputErr := fmt.Fprintf(stderr, "harmonik eval collect: %v\n", writeErr); outputErr != nil {
			return 1
		}
		return 1
	}

	if _, err := fmt.Fprintf(stdout, "harmonik eval collect: wrote %d record(s) to %s (%d already present, skipped)\n", written, *outputFile, skipped); err != nil {
		return 1
	}
	return 0
}

func evalWriteRecords(
	w io.Writer,
	states map[string]*evalRunState,
	existing map[string]struct{},
	absProject, piModel string,
	stderr io.Writer,
) (written, skipped int, err error) {
	runIDs := make([]string, 0, len(states))
	for runID := range states {
		runIDs = append(runIDs, runID)
	}
	sort.Strings(runIDs)

	for _, runID := range runIDs {
		st := states[runID]
		if !st.gradeDispatched {
			continue // not an eval run
		}
		if _, dup := existing[runID]; dup {
			skipped++
			continue // already collected on a prior run
		}
		rec, buildErr := evalBuildRecord(runID, st, absProject, piModel)
		if buildErr != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik eval collect: building record for run %s: %v\n", runID, buildErr); writeErr != nil {
				return written, skipped, writeErr
			}
			continue
		}
		line, marshalErr := json.Marshal(rec)
		if marshalErr != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik eval collect: marshalling record: %v\n", marshalErr); writeErr != nil {
				return written, skipped, writeErr
			}
			continue
		}
		if _, writeErr := w.Write(append(line, '\n')); writeErr != nil {
			return written, skipped, fmt.Errorf("writing record for run %s: %w", runID, writeErr)
		}
		written++
	}
	return written, skipped, nil
}

func evalReadExistingRunIDs(path string) (map[string]struct{}, error) {
	ids := map[string]struct{}{}
	f, err := os.Open(path) //nolint:gosec // G304: path is the operator-supplied eval-collect output file
	if err != nil {
		if os.IsNotExist(err) {
			return ids, nil
		}
		return nil, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik eval: close %s: %v\n", path, closeErr)
		}
	}()

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

func evalReadEvents(path, filterRunID string) (map[string]*evalRunState, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik eval: close %s: %v\n", path, closeErr)
		}
	}()

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

func evalBuildRecord(runID string, st *evalRunState, projectDir, piModel string) (evalResultRecord, error) {
	rec := evalResultRecord{
		SchemaVersion: 1,
		RunID:         runID,
		BeadID:        st.beadID,
		Harness:       st.harness,
		CommitSHA:     st.commitSHA,
	}

	rec.Pass = st.judgeOutcome

	if st.startedAt != "" && st.endedAt != "" {
		start, err1 := time.Parse(time.RFC3339, st.startedAt)
		end, err2 := time.Parse(time.RFC3339, st.endedAt)
		if err1 == nil && err2 == nil {
			rec.WallTimeS = end.Sub(start).Seconds()
		}
	}

	rec.ImplementTimeS = st.implSecs

	if !st.completedWall.IsZero() {
		rec.Timestamp = st.completedWall.UTC().Format(time.RFC3339)
	}

	switch st.harness {
	case "pi":
		rec.Model = piModel
	default:
		rec.Model = st.harness
	}

	if st.beadID != "" {
		labels, err := evalFetchBeadLabels(st.beadID, projectDir)
		if err == nil {
			rec.TaskID = evalLabelValue(labels, "task_id")
			rec.Difficulty = evalLabelValue(labels, "difficulty")
			rec.CheckKind = evalLabelValue(labels, "check_kind")
			if m := evalLabelValue(labels, "model"); m != "" {
				rec.Model = m
			}
		}
	}

	return rec, nil
}

func evalFetchBeadLabels(beadID, projectDir string) ([]string, error) {
	cmd := exec.CommandContext(context.Background(), "br", "show", "--json", beadID)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
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

func evalLabelValue(labels []string, key string) string {
	prefix := key + ":"
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return ""
}

type rawMinimalConfig struct {
	Harnesses struct {
		Pi struct {
			Model string `yaml:"model"`
		} `yaml:"pi"`
	} `yaml:"harnesses"`
}

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

func runEvalCmd(subArgs []string, stdout, stderr io.Writer) int {
	if len(subArgs) == 0 || subArgs[0] == "--help" || subArgs[0] == "-h" {
		if _, err := fmt.Fprint(stdout, evalCmdHelp); err != nil {
			return 1
		}
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
		if _, err := fmt.Fprintf(stderr, "harmonik eval: unknown verb %q\n\n%s", subArgs[0], evalCmdHelp); err != nil {
			return 1
		}
		return 2
	}
}
