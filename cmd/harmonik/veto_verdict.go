package main

import (
	"fmt"
	"os"
	"strings"
)

func vetoVerdictUsage() {
	fmt.Print(`harmonik veto-verdict — veto a pending reconciliation verdict

NOT CONNECTED
  This command cannot succeed under any input. Nothing in this build parks a run
  awaiting an operator decision, so there is never a pending verdict to veto, and
  every invocation against a running daemon exits 16. Do not spend time looking
  for the run — the feature is not connected to anything yet. See NOTES for what
  is and is not built.

USAGE
  harmonik veto-verdict <run_id> [--promote-to escalate-to-human] [--project DIR]

ARGUMENTS
  <run_id>  Run ID of the reconciliation run whose verdict to veto.
            The daemon must have a pending-confirmation entry for this run_id.
            It never has one — see NOT CONNECTED above.
            No command lists pending verdicts. The daemon holds them in memory
            only. Reconciliation run IDs appear in the daemon event stream
            ('harmonik subscribe --types reconciliation_started'), but no event
            reports that a run waits for an operator verdict.

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
  What is built: this command, its argument checks, the socket op, the daemon
  route, and the release rendezvous. All of it is tested.

  What is not built: the caller. No production code parks a run, calls the
  verdict-executor, or reads a policy's confirm_required field. Each of those
  three gaps alone is enough to make this command unreachable, so connecting one
  of them does not make it work.

  RC-027's design, for when it is connected. Without --promote-to, a veto
  discards the verdict without executing any action (equivalent to
  no-op-accept: the run remains in its current state, unmodified). With
  --promote-to escalate-to-human, the daemon substitutes escalate-to-human and
  executes it: the operator_escalation_required event is emitted, and the run
  remains in its current state awaiting manual resolution.

  Tracked by hk-verdict-override-unwired-aqjxo.

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
