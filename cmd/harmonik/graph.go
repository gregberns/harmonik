package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func runGraphSubcommand(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(`harmonik graph — workflow graph utilities

USAGE
  harmonik graph <verb> [flags]

VERBS
  validate    Validate a .dot workflow file (EM-038 pre-run checks)

Run 'harmonik graph <verb> --help' for verb-specific flags.

EXIT CODES
  0   This help was printed, either by --help or by 'harmonik graph' with no
      verb. Also 0 when the verb you named succeeded.
  2   The verb is not one this command has.
  Each verb sets its own codes for its own work. Read them with
  'harmonik graph <verb> --help'.
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

type diagnostic struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func runGraphValidate(args []string) int {
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
			if len(args[i]) > 1 && args[i][0] == '-' {
				fmt.Fprintln(os.Stderr, "harmonik graph validate: unrecognized flag:", args[i])
				return 2
			}
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

func validateDot(src string) []diagnostic {
	graph, parseErr := dot.Parse(src, "")
	if parseErr != nil {
		var multi dot.ParseErrors
		var single *dot.ParseError
		switch {
		case errors.As(parseErr, &multi):
		case errors.As(parseErr, &single):
			multi = dot.ParseErrors{single}
		default:
			return []diagnostic{{Code: "em038_not_parseable", Detail: parseErr.Error()}}
		}
		diags := make([]diagnostic, 0, len(multi))
		for _, pe := range multi {
			diags = append(diags, diagnostic{Code: "em038_not_parseable", Detail: pe.Error()})
		}
		return diags
	}

	findings := dot.Validate(graph)
	diags := make([]diagnostic, 0, len(findings))
	for _, d := range findings {
		if d.Severity != dot.SeverityError {
			continue
		}
		diags = append(diags, diagnostic{Code: d.Code, Detail: diagnosticDetail(d)})
	}
	return diags
}

func diagnosticDetail(d dot.Diagnostic) string {
	if d.Line > 0 {
		return fmt.Sprintf("dot:%d: %s", d.Line, d.Message)
	}
	return d.Message
}
