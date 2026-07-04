package main

// agent.go — `harmonik agent` CLI subcommand block.
//
// Verbs:
//
//	check <type> [--project DIR]
//	    Validate an agent type folder: manifest schema, file presence,
//	    context[].ref resolution, parent_intent reachability.
//	    Exit 0 + "ok" when well-formed; non-zero + defect list otherwise.
//
// Spec ref: .kerf/works/agent-manifest/SPEC.md §3 (C-C checks).
// Bead ref: hk-9cheh (T5 schema-check verb).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/agentmanifest"
)

// runAgentSubcommand routes `harmonik agent <verb> [args]`.
// subArgs is os.Args[2:].
func runAgentSubcommand(subArgs []string) int {
	verb := ""
	if len(subArgs) > 0 {
		verb = subArgs[0]
	}

	switch verb {
	case "", "--help", "-h":
		agentUsage()
		return 0
	case "check":
		return runAgentCheck(subArgs[1:])
	default:
		fmt.Fprintf(os.Stderr, "harmonik agent: unknown verb %q\n", verb)
		agentUsage()
		return 2
	}
}

// resolveAgentCheckArgs parses the arg list for `harmonik agent check`.
// Returns (typeName, projectFlag, showHelp, usageErr).
func resolveAgentCheckArgs(args []string) (typeName, projectFlag string, showHelp bool, usageErr string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			showHelp = true
			return
		case arg == "--project" && i+1 < len(args):
			i++
			projectFlag = args[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case !strings.HasPrefix(arg, "-"):
			if typeName != "" {
				usageErr = fmt.Sprintf("unexpected argument %q (type already set to %q)", arg, typeName)
				return
			}
			typeName = arg
		default:
			usageErr = fmt.Sprintf("unknown flag %q", arg)
			return
		}
	}
	return
}

// runAgentCheck implements `harmonik agent check <type> [--project DIR]`.
func runAgentCheck(args []string) int {
	typeName, projectFlag, showHelp, usageErr := resolveAgentCheckArgs(args)
	if showHelp {
		agentCheckUsage()
		return 0
	}
	if usageErr != "" {
		fmt.Fprintf(os.Stderr, "harmonik agent check: %s\n", usageErr)
		agentCheckUsage()
		return 1
	}
	if typeName == "" {
		fmt.Fprintf(os.Stderr, "harmonik agent check: type name is required\n")
		agentCheckUsage()
		return 1
	}

	projectDir := projectFlag
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik agent check: cannot determine cwd: %v\n", err)
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik agent check: cannot resolve project dir %q: %v\n", projectDir, err)
		return 1
	}

	agentsDir := filepath.Join(absProject, ".harmonik", "agents")
	defects := agentmanifest.Check(agentsDir, typeName, absProject)
	if len(defects) == 0 {
		fmt.Println("ok")
		return 0
	}
	for _, d := range defects {
		fmt.Fprintf(os.Stderr, "harmonik agent check: %s\n", d)
	}
	return 1
}

func agentUsage() {
	fmt.Fprintf(os.Stderr, `Usage: harmonik agent <verb> [flags]

Verbs:
  check <type> [--project DIR]   Validate an agent type folder.

`)
}

func agentCheckUsage() {
	fmt.Fprintf(os.Stderr, `Usage: harmonik agent check <type> [--project DIR]

Validates that <type> is a well-formed agent type folder:
  - manifest.yaml present and schema-valid
  - soul.md and operating.md present
  - each context[].ref resolves (_skills/ first, then type folder, or literal path)
  - identity.parent_intent names an existing type with a soul.md, or "operator"

Flags:
  --project DIR   Project directory containing .harmonik/agents/ (default: cwd)

Exit codes:
  0   Well-formed (prints "ok")
  1   One or more validation defects (printed to stderr)
  2   Unrecognised verb

`)
}
