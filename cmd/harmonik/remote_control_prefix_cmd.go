package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/projectconfig"
)

func runRemoteControlPrefixSubcommand(args []string) int {
	projectDir := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
			if projectDir == "" {
				fmt.Fprintln(os.Stderr, "harmonik remote-control-prefix: --project= requires a directory")
				return 1
			}
		case args[i] == "--help" || args[i] == "-h":
			fmt.Print(`harmonik remote-control-prefix — print the per-project Claude RC label prefix

USAGE
  harmonik remote-control-prefix [--project DIR]

FLAGS
  --project DIR  Project directory (default: current working directory)

OUTPUT
  Prints the daemon.remote_control_prefix value from .harmonik/config.yaml
  followed by a newline. An absent/empty prefix prints an empty line (exit 0),
  so the bare --remote-control label is used (backward compatible).

NOTES
  Side-effect-free: does not start, contact, or require a running daemon.
  Shell launchers use this to fetch the prefix without parsing YAML:
    RC_PREFIX="$(harmonik remote-control-prefix --project "$P" 2>/dev/null || true)"
    ... --remote-control "${RC_PREFIX:+$RC_PREFIX-}$NAME"

EXAMPLES
  harmonik remote-control-prefix
  harmonik remote-control-prefix --project /path/to/project

SPEC
  hk-igpg (per-project Remote-Control session-label prefix)
`)
			return 0
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik remote-control-prefix: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	cfg, err := projectconfig.LoadProjectConfig(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik remote-control-prefix: load .harmonik/config.yaml: %v\n", err)
		return 1
	}

	fmt.Println(cfg.Daemon.RemoteControlPrefix)
	return 0
}
