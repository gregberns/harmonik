package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/gregberns/harmonik/internal/queuewiring"
)

func resolveGroupKind(subArgs []string) queue.GroupKind {
	for _, arg := range subArgs {
		if arg == "--wave" {
			return queue.GroupKindWave
		}
	}
	return queue.GroupKindStream
}

const signalGracePeriod = 5 * time.Second

var runBeadSelfWrapExec = syscall.Exec

func runBeadSubcommand(subArgs []string) int {
	return runBeadSubcommandIO(subArgs, os.Stdout)
}

func runBeadSubcommandIO(subArgs []string, stdout io.Writer) int {
	projectDirFlag := ""
	beadsFlag := ""    // --beads id1,id2,... (hk-w3cp1)
	maxConcurrent := 1 // --max-concurrent N (hk-w3cp1); default 1 for back-compat
	contextFlag := ""  // --context <inline|@file> (hk-boiwe)
	notifyStream := "" // --notify-stream[=path] (hk-ibilr); empty = disabled, "-" = stdout, else file path
	notifyStreamSet := false
	workflowModeFlag := ""                // --workflow-mode <builtin|single|dot> (hk-qo9pq); empty = "builtin"
	workflowRefFlag := ""                 // --workflow-ref <path> (hk-qo9pq); required when --workflow-mode dot
	noNotifyStream := false               // --no-notify-stream: opt out of auto-enable on multi-bead runs (hk-ze3op)
	templateParams := map[string]string{} // --param KEY=VALUE (hk-55zv2 / WG-045); repeatable
	dryRun := false                       // --dry-run / --plan-only: print plan without launching (hk-cebjc)
	targetBranchFlag := ""                // --target-branch (hk-mkxw1)
	var protectBranchesFlag []string      // --protect-branch repeatable (hk-mkxw1)
	forbidUnprotectedDefaultFlag := false // --forbid-default-main (hk-mkxw1)
	positional := []string{}

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectDirFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectDirFlag = strings.TrimPrefix(arg, "--project=")

		case arg == "--beads" && i+1 < len(subArgs):
			i++
			beadsFlag = subArgs[i]
		case strings.HasPrefix(arg, "--beads="):
			beadsFlag = strings.TrimPrefix(arg, "--beads=")

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

		case arg == "--context" && i+1 < len(subArgs):
			i++
			contextFlag = subArgs[i]
		case strings.HasPrefix(arg, "--context="):
			contextFlag = strings.TrimPrefix(arg, "--context=")

		case arg == "--review-loop":
			fmt.Fprintln(os.Stderr,
				"harmonik run: --review-loop was retired along with the review-loop mode.\n"+
					"  Drop the flag: dot is the default and is already reviewed.")
			return 1
		case arg == "--no-review-loop":
			fmt.Fprintln(os.Stderr,
				"harmonik run: --no-review-loop was retired along with the review-loop mode.\n"+
					"  It used to mean \"run single-node, unreviewed\". If that is what you want,\n"+
					"  say it directly: --workflow-mode single.")
			return 1

		case arg == "--notify-stream":
			notifyStreamSet = true
			notifyStream = "-" // stdout
		case strings.HasPrefix(arg, "--notify-stream="):
			notifyStreamSet = true
			notifyStream = strings.TrimPrefix(arg, "--notify-stream=")
			if notifyStream == "" {
				notifyStream = "-"
			}

		case arg == "--workflow-mode" && i+1 < len(subArgs):
			i++
			workflowModeFlag = subArgs[i]
		case strings.HasPrefix(arg, "--workflow-mode="):
			workflowModeFlag = strings.TrimPrefix(arg, "--workflow-mode=")

		case arg == "--workflow-ref" && i+1 < len(subArgs):
			i++
			workflowRefFlag = subArgs[i]
		case strings.HasPrefix(arg, "--workflow-ref="):
			workflowRefFlag = strings.TrimPrefix(arg, "--workflow-ref=")

		case arg == "--no-notify-stream":
			noNotifyStream = true

		case arg == "--wave":

		case arg == "--param" && i+1 < len(subArgs):
			i++
			kv := subArgs[i]
			eqIdx := strings.IndexByte(kv, '=')
			if eqIdx <= 0 {
				fmt.Fprintf(os.Stderr, "harmonik run: --param must be KEY=VALUE, got %q\n", kv)
				return 1
			}
			key := kv[:eqIdx]
			val := kv[eqIdx+1:]
			templateParams[key] = val
		case strings.HasPrefix(arg, "--param="):
			kv := strings.TrimPrefix(arg, "--param=")
			eqIdx := strings.IndexByte(kv, '=')
			if eqIdx <= 0 {
				fmt.Fprintf(os.Stderr, "harmonik run: --param must be KEY=VALUE, got %q\n", kv)
				return 1
			}
			key := kv[:eqIdx]
			val := kv[eqIdx+1:]
			templateParams[key] = val

		case arg == "--target-branch" && i+1 < len(subArgs):
			i++
			targetBranchFlag = subArgs[i]
		case strings.HasPrefix(arg, "--target-branch="):
			targetBranchFlag = strings.TrimPrefix(arg, "--target-branch=")

		case arg == "--protect-branch" && i+1 < len(subArgs):
			i++
			protectBranchesFlag = append(protectBranchesFlag, subArgs[i])
		case strings.HasPrefix(arg, "--protect-branch="):
			protectBranchesFlag = append(protectBranchesFlag, strings.TrimPrefix(arg, "--protect-branch="))

		case arg == "--forbid-default-main":
			forbidUnprotectedDefaultFlag = true

		case arg == "--dry-run" || arg == "--plan-only":
			dryRun = true

		case arg == "--help" || arg == "-h":
			if err := runUsage(stdout); err != nil {
				return 1
			}
			return 0

		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik run: unknown flag %q\n", arg)
			return 1

		default:
			positional = append(positional, arg)
		}
	}

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
		fmt.Fprintln(os.Stderr, "usage: harmonik run <bead-id> [--project DIR] [--context TEXT] [--workflow-mode MODE]")
		fmt.Fprintln(os.Stderr, "       harmonik run --beads id1,id2,... [--max-concurrent N] [--project DIR] [--context TEXT] [--workflow-mode MODE]")
		return 1
	default:
		fmt.Fprintf(os.Stderr, "harmonik run: too many positional arguments (got %d, expected 1); use --beads for multiple\n", len(positional))
		return 1
	}

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

	var itemWorkflowMode string
	var itemWorkflowRef string
	switch workflowModeFlag {
	case "", "builtin":
		if workflowRefFlag != "" {
			fmt.Fprintln(os.Stderr, "harmonik run: --workflow-ref requires --workflow-mode dot")
			return 1
		}
	case "single":
		itemWorkflowMode = string(core.WorkflowModeSingle)
		if workflowRefFlag != "" {
			fmt.Fprintln(os.Stderr, "harmonik run: --workflow-ref requires --workflow-mode dot")
			return 1
		}
	case core.WorkflowModeRetiredReviewLoop:
		fmt.Fprintln(os.Stderr,
			"harmonik run: --workflow-mode review-loop was retired; use --workflow-mode dot.\n"+
				"  review-loop was a hand-written implementer→reviewer cycle; dot is the general\n"+
				"  graph walker it was a special case of, and the embedded standard-bead.dot\n"+
				"  default already runs an implementer→reviewer→close shape.")
		return 1
	case "dot":
		itemWorkflowMode = string(core.WorkflowModeDot)
		if workflowRefFlag == "" {
			fmt.Fprintln(os.Stderr, "harmonik run: --workflow-mode dot requires --workflow-ref <path>")
			return 1
		}
		itemWorkflowRef = workflowRefFlag
	default:
		fmt.Fprintf(os.Stderr, "harmonik run: unknown --workflow-mode %q (valid: builtin, single, dot)\n", workflowModeFlag)
		return 1
	}

	if !notifyStreamSet && !noNotifyStream && (len(beadIDs) > 1 || maxConcurrent > 1) {
		notifyStream = "-" // stdout
		notifyStreamSet = true
	}

	var notifyWriter io.Writer
	var notifyFile *os.File
	if notifyStreamSet {
		if notifyStream == "-" {
			notifyWriter = stdout
		} else {
			var openErr error
			//nolint:gosec // G304: operator-controlled path from CLI flag
			notifyFile, openErr = os.OpenFile(notifyStream, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
			if openErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik run: --notify-stream: cannot open %q: %v\n", notifyStream, openErr)
				return 1
			}
			notifyWriter = notifyFile
		}
	}

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

	daemonBinaryPath, binaryErr := os.Executable()
	if binaryErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: os.Executable() failed: %v\n", binaryErr)
		return 1
	}

	if !dryRun && os.Getenv("TMUX") == "" {
		tmuxBin, lookErr := exec.LookPath("tmux")
		if lookErr != nil {
			fmt.Fprintln(os.Stderr, "harmonik run: $TMUX is not set and tmux is not in PATH.\n"+
				"  Start a tmux session:  tmux new-session -s harmonik\n"+
				"  Then re-run inside tmux. Or use: harmonik tmux-start")
			return 1
		}
		selfWrapArgv := append([]string{"tmux", "new-session", "--", daemonBinaryPath, "run"}, subArgs...)
		if wrapErr := runBeadSelfWrapExec(tmuxBin, selfWrapArgv, os.Environ()); wrapErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: tmux self-wrap failed: %v\n", wrapErr)
			return 1
		}
		return 0 // unreachable; process replaced on success
	}

	brPath, brErr := exec.LookPath("br")
	if brErr != nil {
		fmt.Fprintln(os.Stderr, "harmonik run: 'br' not found on PATH — bead ledger required")
		return 1
	}

	adapter, adapterErr := brcli.NewForProject(brPath, projectDir)
	if adapterErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot initialise brcli adapter: %v\n", adapterErr)
		return 1
	}

	validateCtx, validateCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer validateCancel()

	beadRecords := make([]core.BeadRecord, 0, len(beadIDs))
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
		if record.Status != core.CoarseStatusOpen {
			fmt.Fprintf(os.Stderr, "harmonik run: bead %q is not in a claimable state (status=%q; want %q)\n",
				beadID, record.Status, core.CoarseStatusOpen)
			return 1
		}
		beadRecords = append(beadRecords, record)
	}

	if dryRun {
		if err := printDryRunPlan(stdout, beadRecords, itemWorkflowMode, itemWorkflowRef, maxConcurrent, resolveGroupKind(subArgs)); err != nil {
			return 1
		}
		return 0
	}

	if isDaemonUp(projectDir) {
		var sealedParams map[string]string
		if len(templateParams) > 0 {
			sealedParams = templateParams
		}
		return runBeadSubcommandViaDaemon(
			projectDir,
			beadIDs,
			itemWorkflowMode,
			itemWorkflowRef,
			extraContext,
			sealedParams,
			resolveGroupKind(subArgs),
			notifyWriter,
		)
	}

	var sealedParams map[string]string
	if len(templateParams) > 0 {
		sealedParams = templateParams
	}

	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.NewPendingItem(queue.Item{
			BeadID:         id,
			Context:        extraContext,     // hk-boiwe
			WorkflowMode:   itemWorkflowMode, // hk-hiqrl
			WorkflowRef:    itemWorkflowRef,  // hk-qo9pq
			TemplateParams: sealedParams,     // hk-55zv2 / WG-045
		})
	}

	queueUUID, uuidErr := uuid.NewV7()
	if uuidErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: generate queue ID: %v\n", uuidErr)
		return 1
	}
	now := time.Now().UTC()
	initialQueue := queue.NewActiveQueue(queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueUUID.String(),
		SubmittedAt:   now,
		Groups: []queue.Group{
			queue.NewActiveGroup(queue.Group{
				GroupIndex: 0,
				Kind:       resolveGroupKind(subArgs),
				Items:      items,
				CreatedAt:  now,
			}),
		},
	})
	q := &initialQueue

	if mkErr := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), core.HarmonikDirMode); mkErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot create .harmonik/: %v\n", mkErr)
		return 1
	}

	persistCtx := context.Background()

	existingQueue, loadErr := queue.Load(persistCtx, projectDir, queue.QueueNameMain)
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot check existing queue: %v\n", loadErr)
		return 1
	}
	if existingQueue != nil && existingQueue.Status != queue.QueueStatusCompleted {
		switch existingQueue.Status {
		case queue.QueueStatusPausedByFailure, queue.QueueStatusCancelled:
			archivePath, archiveErr := queue.ArchiveFailedQueue(persistCtx, projectDir, queue.QueueNameMain, time.Now())
			if archiveErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik run: cannot archive stale queue: %v\n", archiveErr)
				return 1
			}
			fmt.Fprintf(os.Stderr, "harmonik run: archived stale queue to %s\n", archivePath)
		default:
			pidStatus, _, pidErr := lifecycle.ProbePidfileLock(projectDir)
			daemonDead := errors.Is(pidErr, os.ErrNotExist) || pidStatus == lifecycle.PidfileLockStatusStale
			if daemonDead {
				archivePath, archiveErr := queue.ArchiveFailedQueue(persistCtx, projectDir, queue.QueueNameMain, time.Now())
				if archiveErr != nil {
					fmt.Fprintf(os.Stderr, "harmonik run: cannot archive orphaned queue: %v\n", archiveErr)
					return 1
				}
				fmt.Fprintf(os.Stderr, "harmonik run: daemon appears dead; archived orphaned queue to %s\n", archivePath)
			} else {
				fmt.Fprintf(os.Stderr, "harmonik run: a queue is already active for this project\n")
				fmt.Fprintf(os.Stderr, "  queue_id=%s status=%s\n", existingQueue.QueueID, existingQueue.Status)
				fmt.Fprintln(os.Stderr, "  use 'harmonik queue cancel' to cancel a queue whose daemon is no longer running,")
				fmt.Fprintln(os.Stderr, "  or 'harmonik queue status' to inspect the live queue")
				return 1
			}
		}
	}

	if persistErr := queue.Persist(persistCtx, projectDir, q); persistErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot persist queue.json: %v\n", persistErr)
		return 1
	}

	qs := queuewiring.NewQueueStore()

	if mkErr := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "events"), core.HarmonikDirMode); mkErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot create .harmonik/events/: %v\n", mkErr)
		return 1
	}
	if mkErr := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "beads-intents"), core.HarmonikDirMode); mkErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot create .harmonik/beads-intents/: %v\n", mkErr)
		return 1
	}

	sessionName := tmux.DefaultSessionName(projectDir)

	tmuxAdapter := tmux.OSAdapter{}
	probeCtx := context.Background()
	if probeErr := tmuxAdapter.ProbeTmux(probeCtx); probeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: tmux probe failed: %v\n", probeErr)
		return 1
	}
	if ensErr := tmuxAdapter.EnsureSession(probeCtx, sessionName, projectDir); ensErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot ensure tmux session %q: %v\n", sessionName, ensErr)
		return 1
	}

	jsonlLogPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	stopDispatchCtx, cancelStopDispatch := context.WithCancel(runCtx)
	defer cancelStopDispatch()

	beadIDStrs := make([]string, len(beadIDs))
	for i, id := range beadIDs {
		beadIDStrs[i] = string(id)
	}
	fmt.Fprintf(os.Stderr, "harmonik run: starting run for [%s] (max-concurrent=%d) in %s\n",
		strings.Join(beadIDStrs, ", "), maxConcurrent, projectDir)

	maxSessions := spawnCapFromEnv(maxConcurrent)

	crewSubstrate, codexRegObserver, reviewerSubstrate := selectSubstrate(daemon.NewTmuxSubstrate(tmuxAdapter, sessionName, daemon.WithSpawnCap(maxSessions), daemon.WithCrewProjectHash(lifecycle.ComputeProjectHash(projectDir))), "") // fleet-portability T2

	cfg := daemon.Config{
		ProjectDir:               projectDir,
		BrPath:                   brPath,
		JSONLLogPath:             jsonlLogPath,
		MaxConcurrent:            maxConcurrent, // hk-w3cp1: user-controlled concurrency
		Substrate:                crewSubstrate,
		ReviewerSubstrate:        reviewerSubstrate,
		WorkerRegistryObserver:   codexRegObserver, // M4-C3: late-bind live registry into Codex runner
		DaemonBinaryPath:         daemonBinaryPath,
		BinaryCommitHash:         commitHash,
		CancelOnQueueDrain:       cancelStopDispatch,           // stop dispatch on success (hk-icecw, hk-2o2i9)
		CancelOnQueueExit:        cancelStopDispatch,           // stop dispatch on failure (hk-8jh26, hk-2o2i9)
		StopDispatchCtx:          stopDispatchCtx,              // dispatch-halt ctx separate from in-flight ctx (hk-2o2i9)
		QueueStore:               qs,                           // retained for post-Start status inspection (hk-8jh26 Fix 2)
		NotifyStream:             notifyWriter,                 // hk-ibilr: per-bead completion lines
		TargetBranch:             targetBranchFlag,             // hk-mkxw1: merge target branch
		ProtectBranches:          protectBranchesFlag,          // hk-mkxw1: branches protected from daemon merges
		ForbidUnprotectedDefault: forbidUnprotectedDefaultFlag, // hk-mkxw1: guard against unprotected default branch
	}

	daemonDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "harmonik run: signal received — arming 5s hard-exit timer\n")
			select {
			case <-daemonDone:
			case <-time.After(signalGracePeriod):
				fmt.Fprintf(os.Stderr, "harmonik run: grace period expired — forcing os.Exit(1)\n")
				os.Exit(1)
			}
		case <-daemonDone:
		}
	}()

	startErr := daemon.Start(runCtx, cfg)
	close(daemonDone) // disarm watchdog
	if notifyFile != nil {
		if closeErr := notifyFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: closing notify stream: %v\n", closeErr)
		}
	}
	if startErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: %v\n", startErr)
		if errors.Is(startErr, lifecycle.ErrPidfileLocked) {
			return 5
		}
		return 1
	}

	finalQueue := qs.Queue()
	decision := classifyRunExit(finalQueue, ctx.Err() != nil)
	if decision.message != "" {
		fmt.Fprint(os.Stderr, decision.message)
	}
	if finalQueue != nil && finalQueue.Status == queue.QueueStatusPausedByFailure {
		archivePath, archiveErr := queue.ArchiveFailedQueue(context.Background(), projectDir, queue.NormaliseQueueName(finalQueue.Name), time.Now())
		if archiveErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: warning: could not archive queue file: %v\n", archiveErr)
		} else if archivePath != "" {
			fmt.Fprintf(os.Stderr, "harmonik run: archived failed queue → %s\n", archivePath)
		}
	}
	return decision.code
}

type runExitDecision struct {
	code    int
	message string
}

func classifyRunExit(finalQueue *queue.Queue, signalled bool) runExitDecision {
	if finalQueue == nil {
		if signalled {
			return runExitDecision{code: 1, message: "harmonik run: cancelled by operator (signal)\n"}
		}
		return runExitDecision{code: 0}
	}
	if finalQueue.Status == queue.QueueStatusPausedByFailure {
		return runExitDecision{code: 1, message: "harmonik run: one or more beads failed (queue paused-by-failure)\n"}
	}
	if finalQueue.Status == queue.QueueStatusPausedByDrain && finalQueue.ResumeOnStart {
		if signalled {
			return runExitDecision{code: 1, message: "harmonik run: cancelled by operator (signal)\n"}
		}
		return runExitDecision{code: 1, message: "harmonik run: run ended before the queue finished (queue parked for restart)\n"}
	}
	return runExitDecision{
		code: 2,
		message: fmt.Sprintf("harmonik run: unexpected queue state after exit: %s (queue_id=%s)\n",
			finalQueue.Status, finalQueue.QueueID),
	}
}

func printDryRunPlan(out io.Writer, beadRecords []core.BeadRecord, workflowMode, workflowRef string, maxConcurrent int, groupKind queue.GroupKind) error {
	n := len(beadRecords)
	plan := fmt.Sprintf("harmonik run --dry-run: plan for %d bead(s) (max-concurrent=%d, queue=%s)\n\n",
		n, maxConcurrent, groupKind)

	totalImplementers := 0

	for _, rec := range beadRecords {
		title := rec.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}

		var spawnDesc string
		switch core.WorkflowMode(workflowMode) {
		case core.WorkflowModeDot:
			ref := workflowRef
			if ref == "" {
				ref = "(no --workflow-ref)"
			}
			spawnDesc = fmt.Sprintf("agents per graph %s", ref)
			totalImplementers++ // at minimum 1 node fires
		default: // single
			spawnDesc = "1 implementer"
			totalImplementers++
		}

		plan += fmt.Sprintf("  %-12s  %-52s  workflow=%-12s  → %s\n",
			string(rec.BeadID), fmt.Sprintf("%q", title), workflowMode, spawnDesc)
	}

	plan += "\n"
	switch core.WorkflowMode(workflowMode) {
	case core.WorkflowModeDot:
		plan += fmt.Sprintf("Total: %d+ agent(s) across %d bead(s) — exact count depends on graph (max-concurrent=%d)\n",
			totalImplementers, n, maxConcurrent)
	default:
		plan += fmt.Sprintf("Total: %d implementer(s) across %d bead(s) (max-concurrent=%d)\n",
			totalImplementers, n, maxConcurrent)
	}
	plan += "No changes written. Run without --dry-run to execute.\n"
	_, err := io.WriteString(out, plan)
	return err
}

func runUsage(w io.Writer) error {
	_, err := io.WriteString(w, `harmonik run — legacy/solo-bootstrap bead execution

  Not the primary dispatcher. For ongoing work, start one persistent daemon
  (queue-only) and submit beads with 'harmonik queue submit'. 'harmonik run'
  submits to a running daemon if one exists, else runs the beads inline and
  exits on completion (see EXIT CODES).

USAGE
  harmonik run <bead-id> [flags]
  harmonik run --beads id1,id2,... [flags]

FLAGS
  --beads id1,id2,...           Comma-separated bead IDs (mutually exclusive with positional <bead-id>)
  --max-concurrent N            Maximum simultaneous beads (default 1)
  --context TEXT                Free-form extra context injected into each agent task
  --context @FILE               Same, but read context from a file
  --workflow-mode MODE          Workflow dispatch shape: builtin (default), single, dot
  --workflow-ref PATH           Path to the .dot workflow file; required with --workflow-mode dot
  --notify-stream               Write one line per bead completion to stdout (auto-enabled for multi-bead runs)
  --notify-stream=PATH          Same, but write to a FIFO or file
  --no-notify-stream            Disable per-bead completion lines (opt out of auto-enable on multi-bead runs)
  --wave                        Use wave-mode queue (no mid-flight appends; default: stream)
  --param KEY=VALUE             Template substitution param for .dot workflows (repeatable); replaces __KEY__ in source
  --dry-run                     Print intended spawns without launching claude or mutating state
  --plan-only                   Alias for --dry-run
  --project DIR                 Project directory (default: current working directory)

EXIT CODES
  0   All beads succeeded (or --dry-run plan printed)
  1   At least one bead failed, the operator cancelled the run (Ctrl-C), or
      an argument/validation error
  2   Unexpected queue state (diagnostic; inline-daemon path only)
  5   Another harmonik instance is already running (inline-daemon path only;
      when a daemon is detected via daemon.sock, beads are submitted to it
      instead, avoiding the collision — exit 5 is not returned in that case)

EXAMPLES
  harmonik run hk-abc123
  harmonik run --beads hk-abc123,hk-def456 --max-concurrent 2
  harmonik run hk-abc123 --context "Focus on the migration spec only"
  harmonik run hk-abc123 --context @/path/to/context.txt
  harmonik run hk-abc123 --workflow-mode dot --workflow-ref ./my-workflow.dot
  harmonik run hk-abc123 --workflow-mode dot --workflow-ref ./my.dot --param ISSUE_NUMBER=172
  harmonik run --beads hk-abc123,hk-def456 --project /path/to/project --max-concurrent 4
  harmonik run --beads hk-abc123,hk-def456 --notify-stream
  harmonik run --beads hk-abc123,hk-def456 --notify-stream=/tmp/hk-events.fifo
  harmonik run --beads hk-abc123,hk-def456 --dry-run
  harmonik run --beads hk-abc123,hk-def456 --plan-only --max-concurrent 4
`)
	return err
}
