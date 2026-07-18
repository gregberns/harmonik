package main

// smoke.go — `harmonik smoke` subcommand (hk-4rkrg).
//
// Runs a 5-signal self-checking end-to-end verification over a live daemon.
// Creates a minimal smoke bead, submits it to the daemon's queue, subscribes
// to events, and asserts the full dispatch→commit→review→closure arc:
//
//	Signal 1 — run_started (bead dispatched; isolating worktree allocated)
//	Signal 2 — run_completed (implementer committed; daemon merged)
//	Signal 3 — commit on target branch (correct-branch assertion)
//	Signal 4 — reviewer_verdict (review gate confirmed)
//	Signal 5 — bead_closed (terminal lifecycle completed)
//
// Signals 2 and 3 are verified together: git is checked after run_completed
// fires to confirm the commit landed on the configured target branch.
//
// # Smoke bead task
//
// The smoke bead instructs the implementer agent to append a single line to
// docs/smoke-log.md (creating the file if absent) and commit. This produces
// a real git commit on the target branch, satisfying Signal 3.
//
// # Exit codes
//
//	0  — all signals observed within the timeout; all assertions passed
//	1  — argument, setup, or assertion failure (details on stderr)
//	2  — timeout: one or more signals not observed before --timeout elapsed
//	17 — daemon not running (socket missing or ECONNREFUSED)
//
// Bead ref: hk-4rkrg.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const smokeDefaultTimeout = 20 * time.Minute

// smokeSignalName maps signal index → display name for the result table.
var smokeSignalNames = [5]string{
	"run_started",
	"run_completed",
	"commit on target branch",
	"reviewer_verdict",
	"bead_closed",
}

// smokeResult holds the outcome of each signal check.
type smokeResult struct {
	observed [5]bool
	detail   [5]string // extra context per signal (run_id, branch, verdict, ...)
}

// runSmokeSubcommand dispatches `harmonik smoke [flags]`.
//
// Exit codes:
//
//	0  — all signals observed
//	1  — argument or setup error
//	2  — timeout
//	17 — daemon not running
func runSmokeSubcommand(args []string) int {
	return runSmoke(args, os.Stdout, os.Stderr)
}

// runSmoke is the testable core of the smoke subcommand.
func runSmoke(args []string, stdout, stderr io.Writer) int {
	var (
		projectFlag string
		timeoutFlag = smokeDefaultTimeout
		branchFlag  string
		queueFlag   string
		beadIDFlag  string
	)

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, smokeUsage)
			return 0
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectFlag = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectFlag = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--timeout" && i+1 < len(args):
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				fmt.Fprintf(stderr, "harmonik smoke: --timeout: %v\n", err)
				return 1
			}
			timeoutFlag = d
		case strings.HasPrefix(args[i], "--timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(args[i], "--timeout="))
			if err != nil {
				fmt.Fprintf(stderr, "harmonik smoke: --timeout: %v\n", err)
				return 1
			}
			timeoutFlag = d
		case args[i] == "--branch" && i+1 < len(args):
			i++
			branchFlag = args[i]
		case strings.HasPrefix(args[i], "--branch="):
			branchFlag = strings.TrimPrefix(args[i], "--branch=")
		case args[i] == "--queue" && i+1 < len(args):
			i++
			queueFlag = args[i]
		case strings.HasPrefix(args[i], "--queue="):
			queueFlag = strings.TrimPrefix(args[i], "--queue=")
		case args[i] == "--bead-id" && i+1 < len(args):
			i++
			beadIDFlag = args[i]
		case strings.HasPrefix(args[i], "--bead-id="):
			beadIDFlag = strings.TrimPrefix(args[i], "--bead-id=")
		default:
			fmt.Fprintf(stderr, "harmonik smoke: unknown argument %q\n", args[i])
			return 1
		}
	}

	// Resolve project directory.
	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik smoke: cannot determine working directory: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	absProject, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik smoke: cannot resolve project path %q: %v\n", projectFlag, err)
		return 1
	}
	projectDir := absProject
	harmonikDir := filepath.Join(projectDir, ".harmonik")

	// Resolve target branch (flag > branching.yaml > "main").
	targetBranch := branchFlag
	if targetBranch == "" {
		targetBranch = smokeReadTargetBranch(harmonikDir)
	}

	sockPath := filepath.Join(harmonikDir, "daemon.sock")

	fmt.Fprintf(stdout, "harmonik smoke: project=%s target-branch=%s timeout=%s\n",
		projectDir, targetBranch, timeoutFlag)

	// Step 1: create or reuse the smoke bead.
	smokeBeadID := beadIDFlag
	ownBead := smokeBeadID == ""
	if ownBead {
		id, code := smokeCreateBead(projectDir, stdout, stderr)
		if code != 0 {
			return code
		}
		smokeBeadID = id
	}
	fmt.Fprintf(stdout, "harmonik smoke: smoke bead %s\n", smokeBeadID)

	// Step 2: submit the smoke bead to the queue.
	if code := smokeSubmitBead(projectDir, smokeBeadID, queueFlag, stderr); code != 0 {
		if ownBead {
			smokeCleanupBead(projectDir, smokeBeadID, stderr)
		}
		return code
	}
	fmt.Fprintf(stdout, "harmonik smoke: submitted %s to queue\n", smokeBeadID)

	// Step 3: subscribe and wait for all signals.
	ctx, cancel := context.WithTimeout(context.Background(), timeoutFlag)
	defer cancel()

	result, exitCode := smokeWatchSignals(ctx, sockPath, projectDir, targetBranch, smokeBeadID, stdout, stderr)

	// Print result table.
	smokePrintResults(stdout, smokeBeadID, result)

	if exitCode != 0 {
		if exitCode == 2 {
			fmt.Fprintf(stderr, "harmonik smoke: TIMEOUT — not all signals observed within %s\n", timeoutFlag)
		} else {
			fmt.Fprintf(stderr, "harmonik smoke: FAILED\n")
		}
	} else {
		fmt.Fprintln(stdout, "harmonik smoke: PASS — all 5 signals observed")
	}
	return exitCode
}

// smokeReadTargetBranch reads lands_on from .harmonik/branching.yaml.
// Falls back to "main" on any error.
func smokeReadTargetBranch(harmonikDir string) string {
	//nolint:gosec // G304: harmonikDir is operator-controlled
	data, err := os.ReadFile(filepath.Join(harmonikDir, "branching.yaml"))
	if err != nil {
		return "main"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "lands_on:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "lands_on:"))
			// Strip inline comments.
			if idx := strings.Index(val, "#"); idx >= 0 {
				val = strings.TrimSpace(val[:idx])
			}
			if val != "" {
				return val
			}
		}
	}
	return "main"
}

// smokeCreateBead creates a smoke bead via `br create` and returns its ID.
func smokeCreateBead(projectDir string, stdout, stderr io.Writer) (string, int) {
	brPath, err := exec.LookPath("br")
	if err != nil {
		fmt.Fprintf(stderr, "harmonik smoke: 'br' not found on PATH\n")
		return "", 1
	}

	ts := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	title := fmt.Sprintf("smoke: 5-signal self-check %s", ts)
	body := `Smoke verification task (harmonik smoke, hk-4rkrg).

Append exactly one line to docs/smoke-log.md (create the file if it does not exist):
  smoke <BEAD_ID> <ISO_TIMESTAMP>

Where <BEAD_ID> is the bead ID shown in the agent-task.md header and
<ISO_TIMESTAMP> is the current UTC time in RFC 3339 format.

Then commit with message: smoke(<BEAD_ID>): 5-signal verification
and include the line "Refs: <BEAD_ID>" on its own line in the commit body.

This task is complete when the file is updated and committed.`

	//nolint:gosec // G204: brPath from LookPath; args are literals or validated values
	cmd := exec.Command(brPath,
		"create",
		"--title", title,
		"--description", body,
		"--type", "task",
		"--priority", "4",
		"--labels", "codename:productization,smoke",
		"--silent",
	)
	cmd.Dir = projectDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = stderr
	if runErr := cmd.Run(); runErr != nil {
		fmt.Fprintf(stderr, "harmonik smoke: br create failed: %v\n", runErr)
		return "", 1
	}
	id := strings.TrimSpace(out.String())
	if id == "" {
		fmt.Fprintf(stderr, "harmonik smoke: br create returned empty ID\n")
		return "", 1
	}
	fmt.Fprintf(stdout, "harmonik smoke: created smoke bead %s\n", id)
	return id, 0
}

// smokeSubmitBead submits the smoke bead to the daemon queue.
func smokeSubmitBead(projectDir, beadID, queueName string, stderr io.Writer) int {
	exe, err := os.Executable()
	if err != nil {
		exe = "harmonik"
	}
	queueArgs := []string{"queue", "submit", "--project", projectDir, "--beads", beadID}
	if queueName != "" {
		queueArgs = append(queueArgs, "--queue", queueName)
	}
	//nolint:gosec // G204: exe from os.Executable; args are validated values
	cmd := exec.Command(exe, queueArgs...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	if runErr := cmd.Run(); runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 17 {
				fmt.Fprintf(stderr, "harmonik smoke: daemon not running (exit 17 from queue submit)\n")
				return 17
			}
		}
		fmt.Fprintf(stderr, "harmonik smoke: queue submit failed: %v\n", runErr)
		return 1
	}
	return 0
}

// smokeCleanupBead closes the smoke bead if the smoke run failed before
// the daemon could close it naturally.
func smokeCleanupBead(projectDir, beadID string, stderr io.Writer) {
	brPath, err := exec.LookPath("br")
	if err != nil {
		return
	}
	//nolint:gosec // G204: brPath from LookPath; beadID from br create output
	cmd := exec.Command(brPath, "close", beadID, "--reason", "smoke-test-cleanup: run failed before daemon closed bead")
	cmd.Dir = projectDir
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	_ = cmd.Run()
}

// smokeWatchSignals subscribes to daemon events and collects the 5 signals.
// Returns a populated smokeResult and an exit code (0=pass, 1=fail, 2=timeout).
func smokeWatchSignals(
	ctx context.Context,
	sockPath, projectDir, targetBranch, smokeBeadID string,
	stdout, stderr io.Writer,
) (smokeResult, int) {
	var result smokeResult

	// Dial the daemon socket.
	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	defer cancelDial()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	if err != nil {
		var sysErr *os.PathError
		if errors.As(err, &sysErr) && errors.Is(sysErr.Err, syscall.ENOENT) {
			fmt.Fprintf(stderr, "harmonik smoke: daemon not running (socket %s missing)\n", sockPath)
			return result, 17
		}
		if errors.Is(err, syscall.ECONNREFUSED) {
			fmt.Fprintf(stderr, "harmonik smoke: daemon not running (ECONNREFUSED on %s)\n", sockPath)
			return result, 17
		}
		fmt.Fprintf(stderr, "harmonik smoke: dial daemon socket: %v\n", err)
		return result, 1
	}
	defer func() { _ = conn.Close() }()

	// Close the connection when the context expires so the reader goroutine exits.
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	// Send subscribe request for the relevant event types.
	reqBody := map[string]any{
		"op":                "subscribe",
		"heartbeat_seconds": 60,
		"types": []string{
			"run_started",
			"run_completed",
			"run_failed",
			"reviewer_verdict",
			"bead_closed",
		},
	}
	reqBytes, marshalErr := json.Marshal(reqBody)
	if marshalErr != nil {
		fmt.Fprintf(stderr, "harmonik smoke: marshal subscribe request: %v\n", marshalErr)
		return result, 1
	}
	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(stderr, "harmonik smoke: write subscribe request: %v\n", writeErr)
		return result, 1
	}

	// smokeRunID is populated after Signal 1 fires.
	var smokeRunID string
	runFailed := false

	// Read the NDJSON stream line by line. Large-but-valid event lines (e.g.
	// a big reviewer_verdict notes field) must not abort the scan.
	scanner := bufio.NewScanner(conn)
	setLargeScanBuffer(scanner)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		// Parse the event envelope: {"event_id":"...","type":"...","payload":{...},...}
		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if jsonErr := json.Unmarshal(line, &env); jsonErr != nil {
			continue // skip malformed lines (heartbeats arrive as plain JSON too)
		}

		switch env.Type {
		case "run_started":
			var p struct {
				RunID  string  `json:"run_id"`
				BeadID *string `json:"bead_id"`
			}
			if jsonErr := json.Unmarshal(env.Payload, &p); jsonErr != nil {
				continue
			}
			if p.BeadID == nil || *p.BeadID != smokeBeadID {
				continue
			}
			if !result.observed[0] {
				result.observed[0] = true
				result.detail[0] = fmt.Sprintf("run_id=%s", p.RunID)
				smokeRunID = p.RunID
				fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 1] run_started run_id=%s\n", p.RunID)
			}

		case "run_completed":
			if smokeRunID == "" {
				continue
			}
			var p struct {
				RunID string `json:"run_id"`
			}
			if jsonErr := json.Unmarshal(env.Payload, &p); jsonErr != nil {
				continue
			}
			if p.RunID != smokeRunID {
				continue
			}
			if !result.observed[1] {
				result.observed[1] = true
				result.detail[1] = fmt.Sprintf("run_id=%s", p.RunID)
				fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 2] run_completed run_id=%s\n", p.RunID)

				// Signal 3: verify the commit landed on the target branch.
				branchOK, commitRef := smokeCheckCommitOnBranch(projectDir, targetBranch, smokeBeadID, stderr)
				result.observed[2] = branchOK
				if branchOK {
					result.detail[2] = fmt.Sprintf("branch=%s commit=%s", targetBranch, commitRef)
					fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 3] commit on target branch=%s commit=%s\n",
						targetBranch, commitRef)
				} else {
					result.detail[2] = fmt.Sprintf("FAIL: no commit referencing %s on branch %s", smokeBeadID, targetBranch)
					fmt.Fprintf(stderr, "harmonik smoke: [SIGNAL 3 FAIL] no commit referencing %s on branch %s\n",
						smokeBeadID, targetBranch)
				}
			}

		case "run_failed":
			if smokeRunID == "" {
				continue
			}
			var p struct {
				RunID string `json:"run_id"`
			}
			if jsonErr := json.Unmarshal(env.Payload, &p); jsonErr != nil {
				continue
			}
			if p.RunID != smokeRunID {
				continue
			}
			runFailed = true
			fmt.Fprintf(stderr, "harmonik smoke: run_failed received for smoke run — smoke bead dispatch failed\n")

		case "reviewer_verdict":
			if smokeRunID == "" {
				continue
			}
			var p struct {
				RunID   string `json:"run_id"`
				Verdict string `json:"verdict"`
				Notes   string `json:"notes"`
			}
			if jsonErr := json.Unmarshal(env.Payload, &p); jsonErr != nil {
				continue
			}
			if p.RunID != smokeRunID {
				continue
			}
			if !result.observed[3] {
				result.observed[3] = true
				result.detail[3] = fmt.Sprintf("verdict=%s", p.Verdict)
				fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 4] reviewer_verdict=%s\n", p.Verdict)
			}

		case "bead_closed":
			var p struct {
				RunID  string `json:"run_id"`
				BeadID string `json:"bead_id"`
			}
			if jsonErr := json.Unmarshal(env.Payload, &p); jsonErr != nil {
				continue
			}
			if p.BeadID != smokeBeadID {
				continue
			}
			if !result.observed[4] {
				result.observed[4] = true
				result.detail[4] = fmt.Sprintf("bead_id=%s", p.BeadID)
				fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 5] bead_closed bead_id=%s\n", p.BeadID)
			}
		}

		// All signals collected — done.
		if result.observed[0] && result.observed[1] && result.observed[2] &&
			result.observed[3] && result.observed[4] {
			return result, 0
		}

		// Run failed — no point waiting for more signals.
		if runFailed {
			return result, 1
		}
	}

	// Scanner exited — either context timeout or connection closed.
	if ctx.Err() != nil {
		return result, 2
	}
	if scanErr := scanner.Err(); scanErr != nil && !strings.Contains(scanErr.Error(), "use of closed") {
		fmt.Fprintf(stderr, "harmonik smoke: event stream error: %v\n", scanErr)
		return result, 1
	}
	// Connection closed cleanly without all signals: treat as timeout.
	return result, 2
}

// smokeCheckCommitOnBranch checks whether a commit referencing <beadID> in its
// message exists on <branch> in the project git repo. The smoke task commits
// with subject "smoke(<beadID>): ..." and a "Refs: <beadID>" trailer; matching
// the bead ID as a fixed string covers both forms, so a correct commit passes
// even if the agent omits the trailer.
// Returns (true, short-sha) on success; (false, "") on failure.
func smokeCheckCommitOnBranch(projectDir, branch, beadID string, stderr io.Writer) (bool, string) {
	//nolint:gosec // G204: git args are validated values; projectDir is operator-controlled
	cmd := exec.Command("git", "-C", projectDir,
		"log", "--oneline", "--max-count=1",
		"--fixed-strings", "--grep", beadID,
		branch,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return false, ""
	}
	commitLine := strings.TrimSpace(out.String())
	if commitLine == "" {
		return false, ""
	}
	// commitLine is "<sha> <message>"; extract the sha.
	parts := strings.SplitN(commitLine, " ", 2)
	return true, parts[0]
}

// smokePrintResults prints the signal result table to stdout.
func smokePrintResults(stdout io.Writer, beadID string, result smokeResult) {
	fmt.Fprintf(stdout, "\nharmonik smoke: result for bead %s\n", beadID)
	fmt.Fprintf(stdout, "%-5s %-30s %-6s %s\n", "#", "Signal", "Result", "Detail")
	fmt.Fprintf(stdout, "%-5s %-30s %-6s %s\n", "---", "------------------------------", "------", "------")
	for i, name := range smokeSignalNames {
		status := "FAIL"
		if result.observed[i] {
			status = "PASS"
		}
		detail := result.detail[i]
		if detail == "" && !result.observed[i] {
			detail = "(not observed)"
		}
		fmt.Fprintf(stdout, "%-5d %-30s %-6s %s\n", i+1, name, status, detail)
	}
	fmt.Fprintln(stdout)
}

const smokeUsage = `harmonik smoke — 5-signal end-to-end verification of a live daemon

USAGE
  harmonik smoke [flags]

FLAGS
  --project DIR       Project directory (default: current working directory)
  --branch BRANCH     Target branch to verify commit on (default: from branching.yaml or "main")
  --queue NAME        Queue name to submit the smoke bead to (default: main)
  --timeout DUR       Maximum time to wait for all signals (default: 20m)
  --bead-id ID        Reuse an existing bead instead of creating a new one

SIGNALS VERIFIED
  1. run_started       — smoke bead was dispatched and a run was allocated
  2. run_completed     — implementer finished and daemon merged the commit
  3. commit on branch  — a commit referencing <bead-id> exists on the target branch
  4. reviewer_verdict  — the review gate confirmed the work
  5. bead_closed       — the bead reached the terminal lifecycle state

EXIT CODES
   0  All 5 signals observed within the timeout; all assertions passed
   1  Argument, setup, or assertion failure
   2  Timeout: one or more signals not observed before --timeout elapsed
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik smoke
  harmonik smoke --project /path/to/project
  harmonik smoke --timeout 30m --branch integration
  harmonik smoke --bead-id hk-abc123
`
