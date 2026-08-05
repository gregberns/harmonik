package main

// queue_readiness.go — `harmonik queue readiness capture` and
// `harmonik queue readiness validate`.
//
// This is the shell in front of internal/queue/readiness. Until it existed the
// gate had no way in from a terminal: nothing in the tree imported the package,
// so the only way to produce or read a readiness record was to write Go. The
// reader of that record is an assessor in a separate session with none of the
// capturing session's context, which is exactly the reader who cannot write Go
// against an internal package.
//
// It lives here and not in internal/queue/cli because of the component matrix:
// internal/queue/** may import $gostd, uuid and internal/core, and nothing else.
// A capture reads the live bead ledger through internal/brcli, so the wiring
// belongs in the composition root. The judging half stays pure in the package.
//
// Spec ref: specs/beads-integration.md §4.5b BI-013e; specs/operator-nfr.md
// ON-032a. Task: T9 of the queue-dogfood-readiness work.

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
	"sort"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue/readiness"
)

// Exit codes. They are the queue CLI's own three, repeated here rather than
// imported because internal/queue/cli keeps them unexported.
const (
	readinessExitOK       = 0 // captured, or validated and accepted
	readinessExitRejected = 1 // validated and refused
	readinessExitUsage    = 2 // the command could not be run at all
)

const queueReadinessUsage = `harmonik queue readiness capture|validate

capture   read the live ledger and write the readiness evidence record
validate  judge a captured record, a run plan and this host, and write the verdict

Run either with --help for its flags.
`

// say writes one line to a terminal.
//
// A failed write to stdout or stderr cannot be reported anywhere the caller
// would see it, so the error is dropped ONCE, here, with a name on it — rather
// than at each of the two dozen call sites, where the repetition would make it
// look like a decision each time.
func say(w io.Writer, format string, a ...any) {
	//nolint:errcheck // the write target IS the error channel; there is nowhere left to report a failure to
	fmt.Fprintf(w, format, a...)
}

// runQueueReadiness dispatches the two halves of the readiness gate.
func runQueueReadiness(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		say(out, "%s", queueReadinessUsage)
		if len(args) == 0 {
			return readinessExitUsage
		}
		return readinessExitOK
	}
	switch args[0] {
	case "capture":
		return runQueueReadinessCapture(ctx, args[1:], time.Now(), liveBeadReader, out, errOut)
	case "validate":
		return runQueueReadinessValidate(args[1:], time.Now(), out, errOut)
	default:
		say(errOut, "harmonik queue readiness: unrecognised verb %q; verbs are: capture, validate\n", args[0])
		return readinessExitUsage
	}
}

// liveBeadReader is the production ledger port: `br` resolved from PATH, bound
// to the project directory.
func liveBeadReader(projectDir string) (readiness.BeadReader, error) {
	brPath, err := exec.LookPath("br")
	if err != nil {
		return nil, fmt.Errorf("br is not on PATH: %w", err)
	}
	return brcli.NewForProject(brPath, projectDir)
}

// flagsGiven names the flags that were actually on the command line. A flag
// package default is indistinguishable from a stated value once parsing is
// over, and for a record that is read as evidence the difference matters.
func flagsGiven(fs *flag.FlagSet) map[string]bool {
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	return given
}

// repeatedFlag collects a flag given more than once, in the order given. Order
// matters here: the snapshot records the selected items in selection order and
// reads the ledger in the same order, so two runs of the same command produce
// the same record.
type repeatedFlag []string

func (r *repeatedFlag) String() string { return strings.Join(*r, ",") }

func (r *repeatedFlag) Set(v string) error {
	*r = append(*r, v)
	return nil
}

// splitPair splits `bead=reason` on the FIRST equals sign only, so a reason may
// contain one. Both halves must be non-empty: a bead with no reason and a
// reason with no bead are each half of a record, and the evidence package
// refuses both.
func splitPair(raw, flagName string) (left, right string, err error) {
	at := strings.Index(raw, "=")
	if at < 0 {
		return "", "", fmt.Errorf("--%s %q: expected <bead>=<reason>", flagName, raw)
	}
	left = strings.TrimSpace(raw[:at])
	right = strings.TrimSpace(raw[at+1:])
	if left == "" {
		return "", "", fmt.Errorf("--%s %q: names no bead", flagName, raw)
	}
	if right == "" {
		return "", "", fmt.Errorf("--%s %q: states no reason", flagName, raw)
	}
	return left, right, nil
}

// findingsFile is the optional JSON document that carries the two finding
// lists. They are too many fields to put on a command line — a stale finding
// alone needs five — and they are written once and reused across captures.
type findingsFile struct {
	StaleFindings   []readiness.StaleFinding   `json:"stale_findings"`
	CurrentFindings []readiness.CurrentFinding `json:"current_findings"`
}

// captureConfig is what the capture flags parsed to.
type captureConfig struct {
	ProjectDir string
	Selected   []readiness.SelectedID
	Excluded   []readiness.ExcludedID
	Posture    readiness.Posture
	EventLogs  []string
	Commands   []readiness.Command
	Findings   findingsFile
	FindingsAt string
	OutPath    string
	OutputJSON bool
}

// parseCaptureArgs turns the capture flags into a config, or into the one error
// that says what is missing.
//
// It refuses an unset item count and an unset concurrency rather than deriving
// them. The item count in particular MUST come from the operator: the evidence
// record cross-checks it against the number of items actually named, and a
// count derived from that list would agree with itself and prove nothing.
func parseCaptureArgs(args []string, errOut io.Writer) (captureConfig, error) {
	var cfg captureConfig
	var selects, excludes, eventLogs, commands repeatedFlag

	fs := flag.NewFlagSet("harmonik queue readiness capture", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&cfg.ProjectDir, "project", ".", "project directory")
	fs.Var(&selects, "select", "a canary item as <bead>=<why re-running it is safe>; repeat for several")
	fs.Var(&excludes, "exclude", "a considered-and-rejected bead as <bead>=<why>; repeat for several")
	itemCount := fs.Int("item-count", 0, "how many items the run carries (required; cross-checked against --select)")
	concurrency := fs.Int("concurrency", 0, "how many items run at the same time (required)")
	local := fs.Bool("local", true, "the run stays on this machine (required; state it as --local=true or --local=false)")
	fs.Var(&eventLogs, "event-log", "an event log this evidence came from; repeat for a rotation. Default: the project's own logs")
	fs.Var(&commands, "command", "an extra retained command as <argv>|<output path>; repeat for several")
	fs.StringVar(&cfg.FindingsAt, "findings", "", "JSON file holding stale_findings and current_findings")
	fs.StringVar(&cfg.OutPath, "out", "", "where to write the readiness record (required)")
	fs.BoolVar(&cfg.OutputJSON, "json", false, "print the record as JSON")

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	given := flagsGiven(fs)

	for _, raw := range selects {
		bead, reason, err := splitPair(raw, "select")
		if err != nil {
			return cfg, err
		}
		cfg.Selected = append(cfg.Selected, readiness.SelectedID{BeadID: core.BeadID(bead), RepeatSafeReason: reason})
	}
	for _, raw := range excludes {
		bead, reason, err := splitPair(raw, "exclude")
		if err != nil {
			return cfg, err
		}
		cfg.Excluded = append(cfg.Excluded, readiness.ExcludedID{BeadID: core.BeadID(bead), Reason: reason})
	}
	for _, raw := range commands {
		argv, outputPath, found := strings.Cut(raw, "|")
		fields := strings.Fields(argv)
		if len(fields) == 0 {
			return cfg, fmt.Errorf("--command %q: names no command", raw)
		}
		cmd := readiness.Command{Argv: fields}
		if found {
			cmd.OutputPath = strings.TrimSpace(outputPath)
		}
		cfg.Commands = append(cfg.Commands, cmd)
	}

	if len(cfg.Selected) == 0 {
		return cfg, errors.New("--select is required: name at least one canary item and why re-running it is safe")
	}
	if *itemCount == 0 {
		return cfg, errors.New("--item-count is required: the record states how many items the run carries and refuses a count that disagrees with --select")
	}
	if *concurrency == 0 {
		return cfg, errors.New("--concurrency is required: the record states how many items run at the same time")
	}
	// --local carries a value whether or not anybody set it, and a default reads
	// in the finished record exactly like a measurement. The one run-shape rule
	// the operator kept is that the run is local, so it has to be stated.
	if !given["local"] {
		return cfg, errors.New("--local is required: state it as --local=true or --local=false; a default would put an unmeasured claim in the record")
	}
	if cfg.OutPath == "" {
		return cfg, errors.New("--out is required: name the file to write the readiness record to")
	}
	cfg.Posture = readiness.Posture{Local: *local, ItemCount: *itemCount, Concurrency: *concurrency}
	cfg.EventLogs = eventLogs
	return cfg, nil
}

// runQueueReadinessCapture reads the live ledger and writes the evidence record.
func runQueueReadinessCapture(
	ctx context.Context,
	args []string,
	now time.Time,
	newLedger func(projectDir string) (readiness.BeadReader, error),
	out, errOut io.Writer,
) int {
	cfg, err := parseCaptureArgs(args, errOut)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return readinessExitOK
		}
		say(errOut, "harmonik queue readiness capture: %v\n", err)
		return readinessExitUsage
	}

	harmonikDir := filepath.Join(cfg.ProjectDir, ".harmonik")

	if cfg.FindingsAt != "" {
		cfg.Findings, err = readFindingsFile(cfg.FindingsAt)
		if err != nil {
			say(errOut, "harmonik queue readiness capture: %v\n", err)
			return readinessExitUsage
		}
	}

	eventLogs := cfg.EventLogs
	if len(eventLogs) == 0 {
		eventLogs = discoverEventLogs(harmonikDir)
	}
	if len(eventLogs) == 0 {
		say(errOut, "harmonik queue readiness capture: no event log found under %s; "+
			"name one with --event-log\n", harmonikDir)
		return readinessExitUsage
	}

	intents, err := readiness.ReadTerminalIntents(harmonikDir)
	if err != nil {
		say(errOut, "harmonik queue readiness capture: %v\n", err)
		return readinessExitUsage
	}

	ledger, err := newLedger(cfg.ProjectDir)
	if err != nil {
		say(errOut, "harmonik queue readiness capture: cannot read the ledger: %v\n", err)
		return readinessExitUsage
	}

	snap, err := readiness.Capture(ctx, ledger, readiness.CaptureRequest{
		CapturedAt:      now,
		Posture:         cfg.Posture,
		Selected:        cfg.Selected,
		ExcludedIDs:     cfg.Excluded,
		Commands:        append(ledgerReadCommands(cfg.Selected, cfg.Excluded), cfg.Commands...),
		EventLogPaths:   eventLogs,
		TerminalIntent:  intents,
		StaleFindings:   cfg.Findings.StaleFindings,
		CurrentFindings: cfg.Findings.CurrentFindings,
	})
	if err != nil {
		say(errOut, "harmonik queue readiness capture: %v\n", err)
		return readinessExitRejected
	}

	if err := readiness.WriteSnapshot(cfg.OutPath, snap); err != nil {
		say(errOut, "harmonik queue readiness capture: %v\n", err)
		return readinessExitUsage
	}

	if cfg.OutputJSON {
		body, marshalErr := json.MarshalIndent(snap, "", "  ")
		if marshalErr != nil {
			say(errOut, "harmonik queue readiness capture: %v\n", marshalErr)
			return readinessExitUsage
		}
		say(out, "%s\n", body)
		return readinessExitOK
	}

	say(out, "readiness record: %s\n", cfg.OutPath)
	say(out, "run shape: %d items, %d at the same time, local=%t\n",
		snap.Posture.ItemCount, snap.Posture.Concurrency, snap.Posture.Local)
	say(out, "selected: %s\n", strings.Join(snap.SelectedBeadIDs(), ", "))
	say(out, "set aside: %d\n", len(snap.Excluded))
	say(out, "pending terminal intents: %d\n", len(snap.TerminalIntent.Pending))
	return readinessExitOK
}

// ledgerReadCommands is the record of what the capture itself ran. Every
// candidate status in the snapshot came from one of these, so the record names
// them rather than leaving the reader to assume.
func ledgerReadCommands(selected []readiness.SelectedID, excluded []readiness.ExcludedID) []readiness.Command {
	cmds := make([]readiness.Command, 0, len(selected)+len(excluded))
	for _, s := range selected {
		cmds = append(cmds, readiness.Command{Argv: []string{"br", "show", string(s.BeadID), "--json"}})
	}
	for _, e := range excluded {
		cmds = append(cmds, readiness.Command{Argv: []string{"br", "show", string(e.BeadID), "--json"}})
	}
	return cmds
}

// discoverEventLogs finds the project's event logs, live file and rotations
// together, sorted so the record is stable across captures.
//
// The rotations matter: a capture that spans one reads two files, and a record
// that could name only the live one would drop the other in silence.
func discoverEventLogs(harmonikDir string) []string {
	matches, err := filepath.Glob(filepath.Join(harmonikDir, "events", "events.jsonl*"))
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}

func readFindingsFile(path string) (findingsFile, error) {
	body, err := os.ReadFile(path) //nolint:gosec // G304: an operator-supplied evidence path is a runtime value by construction
	if err != nil {
		return findingsFile{}, fmt.Errorf("read findings file: %w", err)
	}
	var f findingsFile
	if err := json.Unmarshal(body, &f); err != nil {
		return findingsFile{}, fmt.Errorf("decode findings file %q: %w", path, err)
	}
	return f, nil
}

// validateConfig is what the validate flags parsed to.
type validateConfig struct {
	SnapshotPath string
	OutPath      string
	Plan         readiness.RunPlan
	Host         readiness.HostFacts
	Limits       readiness.HostLimits
	OutputJSON   bool
}

// parseValidateArgs turns the validate flags into a config.
//
// Every host fact is required and none is defaulted. A missing daemon count in
// particular would otherwise read as zero daemons alive, which CLEARS the
// ceiling — the one direction a gate must never fail. The validator cannot tell
// an unmeasured zero from a measured one, so the refusal has to happen here.
func parseValidateArgs(args []string, errOut io.Writer) (validateConfig, error) {
	var cfg validateConfig

	fs := flag.NewFlagSet("harmonik queue readiness validate", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&cfg.SnapshotPath, "snapshot", "", "the readiness record to judge (required)")
	fs.StringVar(&cfg.OutPath, "out", "", "where to write the verdict (required)")

	fs.StringVar(&cfg.Plan.Harness, "harness", "", "the agent harness the items would run on (required)")
	fs.StringVar(&cfg.Plan.RemoteWorker, "remote-worker", "", "the named remote worker, empty for a local run")
	fs.StringVar(&cfg.Plan.RepoTarget, "repo-target", "", "the repository the run would land in (required)")
	fs.StringVar(&cfg.Plan.ScratchRepo, "scratch-repo", "", "the throwaway clone the pass may touch (required)")
	queueKind := fs.String("queue-kind", "", "stream or wave (required)")
	fs.BoolVar(&cfg.Plan.FeedbackEnabled, "feedback", false, "the run uses the feedback loop")
	itemCount := fs.Int("item-count", 0, "how many items the run carries (required)")
	concurrency := fs.Int("concurrency", 0, "how many items run at the same time (required)")

	loadAverage := fs.Float64("load-average", -1, "measured load average (required)")
	cpuCount := fs.Int("cpu-count", 0, "measured CPU count (required)")
	freeDiskGB := fs.Float64("free-disk-gb", -1, "measured free disk in GB (required)")
	daemonsAlive := fs.Int("daemons-alive", -1, "measured count of live daemons (required)")

	fs.Float64Var(&cfg.Limits.MaxLoadPerCPU, "max-load-per-cpu", readiness.DefaultHostLimits.MaxLoadPerCPU, "load ceiling per CPU")
	fs.Float64Var(&cfg.Limits.MinFreeDiskGB, "min-free-disk-gb", readiness.DefaultHostLimits.MinFreeDiskGB, "free disk floor in GB")
	fs.IntVar(&cfg.Limits.MaxDaemons, "max-daemons", readiness.DefaultHostLimits.MaxDaemons, "how many daemons may be alive")
	fs.BoolVar(&cfg.OutputJSON, "json", false, "print the verdict as JSON")

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	// A slice and not a map: ranging a map reports a different missing flag on
	// each run when two are missing, and an error message that changes between
	// identical runs sends the reader looking for a difference that is not there.
	for _, req := range []struct {
		name    string
		missing bool
	}{
		{"snapshot", cfg.SnapshotPath == ""},
		{"out", cfg.OutPath == ""},
		{"harness", cfg.Plan.Harness == ""},
		{"repo-target", cfg.Plan.RepoTarget == ""},
		{"scratch-repo", cfg.Plan.ScratchRepo == ""},
		{"queue-kind", *queueKind == ""},
	} {
		if req.missing {
			return cfg, fmt.Errorf("--%s is required", req.name)
		}
	}
	// The host facts are listed separately so the message says which
	// MEASUREMENT is missing, not that "a flag" is.
	for _, req := range []struct {
		name    string
		missing bool
	}{
		{"load-average", *loadAverage < 0},
		{"cpu-count", *cpuCount == 0},
		{"free-disk-gb", *freeDiskGB < 0},
		{"daemons-alive", *daemonsAlive < 0},
	} {
		if req.missing {
			return cfg, fmt.Errorf("--%s is required: the verdict records what was measured, and an unmeasured host cannot be judged", req.name)
		}
	}

	cfg.Plan.QueueKind = readiness.QueueKind(*queueKind)
	cfg.Plan.ItemCount = *itemCount
	cfg.Plan.Concurrency = *concurrency
	cfg.Host = readiness.HostFacts{
		LoadAverage:  *loadAverage,
		CPUCount:     *cpuCount,
		FreeDiskGB:   *freeDiskGB,
		DaemonsAlive: *daemonsAlive,
	}
	return cfg, nil
}

// runQueueReadinessValidate judges a captured record and writes the verdict.
//
// It reaches no daemon and no ledger. The only files it touches are the record
// it was given and the verdict it writes.
func runQueueReadinessValidate(args []string, now time.Time, out, errOut io.Writer) int {
	cfg, err := parseValidateArgs(args, errOut)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return readinessExitOK
		}
		say(errOut, "harmonik queue readiness validate: %v\n", err)
		return readinessExitUsage
	}

	file, err := os.Open(cfg.SnapshotPath)
	if err != nil {
		say(errOut, "harmonik queue readiness validate: open the record: %v\n", err)
		return readinessExitUsage
	}
	snap, decodeErr := readiness.DecodeSnapshot(file)
	closeErr := file.Close()
	if decodeErr != nil {
		say(errOut, "harmonik queue readiness validate: %v\n", decodeErr)
		return readinessExitRejected
	}
	if closeErr != nil {
		say(errOut, "harmonik queue readiness validate: %v\n", closeErr)
		return readinessExitUsage
	}

	verdict := readiness.Validate(snap, cfg.Plan, cfg.Host, cfg.Limits, now)

	if err := readiness.WriteValidation(cfg.OutPath, verdict); err != nil {
		say(errOut, "harmonik queue readiness validate: %v\n", err)
		return readinessExitUsage
	}

	if cfg.OutputJSON {
		body, marshalErr := json.MarshalIndent(verdict, "", "  ")
		if marshalErr != nil {
			say(errOut, "harmonik queue readiness validate: %v\n", marshalErr)
			return readinessExitUsage
		}
		say(out, "%s\n", body)
	} else {
		say(out, "verdict: %s\n", acceptedWord(verdict.Accepted))
		say(out, "record: %s\n", cfg.OutPath)
		say(out, "selected: %s\n", strings.Join(verdict.SelectedBeads, ", "))
		say(out, "run shape: %d items, %d at the same time\n", verdict.Plan.ItemCount, verdict.Plan.Concurrency)
		for _, r := range verdict.Rejections {
			say(out, "  refused (%s): %s\n", r.Reason, r.Detail)
		}
	}

	// The exit code is the verdict. A gate whose refusal only appears in a file
	// is a gate a script walks straight past.
	if !verdict.Accepted {
		return readinessExitRejected
	}
	return readinessExitOK
}

func acceptedWord(accepted bool) string {
	if accepted {
		return "accepted"
	}
	return "refused"
}
