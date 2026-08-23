package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strings"
	"syscall"
)

func confirmVerdictUsage() {
	fmt.Print(`harmonik confirm-verdict — confirm a pending reconciliation verdict

NOT CONNECTED
  This command cannot succeed under any input. Nothing in this build parks a run
  awaiting an operator decision, so there is never a pending verdict to confirm,
  and every invocation against a running daemon exits 16. Do not spend time
  looking for the run — the feature is not connected to anything yet. See NOTES
  for what is and is not built.

USAGE
  harmonik confirm-verdict <run_id> [--project DIR]

ARGUMENTS
  <run_id>  Run ID of the reconciliation run whose verdict to confirm.
            The daemon must have a pending-confirmation entry for this run_id.
            It never has one — see NOT CONNECTED above.
            No command lists pending verdicts. The daemon holds them in memory
            only. Reconciliation run IDs appear in the daemon event stream
            ('harmonik subscribe --types reconciliation_started'), but no event
            reports that a run waits for an operator verdict.

FLAGS
  --project DIR  Project directory (default: current working directory)

EXIT CODES
   0  Success — the daemon will proceed with verdict execution
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

  RC-027's design, for when it is connected: a reconciliation workflow whose
  YAML policy declares confirm_required: true pauses verdict execution and waits
  for operator input. When confirm_required is false (the default), the daemon
  executes verdicts automatically without waiting. The Cat 6a S01 policy ships
  with confirm_required: true by default (per OQ-RC-012 resolution). Cat 2 and
  Cat 3 default to confirm_required: false.

  Tracked by hk-verdict-override-unwired-aqjxo.

EXAMPLES
  harmonik confirm-verdict run-abc123
  harmonik confirm-verdict run-abc123 --project /path/to/project

SPEC
  specs/reconciliation/spec.md §4.5 RC-027
  specs/operator-nfr.md §4.3 ON-014
`)
}

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

	return sendVerdictOverrideRequest(projectDirFlag, runID, "confirm_verdict", "")
}

func sendVerdictOverrideRequest(projectDir, runID, op, promoteTo string) int {
	cmdName := strings.ReplaceAll(op, "_", "-")
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
		fmt.Fprintf(os.Stderr, "harmonik %s: internal error marshalling request: %v\n", cmdName, err)
		return 1
	}

	ctx := context.Background()
	conn, dialErr := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if dialErr != nil {
		if isVerdictSocketAbsent(dialErr) || isVerdictConnectionRefused(dialErr) {
			fmt.Fprintf(os.Stderr, "harmonik %s: daemon is not running (socket absent or connection refused)\n", cmdName)
			fmt.Fprintf(os.Stderr, "harmonik %s: start the daemon with 'harmonik start daemon --project %s' and retry\n", cmdName, projectDir)
			return 17
		}
		fmt.Fprintf(os.Stderr, "harmonik %s: socket dial error: %v\n", cmdName, dialErr)
		return 17
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck

	if _, writeErr := conn.Write(payload); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: socket write error: %v\n", cmdName, writeErr)
		return 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		if closeErr := uw.CloseWrite(); !isBenignCloseWrite(closeErr) {
			fmt.Fprintf(os.Stderr, "harmonik %s: socket close-write error: %v\n", cmdName, closeErr)
			return 1
		}
	}

	var resp verdictOverrideResp
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: socket read error: %v\n", cmdName, decErr)
		return 1
	}

	if !resp.Ok {
		if resp.ErrorCode == 16 {
			fmt.Fprintf(os.Stderr, "harmonik %s: no pending verdict for run %q (operator-control-invalid-state)\n", cmdName, runID)
			fmt.Fprintf(os.Stderr, "harmonik %s: NOT CONNECTED — no run can have a pending verdict in this build, so this command always fails\n", cmdName)
			fmt.Fprintf(os.Stderr, "harmonik %s: nothing parks a run awaiting an operator decision; the run_id you gave is not the problem\n", cmdName)
			fmt.Fprintf(os.Stderr, "harmonik %s: '%s --help' has the detail; tracked by hk-verdict-override-unwired-aqjxo\n", cmdName, cmdName)
			return 16
		}
		fmt.Fprintf(os.Stderr, "harmonik %s: daemon rejected request (code %d): %s\n", cmdName, resp.ErrorCode, resp.Error)
		return 1
	}

	if op == "confirm_verdict" {
		fmt.Fprintf(os.Stderr, "harmonik %s: verdict confirmed for run %q — daemon will proceed with execution\n", cmdName, runID)
	}
	return 0
}

func isVerdictSocketAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.EINVAL)
}

func isVerdictConnectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}
