package main

// veto_verdict.go — `harmonik veto-verdict <run_id>` subcommand.
//
// # Purpose (RC-027)
//
// Implements the operator verdict-veto surface per RC-027. When a reconciliation
// workflow's YAML policy declares confirm_required: true, the daemon pauses
// verdict execution and waits for operator input. This command sends the "veto"
// decision to the daemon, causing it to discard the investigator's verdict.
//
// With --promote-to escalate-to-human, the daemon substitutes the discarded
// verdict with escalate-to-human and executes that instead (signalling that
// the operator escalated the case beyond the investigator's resolution).
//
// # Grammar
//
//	harmonik veto-verdict <run_id> [--promote-to escalate-to-human] [--project DIR]
//
// Positional argument: run_id — the run whose pending verdict to veto.
//
// # Exit codes
//
//	0  — success; the daemon will discard the pending verdict (and optionally
//	     promote to escalate-to-human)
//	1  — argument or flag error
//	16 — no pending verdict for the given run_id (operator-control-invalid-state)
//	17 — daemon not running (socket absent or ECONNREFUSED)
//
// Spec refs:
//   - specs/reconciliation/spec.md §4.5 RC-027
//   - specs/operator-nfr.md §4.3 ON-014
//
// Bead ref: hk-63oh.39.

import (
	"fmt"
	"os"
	"strings"
)

// vetoVerdictUsage prints help for `harmonik veto-verdict`.
func vetoVerdictUsage() {
	fmt.Print(`harmonik veto-verdict — veto a pending reconciliation verdict

USAGE
  harmonik veto-verdict <run_id> [--promote-to escalate-to-human] [--project DIR]

ARGUMENTS
  <run_id>  Run ID of the reconciliation run whose verdict to veto.
            The daemon must have a pending-confirmation entry for this run_id;
            see 'harmonik status' to list pending verdicts.

FLAGS
  --promote-to escalate-to-human
            After vetoing the investigator's verdict, substitute it with
            'escalate-to-human' and execute that verdict instead. This signals
            that the operator has reviewed the investigator's findings and
            determined that human intervention is required.
            Currently the only valid promotion target is 'escalate-to-human'.

  --project DIR  Project directory (default: current working directory)

EXIT CODES
   0  Success — the daemon discarded the pending verdict (or promoted it)
   1  Argument or flag error
  16  No pending verdict for the given run_id (operator-control-invalid-state)
  17  Daemon not running

NOTES
  Without --promote-to, a veto discards the verdict without executing any action
  (equivalent to no-op-accept: the run remains in its current state, unmodified).

  With --promote-to escalate-to-human, the daemon substitutes escalate-to-human
  and executes it: the operator_escalation_required event is emitted, and the
  run remains in its current state awaiting manual resolution.

EXAMPLES
  # Plain veto — discard the verdict, leave run in current state
  harmonik veto-verdict run-abc123

  # Veto with promotion — discard verdict, escalate to human instead
  harmonik veto-verdict run-abc123 --promote-to escalate-to-human

  # With explicit project directory
  harmonik veto-verdict run-abc123 --project /path/to/project

SPEC
  specs/reconciliation/spec.md §4.5 RC-027
  specs/operator-nfr.md §4.3 ON-014
`)
}

// runVetoVerdictSubcommand implements
// `harmonik veto-verdict <run_id> [--promote-to escalate-to-human] [--project DIR]`.
// subArgs is os.Args[2:] (everything after "veto-verdict").
func runVetoVerdictSubcommand(subArgs []string) int {
	var projectDirFlag string
	var promoteTo string
	var runID string

	for i := 0; i < len(subArgs); i++ {
		switch {
		case subArgs[i] == "--help" || subArgs[i] == "-h":
			vetoVerdictUsage()
			return 0
		case subArgs[i] == "--project" && i+1 < len(subArgs):
			i++
			projectDirFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--project="):
			projectDirFlag = strings.TrimPrefix(subArgs[i], "--project=")
		case subArgs[i] == "--promote-to" && i+1 < len(subArgs):
			i++
			promoteTo = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--promote-to="):
			promoteTo = strings.TrimPrefix(subArgs[i], "--promote-to=")
		case strings.HasPrefix(subArgs[i], "-"):
			fmt.Fprintf(os.Stderr, "harmonik veto-verdict: unknown flag %q\n", subArgs[i])
			return 1
		default:
			if runID != "" {
				fmt.Fprintf(os.Stderr, "harmonik veto-verdict: unexpected extra argument %q (run_id already set to %q)\n", subArgs[i], runID)
				fmt.Fprintln(os.Stderr, "usage: harmonik veto-verdict <run_id> [--promote-to escalate-to-human] [--project DIR]")
				return 1
			}
			runID = subArgs[i]
		}
	}

	if runID == "" {
		fmt.Fprintln(os.Stderr, "harmonik veto-verdict: missing required argument <run_id>")
		fmt.Fprintln(os.Stderr, "usage: harmonik veto-verdict <run_id> [--promote-to escalate-to-human] [--project DIR]")
		return 1
	}

	// Validate --promote-to: only "escalate-to-human" is accepted.
	if promoteTo != "" && promoteTo != "escalate-to-human" {
		fmt.Fprintf(os.Stderr, "harmonik veto-verdict: unknown --promote-to value %q; the only valid value is 'escalate-to-human'\n", promoteTo)
		return 1
	}

	if projectDirFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik veto-verdict: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDirFlag = wd
	}

	if _, err := os.Stat(projectDirFlag); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik veto-verdict: project directory %q does not exist: %v\n", projectDirFlag, err)
		return 1
	}

	return sendVetoVerdictRequest(projectDirFlag, runID, promoteTo)
}

// sendVetoVerdictRequest sends the veto decision to the daemon via the socket.
// It delegates to sendVerdictOverrideRequest in confirm_verdict.go, overriding
// the exit-code error messages for the veto context.
func sendVetoVerdictRequest(projectDir, runID, promoteTo string) int {
	code := sendVerdictOverrideRequest(projectDir, runID, "veto_verdict", promoteTo)
	if code == 0 {
		if promoteTo == "escalate-to-human" {
			fmt.Fprintf(os.Stderr, "harmonik veto-verdict: verdict vetoed for run %q — escalating to human\n", runID)
		} else {
			fmt.Fprintf(os.Stderr, "harmonik veto-verdict: verdict vetoed for run %q — run left in current state\n", runID)
		}
	}
	return code
}
