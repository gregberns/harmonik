package main

// agent.go — `harmonik agent` CLI subcommand block.
//
// Verbs:
//
//	brief [--agent NAME] [--wake REASON] [--format FMT] [--project DIR] [--override]
//	    Resolve an agent → its type → manifest and emit the ordered boot document.
//	    SPEC §4 order: identity → wake → operating+skills → triggers → handoff.
//	    Exit 0 on success; non-zero on resolution or I/O error.
//
//	check <type> [--project DIR]
//	    Validate an agent type folder: manifest schema, file presence,
//	    context[].ref resolution, parent_intent reachability.
//	    Exit 0 + "ok" when well-formed; non-zero + defect list otherwise.
//
// Spec ref: .kerf/works/agent-manifest/SPEC.md §3–§4.
// Bead ref: hk-j784q (T3 — brief command), hk-9cheh (T5 — check verb).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/agentmanifest"
	"github.com/gregberns/harmonik/internal/crew"
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
	case "brief":
		return runAgentBrief(subArgs[1:])
	case "check":
		return runAgentCheck(subArgs[1:])
	default:
		fmt.Fprintf(os.Stderr, "harmonik agent: unknown verb %q\n", verb)
		agentUsage()
		return 2
	}
}

// briefArgs holds the resolved inputs to `harmonik agent brief`.
type briefArgs struct {
	agentName   string // from --agent or $HARMONIK_AGENT
	wake        string // --wake value (fresh | keeper-restart | trigger:<id>)
	format      string // --format value (markdown | json | yaml | toon)
	projectFlag string // --project value
	override    bool   // --override bypasses --agent vs $HARMONIK_AGENT conflict check
	showHelp    bool
	usageErr    string
}

// resolveAgentBriefArgs parses the arg list for `harmonik agent brief`.
func resolveAgentBriefArgs(args []string) briefArgs {
	var out briefArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			out.showHelp = true
			return out
		case arg == "--agent" && i+1 < len(args):
			i++
			out.agentName = args[i]
		case strings.HasPrefix(arg, "--agent="):
			out.agentName = strings.TrimPrefix(arg, "--agent=")
		case arg == "--wake" && i+1 < len(args):
			i++
			out.wake = args[i]
		case strings.HasPrefix(arg, "--wake="):
			out.wake = strings.TrimPrefix(arg, "--wake=")
		case arg == "--format" && i+1 < len(args):
			i++
			out.format = args[i]
		case strings.HasPrefix(arg, "--format="):
			out.format = strings.TrimPrefix(arg, "--format=")
		case arg == "--project" && i+1 < len(args):
			i++
			out.projectFlag = args[i]
		case strings.HasPrefix(arg, "--project="):
			out.projectFlag = strings.TrimPrefix(arg, "--project=")
		case arg == "--override":
			out.override = true
		case strings.HasPrefix(arg, "-"):
			out.usageErr = fmt.Sprintf("unknown flag %q", arg)
			return out
		default:
			out.usageErr = fmt.Sprintf("unexpected positional argument %q", arg)
			return out
		}
	}
	return out
}

// validBriefFormats is the set of accepted --format values.
var validBriefFormats = map[string]bool{"markdown": true, "json": true, "yaml": true, "toon": true}

// runAgentBrief implements `harmonik agent brief [--agent NAME] [flags]`.
func runAgentBrief(args []string) int {
	parsed := resolveAgentBriefArgs(args)
	if parsed.showHelp {
		agentBriefUsage()
		return 0
	}
	if parsed.usageErr != "" {
		fmt.Fprintf(os.Stderr, "harmonik agent brief: %s\n", parsed.usageErr)
		agentBriefUsage()
		return 1
	}

	// Name resolution: --agent vs $HARMONIK_AGENT (SPEC §3 load-bearing safety check).
	envAgent := os.Getenv("HARMONIK_AGENT")
	agentName := parsed.agentName
	switch {
	case agentName == "" && envAgent == "":
		fmt.Fprintf(os.Stderr, "harmonik agent brief: agent name required (use --agent or set $HARMONIK_AGENT)\n")
		agentBriefUsage()
		return 1
	case agentName == "":
		agentName = envAgent
	case envAgent != "" && envAgent != agentName && !parsed.override:
		fmt.Fprintf(os.Stderr,
			"harmonik agent brief: --agent %q conflicts with $HARMONIK_AGENT=%q; use --override to bypass\n",
			agentName, envAgent)
		return 1
	}

	format := parsed.format
	if format == "" {
		format = "markdown"
	}
	if !validBriefFormats[format] {
		fmt.Fprintf(os.Stderr, "harmonik agent brief: unknown format %q; must be one of markdown|json|yaml|toon\n", format)
		return 1
	}

	wake := parsed.wake

	projectDir := parsed.projectFlag
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik agent brief: cannot determine cwd: %v\n", err)
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik agent brief: cannot resolve project dir %q: %v\n", projectDir, err)
		return 1
	}

	agentsDir := filepath.Join(absProject, ".harmonik", "agents")

	// Instance → type resolution (T2: crew.ResolveType).
	typeName, err := crew.ResolveType(absProject, agentsDir, agentName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik agent brief: cannot resolve type for %q: %v\n", agentName, err)
		return 1
	}

	doc, err := agentmanifest.BuildBootDoc(agentsDir, absProject, agentName, typeName, wake)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik agent brief: %v\n", err)
		return 1
	}

	switch format {
	case "markdown":
		agentmanifest.RenderMarkdown(doc, os.Stdout)
	case "toon":
		agentmanifest.RenderToon(doc, os.Stdout)
	case "json":
		if err := agentmanifest.RenderJSON(doc, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik agent brief: json encode: %v\n", err)
			return 1
		}
	case "yaml":
		if err := agentmanifest.RenderYAML(doc, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik agent brief: yaml encode: %v\n", err)
			return 1
		}
	}
	return 0
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
  brief [--agent NAME] [--wake REASON] [--format FMT] [--project DIR] [--override]
                         Emit the ordered boot document for an agent (SPEC §4).
  check <type> [--project DIR]
                         Validate an agent type folder.

`)
}

func agentBriefUsage() {
	fmt.Fprintf(os.Stderr, `Usage: harmonik agent brief [--agent NAME] [flags]

Resolves an agent to its type manifest and emits the ordered boot document.
Section order (SPEC §4): identity → wake reason → operating+skills → triggers → handoff.

Name resolution:
  --agent NAME is optional; falls back to $HARMONIK_AGENT.
  If both are set and differ, the command errors unless --override is given.

Flags:
  --agent NAME      Agent name (instance or bare type). Optional when $HARMONIK_AGENT is set.
  --wake REASON     Wake reason: fresh | keeper-restart | trigger:<id>  (default: fresh)
  --format FMT      Output format: markdown | json | yaml | toon  (default: markdown)
  --project DIR     Project directory containing .harmonik/agents/ (default: cwd)
  --override        Bypass --agent vs $HARMONIK_AGENT conflict error

Exit codes:
  0   Success (boot document emitted to stdout)
  1   Argument error, resolution failure, or I/O error
  2   Unrecognised verb

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
