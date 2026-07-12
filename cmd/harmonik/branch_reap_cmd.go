package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// runBranchReapSubcommand implements `harmonik gc branches`: enumerate run/*
// and worktree-agent-* branches, delete those that are merged into the target
// branch or are aged orphans with no active worktree.
//
// This is the on-demand counterpart to the daemon's per-run branch cleanup;
// it addresses the unbounded bloat that accumulates when branches are not
// pruned after merge (hk-fpjxi).
//
// Exit codes:
//
//	0  — reap pass completed (zero or more branches reaped)
//	1  — argument or operational error
func runBranchReapSubcommand(args []string, stdout, stderr io.Writer) int {
	var (
		projectDir   string
		targetBranch string
		maxAgeStr    string
		dryRun       bool
		asJSON       bool
	)

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, branchReapUsage)
			return 0
		case args[i] == "--dry-run" || args[i] == "-n":
			dryRun = true
		case args[i] == "--json":
			asJSON = true
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--target" && i+1 < len(args):
			i++
			targetBranch = args[i]
		case strings.HasPrefix(args[i], "--target="):
			targetBranch = strings.TrimPrefix(args[i], "--target=")
		case args[i] == "--max-age" && i+1 < len(args):
			i++
			maxAgeStr = args[i]
		case strings.HasPrefix(args[i], "--max-age="):
			maxAgeStr = strings.TrimPrefix(args[i], "--max-age=")
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik gc branches: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	maxAge := 30 * 24 * time.Hour
	if maxAgeStr != "" {
		parsed, err := time.ParseDuration(maxAgeStr)
		if err != nil {
			fmt.Fprintf(stderr, "harmonik gc branches: invalid --max-age %q: %v\n", maxAgeStr, err)
			return 1
		}
		maxAge = parsed
	}

	opts := lifecycle.BranchReapOptions{
		RepoDir:      projectDir,
		TargetBranch: targetBranch, // empty → lifecycle defaults to "main"
		OrphanMaxAge: maxAge,
		DryRun:       dryRun,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := lifecycle.ReapBranches(ctx, opts)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik gc branches: %v\n", err)
		return 1
	}

	dryRunTag := ""
	if dryRun {
		dryRunTag = " [dry-run]"
	}

	// Emit one branch_reaped event per deleted branch (newline-delimited JSON).
	for _, ev := range result.Events {
		b, mErr := json.Marshal(ev)
		if mErr != nil {
			continue
		}
		fmt.Fprintln(stdout, string(b))
	}

	if asJSON {
		type jsonSummary struct {
			Scanned int      `json:"scanned"`
			Reaped  []string `json:"reaped"`
			Skipped int      `json:"skipped"`
			DryRun  bool     `json:"dry_run"`
		}
		summary := jsonSummary{
			Scanned: result.Scanned,
			Reaped:  result.Reaped,
			Skipped: result.Skipped,
			DryRun:  dryRun,
		}
		if summary.Reaped == nil {
			summary.Reaped = []string{}
		}
		b, _ := json.Marshal(summary)
		fmt.Fprintln(stdout, string(b))
		return 0
	}

	fmt.Fprintf(stdout,
		"harmonik gc branches%s: scanned %d branch(es), reaped %d, skipped %d\n",
		dryRunTag, result.Scanned, len(result.Reaped), result.Skipped)
	return 0
}

// runGCSubcommand dispatches `harmonik gc <verb>`.
func runGCSubcommand(args []string) int {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	if verb == "--help" || verb == "-h" || verb == "" {
		fmt.Print(gcTopUsage)
		return 0
	}
	subArgs := []string{}
	if len(args) > 1 {
		subArgs = args[1:]
	}
	switch verb {
	case "branches":
		return runBranchReapSubcommand(subArgs, os.Stdout, os.Stderr)
	default:
		fmt.Fprintf(os.Stderr, "harmonik gc: unrecognised verb %q; verbs are: branches\n", verb)
		return 2
	}
}

const branchReapUsage = `harmonik gc branches — reap merged and orphaned run/* + worktree-agent-* branches

USAGE
  harmonik gc branches [--project DIR] [--target BRANCH] [--max-age DURATION]
                       [--dry-run] [--json]

DESCRIPTION
  Enumerates run/* and worktree-agent-* branches and deletes those that are:

    • merged     — tip commit is fully reachable from --target (default: main).
    • orphaned   — older than --max-age with no active registered worktree.

  worktree-agent-* branches are legacy (retired naming convention); they are
  always eligible once --max-age is exceeded, regardless of merge status.

  Branches currently checked out in a registered git worktree are NEVER deleted.
  The command emits one JSON branch_reaped event per deleted branch on stdout
  (even without --json), making output pipeable to the daemon event stream.

FLAGS
  --project DIR      Project directory / git repo root (default: current directory)
  --target BRANCH    Merge-check target branch (default: main)
  --max-age DURATION Minimum age before unmerged branches are considered orphaned
                     (default: 720h = 30 days). Go duration syntax: 24h, 168h, 720h.
  --dry-run, -n      Identify candidates but do not delete them
  --json             Emit a machine-readable summary line after per-branch events

EXIT CODES
   0  Reap pass completed (zero or more branches reaped)
   1  Argument or operational error
   2  Unrecognised verb

EXAMPLES
  harmonik gc branches
  harmonik gc branches --dry-run
  harmonik gc branches --target main --max-age 168h --json
`

const gcTopUsage = `harmonik gc — garbage-collect stale harmonik artifacts

USAGE
  harmonik gc <verb> [flags]

VERBS
  branches   Reap merged and orphaned run/* and worktree-agent-* branches

EXIT CODES
   0  Success
   1  Argument or operational error
   2  Unrecognised verb

EXAMPLES
  harmonik gc branches --dry-run
  harmonik gc branches --json
`
