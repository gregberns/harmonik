package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// runProjectHashSubcommand implements `harmonik project-hash [--project DIR]`.
//
// Prints the PL-006a project_hash — the first 12 hexadecimal characters of
// SHA-256(realpath(project_root)) — followed by a newline, and exits 0.
//
// The computation delegates to lifecycle.ComputeProjectHash, which is the
// identical accessor the Go core uses for tmux-session scoping and
// process-provenance markers. No second hashing scheme is permitted per PL-031.
//
// Spec ref: specs/process-lifecycle.md §4.2 PL-031.
// Bead ref: hk-dmw.
func runProjectHashSubcommand(args []string) int {
	projectDir, exitCode, done := parseProjectHashArgs(args)
	if done {
		return exitCode
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik project-hash: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	// Resolve to absolute path first, then EvalSymlinks for the canonical realpath.
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik project-hash: cannot resolve path %q: %v\n", projectDir, err)
		return 1
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik project-hash: cannot resolve real path of %q: %v\n", absDir, err)
		return 1
	}

	hash := lifecycle.ComputeProjectHash(realDir)
	fmt.Println(hash.String())
	return 0
}

// parseProjectHashArgs reads the argv for `harmonik project-hash`.
//
// done=true means the caller returns exitCode without hashing anything: the
// help was printed, or the argv cannot be honoured. done=false carries the
// project directory, which is "" when no --project was given and the caller
// falls back to the working directory.
//
// Every argument this command does not have is refused. The loop used to skip
// them, so `project-hash --proj /elsewhere` printed the hash of the CURRENT
// directory and exited 0 — a typed flag name produced a wrong answer that
// looked right. The caller cannot tell that apart from a correct run, and the
// hash keys tmux sessions and process markers, so the wrong one gets acted on.
// Exit 2 matches the usage-error code the rest of this CLI uses (see
// `graph validate`).
func parseProjectHashArgs(args []string) (projectDir string, exitCode int, done bool) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--project" && i+1 < len(args):
			i++
			if args[i] == "" {
				return "", projectHashRefuseEmptyProject(), true
			}
			projectDir = args[i]
		case len(args[i]) >= 10 && args[i][:10] == "--project=":
			// >= 10, not > 10: `--project=` on its own is exactly 10 characters
			// and carries an empty value, so it belongs here and gets the same
			// refusal as `--project ""`.
			projectDir = args[i][10:]
			if projectDir == "" {
				return "", projectHashRefuseEmptyProject(), true
			}
		case args[i] == "--help" || args[i] == "-h":
			fmt.Print(`harmonik project-hash — print the PL-006a project hash for a project directory

USAGE
  harmonik project-hash [--project DIR]

FLAGS
  --project DIR  Project directory to hash (default: current working directory)

OUTPUT
  Prints exactly 12 lowercase hexadecimal characters followed by a newline.
  This is the first 12 characters of SHA-256(realpath(DIR)).

NOTES
  Side-effect-free: does not start, contact, or require a running daemon.
  Does not write any file. Does not require $TMUX.
  On error: exits non-zero with a diagnostic on stderr and no stdout output.

EXIT CODES
  0   The hash was printed.
  1   The directory could not be resolved: no such path, or the working
      directory could not be read.
  2   Usage error: an argument this command does not have, or --project with
      no value after it.

EXAMPLES
  harmonik project-hash
  harmonik project-hash --project /path/to/project
  HASH="$(harmonik project-hash --project "$P" 2>/dev/null || true)"

SPEC
  specs/process-lifecycle.md §4.2 PL-031
`)
			return "", 0, true
		default:
			if args[i] == "--project" {
				return "", projectHashRefuseEmptyProject(), true
			}
			fmt.Fprintf(os.Stderr, "harmonik project-hash: unrecognized argument %q\n", args[i])
			fmt.Fprintln(os.Stderr, "Run 'harmonik project-hash --help' for usage.")
			return "", 2, true
		}
	}
	return projectDir, 0, false
}

// projectHashRefuseEmptyProject reports a --project flag that carries no
// directory and returns the usage exit code.
//
// An empty value is the same defect as a misspelled flag, and it is easier to
// reach: a shell that expands "$P" to nothing hands this command `--project ""`,
// which passed the "is there a token after the flag" guard, set an empty
// directory, and then fell through to the WORKING directory. The caller got a
// real hash and exit 0 for a directory it never named. scripts/scratch-daemon.sh
// and scripts/run-st5-scenario.sh both build the flag from a shell variable, so
// both can reach this if that variable is ever empty.
func projectHashRefuseEmptyProject() int {
	fmt.Fprintln(os.Stderr, "harmonik project-hash: --project needs a directory after it")
	return 2
}
