package main

// greenlight_cmd.go — `harmonik greenlight` CLI subcommand (AC2, hk-lacr).
//
// Removes the "needs-greenlight" label from a staged deploy+verify follow-up
// bead so the daemon's dispatch loop can claim it. This is the captain's
// explicit approval mechanism required by flywheel-motion.md §5.3/§6.2.
//
// Usage:
//
//	harmonik greenlight <bead-id> [--project DIR]
//
// Exit codes:
//
//	0   Label removed (or bead did not carry the label — idempotent).
//	1   Argument or exec error.
//	2   Unrecognised arguments.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// labelNeedsGreenlightCLI mirrors the daemon + brcli constant; kept local so
// this file has no import dependency on internal packages.
const labelNeedsGreenlightCLI = "needs-greenlight"

// runGreenlightSubcommand implements `harmonik greenlight <bead-id> [--project DIR]`.
// subArgs is os.Args[2:].
func runGreenlightSubcommand(subArgs []string) int {
	var (
		projectFlag string
		positional  []string
	)

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			greenlightUsage()
			return 0
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik greenlight: unknown flag %q\n", arg)
			greenlightUsage()
			return 2
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		fmt.Fprintf(os.Stderr, "harmonik greenlight: exactly one <bead-id> argument is required\n")
		greenlightUsage()
		return 1
	}
	beadID := positional[0]

	// Resolve project directory.
	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik greenlight: cannot determine cwd: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	abs, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik greenlight: resolve project path: %v\n", err)
		return 1
	}

	brPath, err := exec.LookPath("br")
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik greenlight: br not found in PATH: %v\n", err)
		return 1
	}

	//nolint:gosec // G204: brPath from exec.LookPath; beadID is operator-supplied bead id
	cmd := exec.CommandContext(context.Background(), brPath,
		"label", "remove", beadID, "-l", labelNeedsGreenlightCLI)
	cmd.Dir = abs
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if runErr := cmd.Run(); runErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik greenlight: br label remove: %v\n", runErr)
		return 1
	}

	fmt.Fprintf(os.Stdout, "greenlit %s: needs-greenlight label removed; bead is now dispatchable\n", beadID)
	return 0
}

func greenlightUsage() {
	fmt.Print(`harmonik greenlight — captain approval for staged deploy+verify beads (AC2, hk-lacr)

USAGE
  harmonik greenlight <bead-id> [--project DIR]

DESCRIPTION
  Removes the "needs-greenlight" label from a staged deploy+verify follow-up
  bead, allowing the daemon's dispatch loop to claim it. The label is applied
  automatically by the flywheel staged-bead generator so the captain must
  review and approve before dispatch (flywheel-motion.md §5.3/§6.2).

  Idempotent: if the bead does not carry the label, the command exits 0.

FLAGS
  --project DIR   Project directory (default: cwd)

EXIT CODES
  0   Label removed (or bead did not carry the label)
  1   Argument or exec error
  2   Unrecognised flag
`)
}
