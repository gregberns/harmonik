package main

// graph.go — `harmonik graph validate <path>` subcommand.
//
// Reads a .dot workflow file, runs the PreRunValidator (EM-038) against it,
// prints diagnostics to stdout, and exits non-zero on any validation failure.
//
// Spec ref: Operator-NFR §4.3 needs-attention surfacing.
// Bead ref: hk-voyf4 (T-IMPL-014).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/workflowvalidator"
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

	v := workflowvalidator.New(nil, nil)
	validationErr := v.Validate(string(src))

	if validationErr == nil {
		if jsonMode {
			fmt.Println("[]")
		} else {
			fmt.Printf("%s: valid\n", dotPath)
		}
		return 0
	}

	// Collect all ValidationError leaves.
	var diags []diagnostic
	for _, e := range collectValidationErrors(validationErr) {
		diags = append(diags, diagnostic{Code: e.Code, Detail: e.Detail})
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

// collectValidationErrors unwraps a joined error tree and returns all
// *workflowvalidator.ValidationError leaves.
func collectValidationErrors(err error) []*workflowvalidator.ValidationError {
	if err == nil {
		return nil
	}
	var ve *workflowvalidator.ValidationError
	if errors.As(err, &ve) {
		// Single ValidationError — check whether it is also a join root.
		// errors.As returns the first match; unwrap all via the join interface.
	}

	// Use errors.Join-unwrapping: try the Unwrap() []error interface first.
	type unwrapMulti interface {
		Unwrap() []error
	}
	if u, ok := err.(unwrapMulti); ok {
		var all []*workflowvalidator.ValidationError
		for _, child := range u.Unwrap() {
			all = append(all, collectValidationErrors(child)...)
		}
		return all
	}

	// Single error — try to cast directly.
	if errors.As(err, &ve) {
		return []*workflowvalidator.ValidationError{ve}
	}

	// Non-ValidationError leaf (e.g. parse error wrapped directly).
	return []*workflowvalidator.ValidationError{
		{Code: "parse_error", Detail: err.Error()},
	}
}
