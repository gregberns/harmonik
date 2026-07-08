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
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
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

	// Full execution path.
	//
	// TODO(G-02): replace the stub below with the actual per-scenario
	// orchestration loop (DriveOrchestration + TeardownFixture + result
	// emission). Pass twinSearchPaths to BootstrapFixture for each scenario
	// per SH-009. Each completed scenario's ScenarioResult appends to
	// completedResults. The loop MUST check ctx.Done() between scenarios to
	// detect operator interruption.
	//
	// The stub blocks until ctx is cancelled (by a signal) so that SH-033
	// signal handling is exercisable before the real loop lands. Operators
	// see the "not yet implemented" message and can Ctrl-C to exit.
	var completedResults []scenario.ScenarioResult // populated by G-02 loop

	_, _ = fmt.Fprintf(stderr, "harmonik harness: full scenario execution not yet implemented (G-02 pending)\n") //nolint:errcheck // best-effort diagnostic
	_, _ = fmt.Fprintf(stderr, "harmonik harness: use --list or --dry-run; send SIGINT/SIGTERM to exit\n")       //nolint:errcheck // best-effort diagnostic

	// Block until a signal cancels ctx. When G-02 lands this becomes the
	// scenario execution loop that selects on ctx.Done() to detect interrupts.
	<-ctx.Done()

	// Determine whether cancellation was signal-driven.
	select {
	case sig := <-interruptCh:
		// SH-033: check for a second SIGINT already buffered (operator pressed
		// Ctrl-C twice in rapid succession) before starting teardown. If found,
		// exit immediately without emitting a SuiteResult — the hard-exit path
		// per SH-033. A second SIGINT that arrives later (after this check) is
		// caught by the signal goroutine's second select above.
		select {
		case sig2 := <-sigCh:
			if sig2 == syscall.SIGINT {
				return harnessExitOperatorInterrupt
			}
		default:
		}
		// SH-033: graceful shutdown — emit partial SuiteResult and exit.
		harnessEmitInterruptResult(
			stdout, stderr,
			scenario.SuiteResultOutputFormat(outputFlag),
			suiteID, suiteStart, fixtureRootFlag, cadenceFilter,
			completedResults, sig,
		)
		return harnessInterruptExitCode(sig)
	default:
		// ctx cancelled by some other means (e.g. deferred cancel after a
		// future internal error). Should not occur in the current stub.
		return harnessExitInternalError
	}
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
) ([]scenario.ScenarioFile, []error) {
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
	allLoaded := make([]scenario.ScenarioFile, 0, len(paths))

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
		allLoaded = append(allLoaded, sf)
	}

	// Apply cadence filter.
	scenarios := make([]scenario.ScenarioFile, 0, len(allLoaded))
	for _, sf := range allLoaded {
		if !cadenceFilter.Includes(sf.CadenceTag) {
			if verbose {
				fmt.Fprintf(stderr, "harness: skip %q (cadence=%s not in filter=%s)\n",
					sf.Name, sf.CadenceTag, cadenceFilter)
			}
			continue
		}
		scenarios = append(scenarios, sf)
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
