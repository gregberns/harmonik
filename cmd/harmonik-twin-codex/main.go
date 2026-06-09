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

// run is the testable entry-point; it returns an exit code.
//
// Exit codes:
//
//	0 — scenario completed successfully.
//	1 — precondition failure (unknown scenario, missing required flag, etc.).
func run() int {
	fs := flag.NewFlagSet("harmonik-twin-codex", flag.ContinueOnError)

	showVersion := fs.Bool("version", false, "print the build-time commit hash and exit (HC-043)")
	scenarioName := fs.String("scenario", "", "canned scenario name (one of: trailer-commit, edits-no-commit, no-edits, turn-failed)")
	beadID := fs.String("bead-id", "", "bead identifier for the Refs: commit trailer (trailer-commit scenario)")

	// Codex exec interface flags — accepted for CLI compatibility, behaviour notes below.
	worktreePath := fs.String("C", "", "working directory for git operations (codex -C / --cd flag)")
	_ = fs.String("cd", "", "alias for -C (codex --cd flag)")
	_ = fs.Bool("json", false, "enable JSONL output (accepted; twin always emits JSONL)")
	_ = fs.String("sandbox", "", "codex sandbox mode (accepted; ignored by twin)")
	_ = fs.String("a", "", "codex approval mode (accepted; ignored by twin)")
	_ = fs.String("output-last-message", "", "codex -o flag (accepted; ignored by twin)")

	// Parse: consume the "exec" subcommand and optional "resume <id>" prefix
	// before handing the rest to flag.Parse.  This lets the twin be invoked
	// exactly as the codex adapter would invoke codex:
	//   harmonik-twin-codex exec --json -C <dir> ...
	//   harmonik-twin-codex exec resume <thread_id> --json -C <dir> ...
	args := os.Args[1:]
	args = stripExecSubcommand(args)

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *showVersion {
		writeVersion(os.Stdout)
		return 0
	}

	// Resolve -C vs --cd: prefer -C if set; fall back to --cd.
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

// stripExecSubcommand removes the leading "exec" subcommand and optional
// "resume <thread_id>" prefix from args so the remaining args are pure flags.
//
// Handles three forms:
//
//	exec --json ...              → --json ...
//	exec resume <id> --json ...  → --json ...
//	--json ...                   → --json ...   (no exec prefix — pass-through)
func stripExecSubcommand(args []string) []string {
	if len(args) == 0 || args[0] != "exec" {
		return args
	}
	args = args[1:] // drop "exec"
	if len(args) > 0 && args[0] == "resume" {
		// drop "resume" and the thread_id
		if len(args) >= 2 {
			args = args[2:]
		} else {
			args = args[1:]
		}
	}
	// Drop any remaining positional prompt text (non-flag args) that appear
	// after the flags.  flag.Parse stops at the first non-flag-looking arg
	// (i.e. an arg not starting with "-"), so any positional prompt comes
	// after the parsed flags and is handled automatically via fs.Args().
	// However, if the prompt appears BEFORE flags we need to skip it here.
	// Heuristic: skip leading args that don't start with '-'.
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = args[1:]
	}
	return args
}
