package main

// write_review_verdict_cmd.go — `harmonik write-review-verdict` subcommand.
//
// # Purpose (hk-9w79a)
//
// The reviewer-phase Claude session (kicked off by pasteInjectReviewer) is
// instructed to "write .harmonik/review.json" by hand-typing raw JSON via its
// Write tool. When the free-text Notes field quotes a code snippet containing
// a backtick, the model sometimes emits an illegal "\`" backslash-escape
// (backtick needs no JSON escape at all), producing invalid JSON that fails
// ErrMalformed after the ~1hr verdict-read retry budget is spent — recurring
// fleet-wide per the gurney log linked from hk-9w79a.
//
// This command gives the reviewer a non-hand-rolled path: pass the verdict
// fields as flags/args and this command does the JSON encoding via
// encoding/json (internal/workspace.WriteReviewVerdictAtomic), which escapes
// only what JSON actually requires and can never mis-escape a backtick.
//
// # Grammar
//
//	harmonik write-review-verdict --verdict=APPROVE|REQUEST_CHANGES|BLOCK \
//	    --notes="..." [--flags=a,b,c] [--project DIR]
//
// notes and flags are read verbatim — no shell quoting hazards beyond normal
// flag parsing, since the value is JSON-encoded by this process, not typed as
// JSON text by the caller.
//
// # Exit codes
//
//	0  — success; .harmonik/review.json written atomically
//	1  — argument error or write failure
//
// Bead ref: hk-9w79a.

import (
	"fmt"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/workspace"
)

func writeReviewVerdictUsage() {
	fmt.Print(`harmonik write-review-verdict — write .harmonik/review.json without hand-typing JSON

USAGE
  harmonik write-review-verdict --verdict=<VERDICT> --notes=<TEXT> [--flags=<a,b,c>] [--project DIR]

FLAGS
  --verdict VERDICT  One of APPROVE, REQUEST_CHANGES, BLOCK. Required.
  --notes TEXT       Free-text rationale (1-3 sentences). Required, non-empty.
  --flags a,b,c      Comma-separated issue tags. Optional; defaults to none.
  --project DIR      Workspace root containing .harmonik/ (default: current working directory)

NOTES
  Writes ${project}/.harmonik/review.json atomically (temp file + rename +
  fsync), schema_version=1, using encoding/json — never hand-typed JSON text.
  This avoids the invalid backslash-backtick escape a model can introduce when
  hand-typing a Notes value that quotes a code snippet containing a backtick.

EXAMPLES
  harmonik write-review-verdict --verdict=APPROVE --notes="All checks pass."
  harmonik write-review-verdict --verdict=REQUEST_CHANGES --flags=missing-tests --notes="No unit tests for the new sentinel set."
`)
}

// runWriteReviewVerdictSubcommand implements `harmonik write-review-verdict`.
// subArgs is os.Args[2:] (everything after "write-review-verdict").
func runWriteReviewVerdictSubcommand(subArgs []string) int {
	var verdictFlag, notesFlag, flagsFlag, projectDirFlag string

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			writeReviewVerdictUsage()
			return 0
		case arg == "--verdict" && i+1 < len(subArgs):
			i++
			verdictFlag = subArgs[i]
		case strings.HasPrefix(arg, "--verdict="):
			verdictFlag = strings.TrimPrefix(arg, "--verdict=")
		case arg == "--notes" && i+1 < len(subArgs):
			i++
			notesFlag = subArgs[i]
		case strings.HasPrefix(arg, "--notes="):
			notesFlag = strings.TrimPrefix(arg, "--notes=")
		case arg == "--flags" && i+1 < len(subArgs):
			i++
			flagsFlag = subArgs[i]
		case strings.HasPrefix(arg, "--flags="):
			flagsFlag = strings.TrimPrefix(arg, "--flags=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectDirFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectDirFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik write-review-verdict: unknown flag %q\n", arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik write-review-verdict: unexpected extra argument %q\n", arg)
			return 1
		}
	}

	switch verdictFlag {
	case workspace.ReviewVerdictApprove, workspace.ReviewVerdictRequestChanges, workspace.ReviewVerdictBlock:
		// valid
	default:
		fmt.Fprintf(os.Stderr, "harmonik write-review-verdict: --verdict must be one of APPROVE, REQUEST_CHANGES, BLOCK (got %q)\n", verdictFlag)
		return 1
	}

	if notesFlag == "" {
		fmt.Fprintln(os.Stderr, "harmonik write-review-verdict: --notes is required and must be non-empty")
		return 1
	}

	var flags []string
	if flagsFlag != "" {
		for _, f := range strings.Split(flagsFlag, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				flags = append(flags, f)
			}
		}
	}

	if projectDirFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik write-review-verdict: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDirFlag = wd
	}

	verdict := &workspace.ReviewVerdict{
		SchemaVersion: workspace.ReviewVerdictSchemaVersion,
		Verdict:       verdictFlag,
		Flags:         flags,
		Notes:         notesFlag,
	}

	if err := workspace.WriteReviewVerdictAtomic(projectDirFlag, verdict); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik write-review-verdict: %v\n", err)
		return 1
	}

	fmt.Printf("wrote %s\n", workspace.ReviewVerdictPath(projectDirFlag))
	return 0
}
