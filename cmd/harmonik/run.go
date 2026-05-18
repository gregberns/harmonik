package main

// run.go — `harmonik run <bead-id>` subcommand implementation.
//
// Semantics (hk-icecw + hk-w3cp1 + hk-boiwe + hk-hiqrl):
//  1. Parse flags: --project, --beads, --max-concurrent, --context, --review-loop.
//  2. Resolve br binary via PATH.
//  3. Validate every bead exists and is in a claimable state (open/ready).
//  4. Guard against an existing active queue (QM-027).
//  5. Construct a wave queue (one group, N items) and persist it to .harmonik/queue.json.
//  6. Start the daemon with a context cancelled when the queue exits (drain or failure).
//  7. Return 0 on success, 1 on failure, 2 on unexpected state, 5 on pidfile lock.
//
// Flag reference:
//
//	<bead-id>              Positional single-bead shorthand (back-compat).
//	--beads id1,id2,...    Comma-separated multi-bead list (hk-w3cp1). Mutually
//	                       exclusive with positional <bead-id>.
//	--max-concurrent N     Maximum simultaneous items (default 1, hk-w3cp1).
//	--context <string>     Free-form text injected as "## Extra Context" in the
//	                       agent-task.md for every dispatched item (hk-boiwe).
//	--context @<file>      Same, but read context from a file (hk-boiwe).
//	--review-loop          Route all items through WorkflowModeReviewLoop (hk-hiqrl).
//
// Exit-code contract:
//
//	0  — all beads reached SUCCESS terminal
//	1  — at least one bead failed, or argument/validation/daemon error
//	2  — unexpected queue state after daemon exit (diagnostic)
//	5  — pidfile locked (another harmonik instance is running)
//
// Spec ref: specs/queue-model.md §2.3, §3.1 QM-001, §QM-027.
// Bead ref: hk-icecw, hk-8jh26, hk-w3cp1, hk-boiwe, hk-hiqrl.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
)

// runBeadSubcommand implements `harmonik run <bead-id> [flags]`.
// subArgs is os.Args[2:] (everything after "run").
func runBeadSubcommand(subArgs []string) int {
	// --- Parse flags ---

	projectDirFlag := ""
	beadsFlag := ""       // --beads id1,id2,... (hk-w3cp1)
	maxConcurrent := 1   // --max-concurrent N (hk-w3cp1); default 1 for back-compat
	contextFlag := ""    // --context <inline|@file> (hk-boiwe)
	reviewLoop := false  // --review-loop (hk-hiqrl)
	positional := []string{}

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		// --project
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectDirFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectDirFlag = strings.TrimPrefix(arg, "--project=")

		// --beads (hk-w3cp1)
		case arg == "--beads" && i+1 < len(subArgs):
			i++
			beadsFlag = subArgs[i]
		case strings.HasPrefix(arg, "--beads="):
			beadsFlag = strings.TrimPrefix(arg, "--beads=")

		// --max-concurrent (hk-w3cp1)
		case arg == "--max-concurrent" && i+1 < len(subArgs):
			i++
			n, convErr := strconv.Atoi(subArgs[i])
			if convErr != nil || n < 1 {
				fmt.Fprintf(os.Stderr, "harmonik run: --max-concurrent must be a positive integer, got %q\n", subArgs[i])
				return 1
			}
			maxConcurrent = n
		case strings.HasPrefix(arg, "--max-concurrent="):
			val := strings.TrimPrefix(arg, "--max-concurrent=")
			n, convErr := strconv.Atoi(val)
			if convErr != nil || n < 1 {
				fmt.Fprintf(os.Stderr, "harmonik run: --max-concurrent must be a positive integer, got %q\n", val)
				return 1
			}
			maxConcurrent = n

		// --context (hk-boiwe)
		case arg == "--context" && i+1 < len(subArgs):
			i++
			contextFlag = subArgs[i]
		case strings.HasPrefix(arg, "--context="):
			contextFlag = strings.TrimPrefix(arg, "--context=")

		// --review-loop (hk-hiqrl)
		case arg == "--review-loop":
			reviewLoop = true

		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik run: unknown flag %q\n", arg)
			return 1

		default:
			positional = append(positional, arg)
		}
	}

	// Resolve the bead ID list.
	// Accepts either a single positional <bead-id> (back-compat) or --beads id1,id2,...
	// Mixing both is an error.
	var beadIDs []core.BeadID
	switch {
	case beadsFlag != "" && len(positional) > 0:
		fmt.Fprintln(os.Stderr, "harmonik run: cannot mix positional <bead-id> with --beads; use one or the other")
		return 1
	case beadsFlag != "":
		for _, raw := range strings.Split(beadsFlag, ",") {
			id := strings.TrimSpace(raw)
			if id == "" {
				continue
			}
			beadIDs = append(beadIDs, core.BeadID(id))
		}
		if len(beadIDs) == 0 {
			fmt.Fprintln(os.Stderr, "harmonik run: --beads requires at least one bead ID")
			return 1
		}
	case len(positional) == 1:
		beadIDs = []core.BeadID{core.BeadID(positional[0])}
	case len(positional) == 0:
		fmt.Fprintln(os.Stderr, "harmonik run: missing <bead-id> argument")
		fmt.Fprintln(os.Stderr, "usage: harmonik run <bead-id> [--project DIR] [--context TEXT] [--review-loop]")
		fmt.Fprintln(os.Stderr, "       harmonik run --beads id1,id2,... [--max-concurrent N] [--project DIR] [--context TEXT] [--review-loop]")
		return 1
	default:
		fmt.Fprintf(os.Stderr, "harmonik run: too many positional arguments (got %d, expected 1); use --beads for multiple\n", len(positional))
		return 1
	}

	// Resolve --context: inline string or @file path.
	var extraContext string
	if contextFlag != "" {
		if strings.HasPrefix(contextFlag, "@") {
			filePath := contextFlag[1:]
			data, readErr := os.ReadFile(filePath) //nolint:gosec // G304: operator-controlled path from CLI flag
			if readErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik run: --context @file: cannot read %q: %v\n", filePath, readErr)
				return 1
			}
			extraContext = string(data)
		} else {
			extraContext = contextFlag
		}
	}

	// Resolve workflow mode for --review-loop.
	itemWorkflowMode := ""
	if reviewLoop {
		itemWorkflowMode = string(core.WorkflowModeReviewLoop)
	}

	// --- Resolve project directory ---

	if projectDirFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDirFlag = wd
	}
	projectDir, err := filepath.Abs(projectDirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot resolve project path %q: %v\n", projectDirFlag, err)
		return 1
	}
	if _, err := os.Stat(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: project directory %q does not exist or is not accessible: %v\n", projectDir, err)
		return 1
	}

	// --- Resolve br binary ---

	brPath, brErr := exec.LookPath("br")
	if brErr != nil {
		fmt.Fprintln(os.Stderr, "harmonik run: 'br' not found on PATH — bead ledger required")
		return 1
	}

	// --- Validate every bead exists and is claimable ---

	adapter, adapterErr := brcli.NewForProject(brPath, projectDir)
	if adapterErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot initialise brcli adapter: %v\n", adapterErr)
		return 1
	}

	validateCtx, validateCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer validateCancel()

	for _, beadID := range beadIDs {
		record, showErr := adapter.ShowBead(validateCtx, beadID)
		if showErr != nil {
			if errors.Is(showErr, brcli.ErrBeadNotFound) {
				fmt.Fprintf(os.Stderr, "harmonik run: bead %q not found\n", beadID)
				return 1
			}
			fmt.Fprintf(os.Stderr, "harmonik run: cannot look up bead %q: %v\n", beadID, showErr)
			return 1
		}
		// Only open (and ready-equivalent) beads are claimable.
		if record.Status != core.CoarseStatusOpen {
			fmt.Fprintf(os.Stderr, "harmonik run: bead %q is not in a claimable state (status=%q; want %q)\n",
				beadID, record.Status, core.CoarseStatusOpen)
			return 1
		}
	}

	// --- Construct a wave queue (one group, N items) and persist it ---

	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{
			BeadID:       id,
			Status:       queue.ItemStatusPending,
			Context:      extraContext,      // hk-boiwe
			WorkflowMode: itemWorkflowMode,  // hk-hiqrl
		}
	}

	queueUUID, uuidErr := uuid.NewV7()
	if uuidErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: generate queue ID: %v\n", uuidErr)
		return 1
	}
	now := time.Now().UTC()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueUUID.String(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      items,
				CreatedAt:  now,
			},
		},
	}

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), 0o755); mkErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot create .harmonik/: %v\n", mkErr)
		return 1
	}

	persistCtx := context.Background()

	// QM-027: refuse if an active (non-completed) queue already exists.
	// Silently overwriting an in-flight queue would corrupt its state.
	existingQueue, loadErr := queue.Load(persistCtx, projectDir)
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot check existing queue: %v\n", loadErr)
		return 1
	}
	if existingQueue != nil && existingQueue.Status != queue.QueueStatusCompleted {
		fmt.Fprintf(os.Stderr, "harmonik run: a queue is already active for this project\n")
		fmt.Fprintf(os.Stderr, "  queue_id=%s status=%s\n", existingQueue.QueueID, existingQueue.Status)
		fmt.Fprintln(os.Stderr, "  use 'harmonik queue status' to inspect, or remove .harmonik/queue.json to reset")
		return 1
	}

	if persistErr := queue.Persist(persistCtx, projectDir, q); persistErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot persist queue.json: %v\n", persistErr)
		return 1
	}

	// qs is created here so that run.go can inspect final queue status after
	// daemon.Start returns (Fix 2: exit code reflects bead outcome, hk-8jh26).
	qs := daemon.NewQueueStore()

	// --- Resolve daemon binary path and tmux session ---

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "events"), 0o755); mkErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot create .harmonik/events/: %v\n", mkErr)
		return 1
	}
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "beads-intents"), 0o755); mkErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot create .harmonik/beads-intents/: %v\n", mkErr)
		return 1
	}

	daemonBinaryPath, execErr := os.Executable()
	if execErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: os.Executable() failed: %v\n", execErr)
		return 1
	}

	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "harmonik run: $TMUX is not set — run hk inside a tmux session or via hk tmux-start")
		return 1
	}

	sessionNameBytes, tmuxErr := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output() //nolint:gosec // G204: arguments are hard-coded constants
	if tmuxErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot resolve tmux session name: %v\n", tmuxErr)
		return 1
	}
	sessionName := strings.TrimSpace(string(sessionNameBytes))
	if sessionName == "" {
		fmt.Fprintln(os.Stderr, "harmonik run: tmux returned an empty session name — cannot attach substrate")
		return 1
	}

	tmuxAdapter := tmux.OSAdapter{}
	probeCtx := context.Background()
	if probeErr := tmuxAdapter.ProbeTmux(probeCtx); probeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: tmux probe failed: %v\n", probeErr)
		return 1
	}

	jsonlLogPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")

	// --- Build a context that cancels on SIGINT/SIGTERM or after queue exits ---

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// cancelOnExit cancels ctx when the queue reaches any terminal state
	// (all-success OR paused-by-failure), ensuring the process exits promptly
	// on both outcome paths (hk-8jh26 Fix 1).
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	beadIDStrs := make([]string, len(beadIDs))
	for i, id := range beadIDs {
		beadIDStrs[i] = string(id)
	}
	fmt.Fprintf(os.Stderr, "harmonik run: starting run for [%s] (max-concurrent=%d) in %s\n",
		strings.Join(beadIDStrs, ", "), maxConcurrent, projectDir)

	cfg := daemon.Config{
		ProjectDir:         projectDir,
		BrPath:             brPath,
		JSONLLogPath:       jsonlLogPath,
		MaxConcurrent:      maxConcurrent, // hk-w3cp1: user-controlled concurrency
		Substrate:          daemon.NewTmuxSubstrate(tmuxAdapter, sessionName),
		DaemonBinaryPath:   daemonBinaryPath,
		BinaryCommitHash:   commitHash,
		CancelOnQueueDrain: cancelRun, // success path (hk-icecw)
		CancelOnQueueExit:  cancelRun, // failure path (hk-8jh26 Fix 1)
		QueueStore:         qs,        // retained for post-Start status inspection (hk-8jh26 Fix 2)
	}

	if startErr := daemon.Start(runCtx, cfg); startErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: %v\n", startErr)
		if errors.Is(startErr, lifecycle.ErrPidfileLocked) {
			return 5
		}
		return 1
	}

	// Fix 2: map final queue status to exit code (hk-8jh26).
	// After daemon.Start returns, qs reflects the terminal queue state:
	//   nil           → CompleteAndUnlink ran → all-success → exit 0
	//   paused-by-failure → bead failed → exit 1
	//   other non-nil → unexpected state → exit 2 with diagnostic
	finalQueue := qs.Queue()
	if finalQueue == nil {
		// Queue was cleared via CompleteAndUnlink → all-success.
		return 0
	}
	if finalQueue.Status == queue.QueueStatusPausedByFailure {
		fmt.Fprintf(os.Stderr, "harmonik run: one or more beads failed (queue paused-by-failure)\n")
		return 1
	}
	// Unexpected terminal state — surface for debugging.
	fmt.Fprintf(os.Stderr, "harmonik run: unexpected queue state after exit: %s (queue_id=%s)\n",
		finalQueue.Status, finalQueue.QueueID)
	return 2
}
