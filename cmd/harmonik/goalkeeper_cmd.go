package main

// goalkeeper_cmd.go — harmonik goal-keeper subcommand (flywheel V6, hk-owz1).
//
// The goal-keeper is an ephemeral, minimal-context process that:
//  1. Reads .harmonik/intent/goal-state.json to get the last_event_id cursor.
//  2. Reads harmonik comms log --from operator --since <cursor> --json to get
//     operator messages since the previous run.
//  3. Appends each message body verbatim to operator_directives (bounded by
//     goalstate.MaxDirectives; oldest entries pruned when over limit).
//  4. Updates last_event_id to the last seen event_id.
//  5. Writes the updated goal-state atomically and exits.
//
// This command is designed to be:
//   - Spawned by the daemon schedule primitive (harmonik schedule run-now
//     goal-keeper) on idle-triggered realign.
//   - Spawned by the Pi flywheel extension when the captain detects an empty
//     queue and the operator may have sent new directives.
//   - Run manually by the operator: harmonik goal-keeper [--project DIR].
//
// Exit codes:
//
//	0  — success (goal-state updated or no new messages)
//	1  — argument or I/O error
//
// Spec ref: docs/flywheel-self-reinforcing-design.md §6.

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/goalstate"
)

// runGoalkeeperSubcommand implements `harmonik goal-keeper`.
func runGoalkeeperSubcommand(args []string) int {
	fs := flag.NewFlagSet("goal-keeper", flag.ContinueOnError)
	var projectFlag string
	fs.StringVar(&projectFlag, "project", "", "project directory (default: current working directory)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			goalkeeperUsage()
			return 0
		}
		fmt.Fprintf(os.Stderr, "harmonik goal-keeper: %v\n", err)
		return 1
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "harmonik goal-keeper: unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return 1
	}

	// Resolve project directory.
	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik goal-keeper: cannot determine working directory: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	projectDir, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik goal-keeper: cannot resolve project path %q: %v\n", projectFlag, err)
		return 1
	}

	// 1. Load current goal-state (or create a default if absent).
	gs, err := goalstate.Read(projectDir)
	if err != nil {
		if !errors.Is(err, goalstate.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "harmonik goal-keeper: read goal-state: %v\n", err)
			return 1
		}
		gs = goalstate.Default()
	}

	// 2. Read operator comms since last_event_id cursor.
	messages, lastSeen, err := readOperatorComms(projectDir, gs.LastEventID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik goal-keeper: read comms: %v\n", err)
		return 1
	}

	if len(messages) == 0 {
		// No new operator messages — goal-state is current; exit cleanly.
		fmt.Fprintf(os.Stderr, "harmonik goal-keeper: no new operator messages since %q\n", gs.LastEventID)
		return 0
	}

	// 3. Append new directives verbatim and advance cursor.
	goalstate.Distill(gs, messages, lastSeen)

	// 4. Persist goal-state atomically.
	if err := goalstate.Write(projectDir, gs); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik goal-keeper: write goal-state: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "harmonik goal-keeper: added %d directive(s); last_event_id=%s\n", len(messages), lastSeen)
	return 0
}

// gkCommsEvent is a minimal struct to decode the NDJSON lines emitted by
// `harmonik comms log --json`. Each line is a core.Event envelope whose
// payload contains an AgentMessagePayload.
type gkCommsEvent struct {
	EventID string          `json:"event_id"`
	Payload json.RawMessage `json:"payload"`
}

type gkCommsPayload struct {
	From string `json:"from"`
	Body string `json:"body"`
}

// readOperatorComms shells out to `harmonik comms log --from operator
// --since <sinceID> --json --project <projectDir>` and returns the ordered
// list of message bodies along with the event_id of the last message seen.
// If sinceID is empty, all operator messages are returned.
func readOperatorComms(projectDir, sinceID string) (bodies []string, lastEventID string, err error) {
	harmonikBin, lookErr := exec.LookPath("harmonik")
	if lookErr != nil {
		harmonikBin = "harmonik"
	}

	cmdArgs := []string{
		"comms", "log",
		"--from", "operator",
		"--json",
		"--project", projectDir,
	}
	if sinceID != "" {
		cmdArgs = append(cmdArgs, "--since", sinceID)
	}

	//nolint:gosec // G204: arguments are controlled; harmonikBin is from PATH
	cmd := exec.Command(harmonikBin, cmdArgs...)
	cmd.Dir = projectDir
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		// `comms log` exits 0 even when no messages are found (it prints to
		// stderr). A non-zero exit is a genuine error.
		return nil, "", fmt.Errorf("harmonik comms log: %w", cmdErr)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	// A long operator directive can exceed the default 64KB token limit;
	// without a larger buffer the whole goal-keeper run hard-fails.
	setLargeScanBuffer(scanner)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev gkCommsEvent
		if decErr := json.Unmarshal([]byte(line), &ev); decErr != nil {
			continue // skip malformed lines
		}
		var p gkCommsPayload
		if decErr := json.Unmarshal(ev.Payload, &p); decErr != nil {
			continue
		}
		// Double-check from filter (comms log applies it server-side, but be safe).
		if p.From != "operator" {
			continue
		}
		if p.Body == "" || ev.EventID == "" {
			continue
		}
		bodies = append(bodies, p.Body)
		lastEventID = ev.EventID
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, "", fmt.Errorf("scan comms output: %w", scanErr)
	}
	return bodies, lastEventID, nil
}

func goalkeeperUsage() {
	fmt.Print(`harmonik goal-keeper — update .harmonik/intent/goal-state.json from operator comms

USAGE
  harmonik goal-keeper [--project DIR]

FLAGS
  --project DIR   Project directory (default: current working directory)

DESCRIPTION
  The goal-keeper is an ephemeral, minimal-context process that reads operator
  messages from the comms log (harmonik comms log --from operator) since the
  last run, appends them as verbatim operator_directives to goal-state.json,
  and exits. It is typically spawned by the daemon's schedule primitive on
  idle-triggered realign — NOT on a clock timer.

  On first run (no goal-state.json exists) all operator comms history is read.
  Subsequent runs are incremental: only messages since last_event_id are read.

  Operator directives are guidance, NOT law — the captain may override them
  when the get-shit-done protocol applies.

EXIT CODES
  0   Success (goal-state updated or already current)
  1   Argument or I/O error

EXAMPLES
  harmonik goal-keeper
  harmonik goal-keeper --project /path/to/project
`)
}
