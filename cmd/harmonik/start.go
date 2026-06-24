package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// startResolveProjectDir scans args (everything after "start") for a --project
// flag, falling back to the cwd. Used to resolve the project directory for the
// skew hint before the role-specific arg parser runs.
func startResolveProjectDir(args []string) string {
	for i, arg := range args {
		if arg == "--project" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--project=") {
			return strings.TrimPrefix(arg, "--project=")
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// startDispatch is the injectable seam for the two downstream launchers so the
// parser can be table-tested without spawning tmux or hitting the daemon RPC.
// captain receives the captain subArgs (everything after `start captain`);
// crew receives a fully-formed `crew start …` argv (verb included) so it lands
// on the existing runCrewSubcommand entry point unchanged.
// skewHint is called with the resolved project dir before dispatching; nil skips
// the check (used in tests that do not want filesystem side-effects).
type startDispatch struct {
	captain   func(subArgs []string) int
	crew      func(subArgs []string) int
	skewHint  func(projectDir string, stderr io.Writer)
}

// defaultStartDispatch wires the real downstream launchers. captain routes to
// the existing bare captain launcher (captain.go, owned by ES2); crew routes to
// the existing crew subcommand (crew.go, owned by ES4) via the `start` verb so
// the daemon-RPC crew-start path is reused verbatim — ES1 does NOT reimplement
// either internal.
func defaultStartDispatch() startDispatch {
	return startDispatch{
		captain:  runCaptainSubcommand,
		crew:     runCrewSubcommand,
		skewHint: PrintSkewHintIfStale,
	}
}

// runStart is the `harmonik start <role> …` umbrella. It owns ALL of main.go's
// start-routing: it validates the positional-XOR-flags rule (operator decision
// D2), resolves the role's name, and delegates to the existing captain/crew
// launchers at the function boundary.
//
// args is everything after `start` (i.e. os.Args[2:]).
func runStart(args []string) int {
	return runStartWith(args, defaultStartDispatch(), os.Stdout, os.Stderr)
}

// runStartWith is runStart with an injectable dispatch + writers for testing.
func runStartWith(args []string, dispatch startDispatch, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		startUsage(stdout)
		// A bare `start` with no role is a usage error; an explicit --help is not.
		if len(args) == 0 {
			fmt.Fprintln(stderr, "harmonik start: a role is required — `start captain` or `start crew <name>`")
			return 2
		}
		return 0
	}

	role := args[0]
	roleArgs := args[1:]

	switch role {
	case "captain":
		// Emit the stale-assets hint before launching so the operator sees it
		// in the launch log. Fires only for valid roles. Best-effort: nil skips.
		if dispatch.skewHint != nil {
			dispatch.skewHint(startResolveProjectDir(args), stderr)
		}
		return runStartRole(roleArgs, startRoleSpec{
			role:           "captain",
			takesName:      false,
			downstreamName: "--name",
			dispatch: func(name string, flags []string) int {
				// captain takes no positional name; flags (incl. an explicit
				// --name) pass straight through to the bare launcher.
				return dispatch.captain(flags)
			},
		}, stderr)
	case "crew":
		if dispatch.skewHint != nil {
			dispatch.skewHint(startResolveProjectDir(args), stderr)
		}
		return runStartRole(roleArgs, startRoleSpec{
			role:           "crew",
			takesName:      true,
			downstreamName: "--name",
			dispatch: func(name string, flags []string) int {
				// Reuse the existing `crew start <name> …` entry point. In the
				// simple form the resolved name is passed as the positional the
				// crew launcher already expects; in the advanced form the name
				// arrives via --name among the flags, so emit no positional.
				argv := []string{"start"}
				if name != "" {
					argv = append(argv, name)
				}
				argv = append(argv, flags...)
				return dispatch.crew(argv)
			},
		}, stderr)
	default:
		fmt.Fprintf(stderr, "harmonik start: unknown role %q — roles are: captain, crew\n", role)
		return 2
	}
}

// startRoleSpec parametrises the shared XOR pre-parser per role.
type startRoleSpec struct {
	role           string
	takesName      bool // crew accepts a bare positional name; captain does not
	downstreamName string
	// dispatch is called with the resolved name ("" when none) and the flag
	// tokens to forward downstream.
	dispatch func(name string, flags []string) int
}

// runStartRole applies the positional-XOR-flags rule (D2) for one role.
//
//   - Simple form: at most one bare positional (the name, crew only) and NO
//     flags.
//   - Advanced form: the moment any --flag appears, ZERO bare positionals are
//     allowed; the name must arrive via --name.
//   - Mixing a bare positional name with any flag is a hard error.
func runStartRole(args []string, spec startRoleSpec, stderr io.Writer) int {
	// The pre-parser is intentionally thin and role-agnostic: it does NOT know
	// which downstream flags take a value (`--name paul`), so a token after a
	// flag is ambiguous between a flag-value and a stray positional. To stay
	// correct without that knowledge we count a bare positional name ONLY in the
	// leading run BEFORE the first flag. Everything from the first flag onward is
	// "flag-land" (flags and their values) and is forwarded verbatim. This is
	// exactly the operator's XOR intent: `crew paul` (leading name, no flags) vs.
	// `crew --name paul …` (all-named); a leading name *plus* any flag is the
	// mixing error. A stray positional buried among flags (rare, agent-proofed
	// against by the very rule) is left to the downstream launcher to reject.
	var positionals []string // leading bare tokens, before any flag
	hasFlag := false

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			// --help/-h is treated as a flag (defers to the downstream launcher's
			// full flag listing) — it implies no positional name.
			hasFlag = true
			continue
		}
		if !hasFlag {
			positionals = append(positionals, arg)
		}
		// A bare token AFTER a flag is flag-land (a value); not counted.
	}

	// XOR enforcement.
	if hasFlag && len(positionals) > 0 {
		fmt.Fprintf(stderr,
			"harmonik start %s: positional name not allowed alongside flags — use %s %s\n",
			spec.role, spec.downstreamName, positionals[0])
		return 2
	}

	if !spec.takesName && len(positionals) > 0 {
		fmt.Fprintf(stderr,
			"harmonik start %s: takes no positional argument (got %q) — captain has no name positional; pass %s NAME if you need a custom identity\n",
			spec.role, positionals[0], spec.downstreamName)
		return 2
	}

	if len(positionals) > 1 {
		fmt.Fprintf(stderr,
			"harmonik start %s: at most one positional name is allowed (got %d) — use %s NAME plus flags\n",
			spec.role, len(positionals), spec.downstreamName)
		return 2
	}

	name := ""
	if len(positionals) == 1 {
		name = positionals[0]
	}

	if hasFlag {
		// Advanced form: forward the flags verbatim; the name (if any) lives in
		// --name among them. No positional is synthesised.
		return spec.dispatch("", args)
	}

	// Simple form: the resolved positional name is forwarded; no flags.
	return spec.dispatch(name, nil)
}

// startUsage prints the `harmonik start` umbrella help. The per-role flag detail
// lives on the downstream launchers (captain.go / crew.go).
func startUsage(w io.Writer) {
	fmt.Fprint(w, `harmonik start — launch a captain or crew (keeper auto-armed)

USAGE
  harmonik start captain                       # all defaults
  harmonik start crew <name>                   # one bare positional = the crew name
  harmonik start captain --name NAME …         # advanced: all named, NO positional
  harmonik start crew --name NAME --queue Q …  # advanced: name via --name, NO positional

RULE (positional XOR flags)
  Simple form  = role + at-most-one bare positional name + NO flags.
  Advanced form = any --flag present => ZERO bare positionals; the name must be --name.
  Mixing a bare name with flags is a hard error.

SEE ALSO
  harmonik captain --help        full captain flags
  harmonik crew start --help     full crew flags
`)
}
