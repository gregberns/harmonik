package main

// harness.go — `harmonik harness` subcommand (hk-nwqa0, hk-5ec1s).
//
// Implements the scenario-harness CLI surface declared in
// specs/scenario-harness.md §4.12 SH-032 and §4.13 SH-033.
//
// At this iteration the subcommand supports:
//   - Flag parsing for all 8 MVH flags (SH-032).
//   - --list: discover scenarios, filter by cadence, print name+cadence.
//   - --dry-run: suite-load + matrix-expansion validation; no orchestration.
//   - SIGINT/SIGTERM graceful shutdown with partial SuiteResult emission (SH-033).
//   - Double-SIGINT hard exit: os.Exit(130) immediately (SH-033).
//
// The full execution path (suite orchestration, assertion evaluation, result
// emission) depends on G-02 (orchestration drive) which is a future bead.
// Until G-02 lands the harness blocks on ctx cancellation in the execution
// phase so that SH-033 signal handling is exercisable without a real loop.
//
// Spec refs: specs/scenario-harness.md §4.12 SH-032, §4.13 SH-033.
// Beads: hk-nwqa0 (CLI surface), hk-5ec1s (signal handling).

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

// Harness CLI exit codes per specs/scenario-harness.md §4.12 SH-032.
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

// harnessScenarioFlags is a repeatable --scenario flag value accumulator.
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

// harnessScenarioEntry pairs a parsed ScenarioFile with the source YAML file
// path so that ScenarioResult.SourcePath is populated without a separate
// tracking data structure. The embedded ScenarioFile fields are promoted, so
// callers can access entry.Name, entry.CadenceTag, etc. directly.
type harnessScenarioEntry struct {
	scenario.ScenarioFile
	SourcePath string
}

// runHarnessSubcommand dispatches `harmonik harness [flags]`.
func runHarnessSubcommand(args []string) int {
	return runHarness(args, os.Stdout, os.Stderr)
}

// runHarness registers OS signal handlers and delegates to runHarnessWithSigs.
//
// Spec ref: specs/scenario-harness.md §4.12 SH-032, §4.13 SH-033.
func runHarness(args []string, stdout, stderr io.Writer) int {
	// Buffered 2: first slot absorbs the interrupt signal; second slot captures
	// a double-SIGINT for the hard-exit path (SH-033).
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	return runHarnessWithSigs(args, stdout, stderr, sigCh)
}

// runHarnessWithSigs is the testable core of the harness subcommand. Callers
// supply the signal channel so that tests can pre-load signals without sending
// real OS signals.
//
// Spec refs: specs/scenario-harness.md §4.12 SH-032, §4.13 SH-033.
func runHarnessWithSigs(args []string, stdout, stderr io.Writer, sigCh <-chan os.Signal) int {
	fset := flag.NewFlagSet("harness", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() { fmt.Fprint(stdout, harnessTopUsage) }

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
		if err == flag.ErrHelp {
			return 0
		}
		return harnessExitInternalError
	}

	// Validate --cadence.
	cadenceFilter := scenario.CadenceFilterAll
	if cadenceFlag != "" {
		cf := scenario.CadenceFilter(cadenceFlag)
		if !cf.Valid() {
			fmt.Fprintf(stderr, "harmonik harness: invalid --cadence value %q; must be one of smoke, regression, nightly, all\n", cadenceFlag)
			return harnessExitInternalError
		}
		cadenceFilter = cf
	}

	// Validate --output.
	if outputFlag != "human" && outputFlag != "json" {
		fmt.Fprintf(stderr, "harmonik harness: invalid --output value %q; must be human or json\n", outputFlag)
		return harnessExitInternalError
	}

	// suiteStart and suiteID are captured at invocation so they are available
	// even if the harness exits early due to a signal.
	suiteStart := time.Now().UTC().Truncate(time.Millisecond)
	suiteUUID, suiteIDErr := uuid.NewV7()
	if suiteIDErr != nil {
		fmt.Fprintf(stderr, "harmonik harness: generate suite ID: %v\n", suiteIDErr)
		return harnessExitInternalError
	}
	suiteID := core.SuiteID(suiteUUID)

	// ctx is cancelled by the signal goroutine on the first SIGINT/SIGTERM so
	// that the execution loop (when G-02 is wired) can detect interruption.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// interruptCh carries the first received signal to the main goroutine.
	// Buffered 1 so the goroutine never blocks writing while the main goroutine
	// is between its ctx.Done() wakeup and the select below.
	interruptCh := make(chan os.Signal, 1)

	// shutdownComplete is closed by defer when runHarnessWithSigs returns,
	// marking the end of the graceful-shutdown window. The signal goroutine
	// watches it so it does not block indefinitely after execution completes.
	shutdownComplete := make(chan struct{})
	defer close(shutdownComplete)

	// SH-033: signal goroutine — handle SIGINT/SIGTERM.
	//
	// First signal: cancel the execution context and notify the main goroutine.
	// Second SIGINT during graceful shutdown: hard-exit immediately per SH-033.
	//
	// The goroutine uses shutdownComplete (not ctx.Done()) for the hard-exit
	// window because ctx is already cancelled by the first signal; selecting
	// on ctx.Done() would close the window immediately.
	go func() {
		select {
		case sig, ok := <-sigCh:
			if !ok {
				return
			}
			cancel()           // cancel execution context for graceful shutdown
			interruptCh <- sig // deliver signal to main goroutine

			// Wait for a second SIGINT during graceful shutdown (SH-033).
			// The window stays open until shutdownComplete is closed (function
			// return), so a second SIGINT at any point during graceful shutdown
			// triggers the immediate hard-exit.
			select {
			case sig2, ok2 := <-sigCh:
				if ok2 && sig2 == syscall.SIGINT {
					// SH-033: second SIGINT during graceful shutdown overrides
					// the cleanup invariant — exit immediately.
					os.Exit(harnessExitOperatorInterrupt)
				}
			case <-shutdownComplete:
				// Graceful shutdown completed; double-SIGINT window closed.
			}
		case <-ctx.Done():
			// Normal exit (no signal arrived before execution completed).
		}
	}()

	// Resolve working directory for scenario discovery and twin-search-path
	// default.
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "harmonik harness: cannot determine working directory: %v\n", err)
		return harnessExitInternalError
	}

	// Twin-search-path precedence: CLI flag > env > <cwd>/twins/ (SH-009).
	// Resolved paths are passed to BootstrapFixture per scenario in the G-02 loop.
	twinSearchPaths := resolveTwinSearchPaths(twinSearchPath, os.Getenv("HARMONIK_TWIN_SEARCH_PATH"), cwd)
	if verboseFlag {
		fmt.Fprintf(stderr, "harness: twin-search-paths: %v\n", twinSearchPaths)
	}

	// Discover + load scenarios (SH-006/SH-007).
	// Duplicate-name detection (SH-005) and wrong-extension rejection (SH-002)
	// are performed inside harnessDiscoverScenarios.
	discovered, loadErrs := harnessDiscoverScenarios(
		cwd,
		[]string(scenarioFiles),
		cadenceFilter,
		verboseFlag,
		stderr,
	)
	if len(loadErrs) > 0 {
		for _, e := range loadErrs {
			fmt.Fprintln(stderr, "harmonik harness:", e)
		}
		return harnessExitSuiteLoadAbort
	}

	if verboseFlag {
		fmt.Fprintf(stderr, "harness: loaded %d scenario(s)\n", len(discovered))
	}

	// --list: print discovered scenarios and cadence; exit 0.
	if listFlag {
		for _, sf := range discovered {
			fmt.Fprintf(stdout, "%s\t%s\n", sf.Name, sf.CadenceTag)
		}
		return harnessExitPass
	}

	// --dry-run: suite-load + matrix expansion validation; no orchestration.
	if dryRunFlag {
		totalCells := 0
		for _, sf := range discovered {
			cells := harnessMatrixCellCount(sf.Matrix)
			totalCells += cells
			if verboseFlag {
				fmt.Fprintf(stderr, "harness dry-run: scenario %q cadence=%s cells=%d\n",
					sf.Name, sf.CadenceTag, cells)
			}
		}
		fmt.Fprintf(stdout, "dry-run: %d scenario(s) loaded, %d total matrix cell(s)\n",
			len(discovered), totalCells)
		return harnessExitPass
	}

	// Full execution path (G-02): per-scenario orchestration loop.
	//
	// Spec ref: specs/scenario-harness.md §7.1.

	// Create or validate the per-suite fixture root (SH-016a / SH-034 durability).
	// When --fixture-root is absent, allocate an OS temp directory. The fixture
	// root is never removed automatically; the operator cleans it manually or via
	// `harmonik harness clean`.
	fixtureRoot := fixtureRootFlag
	if fixtureRoot != "" {
		if mkErr := os.MkdirAll(fixtureRoot, 0o755); mkErr != nil {
			fmt.Fprintf(stderr, "harmonik harness: create fixture root %q: %v\n",
				fixtureRoot, mkErr)
			return harnessExitInternalError
		}
	} else {
		var tmpErr error
		fixtureRoot, tmpErr = os.MkdirTemp("", "harmonik-harness-*")
		if tmpErr != nil {
			fmt.Fprintf(stderr, "harmonik harness: create temp fixture root: %v\n", tmpErr)
			return harnessExitInternalError
		}
	}

	var completedResults []scenario.ScenarioResult

	// interruptExit is called after detecting ctx.Done() inside the loop.
	// It drains the interrupt signal, checks for double-SIGINT, emits a
	// partial SuiteResult, and returns the appropriate exit code.
	// It captures completedResults, fixtureRoot, and other locals by reference.
	interruptExit := func() int {
		select {
		case sig := <-interruptCh:
			select {
			case sig2 := <-sigCh:
				if sig2 == syscall.SIGINT {
					return harnessExitOperatorInterrupt
				}
			default:
			}
			harnessEmitInterruptResult(stdout, stderr,
				scenario.SuiteResultOutputFormat(outputFlag),
				suiteID, suiteStart, fixtureRoot, cadenceFilter,
				completedResults, sig)
			return harnessInterruptExitCode(sig)
		default:
			return harnessExitInternalError
		}
	}

	for _, entry := range discovered {
		sf := entry.ScenarioFile
		scenarioName := sf.Name
		scenarioSource := entry.SourcePath
		startedAt := time.Now().UTC().Truncate(time.Millisecond)
		// Fixture-root-relative path for ScenarioResult.EventLogPath (§6.1).
		evLogRelPath := filepath.Join(scenarioName, "project", scenario.EventLogRelPath)

		// SH-033: check for operator interruption before starting this scenario.
		select {
		case <-ctx.Done():
			return interruptExit()
		default:
		}

		// §7.1 step 1: resolve twin binary (SH-008/SH-009).
		resolvedBinary, handlerArgs, resolveErr := harnessResolveTwinBinary(
			sf.AgentOverrides, twinSearchPaths)
		if resolveErr != nil {
			result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
				evLogRelPath, scenario.FailureClassTwinBinaryNotFound, resolveErr.Error())
			_ = scenario.WriteScenarioResult(fixtureRoot, result)
			completedResults = append(completedResults, result)
			continue
		}

		// SH-INV-003: resolved binary must lie under a declared search-path prefix.
		// Violation is harness-internal-error (highest precedence, rank 1).
		if resolvedBinary != "" {
			if checkErr := scenario.CheckTwinBinaryPath(resolvedBinary, twinSearchPaths); checkErr != nil {
				result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
					evLogRelPath, scenario.FailureClassHarnessInternalError, checkErr.Error())
				_ = scenario.WriteScenarioResult(fixtureRoot, result)
				completedResults = append(completedResults, result)
				continue
			}
		}

		// §7.1 step 2: fixture setup — bootstrap, file seeding, workflow DOT seeding.
		bootstrap, bootstrapErr := scenario.BootstrapFixture(
			ctx, fixtureRoot, scenarioName, twinSearchPaths)
		if bootstrapErr != nil {
			// Best-effort partial teardown per §8.3; do not escalate to cleanup-failed.
			tdParams := scenario.TeardownParams{ScenarioName: scenarioName}
			tdResult, _ := scenario.TeardownFixture(ctx, tdParams)
			result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
				evLogRelPath,
				scenario.BootstrapFixtureFailureClass(bootstrapErr), bootstrapErr.Error())
			result.WorkspaceSnapshotPath = tdResult.WorkspaceSnapshotPath
			_ = scenario.WriteScenarioResult(fixtureRoot, result)
			completedResults = append(completedResults, result)
			continue
		}

		projectRoot := bootstrap.ProjectRoot
		absEvLogPath := scenario.EventLogPath(projectRoot)
		workspacePath := scenario.ScenarioWorkspacePath(fixtureRoot, scenarioName)

		// Seed fixture files into the synthetic project root.
		if fileErr := harnessApplyFixtureFiles(projectRoot, sf.FixtureSetup.Files); fileErr != nil {
			tdParams := scenario.TeardownParams{
				ScenarioName:  scenarioName,
				WorkspacePath: workspacePath,
				EventLogPath:  absEvLogPath,
			}
			tdResult, _ := scenario.TeardownFixture(ctx, tdParams)
			result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
				evLogRelPath, scenario.FailureClassFixtureSetupFailed, fileErr.Error())
			result.WorkspaceSnapshotPath = tdResult.WorkspaceSnapshotPath
			_ = scenario.WriteScenarioResult(fixtureRoot, result)
			completedResults = append(completedResults, result)
			continue
		}

		// Resolve and seed the DOT workflow; determine daemon workflow mode.
		workflowMode, dotErr := harnessApplyWorkflowDOT(projectRoot, cwd, sf)
		if dotErr != nil {
			tdParams := scenario.TeardownParams{
				ScenarioName:  scenarioName,
				WorkspacePath: workspacePath,
				EventLogPath:  absEvLogPath,
			}
			tdResult, _ := scenario.TeardownFixture(ctx, tdParams)
			result := harnessEarlyErrorResult(scenarioName, scenarioSource, startedAt,
				evLogRelPath, scenario.FailureClassFixtureSetupFailed, dotErr.Error())
			result.WorkspaceSnapshotPath = tdResult.WorkspaceSnapshotPath
			_ = scenario.WriteScenarioResult(fixtureRoot, result)
			completedResults = append(completedResults, result)
			continue
		}

		// §7.1 step 3: drive orchestration (SH-025/SH-026).
		orchErr := scenario.DriveOrchestration(ctx, scenario.OrchestrationConfig{
			ProjectDir:    projectRoot,
			JSONLLogPath:  absEvLogPath,
			HandlerBinary: resolvedBinary,
			HandlerArgs:   handlerArgs,
			WorkflowMode:  workflowMode,
			TimeoutSecs:   sf.TimeoutSecs,
		})

		// Classify orchestration outcome — timeout FIRST per §7.1 step 3 note.
		var (
			finalVerdict     scenario.ScenarioVerdict
			finalFC          scenario.FailureClass
			finalErrDetail   string
			assertionResults []scenario.AssertionResult
		)

		switch {
		case errors.Is(orchErr, scenario.ErrScenarioTimeout):
			finalVerdict = scenario.ScenarioVerdictTimeout
			finalFC = scenario.FailureClassScenarioTimeout
			finalErrDetail = orchErr.Error()
		case orchErr != nil:
			finalVerdict = scenario.ScenarioVerdictError
			finalFC = scenario.FailureClassOrchestrationInternalError
			finalErrDetail = orchErr.Error()
		default:
			// §7.1 step 4: read event log and evaluate assertions.
			events, readErr := scenario.ReadEventLog(absEvLogPath)
			if readErr != nil {
				finalVerdict = scenario.ScenarioVerdictError
				finalFC = scenario.FailureClassOrchestrationInternalError
				finalErrDetail = fmt.Sprintf("read event log: %v", readErr)
			} else {
				var assertFC scenario.FailureClass
				assertionResults, finalVerdict, assertFC = scenario.EvaluateAssertions(
					sf, events, workspacePath)
				finalFC = assertFC // empty iff verdict=pass
			}
		}

		// §7.1 step 5: teardown fixture (SH-015) — run-to-completion best-effort.
		tdParams := scenario.TeardownParams{
			ScenarioName:  scenarioName,
			WorkspacePath: workspacePath,
			EventLogPath:  absEvLogPath,
		}
		tdResult, tdErr := scenario.TeardownFixture(ctx, tdParams)
		completedAt := time.Now().UTC().Truncate(time.Millisecond)

		// §8.0 cleanup-failed precedence: rank 8 (lowest) — never overwrites
		// a prior fail/timeout/error verdict; downgrades pass→error only.
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

		// SH-034: write per-scenario result before advancing to the next scenario.
		if writeErr := scenario.WriteScenarioResult(fixtureRoot, result); writeErr != nil {
			fmt.Fprintf(stderr, "harness: write scenario result %q: %v\n",
				scenarioName, writeErr)
		}
		completedResults = append(completedResults, result)

		// SH-033: check for signal-driven cancellation after scenario completes.
		if ctx.Err() != nil {
			return interruptExit()
		}
	}

	// All scenarios completed — build and emit SuiteResult (SH-034).
	suiteVerdict := scenario.SuiteVerdictPass
	for _, r := range completedResults {
		if r.Verdict != scenario.ScenarioVerdictPass {
			suiteVerdict = scenario.SuiteVerdictFail
			break
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
		fmt.Fprintf(stderr, "harness: write suite result: %v\n", writeErr)
	}
	if emitErr := scenario.EmitSuiteResult(
		stdout, scenario.SuiteResultOutputFormat(outputFlag), sr); emitErr != nil {
		fmt.Fprintf(stderr, "harness: emit suite result: %v\n", emitErr)
	}

	if suiteVerdict == scenario.SuiteVerdictFail {
		return harnessExitFail
	}
	return harnessExitPass
}

// harnessInterruptExitCode returns the exit code for a received signal per
// specs/scenario-harness.md §4.12 SH-032:
//   - SIGINT  → 130 (128 + signal number 2)
//   - SIGTERM → 143 (128 + signal number 15)
//   - other   → 130 (treat as SIGINT-equivalent)
func harnessInterruptExitCode(sig os.Signal) int {
	if sig == syscall.SIGTERM {
		return harnessExitSIGTERM
	}
	return harnessExitOperatorInterrupt
}

// harnessEmitInterruptResult builds and emits a partial SuiteResult to stdout
// per the SH-033 graceful-shutdown protocol. The partial result contains
// completed is the list of ScenarioResult values collected before the interrupt.
//
// The SuiteResult is not structurally fully valid when fixtureRoot is empty
// (fixture root not yet created at interrupt time), but it is emitted regardless
// so that operators can inspect completed scenario results.
//
// Spec ref: specs/scenario-harness.md §4.13 SH-033.
func harnessEmitInterruptResult(
	stdout, stderr io.Writer,
	format scenario.SuiteResultOutputFormat,
	suiteID core.SuiteID,
	startedAt time.Time,
	fixtureRoot string,
	cadenceFilter scenario.CadenceFilter,
	completed []scenario.ScenarioResult,
	sig os.Signal,
) {
	sigName := "SIGINT"
	if sig == syscall.SIGTERM {
		sigName = "SIGTERM"
	}
	fmt.Fprintf(stderr, "harmonik harness: %s received — emitting partial SuiteResult\n", sigName)

	// Suite verdict is fail if any completed scenario failed; otherwise pass
	// (the vacuous case of zero completed scenarios is pass per SH-029).
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
		fmt.Fprintf(stderr, "harmonik harness: emit partial SuiteResult: %v\n", emitErr)
	}
}

// harnessDiscoverScenarios discovers scenario YAML files and returns them
// sorted in byte-lexicographic order of their name field per SH-007.
//
// If scenarioPaths is non-empty only those files are loaded (--scenario flag).
// Otherwise all .yaml files under <projectRoot>/scenarios/ are discovered
// recursively (SH-002, SH-006) and filtered by cadenceFilter. Files with a
// .yml or .YAML extension are rejected as scenario-load-failure per SH-002;
// all other non-.yaml extensions are silently skipped.
//
// Suite-wide name uniqueness (SH-005) is enforced across all parsed files
// before cadence filtering; conflicting paths are included in the error.
//
// Any rejection produces an entry in the returned error slice. If any error
// is present the caller MUST abort with exit code 2.
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
			fmt.Fprintf(stderr, "harness: discovering scenarios under %s\n", scenariosDir)
		}
		var walkErr error
		var wrongExt []error
		_ = filepath.WalkDir(scenariosDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				walkErr = err
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
			// SH-002: .yml and uppercase variants look like YAML but use the
			// wrong extension; reject without opening.
			if ext == ".yml" || strings.ToLower(ext) == ".yaml" {
				wrongExt = append(wrongExt, fmt.Errorf(
					"scenario-load-failure: %q has extension %q; scenario files MUST use .yaml (SH-002)",
					path, ext,
				))
			}
			// Other extensions (.dot, .md, etc.) are silently skipped.
			return nil
		})
		if walkErr != nil {
			return nil, []error{fmt.Errorf("walk scenarios dir %q: %w", scenariosDir, walkErr)}
		}
		loadErrs = append(loadErrs, wrongExt...)
		// Stable parse order for deterministic error reporting across platforms.
		sort.Strings(paths)
	}

	// Parse all files. Track name→path for suite-wide duplicate detection (SH-005).
	// Duplicate detection spans all parsed files, before cadence filtering,
	// so a name collision between scenarios of different cadences is caught.
	nameToPath := make(map[string]string, len(paths))
	var allLoaded []harnessScenarioEntry

	for _, path := range paths {
		sf, err := scenario.ParseScenarioFile(path)
		if err != nil {
			loadErrs = append(loadErrs, err)
			continue
		}
		// SH-005: suite-wide name uniqueness; report both conflicting paths.
		if prev, exists := nameToPath[sf.Name]; exists {
			loadErrs = append(loadErrs, fmt.Errorf(
				"scenario-load-failure: duplicate scenario name %q in %q and %q (SH-005)",
				sf.Name, prev, path,
			))
			continue
		}
		nameToPath[sf.Name] = path
		allLoaded = append(allLoaded, harnessScenarioEntry{ScenarioFile: sf, SourcePath: path})
	}

	// Apply cadence filter.
	var scenarios []harnessScenarioEntry
	for _, entry := range allLoaded {
		if !cadenceFilter.Includes(entry.CadenceTag) {
			if verbose {
				fmt.Fprintf(stderr, "harness: skip %q (cadence=%s not in filter=%s)\n",
					entry.Name, entry.CadenceTag, cadenceFilter)
			}
			continue
		}
		scenarios = append(scenarios, entry)
	}

	// SH-007: execute in byte-lexicographic order of the name field.
	// This is locale-independent UTF-8 byte comparison, not file-path order.
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Name < scenarios[j].Name
	})

	return scenarios, loadErrs
}

// resolveTwinSearchPaths returns the ordered twin-binary search-path list per
// the three-level precedence rule in specs/scenario-harness.md §4.3 SH-009:
//
//	(i)  CLI flag --twin-search-path (flagValue non-empty)
//	(ii) environment variable HARMONIK_TWIN_SEARCH_PATH (envValue non-empty)
//	(iii) in-tree default: <cwd>/twins/
//
// The resolved path is returned as a one-element slice because BootstrapFixture
// accepts []string; multiple search directories may be supported in a future
// iteration per OQ-SH-009.
//
// Spec ref: specs/scenario-harness.md §4.3 SH-009.
func resolveTwinSearchPaths(flagValue, envValue, cwd string) []string {
	if flagValue != "" {
		return []string{flagValue}
	}
	if envValue != "" {
		return []string{envValue}
	}
	return []string{filepath.Join(cwd, "twins")}
}

// harnessMatrixCellCount returns the cartesian-product cell count for a matrix
// map. An empty or nil map has 1 cell (the scenario itself, no expansion).
// Zero-length value lists produce 0 cells; callers need not special-case this
// because ScenarioFile.Valid() rejects them at load time.
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

// harnessResolveTwinBinary resolves the first declared AgentOverride binary
// against the ordered twinSearchPaths list per specs/scenario-harness.md
// §4.3 SH-009. Returns the resolved absolute path and forwarded args on
// success, or an error classified as twin-binary-not-found by the caller.
// Returns ("", nil, nil) when overrides is nil or empty.
//
// For G-02, only the first override (sorted by agent-role key for determinism)
// is resolved; OrchestrationConfig supports a single HandlerBinary for now.
//
// Spec ref: specs/scenario-harness.md §4.3 SH-009, §4.4 SH-008.
func harnessResolveTwinBinary(
	overrides map[string]scenario.AgentOverride,
	searchPaths []string,
) (binary string, args []string, err error) {
	if len(overrides) == 0 {
		return "", nil, nil
	}

	// Deterministic iteration: sort keys so the caller always picks the same
	// override when multiple roles are declared.
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

	// Absolute path: use directly without search-path lookup (SH-009 tier (i)).
	if filepath.IsAbs(binaryName) {
		if _, statErr := os.Stat(binaryName); statErr != nil {
			return "", nil, fmt.Errorf("twin-binary-not-found: absolute path %q: %w",
				binaryName, statErr)
		}
		return binaryName, override.Args, nil
	}

	// Relative name: walk each search path (SH-009 tiers (ii)/(iii)).
	for _, sp := range searchPaths {
		candidate := filepath.Join(sp, binaryName)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, override.Args, nil
		}
	}
	return "", nil, fmt.Errorf("twin-binary-not-found: %q not found in search paths %v",
		binaryName, searchPaths)
}

// harnessApplyFixtureFiles seeds the files declared in FixtureSetup.Files into
// projectRoot per specs/scenario-harness.md §6.1 RECORD FixtureSetup.
//
// Each key is a repo-relative path; the value carries the content (utf8 or
// base64) and an optional octal mode string. Absent Encoding defaults to utf8;
// absent Mode defaults to 0644. Failure of any single file write is returned
// immediately; the caller classifies it as fixture-setup-failed per §8.3.
func harnessApplyFixtureFiles(projectRoot string, files map[string]scenario.FileSeed) error {
	for relPath, seed := range files {
		absPath := filepath.Join(projectRoot, relPath)
		if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
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

		//nolint:gosec // G306: mode is declared in the scenario file, not raw user input
		if writeErr := os.WriteFile(absPath, content, mode); writeErr != nil {
			return fmt.Errorf("write %q: %w", relPath, writeErr)
		}
	}
	return nil
}

// harnessApplyWorkflowDOT resolves the DOT workflow declared by sf.WorkflowPath
// and seeds it into <projectRoot>/.harmonik/workflow.dot so the daemon picks
// it up at startup when WorkflowModeDefault=dot. Returns core.WorkflowModeDot
// on success. When sf.WorkflowPath is nil (workflow_id case), returns
// core.WorkflowModeReviewLoop with no filesystem mutation.
//
// Resolution order per specs/scenario-harness.md §6.1 WorkflowPath:
//
//	(i)  <projectRoot>/<WorkflowPath>
//	(ii) <cwd>/scenarios/_workflows/<WorkflowPath>
//
// Failure is classified as fixture-setup-failed by the caller.
func harnessApplyWorkflowDOT(
	projectRoot, cwd string,
	sf scenario.ScenarioFile,
) (core.WorkflowMode, error) {
	if sf.WorkflowPath == nil {
		return core.WorkflowModeReviewLoop, nil
	}
	dotRelPath := *sf.WorkflowPath

	var dotContent []byte
	candidate1 := filepath.Join(projectRoot, dotRelPath)
	if content, readErr := os.ReadFile(candidate1); readErr == nil {
		dotContent = content
	} else {
		candidate2 := filepath.Join(cwd, "scenarios", "_workflows", dotRelPath)
		content, readErr2 := os.ReadFile(candidate2)
		if readErr2 != nil {
			return "", fmt.Errorf("resolve workflow_path %q: not found at %q or %q",
				dotRelPath, candidate1, candidate2)
		}
		dotContent = content
	}

	// Seed to <projectRoot>/.harmonik/workflow.dot (BootstrapFixture already
	// created the .harmonik/ directory via MkdirAll for the event-log dir).
	targetPath := filepath.Join(projectRoot, ".harmonik", "workflow.dot")
	if mkErr := os.MkdirAll(filepath.Dir(targetPath), 0o755); mkErr != nil {
		return "", fmt.Errorf("create .harmonik dir for workflow.dot: %w", mkErr)
	}
	//nolint:gosec // G306: workflow dot, not a user-controlled secret
	if writeErr := os.WriteFile(targetPath, dotContent, 0o644); writeErr != nil {
		return "", fmt.Errorf("write workflow.dot: %w", writeErr)
	}
	return core.WorkflowModeDot, nil
}

// harnessEarlyErrorResult builds a ScenarioResult for scenarios that terminate
// before TeardownFixture can run (or ran with no filesystem access). The
// WorkspaceSnapshotPath is computed via the pure formula WorkspaceSnapshotPath;
// callers that obtained a tdResult from a partial teardown SHOULD override the
// returned field with tdResult.WorkspaceSnapshotPath.
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
