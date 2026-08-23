package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

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

func projectHashRefuseEmptyProject() int {
	fmt.Fprintln(os.Stderr, "harmonik project-hash: --project needs a directory after it")
	return 2
}
