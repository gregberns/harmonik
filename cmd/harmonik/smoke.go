package main

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
	"time"
)

const smokeDefaultTimeout = 20 * time.Minute

var smokeSignalNames = [5]string{
	"run_started",
	"run_completed",
	"commit on target branch",
	"reviewer_verdict",
	"bead_closed",
}

type smokeResult struct {
	observed [5]bool
	detail   [5]string // extra context per signal (run_id, branch, verdict, ...)
}

func runSmokeSubcommand(args []string) int {
	return runSmoke(args, os.Stdout, os.Stderr)
}

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
			if _, err := fmt.Fprint(stdout, smokeUsage); err != nil {
				return 1
			}
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
				if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: --timeout: %v\n", err); writeErr != nil {
					return 1
				}
				return 1
			}
			timeoutFlag = d
		case strings.HasPrefix(args[i], "--timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(args[i], "--timeout="))
			if err != nil {
				if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: --timeout: %v\n", err); writeErr != nil {
					return 1
				}
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
			if _, err := fmt.Fprintf(stderr, "harmonik smoke: unknown argument %q\n", args[i]); err != nil {
				return 1
			}
			return 1
		}
	}

	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		projectFlag = wd
	}
	absProject, err := filepath.Abs(projectFlag)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: cannot resolve project path %q: %v\n", projectFlag, err); writeErr != nil {
			return 1
		}
		return 1
	}
	projectDir := absProject
	harmonikDir := filepath.Join(projectDir, ".harmonik")

	targetBranch := branchFlag
	if targetBranch == "" {
		targetBranch = smokeReadTargetBranch(harmonikDir)
	}

	sockPath := filepath.Join(harmonikDir, "daemon.sock")

	if _, err := fmt.Fprintf(stdout, "harmonik smoke: project=%s target-branch=%s timeout=%s\n",
		projectDir, targetBranch, timeoutFlag); err != nil {
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutFlag)
	defer cancel()

	smokeBeadID := beadIDFlag
	ownBead := smokeBeadID == ""
	if ownBead {
		id, code := smokeCreateBead(ctx, projectDir, stdout, stderr)
		if code != 0 {
			return code
		}
		smokeBeadID = id
	}
	if _, err := fmt.Fprintf(stdout, "harmonik smoke: smoke bead %s\n", smokeBeadID); err != nil {
		return 1
	}

	if code := smokeSubmitBead(ctx, projectDir, smokeBeadID, queueFlag, stderr); code != 0 {
		if ownBead {
			if cleanupErr := smokeCleanupBead(ctx, projectDir, smokeBeadID, stderr); cleanupErr != nil {
				return 1
			}
		}
		return code
	}
	if _, err := fmt.Fprintf(stdout, "harmonik smoke: submitted %s to queue\n", smokeBeadID); err != nil {
		return 1
	}

	result, exitCode := smokeWatchSignals(ctx, sockPath, projectDir, targetBranch, smokeBeadID, stdout, stderr)

	if err := smokePrintResults(stdout, smokeBeadID, result); err != nil {
		return 1
	}

	if exitCode != 0 {
		if exitCode == 2 {
			if _, err := fmt.Fprintf(stderr, "harmonik smoke: TIMEOUT — not all signals observed within %s\n", timeoutFlag); err != nil {
				return 1
			}
		} else {
			if _, err := fmt.Fprintf(stderr, "harmonik smoke: FAILED\n"); err != nil {
				return 1
			}
		}
	} else {
		if _, err := fmt.Fprintln(stdout, "harmonik smoke: PASS — all 5 signals observed"); err != nil {
			return 1
		}
	}
	return exitCode
}

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

func smokeCreateBead(ctx context.Context, projectDir string, stdout, stderr io.Writer) (beadID string, exitCode int) {
	brPath, err := exec.LookPath("br")
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: 'br' not found on PATH\n"); writeErr != nil {
			return "", 1
		}
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
	cmd := exec.CommandContext(ctx, brPath,
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
		if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: br create failed: %v\n", runErr); writeErr != nil {
			return "", 1
		}
		return "", 1
	}
	id := strings.TrimSpace(out.String())
	if id == "" {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: br create returned empty ID\n"); writeErr != nil {
			return "", 1
		}
		return "", 1
	}
	if _, writeErr := fmt.Fprintf(stdout, "harmonik smoke: created smoke bead %s\n", id); writeErr != nil {
		return "", 1
	}
	return id, 0
}

func smokeSubmitBead(ctx context.Context, projectDir, beadID, queueName string, stderr io.Writer) int {
	exe, err := os.Executable()
	if err != nil {
		exe = "harmonik"
	}
	queueArgs := []string{"queue", "submit", "--project", projectDir, "--beads", beadID}
	if queueName != "" {
		queueArgs = append(queueArgs, "--queue", queueName)
	}
	//nolint:gosec // G204: exe from os.Executable; args are validated values
	cmd := exec.CommandContext(ctx, exe, queueArgs...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	if runErr := cmd.Run(); runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			if exitErr.ExitCode() == 17 {
				if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: daemon not running (exit 17 from queue submit)\n"); writeErr != nil {
					return 1
				}
				return 17
			}
		}
		if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: queue submit failed: %v\n", runErr); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func smokeCleanupBead(ctx context.Context, projectDir, beadID string, stderr io.Writer) error {
	brPath, err := exec.LookPath("br")
	if err != nil {
		return fmt.Errorf("find br for smoke bead cleanup: %w", err)
	}
	//nolint:gosec // G204: brPath from LookPath; beadID from br create output
	cmd := exec.CommandContext(ctx, brPath, "close", beadID, "--reason", "smoke-test-cleanup: run failed before daemon closed bead")
	cmd.Dir = projectDir
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("close smoke bead %s: %w", beadID, runErr)
	}
	return nil
}

func smokeWatchSignals(
	ctx context.Context,
	sockPath, projectDir, targetBranch, smokeBeadID string,
	stdout, stderr io.Writer,
) (smokeResult, int) {
	var result smokeResult

	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	defer cancelDial()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	if err != nil {
		if commsIsSocketAbsent(err) || commsIsConnRefused(err) {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: daemon not running (socket %s missing or refused)\n", sockPath); writeErr != nil {
				return result, 1
			}
			return result, 17
		}
		if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: dial daemon socket: %v\n", err); writeErr != nil {
			return result, 1
		}
		return result, 1
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: close daemon connection: %v\n", closeErr); writeErr != nil {
				return
			}
		}
	}()

	go func() {
		<-ctx.Done()
		if closeErr := conn.Close(); closeErr != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: close daemon connection on signal: %v\n", closeErr); writeErr != nil {
				return
			}
		}
	}()

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
		if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: marshal subscribe request: %v\n", marshalErr); writeErr != nil {
			return result, 1
		}
		return result, 1
	}
	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		if _, reportErr := fmt.Fprintf(stderr, "harmonik smoke: write subscribe request: %v\n", writeErr); reportErr != nil {
			return result, 1
		}
		return result, 1
	}

	var smokeRunID string
	runFailed := false

	scanner := bufio.NewScanner(conn)
	setLargeScanBuffer(scanner)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		if reason, refused := subscribeRefusalReason(line); refused {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: daemon refused the subscription: %s\n", reason); writeErr != nil {
				return result, 1
			}
			return result, 1
		}

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
				if _, writeErr := fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 1] run_started run_id=%s\n", p.RunID); writeErr != nil {
					return result, 1
				}
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
				if _, writeErr := fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 2] run_completed run_id=%s\n", p.RunID); writeErr != nil {
					return result, 1
				}

				branchOK, commitRef := smokeCheckCommitOnBranchContext(ctx, projectDir, targetBranch, smokeBeadID, stderr)
				result.observed[2] = branchOK
				if branchOK {
					result.detail[2] = fmt.Sprintf("branch=%s commit=%s", targetBranch, commitRef)
					if _, writeErr := fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 3] commit on target branch=%s commit=%s\n",
						targetBranch, commitRef); writeErr != nil {
						return result, 1
					}
				} else {
					result.detail[2] = fmt.Sprintf("FAIL: no commit referencing %s on branch %s", smokeBeadID, targetBranch)
					if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: [SIGNAL 3 FAIL] no commit referencing %s on branch %s\n",
						smokeBeadID, targetBranch); writeErr != nil {
						return result, 1
					}
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
			if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: run_failed received for smoke run — smoke bead dispatch failed\n"); writeErr != nil {
				return result, 1
			}

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
				if _, writeErr := fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 4] reviewer_verdict=%s\n", p.Verdict); writeErr != nil {
					return result, 1
				}
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
				if _, writeErr := fmt.Fprintf(stdout, "harmonik smoke: [SIGNAL 5] bead_closed bead_id=%s\n", p.BeadID); writeErr != nil {
					return result, 1
				}
			}
		}

		if result.observed[0] && result.observed[1] && result.observed[2] &&
			result.observed[3] && result.observed[4] {
			return result, 0
		}

		if runFailed {
			return result, 1
		}
	}

	if ctx.Err() != nil {
		return result, 2
	}
	if scanErr := scanner.Err(); scanErr != nil && !strings.Contains(scanErr.Error(), "use of closed") {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik smoke: event stream error: %v\n", scanErr); writeErr != nil {
			return result, 1
		}
		return result, 1
	}
	return result, 2
}

func smokeCheckCommitOnBranch(projectDir, branch, beadID string, stderr io.Writer) (found bool, commitRef string) {
	return smokeCheckCommitOnBranchContext(context.Background(), projectDir, branch, beadID, stderr)
}

func smokeCheckCommitOnBranchContext(ctx context.Context, projectDir, branch, beadID string, stderr io.Writer) (found bool, commitRef string) {
	cmd := exec.CommandContext(ctx, "git", "-C", projectDir,
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
	parts := strings.SplitN(commitLine, " ", 2)
	return true, parts[0]
}

func smokePrintResults(stdout io.Writer, beadID string, result smokeResult) error {
	if _, err := fmt.Fprintf(stdout, "\nharmonik smoke: result for bead %s\n", beadID); err != nil {
		return fmt.Errorf("print smoke result header: %w", err)
	}
	if _, err := fmt.Fprintf(stdout, "%-5s %-30s %-6s %s\n", "#", "Signal", "Result", "Detail"); err != nil {
		return fmt.Errorf("print smoke result columns: %w", err)
	}
	if _, err := fmt.Fprintf(stdout, "%-5s %-30s %-6s %s\n", "---", "------------------------------", "------", "------"); err != nil {
		return fmt.Errorf("print smoke result separator: %w", err)
	}
	for i, name := range smokeSignalNames {
		status := "FAIL"
		if result.observed[i] {
			status = "PASS"
		}
		detail := result.detail[i]
		if detail == "" && !result.observed[i] {
			detail = "(not observed)"
		}
		if _, err := fmt.Fprintf(stdout, "%-5d %-30s %-6s %s\n", i+1, name, status, detail); err != nil {
			return fmt.Errorf("print smoke result row %d: %w", i+1, err)
		}
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return fmt.Errorf("finish smoke result table: %w", err)
	}
	return nil
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
