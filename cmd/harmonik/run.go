package main

// run.go — `harmonik run <bead-id>` subcommand implementation.
//
// Semantics (hk-icecw):
//  1. Parse --project flag and positional bead-id argument.
//  2. Resolve br binary via PATH.
//  3. Validate the bead exists and is in a claimable state (open/ready).
//  4. Construct a single-item wave queue and persist it to .harmonik/queue.json.
//  5. Start the daemon (same composition root as the normal run path) with a
//     context that is cancelled when the queue drains (cancelOnQueueDrain).
//  6. Return 0 on success (bead closed), non-zero on error.
//
// Exit-code contract:
//
//	0  — bead reached SUCCESS terminal (daemon exited cleanly after queue drain)
//	1  — argument/validation/daemon error
//	5  — pidfile locked (another harmonik instance is running)
//
// Spec ref: specs/queue-model.md §2.3, §3.1 QM-001.
// Bead ref: hk-icecw.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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

// runBeadSubcommand implements `harmonik run <bead-id> [--project DIR]`.
// subArgs is os.Args[2:] (everything after "run").
func runBeadSubcommand(subArgs []string) int {
	// --- Parse flags ---

	projectDirFlag := ""
	positional := []string{}
	for i := 0; i < len(subArgs); i++ {
		switch {
		case subArgs[i] == "--project" && i+1 < len(subArgs):
			i++
			projectDirFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--project="):
			projectDirFlag = strings.TrimPrefix(subArgs[i], "--project=")
		case strings.HasPrefix(subArgs[i], "-"):
			fmt.Fprintf(os.Stderr, "harmonik run: unknown flag %q\n", subArgs[i])
			return 1
		default:
			positional = append(positional, subArgs[i])
		}
	}

	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "harmonik run: missing <bead-id> argument")
		fmt.Fprintln(os.Stderr, "usage: harmonik run <bead-id> [--project DIR]")
		return 1
	}
	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "harmonik run: too many arguments (got %d positional args, expected 1)\n", len(positional))
		return 1
	}
	beadID := core.BeadID(positional[0])

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

	// --- Validate bead exists and is claimable ---

	adapter, adapterErr := brcli.NewForProject(brPath, projectDir)
	if adapterErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot initialise brcli adapter: %v\n", adapterErr)
		return 1
	}

	validateCtx, validateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer validateCancel()

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

	// --- Construct a single-item queue and persist it ---

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
				Items: []queue.Item{
					{
						BeadID: beadID,
						Status: queue.ItemStatusPending,
					},
				},
				CreatedAt: now,
			},
		},
	}

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), 0o755); mkErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot create .harmonik/: %v\n", mkErr)
		return 1
	}

	persistCtx := context.Background()
	if persistErr := queue.Persist(persistCtx, projectDir, q); persistErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot persist queue.json: %v\n", persistErr)
		return 1
	}

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

	// --- Build a context that cancels on SIGINT/SIGTERM or after queue drains ---

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// cancelOnDrain cancels ctx when the queue drains (exit-on-empty, hk-icecw).
	// We wrap the signal context with a new cancel so that the drain path can
	// trigger context expiry independently from the signal path.
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	fmt.Fprintf(os.Stderr, "harmonik run: starting single-bead run for %s in %s\n", beadID, projectDir)

	cfg := daemon.Config{
		ProjectDir:         projectDir,
		BrPath:             brPath,
		JSONLLogPath:       jsonlLogPath,
		MaxConcurrent:      1,
		Substrate:          daemon.NewTmuxSubstrate(tmuxAdapter, sessionName),
		DaemonBinaryPath:   daemonBinaryPath,
		BinaryCommitHash:   commitHash,
		CancelOnQueueDrain: cancelRun,
	}

	if startErr := daemon.Start(runCtx, cfg); startErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: %v\n", startErr)
		if errors.Is(startErr, lifecycle.ErrPidfileLocked) {
			return 5
		}
		return 1
	}

	return 0
}
