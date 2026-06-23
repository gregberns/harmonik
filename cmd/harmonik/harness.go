package main

// harness.go — `harmonik harness` subcommand (hk-nwqa0).
//
// Implements the scenario-harness CLI surface declared in
// specs/scenario-harness.md §4.12 SH-032.
//
// At this iteration the subcommand supports:
//   - Flag parsing for all 8 MVH flags (SH-032).
//   - --list: discover scenarios, filter by cadence, print name+cadence.
//   - --dry-run: suite-load + matrix-expansion validation; no orchestration.
//   - Signal setup for SIGINT/SIGTERM (SH-033 partial — infrastructure only;
//     graceful-shutdown teardown requires G-02/G-03 which are future beads).
//
// The full execution path (suite orchestration, assertion evaluation, result
// emission) depends on G-02 (orchestration drive), G-03 (fixture teardown),
// G-05 (assertion evaluator), and G-06 (result emission) — all separate beads.
// Attempting full execution returns exit code 3 (harness-internal-error) with
// an explicit "not yet implemented" message until those beads land.
//
// Spec refs: specs/scenario-harness.md §4.12 SH-032, §4.13 SH-033.
// Bead ref: hk-nwqa0.

import (
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

	"github.com/gregberns/harmonik/internal/scenario"
)

// Harness CLI exit codes per specs/scenario-harness.md §4.12 SH-032.
const (
	harnessExitPass              = 0   // SuiteResult.suite_verdict = pass
	harnessExitFail              = 1   // SuiteResult.suite_verdict = fail
	harnessExitSuiteLoadAbort    = 2   // Suite-load aborted (parse/duplicate/schema error)
	harnessExitInternalError     = 3   // Harness-internal error (panic, unrecoverable I/O)
	harnessExitOperatorInterrupt = 130 // Operator interrupt (SIGINT)
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
  130  Operator interrupt (SIGINT)

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

// runHarness is the testable core of the harness subcommand.
//
// Spec ref: specs/scenario-harness.md §4.12 SH-032.
func runHarness(args []string, stdout, stderr io.Writer) int {
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

	// Set up signal handling per SH-033. The channel is buffered so that a
	// second SIGINT during shutdown is captured for hard-exit (SH-033 §2).
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Resolve working directory for scenario discovery and twin-search-path
	// default.
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "harmonik harness: cannot determine working directory: %v\n", err)
		return harnessExitInternalError
	}

	// Twin-search-path precedence: CLI flag > env > <cwd>/twins/ (SH-009).
	if twinSearchPath == "" {
		if env := os.Getenv("HARMONIK_TWIN_SEARCH_PATH"); env != "" {
			twinSearchPath = env
		} else {
			twinSearchPath = filepath.Join(cwd, "twins")
		}
	}
	// twinSearchPath is wired to the execution layer (G-02); not consumed here.
	_ = twinSearchPath
	// fixtureRootFlag is wired to SH-016 via NewFixtureRoot; not consumed for
	// list/dry-run paths which do not create a fixture root.
	_ = fixtureRootFlag

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

	// Full execution path: fixture teardown (G-03), assertion evaluator (G-05),
	// and result emitter (G-06) are not yet implemented. The orchestration drive
	// (G-02) is implemented via scenario.DriveOrchestration but cannot be wired
	// into the full pipeline until G-03/G-05/G-06 land. Return
	// harness-internal-error per the exit-code table (SH-032, code 3).
	fmt.Fprintf(stderr, "harmonik harness: full scenario execution not yet implemented\n")
	fmt.Fprintf(stderr, "harmonik harness: fixture teardown (G-03), assertion evaluator (G-05), and result emitter (G-06) are required\n")
	fmt.Fprintf(stderr, "harmonik harness: use --list to discover scenarios or --dry-run to validate\n")
	return harnessExitInternalError
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
	var allLoaded []scenario.ScenarioFile

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
	var scenarios []scenario.ScenarioFile
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
