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
	projectDir := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case len(args[i]) > 10 && args[i][:10] == "--project=":
			projectDir = args[i][10:]
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

EXAMPLES
  harmonik project-hash
  harmonik project-hash --project /path/to/project
  HASH="$(harmonik project-hash --project "$P" 2>/dev/null || true)"

SPEC
  specs/process-lifecycle.md §4.2 PL-031
`)
			return 0
		}
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
