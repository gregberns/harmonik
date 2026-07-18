package main

// remote_control_prefix_cmd.go — `harmonik remote-control-prefix [--project DIR]`
// (hk-igpg).
//
// Prints the per-project Claude Code Remote-Control LABEL prefix
// (daemon.remote_control_prefix from .harmonik/config.yaml) followed by a
// newline, and exits 0. The bash launchers (captain-launch.sh,
// ctx-watchdog-launch.sh) shell out to this so they don't reimplement YAML
// parsing — exactly mirroring how they call `harmonik project-hash`.
//
// Empty / absent prefix prints an EMPTY line (just "\n") and exits 0, so the
// caller's `RC_PREFIX="$(harmonik remote-control-prefix --project "$P")"` yields
// "" and the bare label is emitted (backward compatible). A config-load error
// exits non-zero with a diagnostic on stderr and no stdout — the caller treats a
// non-zero exit as "no prefix" (bare label), so a missing/broken config never
// blocks a launch.
//
// Side-effect-free: does not start, contact, or require a running daemon.
//
// Bead ref: hk-igpg.

import (
	"fmt"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/daemon"
)

// runRemoteControlPrefixSubcommand implements `harmonik remote-control-prefix [--project DIR]`.
//
// Exit codes: 0 success (prints the prefix, possibly empty), 1 on config-load error.
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

	cfg, err := daemon.LoadProjectConfig(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik remote-control-prefix: load .harmonik/config.yaml: %v\n", err)
		return 1
	}

	fmt.Println(cfg.Daemon.RemoteControlPrefix)
	return 0
}
