package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/scenario"
)

const (
	harnessExitPass              = 0   // SuiteResult.suite_verdict = pass
	harnessExitFail              = 1   // SuiteResult.suite_verdict = fail
	harnessExitSuiteLoadAbort    = 2   // Suite-load aborted (parse/duplicate/schema error)
	harnessExitInternalError     = 3   // Harness-internal error (panic, unrecoverable I/O)
	harnessExitOperatorInterrupt = 130 // Operator interrupt (SIGINT); 128 + signal 2
	harnessExitSIGTERM           = 143 // Operator interrupt (SIGTERM); 128 + signal 15
)

const harnessTopUsage = `harmonik harness — run the scenario harness against a project

USAGE
  harmonik harness [flags]

FLAGS
  --cadence <tag>           Cadence filter: smoke, regression, nightly, all (default: all)
  --scenario <path>         Run a single scenario file; repeatable to select a subset
  --fixture-root <path>     Per-suite fixture root directory (default: OS temp dir)
  --twin-search-path <path> Twin binary search path override (default: <cwd>/twins/)
  --list                    Print discovered scenarios and cadence tags; no execution
  --dry-run                 Suite-load and matrix-expand only; no orchestration
  --output <format>         SuiteResult output format: human or json (default: human)
  --verbose                 Emit operator-facing progress log to stderr

EXIT CODES
  0    SuiteResult.suite_verdict = pass
  1    SuiteResult.suite_verdict = fail (one or more scenarios failed)
  2    Suite-load aborted (duplicate name, parse error, or schema error per SH-006)
  3    Harness-internal error (panic or unrecoverable I/O failure)
  130  Operator interrupt (SIGINT); partial SuiteResult emitted to stdout
  143  Operator interrupt (SIGTERM); partial SuiteResult emitted to stdout

NOTES
  Two concurrent harmonik harness invocations against the same project are
  permitted; each creates its own per-suite ephemeral fixture root (SH-016a)
  and they do not contend for any shared resource.

  SuiteResult is written to stdout; harness-internal log messages go to stderr.

EXAMPLES
  harmonik harness --list
  harmonik harness --dry-run
  harmonik harness --cadence smoke
  harmonik harness --scenario scenarios/smoke/twin-launch-and-ready.yaml
`

type harnessScenarioFlags []string

func (s *harnessScenarioFlags) String() string {
	if s == nil || len(*s) == 0 {
		return ""
	}
	return fmt.Sprintf("%v", []string(*s))
}

func (s *harnessScenarioFlags) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type harnessScenarioEntry struct {
	scenario.ScenarioFile
	SourcePath string
}

func runHarnessSubcommand(args []string) int {
	return runHarness(args, os.Stdout, os.Stderr)
}

func runHarness(args []string, stdout, stderr io.Writer) int {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	return runHarnessWithSigs(args, stdout, stderr, sigCh)
}

func harnessWritef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

func runHarnessWithSigs(args []string, stdout, stderr io.Writer, sigCh <-chan os.Signal) int {
	fset := flag.NewFlagSet("harness", flag.ContinueOnError)
	fset.SetOutput(stderr)
	var usageWriteErr error
	fset.Usage = func() { usageWriteErr = harnessWritef(stdout, "%s", harnessTopUsage) }

	var (
		cadenceFlag     string
		scenarioFiles   harnessScenarioFlags
		fixtureRootFlag string
		twinSearchPath  string
		listFlag        bool
		dryRunFlag      bool
		outputFlag      string
		verboseFlag     bool
	)

	fset.StringVar(&cadenceFlag, "cadence", "", "cadence filter: smoke, regression, nightly, all (default: all)")
	fset.Var(&scenarioFiles, "scenario", "scenario file path (repeatable)")
	fset.StringVar(&fixtureRootFlag, "fixture-root", "", "per-suite fixture root (default: OS temp dir)")
	fset.StringVar(&twinSearchPath, "twin-search-path", "", "twin binary search path (default: <cwd>/twins/)")
	fset.BoolVar(&listFlag, "list", false, "print discovered scenarios and cadence tags; no execution")
	fset.BoolVar(&dryRunFlag, "dry-run", false, "suite-load + matrix-expand only; no orchestration")
	fset.StringVar(&outputFlag, "output", "human", "SuiteResult output format: human or json")
	fset.BoolVar(&verboseFlag, "verbose", false, "emit progress log to stderr")

	if err := fset.Parse(args); err != nil {
		if usageWriteErr != nil {
			return harnessExitInternalError
		}
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return harnessExitInternalError
	}

	cadenceFilter := scenario.CadenceFilterAll
	if cadenceFlag != "" {
		cf := scenario.CadenceFilter(cadenceFlag)
		if !cf.Valid() {
			if err := harnessWritef(stderr, "harmonik harness: invalid --cadence value %q; must be one of smoke, regression, nightly, all\n", cadenceFlag); err != nil {
				return harnessExitInternalError
			}
			return harnessExitInternalError
		}
		cadenceFilter = cf
	}

	if outputFlag != "human" && outputFlag != "json" {
		if err := harnessWritef(stderr, "harmonik harness: invalid --output value %q; must be human or json\n", outputFlag); err != nil {
			return harnessExitInternalError
		}
		return harnessExitInternalError
	}

	suiteStart := time.Now().UTC().Truncate(time.Millisecond)
	suiteUUID, suiteIDErr := uuid.NewV7()
	if suiteIDErr != nil {
		if err := harnessWritef(stderr, "harmonik harness: generate suite ID: %v\n", suiteIDErr); err != nil {
			return harnessExitInternalError
		}
		return harnessExitInternalError
	}
	suiteID := core.SuiteID(suiteUUID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interruptCh := make(chan os.Signal, 1)

	shutdownComplete := make(chan struct{})
	defer close(shutdownComplete)

	go func() {
		select {
		case sig, ok := <-sigCh:
			if !ok {
				return
			}
			cancel()           // cancel execution context for graceful shutdown
			interruptCh <- sig // deliver signal to main goroutine

			select {
			case sig2, ok2 := <-sigCh:
				if ok2 && sig2 == syscall.SIGINT {
					os.Exit(harnessExitOperatorInterrupt)
				}
			case <-shutdownComplete:
			}
		case <-ctx.Done():
		}
	}()

	cwd, err := os.Getwd()
	if err != nil {
		if err := harnessWritef(stderr, "harmonik harness: cannot determine working directory: %v\n", err); err != nil {
			return harnessExitInternalError
		}
		return harnessExitInternalError
	}

	twinSearchPaths := resolveTwinSearchPaths(twinSearchPath, os.Getenv("HARMONIK_TWIN_SEARCH_PATH"), cwd)
	if verboseFlag {
		if err := harnessWritef(stderr, "harness: twin-search-paths: %v\n", twinSearchPaths); err != nil {
			return harnessExitInternalError
		}
	}

	discovered, loadErrs := harnessDiscoverScenarios(
		cwd,
		[]string(scenarioFiles),
		cadenceFilter,
		verboseFlag,
		stderr,
	)
	if len(loadErrs) > 0 {
		for _, e := range loadErrs {
			if err := harnessWritef(stderr, "harmonik harness: %v\n", e); err != nil {
				return harnessExitInternalError
			}
		}
		return harnessExitSuiteLoadAbort
	}

	if verboseFlag {
		if err := harnessWritef(stderr, "harness: loaded %d scenario(s)\n", len(discovered)); err != nil {
			return harnessExitInternalError
		}
	}

	if listFlag {
		for _, sf := range discovered {
			if err := harnessWritef(stdout, "%s\t%s\n", sf.Name, sf.CadenceTag); err != nil {
				return harnessExitInternalError
			}
		}
		return harnessExitPass
	}

	if dryRunFlag {
		totalCells := 0
		for _, sf := range discovered {
			cells := harnessMatrixCellCount(sf.Matrix)
			totalCells += cells
			if verboseFlag {
				if err := harnessWritef(stderr, "harness dry-run: scenario %q cadence=%s cells=%d\n",
					sf.Name, sf.CadenceTag, cells); err != nil {
					return harnessExitInternalError
				}
			}
		}
		if err := harnessWritef(stdout, "dry-run: %d scenario(s) loaded, %d total matrix cell(s)\n",
			len(discovered), totalCells); err != nil {
			return harnessExitInternalError
		}
		return harnessExitPass
	}

	fixtureRoot := fixtureRootFlag
	if fixtureRoot != "" {
		if mkErr := os.MkdirAll(fixtureRoot, 0o755); mkErr != nil { //dirmode:allow operator-supplied --fixture-root under TMPDIR, not .harmonik state
			if err := harnessWritef(stderr, "harmonik harness: create fixture root %q: %v\n",
				fixtureRoot, mkErr); err != nil {
				return harnessExitInternalError
			}
			return harnessExitInternalError
		}
	} else {
		var tmpErr error
		fixtureRoot, tmpErr = os.MkdirTemp("", "harmonik-harness-*")
		if tmpErr != nil {
			if err := harnessWritef(stderr, "harmonik harness: create temp fixture root: %v\n", tmpErr); err != nil {
				return harnessExitInternalError
			}
			return harnessExitInternalError
		}
	}

	completedResults := make([]scenario.ScenarioResult, 0, len(discovered))

	var executedRunIDs []core.RunID
	seenRunIDs := make(map[string]bool)
	recordRunIDs := func(events []scenario.RawEvent) {
		for _, rid := range scenario.RunIDsFromEvents(events) {
			if key := rid.String(); !seenRunIDs[key] {
				seenRunIDs[key] = true
				executedRunIDs = append(executedRunIDs, rid)
			}
		}
	}

	interruptExit := func() int {
		sig := <-interruptCh
		select {
		case sig2 := <-sigCh:
			if sig2 == syscall.SIGINT {
				return harnessExitOperatorInterrupt
			}
		default:
		}
		if err := harnessEmitInterruptResult(stdout, stderr,
			scenario.SuiteResultOutputFormat(outputFlag),
			suiteID, suiteStart, fixtureRoot, cadenceFilter,
			completedResults, sig); err != nil {
			return harnessExitInternalError
		}
		return harnessInterruptExitCode(sig)
	}

	for _, entry := range discovered {
		sf := entry.ScenarioFile
		scenarioName := sf.Name
		scenarioSource := entry.SourcePath
		startedAt := time.Now().UTC().Truncate(time.Millisecond)
		evLogRelPath := filepath.Join(scenarioName, "project", scenario.EventLogRelPath)

		select {
		case <-ctx.Done():
			return interruptExit()
		default:
		}

		resolvedBinary, handlerArgs, resolveErr := harnessResolveTwinBinary(
			sf.AgentOverrides, twinSearchPaths)
		if resolveErr != nil {
			result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
				evLogRelPath, scenario.FailureClassTwinBinaryNotFound, resolveErr.Error())
			if writeErr := scenario.WriteScenarioResult(fixtureRoot, result); writeErr != nil {
				if _, err := fmt.Fprintf(stderr, "harness: write scenario result %q: %v\n", scenarioName, writeErr); err != nil {
					return harnessExitInternalError
				}
				return harnessExitInternalError
			}
			completedResults = append(completedResults, result)
			continue
		}

		if resolvedBinary != "" {
			if checkErr := scenario.CheckTwinBinaryPath(resolvedBinary, twinSearchPaths); checkErr != nil {
				result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
					evLogRelPath, scenario.FailureClassHarnessInternalError, checkErr.Error())
				if writeErr := scenario.WriteScenarioResult(fixtureRoot, result); writeErr != nil {
					if _, err := fmt.Fprintf(stderr, "harness: write scenario result %q: %v\n", scenarioName, writeErr); err != nil {
						return harnessExitInternalError
					}
					return harnessExitInternalError
				}
				completedResults = append(completedResults, result)
				continue
			}
		}

		bootstrap, bootstrapErr := scenario.BootstrapFixture(
			ctx, fixtureRoot, scenarioName, twinSearchPaths)
		if bootstrapErr != nil {
			tdParams := scenario.TeardownParams{ScenarioName: scenarioName}
			tdResult, tdErrPartial := scenario.TeardownFixture(ctx, tdParams)
			result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
				evLogRelPath,
				scenario.BootstrapFixtureFailureClass(bootstrapErr), bootstrapErr.Error())
			result.WorkspaceSnapshotPath = tdResult.WorkspaceSnapshotPath
			if tdErrPartial != nil {
				result.ErrorDetail += "; teardown: " + tdErrPartial.Error()
			}
			if writeErr := scenario.WriteScenarioResult(fixtureRoot, result); writeErr != nil {
				if _, err := fmt.Fprintf(stderr, "harness: write scenario result %q: %v\n", scenarioName, writeErr); err != nil {
					return harnessExitInternalError
				}
				return harnessExitInternalError
			}
			completedResults = append(completedResults, result)
			continue
		}

		projectRoot := bootstrap.ProjectRoot
		absEvLogPath := scenario.EventLogPath(projectRoot)
		workspacePath := scenario.ScenarioWorkspacePath(fixtureRoot, scenarioName)

		if fileErr := harnessApplyFixtureFiles(projectRoot, sf.FixtureSetup.Files); fileErr != nil {
			tdParams := scenario.TeardownParams{
				ScenarioName:  scenarioName,
				WorkspacePath: workspacePath,
				EventLogPath:  absEvLogPath,
			}
			tdResult, tdErrPartial := scenario.TeardownFixture(ctx, tdParams)
			result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
				evLogRelPath, scenario.FailureClassFixtureSetupFailed, fileErr.Error())
			result.WorkspaceSnapshotPath = tdResult.WorkspaceSnapshotPath
			if tdErrPartial != nil {
				result.ErrorDetail += "; teardown: " + tdErrPartial.Error()
			}
			if writeErr := scenario.WriteScenarioResult(fixtureRoot, result); writeErr != nil {
				if _, err := fmt.Fprintf(stderr, "harness: write scenario result %q: %v\n", scenarioName, writeErr); err != nil {
					return harnessExitInternalError
				}
				return harnessExitInternalError
			}
			completedResults = append(completedResults, result)
			continue
		}

		workflowMode, dotErr := harnessApplyWorkflowDOT(projectRoot, cwd, sf)
		if dotErr != nil {
			tdParams := scenario.TeardownParams{
				ScenarioName:  scenarioName,
				WorkspacePath: workspacePath,
				EventLogPath:  absEvLogPath,
			}
			tdResult, tdErrPartial := scenario.TeardownFixture(ctx, tdParams)
			result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
				evLogRelPath, scenario.FailureClassFixtureSetupFailed, dotErr.Error())
			result.WorkspaceSnapshotPath = tdResult.WorkspaceSnapshotPath
			if tdErrPartial != nil {
				result.ErrorDetail += "; teardown: " + tdErrPartial.Error()
			}
			if writeErr := scenario.WriteScenarioResult(fixtureRoot, result); writeErr != nil {
				if _, err := fmt.Fprintf(stderr, "harness: write scenario result %q: %v\n", scenarioName, writeErr); err != nil {
					return harnessExitInternalError
				}
				return harnessExitInternalError
			}
			completedResults = append(completedResults, result)
			continue
		}

		orchErr := scenario.DriveOrchestration(ctx, scenario.OrchestrationConfig{
			ProjectDir:    projectRoot,
			JSONLLogPath:  absEvLogPath,
			HandlerBinary: resolvedBinary,
			HandlerArgs:   handlerArgs,
			WorkflowMode:  workflowMode,
			TimeoutSecs:   sf.TimeoutSecs,
		})

		var (
			finalVerdict     scenario.ScenarioVerdict
			finalFC          scenario.FailureClass
			finalErrDetail   string
			assertionResults []scenario.AssertionResult
		)

		switch {
		case orchErr != nil && ctx.Err() != nil && !errors.Is(orchErr, scenario.ErrScenarioTimeout):
			finalVerdict = scenario.ScenarioVerdictError
			finalFC = scenario.FailureClassHarnessInternalError
			finalErrDetail = fmt.Sprintf("operator interrupt: %v", orchErr)
		case errors.Is(orchErr, scenario.ErrScenarioTimeout):
			finalVerdict = scenario.ScenarioVerdictTimeout
			finalFC = scenario.FailureClassScenarioTimeout
			finalErrDetail = orchErr.Error()
			if partialEvents, readErr := scenario.ReadEventLog(absEvLogPath); readErr == nil {
				assertionResults, _, _ = scenario.EvaluateAssertions(sf, partialEvents, workspacePath)
				recordRunIDs(partialEvents)
			}
		case orchErr != nil:
			finalVerdict = scenario.ScenarioVerdictError
			finalFC = scenario.FailureClassOrchestrationInternalError
			finalErrDetail = orchErr.Error()
		default:
			events, readErr := scenario.ReadEventLog(absEvLogPath)
			if readErr != nil {
				finalVerdict = scenario.ScenarioVerdictError
				finalFC = scenario.FailureClassOrchestrationInternalError
				finalErrDetail = fmt.Sprintf("read event log: %v", readErr)
			} else {
				recordRunIDs(events)
				var assertFC scenario.FailureClass
				assertionResults, finalVerdict, assertFC = scenario.EvaluateAssertions(
					sf, events, workspacePath)
				finalFC = assertFC // empty iff verdict=pass
			}
		}

		tdParams := scenario.TeardownParams{
			ScenarioName:  scenarioName,
			WorkspacePath: workspacePath,
			EventLogPath:  absEvLogPath,
		}
		tdResult, tdErr := scenario.TeardownFixture(ctx, tdParams)
		completedAt := time.Now().UTC().Truncate(time.Millisecond)

		if tdErr != nil {
			if finalVerdict == scenario.ScenarioVerdictPass {
				finalVerdict = scenario.ScenarioVerdictError
				finalFC = tdErr.FailureClass()
				finalErrDetail = tdErr.Error()
			} else {
				if finalErrDetail != "" {
					finalErrDetail += "; " + tdErr.Error()
				} else {
					finalErrDetail = tdErr.Error()
				}
			}
		}

		result := scenario.ScenarioResult{
			ScenarioName:          scenarioName,
			SourcePath:            scenarioSource,
			StartedAt:             startedAt,
			CompletedAt:           completedAt,
			Verdict:               finalVerdict,
			FailureClass:          finalFC,
			AssertionResults:      assertionResults,
			EventLogPath:          evLogRelPath,
			WorkspaceSnapshotPath: tdResult.WorkspaceSnapshotPath,
			ErrorDetail:           finalErrDetail,
		}

		if writeErr := scenario.WriteScenarioResult(fixtureRoot, result); writeErr != nil {
			if err := harnessWritef(stderr, "harness: write scenario result %q: %v\n",
				scenarioName, writeErr); err != nil {
				return harnessExitInternalError
			}
			return harnessExitInternalError
		}
		completedResults = append(completedResults, result)

		if ctx.Err() != nil {
			return interruptExit()
		}
	}

	if ctx.Err() != nil {
		return interruptExit()
	}

	suiteVerdict := scenario.SuiteVerdictPass
	for _, r := range completedResults {
		if r.Verdict != scenario.ScenarioVerdictPass {
			suiteVerdict = scenario.SuiteVerdictFail
			break
		}
	}

	leakReport, leakErr := scenario.CheckPostSuiteLeaks(ctx, scenario.PostSuiteLeakParams{
		FixtureRoot:    fixtureRoot,
		ExecutedRunIDs: executedRunIDs,
	})
	if leakErr != nil {
		fmt.Fprintf(stderr, "harness: post-suite leak sensor: %v\n", leakErr) //nolint:errcheck // diagnostic write to stderr/stdout; failure is non-actionable
	} else if leakReport.HasLeaks() {
		suiteVerdict = scenario.SuiteVerdictFail
		fmt.Fprintf(stderr, "harness: SH-INV-002 post-suite leak(s) detected (%d) — suite fails:\n", //nolint:errcheck // diagnostic write to stderr/stdout; failure is non-actionable
			len(leakReport.Leaks))
		for _, lk := range leakReport.Leaks {
			fmt.Fprintf(stderr, "  - %s: %s\n", lk.Kind, lk.Detail) //nolint:errcheck // diagnostic write to stderr/stdout; failure is non-actionable
		}
	}

	sr := scenario.SuiteResult{
		SuiteID:       suiteID,
		StartedAt:     suiteStart,
		CompletedAt:   time.Now().UTC().Truncate(time.Millisecond),
		FixtureRoot:   fixtureRoot,
		CadenceFilter: cadenceFilter,
		Results:       completedResults,
		SuiteVerdict:  suiteVerdict,
	}

	if writeErr := scenario.WriteSuiteResult(fixtureRoot, sr); writeErr != nil {
		if err := harnessWritef(stderr, "harness: write suite result: %v\n", writeErr); err != nil {
			return harnessExitInternalError
		}
		return harnessExitInternalError
	}
	if emitErr := scenario.EmitSuiteResult(
		stdout, scenario.SuiteResultOutputFormat(outputFlag), sr); emitErr != nil {
		if err := harnessWritef(stderr, "harness: emit suite result: %v\n", emitErr); err != nil {
			return harnessExitInternalError
		}
		return harnessExitInternalError
	}

	if suiteVerdict == scenario.SuiteVerdictFail {
		return harnessExitFail
	}
	return harnessExitPass
}

func harnessInterruptExitCode(sig os.Signal) int {
	if sig == syscall.SIGTERM {
		return harnessExitSIGTERM
	}
	return harnessExitOperatorInterrupt
}

func harnessEmitInterruptResult(
	stdout, stderr io.Writer,
	format scenario.SuiteResultOutputFormat,
	suiteID core.SuiteID,
	startedAt time.Time,
	fixtureRoot string,
	cadenceFilter scenario.CadenceFilter,
	completed []scenario.ScenarioResult,
	sig os.Signal,
) error {
	sigName := "SIGINT"
	if sig == syscall.SIGTERM {
		sigName = "SIGTERM"
	}
	if err := harnessWritef(stderr, "harmonik harness: %s received — emitting partial SuiteResult\n", sigName); err != nil {
		return err
	}

	suiteVerdict := scenario.SuiteVerdictPass
	for _, r := range completed {
		if r.Verdict != scenario.ScenarioVerdictPass {
			suiteVerdict = scenario.SuiteVerdictFail
			break
		}
	}

	sr := scenario.SuiteResult{
		SuiteID:       suiteID,
		StartedAt:     startedAt,
		CompletedAt:   time.Now().UTC().Truncate(time.Millisecond),
		FixtureRoot:   fixtureRoot,
		CadenceFilter: cadenceFilter,
		Results:       completed,
		SuiteVerdict:  suiteVerdict,
	}

	if !format.Valid() {
		format = scenario.SuiteResultOutputFormatHuman
	}
	if emitErr := scenario.EmitSuiteResult(stdout, format, sr); emitErr != nil {
		if err := harnessWritef(stderr, "harmonik harness: emit partial SuiteResult: %v\n", emitErr); err != nil {
			return err
		}
	}
	return nil
}

func harnessDiscoverScenarios(
	projectRoot string,
	scenarioPaths []string,
	cadenceFilter scenario.CadenceFilter,
	verbose bool,
	stderr io.Writer,
) ([]harnessScenarioEntry, []error) {
	var paths []string
	var loadErrs []error

	if len(scenarioPaths) > 0 {
		paths = make([]string, len(scenarioPaths))
		for i, p := range scenarioPaths {
			abs, err := filepath.Abs(p)
			if err != nil {
				return nil, []error{
					fmt.Errorf("cannot resolve --scenario path %q: %w", p, err),
				}
			}
			paths[i] = abs
		}
	} else {
		scenariosDir := filepath.Join(projectRoot, "scenarios")
		if verbose {
			if err := harnessWritef(stderr, "harness: discovering scenarios under %s\n", scenariosDir); err != nil {
				return nil, []error{fmt.Errorf("write scenario discovery progress: %w", err)}
			}
		}
		var wrongExt []error
		walkErr := filepath.WalkDir(scenariosDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext == ".yaml" {
				paths = append(paths, path)
				return nil
			}
			if lower := strings.ToLower(ext); lower == ".yml" || lower == ".yaml" {
				wrongExt = append(wrongExt, fmt.Errorf(
					"scenario-load-failure: %q has extension %q; scenario files MUST use .yaml (SH-002)",
					path, ext,
				))
			}
			return nil
		})
		if walkErr != nil {
			return nil, []error{fmt.Errorf("walk scenarios dir %q: %w", scenariosDir, walkErr)}
		}
		loadErrs = append(loadErrs, wrongExt...)
		sort.Strings(paths)
	}

	nameToPath := make(map[string]string, len(paths))
	var allLoaded []harnessScenarioEntry

	for _, path := range paths {
		sf, err := scenario.ParseScenarioFile(path)
		if err != nil {
			loadErrs = append(loadErrs, err)
			continue
		}
		cells, expErr := sf.ExpandMatrix()
		if expErr != nil {
			loadErrs = append(loadErrs, fmt.Errorf("%q: %w", path, expErr))
			continue
		}
		for _, cell := range cells {
			if prev, exists := nameToPath[cell.Name]; exists {
				loadErrs = append(loadErrs, fmt.Errorf(
					"scenario-load-failure: duplicate scenario name %q in %q and %q (SH-005)",
					cell.Name, prev, path,
				))
				continue
			}
			nameToPath[cell.Name] = path
			allLoaded = append(allLoaded, harnessScenarioEntry{ScenarioFile: cell, SourcePath: path})
		}
	}

	scenarios := make([]harnessScenarioEntry, 0, len(allLoaded))
	for _, entry := range allLoaded {
		if !cadenceFilter.Includes(entry.CadenceTag) {
			if verbose {
				if err := harnessWritef(stderr, "harness: skip %q (cadence=%s not in filter=%s)\n",
					entry.Name, entry.CadenceTag, cadenceFilter); err != nil {
					return nil, append(loadErrs, fmt.Errorf("write scenario filter progress: %w", err))
				}
			}
			continue
		}
		scenarios = append(scenarios, entry)
	}

	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Name < scenarios[j].Name
	})

	return scenarios, loadErrs
}

func resolveTwinSearchPaths(flagValue, envValue, cwd string) []string {
	if flagValue != "" {
		return []string{flagValue}
	}
	if envValue != "" {
		return []string{envValue}
	}
	return []string{filepath.Join(cwd, "twins")}
}

func harnessMatrixCellCount(matrix map[string][]string) int {
	if len(matrix) == 0 {
		return 1
	}
	cells := 1
	for _, vals := range matrix {
		cells *= len(vals)
	}
	return cells
}

func harnessResolveTwinBinary(
	overrides map[string]scenario.AgentOverride,
	searchPaths []string,
) (binary string, args []string, err error) {
	if len(overrides) == 0 {
		return "", nil, nil
	}

	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	override := overrides[keys[0]]

	binaryName := override.Binary
	if binaryName == "" {
		return "", nil, fmt.Errorf("agent_overrides[%q]: binary name is empty", keys[0])
	}

	if filepath.IsAbs(binaryName) {
		if _, statErr := os.Stat(binaryName); statErr != nil {
			return "", nil, fmt.Errorf("twin-binary-not-found: absolute path %q: %w",
				binaryName, statErr)
		}
		return binaryName, override.Args, nil
	}

	for _, sp := range searchPaths {
		candidate := filepath.Join(sp, binaryName)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, override.Args, nil
		}
	}
	return "", nil, fmt.Errorf("twin-binary-not-found: %q not found in search paths %v",
		binaryName, searchPaths)
}

func harnessApplyFixtureFiles(projectRoot string, files map[string]scenario.FileSeed) error {
	for relPath, seed := range files {
		if !filepath.IsLocal(relPath) {
			return fmt.Errorf("fixture file path %q must be repository-relative", relPath)
		}
		absPath := filepath.Join(projectRoot, relPath)
		if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil { //dirmode:allow parent of a scenario-declared seeded fixture file, not .harmonik state
			return fmt.Errorf("create parent dir for %q: %w", relPath, mkErr)
		}

		var content []byte
		switch seed.Encoding {
		case scenario.FileSeedEncodingBase64:
			decoded, decErr := base64.StdEncoding.DecodeString(seed.Contents)
			if decErr != nil {
				return fmt.Errorf("decode base64 content for %q: %w", relPath, decErr)
			}
			content = decoded
		default: // utf8 or empty (defaults to utf8 per §6.1)
			content = []byte(seed.Contents)
		}

		mode := fs.FileMode(0o644)
		if seed.Mode != "" {
			v, parseErr := strconv.ParseUint(seed.Mode, 8, 32)
			if parseErr != nil {
				return fmt.Errorf("parse mode %q for %q: %w", seed.Mode, relPath, parseErr)
			}
			mode = fs.FileMode(v)
		}

		if writeErr := os.WriteFile(absPath, content, mode); writeErr != nil {
			return fmt.Errorf("write %q: %w", relPath, writeErr)
		}
	}
	return nil
}

func harnessApplyWorkflowDOT(
	projectRoot, cwd string,
	sf scenario.ScenarioFile,
) (core.WorkflowMode, error) {
	if sf.WorkflowPath == nil {
		return core.WorkflowModeDot, nil
	}
	dotRelPath := *sf.WorkflowPath
	if !filepath.IsLocal(dotRelPath) {
		return "", fmt.Errorf("workflow_path %q must be repository-relative", dotRelPath)
	}

	var dotContent []byte
	candidate1 := filepath.Join(projectRoot, dotRelPath)
	//nolint:gosec // G304: dotRelPath is constrained by filepath.IsLocal above.
	if content, readErr := os.ReadFile(candidate1); readErr == nil {
		dotContent = content
	} else {
		candidate2 := filepath.Join(cwd, "scenarios", "_workflows", dotRelPath)
		//nolint:gosec // G304: dotRelPath is constrained by filepath.IsLocal above.
		content, readErr2 := os.ReadFile(candidate2)
		if readErr2 != nil {
			return "", fmt.Errorf("resolve workflow_path %q: not found at %q or %q",
				dotRelPath, candidate1, candidate2)
		}
		dotContent = content
	}

	targetPath := filepath.Join(projectRoot, ".harmonik", "workflow.dot")
	if mkErr := os.MkdirAll(filepath.Dir(targetPath), core.HarmonikDirMode); mkErr != nil {
		return "", fmt.Errorf("create .harmonik dir for workflow.dot: %w", mkErr)
	}
	//nolint:gosec // G306: workflow dot, not a user-controlled secret
	if writeErr := os.WriteFile(targetPath, dotContent, 0o644); writeErr != nil {
		return "", fmt.Errorf("write workflow.dot: %w", writeErr)
	}
	return core.WorkflowModeDot, nil
}

func harnessEarlyErrorResult(
	scenarioName, sourcePath string,
	startedAt time.Time,
	eventLogRelPath string,
	fc scenario.FailureClass,
	errorDetail string,
) scenario.ScenarioResult {
	return scenario.ScenarioResult{
		ScenarioName:          scenarioName,
		SourcePath:            sourcePath,
		StartedAt:             startedAt,
		CompletedAt:           time.Now().UTC().Truncate(time.Millisecond),
		Verdict:               scenario.ScenarioVerdictError,
		FailureClass:          fc,
		EventLogPath:          eventLogRelPath,
		WorkspaceSnapshotPath: scenario.WorkspaceSnapshotPath(scenarioName),
		ErrorDetail:           errorDetail,
	}
}
