package main

import (
	"fmt"
	"os"
	"strings"
)

const exitUnknownSubcommand = 2

func unknownSubcommand(args []string) (string, bool) {
	if len(args) < 2 {
		return "", false
	}
	arg := args[1]
	if arg == "" || strings.HasPrefix(arg, "-") {
		return "", false
	}
	return arg, true
}

func daemonStartRefusal(args []string) string {
	var b strings.Builder

	if len(args) < 2 {
		b.WriteString("harmonik: no subcommand given — this does not start a daemon\n")
	} else {
		b.WriteString("harmonik: no subcommand given, only flags — this does not start a daemon\n")
		if trailing := trailingPositional(args); trailing != "" {
			fmt.Fprintf(&b, "  %q was ignored: a subcommand must come first, before any flags (see SUBCOMMANDS below)\n", trailing)
		}
	}

	b.WriteString("  To start a daemon, name it: `harmonik start daemon [--project DIR] [flags]`\n")
	return b.String()
}

func trailingPositional(args []string) string {
	if len(args) < 3 {
		return ""
	}
	last := args[len(args)-1]
	if last == "" || strings.HasPrefix(last, "-") {
		return ""
	}
	prev := args[len(args)-2]
	if strings.HasPrefix(prev, "-") {
		return ""
	}
	return last
}

func harmonikUsage() {
	fmt.Fprint(os.Stderr, harmonikUsageHeader)
	for _, command := range commandVerbs {
		if command.description == "" {
			continue
		}
		fmt.Fprintf(os.Stderr, "  %-22s %s\n", command.name, command.description)
	}
	fmt.Fprint(os.Stderr, harmonikUsageFooter)
}

func daemonUsage() {
	fmt.Fprint(os.Stderr, harmonikUsageHeader, harmonikUsageFooter)
}

const harmonikUsageHeader = `harmonik — agent-driven bead execution daemon

USAGE
  harmonik <subcommand> [flags]

  Every subcommand is a verb. Nothing starts a daemon by accident: "harmonik"
  on its own, and "harmonik --project DIR", print this help and exit 2.
  To start a daemon, name it: "harmonik start daemon".

SUBCOMMANDS
`

const harmonikUsageFooter = `
DAEMON FLAGS (used with "start daemon")
  --project DIR          Project directory (default: current working directory)
  --max-concurrent N     Max simultaneous beads (default 1)
  --auto-pull            Enable br-ready fallback poll (historical topology; default OFF)
  --no-auto-pull         No-op alias; queue-only is the default (back-compat)

EXAMPLES
  harmonik start daemon --project /path/to/project --no-auto-pull --max-concurrent 4
  harmonik queue submit --beads hk-abc123,hk-def456
  harmonik subscribe --types run_completed,run_failed --json

  harmonik run hk-abc123
  harmonik run --beads hk-abc123,hk-def456 --max-concurrent 2

Run 'harmonik <subcommand> --help' for subcommand-specific flags.
`
