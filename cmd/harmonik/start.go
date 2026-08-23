package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

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

type startDispatch struct {
	captain  func(subArgs []string) int
	crew     func(subArgs []string) int
	skewHint func(projectDir string, stderr io.Writer)
}

func defaultStartDispatch() startDispatch {
	return startDispatch{
		captain:  runCaptainSubcommand,
		crew:     runCrewSubcommand,
		skewHint: PrintSkewHintIfStale,
	}
}

func runStart(args []string) int {
	return runStartWith(args, defaultStartDispatch(), os.Stdout, os.Stderr)
}

func runStartWith(args []string, dispatch startDispatch, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		if err := startUsage(stdout); err != nil {
			return 1
		}
		if len(args) == 0 {
			if _, err := fmt.Fprintln(stderr, "harmonik start: a role is required — `start daemon`, `start captain`, `start crew <name>`, `start commodore`, `start admiral`, or `start assessor`"); err != nil {
				return 1
			}
			return 2
		}
		return 0
	}

	role := args[0]
	roleArgs := args[1:]

	switch role {
	case "daemon":
		if _, err := fmt.Fprintln(stderr, "harmonik start daemon: handled by the top-level dispatch, not the role launcher — reaching here means run() no longer intercepts it"); err != nil {
			return 1
		}
		return 2
	case "captain":
		if dispatch.skewHint != nil {
			dispatch.skewHint(startResolveProjectDir(args), stderr)
		}
		return runStartRole(roleArgs, startRoleSpec{
			role:           "captain",
			takesName:      false,
			downstreamName: "--name",
			dispatch: func(name string, flags []string) int {
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
				argv := []string{"start"}
				if name != "" {
					argv = append(argv, name)
				}
				argv = append(argv, flags...)
				return dispatch.crew(argv)
			},
		}, stderr)
	case "commodore", "admiral", "assessor":
		if dispatch.skewHint != nil {
			dispatch.skewHint(startResolveProjectDir(args), stderr)
		}
		return runStartRole(roleArgs, startRoleSpec{
			role:           role,
			takesName:      false,
			downstreamName: "--name",
			dispatch: func(name string, flags []string) int {
				argv := append([]string{"start", role}, flags...)
				return dispatch.crew(argv)
			},
		}, stderr)
	default:
		if _, err := fmt.Fprintf(stderr, "harmonik start: unknown role %q — roles are: daemon, captain, crew, commodore, admiral, assessor\n", role); err != nil {
			return 1
		}
		return 2
	}
}

type startRoleSpec struct {
	role           string
	takesName      bool // crew accepts a bare positional name; captain does not
	downstreamName string
	// dispatch is called with the resolved name ("" when none) and the flag
	// tokens to forward downstream.
	dispatch func(name string, flags []string) int
}

func runStartRole(args []string, spec startRoleSpec, stderr io.Writer) int {
	var positionals []string // leading bare tokens, before any flag
	hasFlag := false

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			hasFlag = true
			continue
		}
		if !hasFlag {
			positionals = append(positionals, arg)
		}
	}

	if hasFlag && len(positionals) > 0 {
		if _, err := fmt.Fprintf(stderr,
			"harmonik start %s: positional name not allowed alongside flags — use %s %s\n",
			spec.role, spec.downstreamName, positionals[0]); err != nil {
			return 1
		}
		return 2
	}

	if !spec.takesName && len(positionals) > 0 {
		if _, err := fmt.Fprintf(stderr,
			"harmonik start %s: takes no positional argument (got %q) — captain has no name positional; pass %s NAME if you need a custom identity\n",
			spec.role, positionals[0], spec.downstreamName); err != nil {
			return 1
		}
		return 2
	}

	if len(positionals) > 1 {
		if _, err := fmt.Fprintf(stderr,
			"harmonik start %s: at most one positional name is allowed (got %d) — use %s NAME plus flags\n",
			spec.role, len(positionals), spec.downstreamName); err != nil {
			return 1
		}
		return 2
	}

	name := ""
	if len(positionals) == 1 {
		name = positionals[0]
	}

	if hasFlag {
		return spec.dispatch("", args)
	}

	return spec.dispatch(name, nil)
}

func startUsage(w io.Writer) error {
	_, err := fmt.Fprint(w,
		`harmonik start — launch a daemon, or a captain/crew/commodore/admiral/assessor session

USAGE
  harmonik start daemon [--project DIR] …      # the ONLY way to start a daemon
  harmonik start captain                       # all defaults
  harmonik start crew <name>                   # one bare positional = the crew name
  harmonik start commodore                     # oversight planner, protected launch
  harmonik start admiral                       # oversight planner, protected launch
  harmonik start assessor                      # standing assessor role, protected launch
  harmonik start captain --name NAME …         # advanced: all named, NO positional
  harmonik start crew --name NAME --queue Q …  # advanced: name via --name, NO positional

RULE (positional XOR flags)
  Simple form  = role + at-most-one bare positional name + NO flags.
  Advanced form = any --flag present => ZERO bare positionals; the name must be --name.
  Mixing a bare name with flags is a hard error.

NOTE
  commodore, admiral, and assessor are singleton roles: the identity is the
  role name, not a free choice, so — like captain — they take no positional
  name. All route through the crew-registry RPC path (crew start), so
  daemon-boot orphan-sweep protection is automatic, not something to remember.
  The assessor is a STANDING role spawned manually by the admiral (spawn:manual).

EXIT CODES
  0   This help was printed, or the role launcher succeeded.
  1   The role launcher failed. captain returns 1 for a bad captain flag or a
      launch that did not complete. crew, commodore, admiral and assessor
      return 1 for a bad argument or for an op the daemon refused.
  2   No role was given, the role is not one of the six, or a bare name was
      mixed with flags.
  17  crew, commodore, admiral and assessor: the daemon is not running.
      Exit 17 does NOT mean nothing happened. These roles provision the
      project and wire the keeper first, then send the launch to the daemon.
      A run that ends in 17 has already written files in the project and in
      ~/.claude/settings.json. Only the session was not started.

  'start daemon' is different: it runs the daemon in this process and returns
  the daemon's own code — 0 on a clean shutdown, 1 on a startup or run
  failure, 5 when another daemon holds the pidfile lock, 9 when this binary
  is yanked.

SEE ALSO
  harmonik captain --help        full captain flags
  harmonik crew start --help     full crew flags
`)
	return err
}
