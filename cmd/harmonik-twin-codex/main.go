// Command harmonik-twin-codex is the codex test twin binary (codex-harness C6/T15, hk-of3h4).
//
// It simulates the OpenAI codex exec CLI surface for use in harmonik scenario
// tests and CI runs in place of a real codex session. The binary mimics codex's
// non-interactive output format — JSONL to stdout under --json — and optionally
// performs worktree git operations.
//
// # Interface compatibility
//
// The twin accepts a subset of the codex exec flag surface so it can be dropped
// in as the HandlerBinary when the codex adapter (C2) resolves the binary path:
//
//	harmonik-twin-codex exec [--json] [--sandbox <mode>] [-a <mode>] [-C <dir>] [prompt]
//	harmonik-twin-codex exec resume <thread_id> [--json] ...
//
// Flags --sandbox, -a, and any prompt text are accepted and silently ignored.
// -C (or --cd) sets the working directory for git operations.
// --json is accepted (twin always emits JSONL regardless).
//
// # Scenario mode
//
// --scenario <name> selects a canned scenario (see scenarios.go for the four
// variants).  --bead-id <id> provides the bead identifier for the Refs:
// trailer in the trailer-commit variant.
//
// # JSONL output format (codex --json surface)
//
//   - {"type":"thread.started","thread_id":"<id>"}  — always first
//   - {"type":"turn.completed","usage":{...}}       — terminal success event
//   - {"type":"turn.failed","error":{"message":"..."}} — terminal failure event
//
// Normative reference: codex-harness 04-research/codex-cli/findings.md §2.
//
// Cite: codex-harness C6-migration-test-spec.md §Approach;
// codex-harness C2-codex-adapter-spec.md §AC2.3–AC2.5.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	os.Exit(run())
}

func run() int {
	fs := flag.NewFlagSet("harmonik-twin-codex", flag.ContinueOnError)

	showVersion := fs.Bool("version", false, "print the build-time commit hash and exit (HC-043)")
	scenarioName := fs.String("scenario", "", "canned scenario name (one of: trailer-commit, edits-no-commit, no-edits, turn-failed)")
	beadID := fs.String("bead-id", "", "bead identifier for the Refs: commit trailer (trailer-commit scenario)")

	worktreePath := fs.String("C", "", "working directory for git operations (codex -C / --cd flag)")
	_ = fs.String("cd", "", "alias for -C (codex --cd flag)")
	_ = fs.Bool("json", false, "enable JSONL output (accepted; twin always emits JSONL)")
	_ = fs.String("sandbox", "", "codex sandbox mode (accepted; ignored by twin)")
	_ = fs.String("a", "", "codex approval mode (accepted; ignored by twin)")
	_ = fs.String("output-last-message", "", "codex -o flag (accepted; ignored by twin)")

	args := os.Args[1:]
	args = stripExecSubcommand(args)

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *showVersion {
		if err := writeVersion(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "write version: %v\n", err)
			return 1
		}
		return 0
	}

	wt := *worktreePath
	if wt == "" {
		if v := fs.Lookup("cd"); v != nil {
			wt = v.Value.String()
		}
	}

	if *scenarioName == "" {
		fmt.Fprintln(os.Stderr, "harmonik-twin-codex: --scenario is required")
		return 1
	}

	cfg := scenarioConfig{
		worktreePath: wt,
		beadID:       *beadID,
	}
	if err := runScenario(os.Stdout, *scenarioName, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "harmonik-twin-codex:", err)
		return 1
	}
	return 0
}

func stripExecSubcommand(args []string) []string {
	if len(args) == 0 || args[0] != "exec" {
		return args
	}
	args = args[1:] // drop "exec"
	if len(args) > 0 && args[0] == "resume" {
		if len(args) >= 2 {
			args = args[2:]
		} else {
			args = args[1:]
		}
	}
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = args[1:]
	}
	return args
}
