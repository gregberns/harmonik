package main

// graph.go — `harmonik graph validate <path>` subcommand.
//
// Reads a .dot workflow file, parses + validates it via internal/workflow/dot
// (the SAME parser+validator the daemon execution path uses through
// workflow.LoadDotWorkflow), prints diagnostics to stdout, and exits non-zero
// on any validation failure.
//
// Unifying on internal/workflow/dot resolves hk-kxygy: the CLI pre-run check
// (EM-038) and the daemon execution path now agree on the DOT dialect. The
// legacy internal/workflowvalidator parser disagreed with the daemon on bare
// graph-level attributes, leading comments, strict digraph, handler_ref on
// non-agentic nodes (EM-007), start_node vs start_node_id, the control-point
// node type, and idempotency_class on gate/sub-workflow nodes — every one of
// those divergences is eliminated by parsing through the daemon's parser.
//
// Spec ref: Operator-NFR §4.3 needs-attention surfacing.
// Bead ref: hk-voyf4 (T-IMPL-014), hk-kxygy (parser unification).

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// runGraphSubcommand dispatches harmonik graph <verb> [args].
func runGraphSubcommand(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(`harmonik graph — workflow graph utilities

USAGE
  harmonik graph <verb> [flags]

VERBS
  validate    Validate a .dot workflow file (EM-038 pre-run checks)

Run 'harmonik graph <verb> --help' for verb-specific flags.
`)
		return 0
	}

	verb := args[0]
	switch verb {
	case "validate":
		return runGraphValidate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "harmonik graph: unrecognised verb %q; supported verbs: validate\n", verb)
		return 2
	}
}

// diagnostic is the serialisable form of a single validation failure.
type diagnostic struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// runGraphValidate implements `harmonik graph validate [--json] <path>`.
func runGraphValidate(args []string) int {
	// Parse flags manually (pre-flag.Parse subcommand pattern).
	jsonMode := false
	var dotPath string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			fmt.Print(`harmonik graph validate — validate a workflow DOT file (EM-038)

USAGE
  harmonik graph validate [--json] <path>

ARGUMENTS
  <path>    Path to a .dot workflow file

FLAGS
  --json    Emit diagnostics as a JSON array instead of plain text

EXIT CODES
  0   Valid — no diagnostics
  1   Invalid — one or more diagnostics found
  2   Usage error (bad flags, missing path)

EXAMPLES
  harmonik graph validate workflow.dot
  harmonik graph validate --json workflow.dot
`)
			return 0
		case "--json":
			jsonMode = true
		default:
			if dotPath != "" {
				fmt.Fprintln(os.Stderr, "harmonik graph validate: unexpected argument:", args[i])
				return 2
			}
			dotPath = args[i]
		}
	}

	if dotPath == "" {
		fmt.Fprintln(os.Stderr, "harmonik graph validate: missing required argument <path>")
		fmt.Fprintln(os.Stderr, "Run 'harmonik graph validate --help' for usage.")
		return 2
	}

	src, err := os.ReadFile(dotPath) //nolint:gosec // G304: operator-supplied path is intentional
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik graph validate: cannot read %q: %v\n", dotPath, err)
		return 1
	}

	// Parse + validate through internal/workflow/dot — the same path the daemon
	// uses via workflow.LoadDotWorkflow. A parse failure (strict WG-031 error)
	// becomes a single em038_not_parseable diagnostic so the run cannot start;
	// validation findings are mapped to their WG-/CP-/EM- codes. Only
	// SeverityError diagnostics count as invalid (matching LoadDotWorkflow),
	// SeverityWarning diagnostics are surfaced but do not flip the exit code.
	diags := validateDot(string(src))

	if len(diags) == 0 {
		if jsonMode {
			fmt.Println("[]")
		} else {
			fmt.Printf("%s: valid\n", dotPath)
		}
		return 0
	}

	if jsonMode {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(diags); encErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik graph validate: JSON encode error: %v\n", encErr)
			return 1
		}
	} else {
		fmt.Printf("%s: %d diagnostic(s)\n", dotPath, len(diags))
		for _, d := range diags {
			fmt.Printf("  [%s] %s\n", d.Code, d.Detail)
		}
	}

	return 1
}

// validateDot parses + validates a DOT source string through
// internal/workflow/dot and returns the failure diagnostics in the CLI's
// presentation shape ({Code, Detail}). It returns an empty slice when the
// workflow is valid (no parse error, no SeverityError validation findings).
//
// A parse failure short-circuits validation and is reported as the single
// stable em038_not_parseable diagnostic, carrying the parser's positional
// message as the detail. This preserves the documented EM-038 contract: a
// workflow that cannot be parsed cannot start. Multiple strict parse errors
// (dot.ParseErrors) are each surfaced as their own em038_not_parseable
// diagnostic.
//
// Validation diagnostics map directly: each SeverityError finding becomes one
// {Code, Detail} entry using the finding's WG-/CP-/EM- code and its message
// (line-prefixed when known). SeverityWarning findings are dropped here — they
// do not block a run per LoadDotWorkflow's contract — so the CLI exit code
// matches what the daemon would do at load time.
func validateDot(src string) []diagnostic {
	graph, parseErr := dot.Parse(src, "")
	if parseErr != nil {
		var multi dot.ParseErrors
		switch e := parseErr.(type) {
		case dot.ParseErrors:
			multi = e
		case *dot.ParseError:
			multi = dot.ParseErrors{e}
		default:
			return []diagnostic{{Code: "em038_not_parseable", Detail: parseErr.Error()}}
		}
		diags := make([]diagnostic, 0, len(multi))
		for _, pe := range multi {
			diags = append(diags, diagnostic{Code: "em038_not_parseable", Detail: pe.Error()})
		}
		return diags
	}

	var diags []diagnostic
	for _, d := range dot.Validate(graph) {
		if d.Severity != dot.SeverityError {
			continue
		}
		diags = append(diags, diagnostic{Code: d.Code, Detail: diagnosticDetail(d)})
	}
	return diags
}

// diagnosticDetail renders a dot.Diagnostic's human-facing detail, prefixing the
// source line when the parser/validator located one.
func diagnosticDetail(d dot.Diagnostic) string {
	if d.Line > 0 {
		return fmt.Sprintf("dot:%d: %s", d.Line, d.Message)
	}
	return d.Message
}
