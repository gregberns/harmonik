package main

// confirm_verdict.go — `harmonik confirm-verdict <run_id>` subcommand.
//
// # Purpose (RC-027)
//
// Implements the operator verdict-confirmation surface per RC-027. When a
// reconciliation workflow's YAML policy declares confirm_required: true, the
// daemon pauses verdict execution and waits for operator input. This command
// sends the "confirm" decision to the daemon so verdict execution proceeds.
//
// # Grammar
//
//	harmonik confirm-verdict <run_id> [--project DIR]
//
// Positional argument: run_id — the run whose pending verdict to confirm.
// The daemon MUST have a pending-confirmation entry for this run_id; if not,
// the command fails with exit code 16 (operator-control-invalid-state).
//
// # Exit codes
//
//	0  — success; the daemon will proceed with verdict execution
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
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// confirmVerdictUsage prints help for `harmonik confirm-verdict`.
func confirmVerdictUsage() {
	fmt.Print(`harmonik confirm-verdict — confirm a pending reconciliation verdict

USAGE
  harmonik confirm-verdict <run_id> [--project DIR]

ARGUMENTS
  <run_id>  Run ID of the reconciliation run whose verdict to confirm.
            The daemon must have a pending-confirmation entry for this run_id;
            see 'harmonik status' to list pending verdicts.

FLAGS
  --project DIR  Project directory (default: current working directory)

EXIT CODES
   0  Success — the daemon will proceed with verdict execution
   1  Argument or flag error
  16  No pending verdict for the given run_id (operator-control-invalid-state)
  17  Daemon not running

NOTES
  This command is only meaningful when the reconciliation workflow's YAML policy
  declares confirm_required: true (per RC-027). When confirm_required is false
  (the default), the daemon executes verdicts automatically without waiting for
  operator input.

  The Cat 6a S01 policy ships with confirm_required: true by default (per
  OQ-RC-012 resolution). Cat 2 and Cat 3 default to confirm_required: false.

EXAMPLES
  harmonik confirm-verdict run-abc123
  harmonik confirm-verdict run-abc123 --project /path/to/project

SPEC
  specs/reconciliation/spec.md §4.5 RC-027
  specs/operator-nfr.md §4.3 ON-014
`)
}

// runConfirmVerdictSubcommand implements `harmonik confirm-verdict <run_id> [--project DIR]`.
// subArgs is os.Args[2:] (everything after "confirm-verdict").
func runConfirmVerdictSubcommand(subArgs []string) int {
	var projectDirFlag string
	var runID string

	for i := 0; i < len(subArgs); i++ {
		switch {
		case subArgs[i] == "--help" || subArgs[i] == "-h":
			confirmVerdictUsage()
			return 0
		case subArgs[i] == "--project" && i+1 < len(subArgs):
			i++
			projectDirFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--project="):
			projectDirFlag = strings.TrimPrefix(subArgs[i], "--project=")
		case strings.HasPrefix(subArgs[i], "-"):
			fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: unknown flag %q\n", subArgs[i])
			return 1
		default:
			if runID != "" {
				fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: unexpected extra argument %q (run_id already set to %q)\n", subArgs[i], runID)
				fmt.Fprintln(os.Stderr, "usage: harmonik confirm-verdict <run_id> [--project DIR]")
				return 1
			}
			runID = subArgs[i]
		}
	}

	if runID == "" {
		fmt.Fprintln(os.Stderr, "harmonik confirm-verdict: missing required argument <run_id>")
		fmt.Fprintln(os.Stderr, "usage: harmonik confirm-verdict <run_id> [--project DIR]")
		return 1
	}

	if projectDirFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDirFlag = wd
	}

	if _, err := os.Stat(projectDirFlag); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: project directory %q does not exist: %v\n", projectDirFlag, err)
		return 1
	}

	return sendVerdictOverrideRequest(projectDirFlag, runID, "confirm", "")
}

// sendVerdictOverrideRequest sends a verdict-override socket request to the
// daemon and returns the appropriate exit code.
//
// op is one of "confirm_verdict" or "veto_verdict"; promoteTo is the optional
// --promote-to value (empty for confirm, or "escalate-to-human" for a promoted
// veto).
//
// Exit codes per the operator-nfr.md §8 taxonomy:
//
//	0  — success
//	1  — local error (arg parsing, stat, etc.)
//	16 — operator-control-invalid-state (no pending verdict for run_id)
//	17 — daemon not running
func sendVerdictOverrideRequest(projectDir, runID, op, promoteTo string) int {
	harmonikDir := projectDir + "/.harmonik"
	sockPath := harmonikDir + "/daemon.sock"

	type verdictOverrideReq struct {
		Op        string `json:"op"`
		RunID     string `json:"run_id"`
		PromoteTo string `json:"promote_to,omitempty"`
	}
	type verdictOverrideResp struct {
		Ok        bool   `json:"ok"`
		Error     string `json:"error,omitempty"`
		ErrorCode int    `json:"error_code,omitempty"`
	}

	payload, err := json.Marshal(verdictOverrideReq{
		Op:        op,
		RunID:     runID,
		PromoteTo: promoteTo,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: internal error marshalling request: %v\n", err)
		return 1
	}

	ctx := context.Background()
	conn, dialErr := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if dialErr != nil {
		if isVerdictSocketAbsent(dialErr) || isVerdictConnectionRefused(dialErr) {
			fmt.Fprintln(os.Stderr, "harmonik confirm-verdict: daemon is not running (socket absent or connection refused)")
			fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: start the daemon with 'harmonik --project %s' and retry\n", projectDir)
			return 17
		}
		fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: socket dial error: %v\n", dialErr)
		return 17
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck

	if _, writeErr := conn.Write(payload); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: socket write error: %v\n", writeErr)
		return 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck
	}

	var resp verdictOverrideResp
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: socket read error: %v\n", decErr)
		return 1
	}

	if !resp.Ok {
		// exit code 16 = operator-control-invalid-state (no pending verdict)
		if resp.ErrorCode == 16 {
			fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: no pending verdict for run %q (operator-control-invalid-state)\n", runID)
			fmt.Fprintln(os.Stderr, "harmonik confirm-verdict: use 'harmonik status' to list reconciliation runs with pending verdicts")
			return 16
		}
		fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: daemon rejected request (code %d): %s\n", resp.ErrorCode, resp.Error)
		return 1
	}

	fmt.Fprintf(os.Stderr, "harmonik confirm-verdict: verdict confirmed for run %q — daemon will proceed with execution\n", runID)
	return 0
}

// isVerdictSocketAbsent reports whether the dial error indicates the socket
// file does not exist.
func isVerdictSocketAbsent(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such file or directory")
}

// isVerdictConnectionRefused reports whether the dial error is ECONNREFUSED.
func isVerdictConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "connection refused")
}
