package main

// run.go — `harmonik run <bead-id>` subcommand implementation.
//
// Semantics (hk-icecw + hk-w3cp1 + hk-boiwe + hk-hiqrl + hk-ibilr + hk-qo9pq + hk-55zv2 + hk-cebjc + hk-b3wqd):
//  1. Parse flags: --project, --beads, --max-concurrent, --context, --review-loop, --notify-stream, --workflow-mode, --workflow-ref, --param, --dry-run/--plan-only.
//  2. Resolve br binary via PATH.
//  3. Validate every bead exists and is in a claimable state (open/ready).
//  3a. [--dry-run] Print intended spawns and exit 0 — no state changes, no daemon.
//  3b. [daemon up] Submit beads to the running daemon via queue-submit socket RPC
//      and block until group completion (hk-b3wqd — submit-to-existing-daemon path).
//  4. Guard against an existing active queue (QM-027).
//  5. Construct a stream/wave queue (one group, N items) and persist it to .harmonik/queue.json.
//  6. Start the daemon with a context cancelled when the queue exits (drain or failure).
//  7. Return 0 on success, 1 on failure, 2 on unexpected state, 5 on pidfile lock.
//
// Flag reference:
//
//	<bead-id>                    Positional single-bead shorthand (back-compat).
//	--beads id1,id2,...          Comma-separated multi-bead list (hk-w3cp1). Mutually
//	                             exclusive with positional <bead-id>.
//	--max-concurrent N           Maximum simultaneous items (default 1, hk-w3cp1).
//	--context <string>           Free-form text injected as "## Extra Context" in the
//	                             agent-task.md for every dispatched item (hk-boiwe).
//	--context @<file>            Same, but read context from a file (hk-boiwe).
//	--workflow-mode <mode>       Workflow dispatch shape: builtin (default), single,
//	                             review-loop, dot (hk-qo9pq). "builtin" defers to
//	                             --review-loop / --no-review-loop.
//	--workflow-ref <path>        Path to the .dot workflow file; required when
//	                             --workflow-mode dot (hk-qo9pq).
//	--no-review-loop             Opt out of review-loop workflow; items run single-node (hk-g0ckv).
//	--review-loop                Deprecated alias; review-loop is now the default (hk-g0ckv).
//	--notify-stream              Write one line per bead completion to stdout (hk-ibilr); auto-enabled
//	                             for multi-bead runs (len>1 or max-concurrent>1) per hk-ze3op.
//	--notify-stream=<path>       Same, but write to a FIFO or file instead of stdout.
//	--no-notify-stream           Opt out of auto-enable; for scripted callers parsing exit code only (hk-ze3op).
//	--dry-run                    Print intended spawns without launching claude (hk-cebjc).
//	--plan-only                  Alias for --dry-run.
//
// Exit-code contract:
//
//	0  — all beads reached SUCCESS terminal
//	1  — at least one bead failed, or argument/validation/daemon error
//	2  — unexpected queue state after daemon exit (diagnostic; inline-daemon path only)
//	5  — pidfile locked (inline-daemon path only; submit-to-daemon path avoids this)
//
// Spec ref: specs/queue-model.md §2.3, §3.1 QM-001, §QM-027.
// Bead ref: hk-icecw, hk-8jh26, hk-w3cp1, hk-boiwe, hk-hiqrl, hk-qo9pq, hk-b3wqd.

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
)

// resolveGroupKind returns the queue.GroupKind that runBeadSubcommand would use
// for the given subArgs slice. It is exported for test use only — it parses
// just the --wave flag and returns GroupKindWave or GroupKindStream accordingly.
// All other flags in subArgs are ignored.
//
// Bead ref: hk-7nbey.
func resolveGroupKind(subArgs []string) queue.GroupKind {
	for _, arg := range subArgs {
		if arg == "--wave" {
			return queue.GroupKindWave
		}
	}
	return queue.GroupKindStream
}

// signalGracePeriod is the maximum time runBeadSubcommand waits for daemon.Start
// to return after SIGINT/SIGTERM before calling os.Exit(1) unconditionally.
//
// The 5-second window gives the work loop time to drain the queue and archive
// queue.json cleanly. If cleanup is not complete within the window, the hard
// exit ensures the operator is never forced to use SIGKILL.
//
// Spec ref: hk-qd3f4.
const signalGracePeriod = 5 * time.Second

// runBeadSelfWrapExec is the exec function used when $TMUX is unset and
// runBeadSubcommand self-wraps into a new tmux session. Replaced in tests so
// the test process is not replaced by a real execve call.
var runBeadSelfWrapExec = func(argv0 string, argv []string, envv []string) error {
	return syscall.Exec(argv0, argv, envv)
}

// runBeadSubcommand implements `harmonik run <bead-id> [flags]`.
// subArgs is os.Args[2:] (everything after "run").
func runBeadSubcommand(subArgs []string) int {
	return runBeadSubcommandIO(subArgs, os.Stdout)
}

// runBeadSubcommandIO is the testable variant that accepts an explicit stdout
// writer. The stdout parameter receives --help output and dry-run plan output;
// error messages go to os.Stderr unchanged.
func runBeadSubcommandIO(subArgs []string, stdout io.Writer) int {
	// --- Parse flags ---

	projectDirFlag := ""
	beadsFlag := ""        // --beads id1,id2,... (hk-w3cp1)
	maxConcurrent := 1     // --max-concurrent N (hk-w3cp1); default 1 for back-compat
	contextFlag := ""      // --context <inline|@file> (hk-boiwe)
	reviewLoop := true     // default ON per hk-g0ckv; --no-review-loop opts out
	reviewLoopSet := false // tracks whether --review-loop or --no-review-loop was explicit
	notifyStream := ""     // --notify-stream[=path] (hk-ibilr); empty = disabled, "-" = stdout, else file path
	notifyStreamSet := false
	workflowModeFlag := ""                // --workflow-mode <builtin|single|review-loop|dot> (hk-qo9pq); empty = "builtin"
	workflowRefFlag := ""                 // --workflow-ref <path> (hk-qo9pq); required when --workflow-mode dot
	noNotifyStream := false               // --no-notify-stream: opt out of auto-enable on multi-bead runs (hk-ze3op)
	templateParams := map[string]string{} // --param KEY=VALUE (hk-55zv2 / WG-045); repeatable
	dryRun := false                       // --dry-run / --plan-only: print plan without launching (hk-cebjc)
	targetBranchFlag := ""                // --target-branch (hk-mkxw1)
	var protectBranchesFlag []string      // --protect-branch repeatable (hk-mkxw1)
	forbidUnprotectedDefaultFlag := false // --forbid-default-main (hk-mkxw1)
	// waveMode is resolved at queue-build time via resolveGroupKind(subArgs) (hk-7nbey)
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

		// --no-review-loop (hk-g0ckv): opt out; --review-loop kept as deprecated alias
		case arg == "--no-review-loop":
			reviewLoop = false
			reviewLoopSet = true
		case arg == "--review-loop": // deprecated: review-loop is now the default (hk-g0ckv)
			reviewLoop = true
			reviewLoopSet = true

		// --notify-stream[=path] (hk-ibilr)
		case arg == "--notify-stream":
			notifyStreamSet = true
			notifyStream = "-" // stdout
		case strings.HasPrefix(arg, "--notify-stream="):
			notifyStreamSet = true
			notifyStream = strings.TrimPrefix(arg, "--notify-stream=")
			if notifyStream == "" {
				notifyStream = "-"
			}

		// --workflow-mode (hk-qo9pq): "builtin" (default), "single", "review-loop", "dot"
		case arg == "--workflow-mode" && i+1 < len(subArgs):
			i++
			workflowModeFlag = subArgs[i]
		case strings.HasPrefix(arg, "--workflow-mode="):
			workflowModeFlag = strings.TrimPrefix(arg, "--workflow-mode=")

		// --workflow-ref (hk-qo9pq): path to DOT workflow file; required with --workflow-mode dot
		case arg == "--workflow-ref" && i+1 < len(subArgs):
			i++
			workflowRefFlag = subArgs[i]
		case strings.HasPrefix(arg, "--workflow-ref="):
			workflowRefFlag = strings.TrimPrefix(arg, "--workflow-ref=")

		// --no-notify-stream (hk-ze3op): opt out of auto-enable on multi-bead runs
		case arg == "--no-notify-stream":
			noNotifyStream = true

		// --wave (hk-7nbey): opt into wave-mode (no appends); resolved via resolveGroupKind
		case arg == "--wave":
			// handled by resolveGroupKind(subArgs) at queue-build time

		// --param KEY=VALUE (hk-55zv2 / WG-045): repeatable template substitution param
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

		// --target-branch (hk-mkxw1)
		case arg == "--target-branch" && i+1 < len(subArgs):
			i++
			targetBranchFlag = subArgs[i]
		case strings.HasPrefix(arg, "--target-branch="):
			targetBranchFlag = strings.TrimPrefix(arg, "--target-branch=")

		// --protect-branch (repeatable, hk-mkxw1)
		case arg == "--protect-branch" && i+1 < len(subArgs):
			i++
			protectBranchesFlag = append(protectBranchesFlag, subArgs[i])
		case strings.HasPrefix(arg, "--protect-branch="):
			protectBranchesFlag = append(protectBranchesFlag, strings.TrimPrefix(arg, "--protect-branch="))

		// --forbid-default-main (hk-mkxw1)
		case arg == "--forbid-default-main":
			forbidUnprotectedDefaultFlag = true

		// --dry-run / --plan-only (hk-cebjc): print plan without launching
		case arg == "--dry-run" || arg == "--plan-only":
			dryRun = true

		// --help / -h (hk-vudz0)
		case arg == "--help" || arg == "-h":
			runUsage(stdout)
			return 0

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
		fmt.Fprintln(os.Stderr, "usage: harmonik run <bead-id> [--project DIR] [--context TEXT] [--no-review-loop]")
		fmt.Fprintln(os.Stderr, "       harmonik run --beads id1,id2,... [--max-concurrent N] [--project DIR] [--context TEXT] [--no-review-loop]")
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

	// Resolve workflow mode (hk-qo9pq).
	// --workflow-mode takes precedence over --review-loop / --no-review-loop when set.
	// Valid --workflow-mode values: "builtin" (default), "single", "review-loop", "dot".
	// "builtin" defers to the --review-loop / --no-review-loop logic.
	// When neither --workflow-mode nor --review-loop/--no-review-loop is explicit, leave
	// itemWorkflowMode empty so the daemon-resolved default (dot/triple-review) wins (hk-zhysl).
	var itemWorkflowMode string
	var itemWorkflowRef string
	switch workflowModeFlag {
	case "", "builtin":
		// When --review-loop / --no-review-loop was explicitly passed, honour it.
		// Otherwise leave empty so the daemon's config default applies.
		if reviewLoopSet {
			if reviewLoop {
				itemWorkflowMode = string(core.WorkflowModeReviewLoop)
			} else {
				itemWorkflowMode = string(core.WorkflowModeSingle)
			}
		}
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
	case "review-loop":
		itemWorkflowMode = string(core.WorkflowModeReviewLoop)
		if workflowRefFlag != "" {
			fmt.Fprintln(os.Stderr, "harmonik run: --workflow-ref requires --workflow-mode dot")
			return 1
		}
	case "dot":
		itemWorkflowMode = string(core.WorkflowModeDot)
		if workflowRefFlag == "" {
			fmt.Fprintln(os.Stderr, "harmonik run: --workflow-mode dot requires --workflow-ref <path>")
			return 1
		}
		itemWorkflowRef = workflowRefFlag
	default:
		fmt.Fprintf(os.Stderr, "harmonik run: unknown --workflow-mode %q (valid: builtin, single, review-loop, dot)\n", workflowModeFlag)
		return 1
	}

	// Auto-enable --notify-stream on multi-bead runs (hk-ze3op).
	// Single-bead runs (len==1 and maxConcurrent==1) leave it off — no benefit.
	// --no-notify-stream opts out for scripted callers parsing exit code only.
	if !notifyStreamSet && !noNotifyStream && (len(beadIDs) > 1 || maxConcurrent > 1) {
		notifyStream = "-" // stdout
		notifyStreamSet = true
	}

	// --- Resolve --notify-stream writer (hk-ibilr) ---

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

	// --- Resolve self binary path (used for self-wrap below and DaemonBinaryPath) ---

	daemonBinaryPath, binaryErr := os.Executable()
	if binaryErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: os.Executable() failed: %v\n", binaryErr)
		return 1
	}

	// --- Guard: $TMUX must be set — self-wrap in a new tmux session if possible ---
	//
	// harmonik run requires an active tmux session: agent windows are created as
	// tmux windows inside the caller's session. When $TMUX is not set, attempt to
	// self-wrap by exec-replacing this process with:
	//
	//   tmux new-session -- <binary> run <subArgs...>
	//
	// This creates a new tmux session that runs harmonik run and attaches the
	// calling terminal to it. The inner process will see $TMUX set and proceed.
	// If tmux is not in PATH, a clear actionable error is printed instead.
	//
	// hk-cebjc: --dry-run does not spawn any processes; skip the tmux guard.
	if !dryRun && os.Getenv("TMUX") == "" {
		tmuxBin, lookErr := exec.LookPath("tmux")
		if lookErr != nil {
			fmt.Fprintln(os.Stderr, "harmonik run: $TMUX is not set and tmux is not in PATH.\n"+
				"  Start a tmux session:  tmux new-session -s harmonik\n"+
				"  Then re-run inside tmux. Or use: harmonik tmux-start")
			return 1
		}
		// Self-wrap: exec-replace this process with tmux new-session.
		// On success syscall.Exec never returns; it replaces the process image.
		selfWrapArgv := append([]string{"tmux", "new-session", "--", daemonBinaryPath, "run"}, subArgs...)
		if wrapErr := runBeadSelfWrapExec(tmuxBin, selfWrapArgv, os.Environ()); wrapErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: tmux self-wrap failed: %v\n", wrapErr)
			return 1
		}
		return 0 // unreachable; process replaced on success
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
		// Only open (and ready-equivalent) beads are claimable.
		if record.Status != core.CoarseStatusOpen {
			fmt.Fprintf(os.Stderr, "harmonik run: bead %q is not in a claimable state (status=%q; want %q)\n",
				beadID, record.Status, core.CoarseStatusOpen)
			return 1
		}
		beadRecords = append(beadRecords, record)
	}

	// hk-cebjc: --dry-run / --plan-only prints the intended spawn plan and exits
	// without persisting queue.json, touching the bead ledger, or launching claude.
	if dryRun {
		printDryRunPlan(stdout, beadRecords, itemWorkflowMode, itemWorkflowRef, maxConcurrent, resolveGroupKind(subArgs))
		return 0
	}

	// hk-b3wqd: if a daemon is already running, submit beads to it via the
	// queue-submit socket RPC instead of starting a competing daemon instance
	// (which would collide on the pidfile lock and exit 5). This lets N
	// concurrent `harmonik run` invocations share one persistent daemon.
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

	// --- Construct a wave queue (one group, N items) and persist it ---

	// Seal templateParams into queue items (WG-045 / hk-55zv2).
	// Only attach when at least one --param was provided; nil means no substitution.
	var sealedParams map[string]string
	if len(templateParams) > 0 {
		sealedParams = templateParams
	}

	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{
			BeadID:         id,
			Status:         queue.ItemStatusPending,
			Context:        extraContext,     // hk-boiwe
			WorkflowMode:   itemWorkflowMode, // hk-hiqrl
			WorkflowRef:    itemWorkflowRef,  // hk-qo9pq
			TemplateParams: sealedParams,     // hk-55zv2 / WG-045
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
				Kind:       resolveGroupKind(subArgs),
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
	// Exception (hk-ly4w5): paused-by-failure and cancelled queues are
	// auto-archived so that re-dispatch is one command.
	// Exception (hk-i6hhn): active queues left by a dead daemon (stale or
	// absent pidfile) are auto-archived so that a kill of a wedged daemon
	// does not permanently block re-dispatch.
	existingQueue, loadErr := queue.Load(persistCtx, projectDir, queue.QueueNameMain)
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot check existing queue: %v\n", loadErr)
		return 1
	}
	if existingQueue != nil && existingQueue.Status != queue.QueueStatusCompleted {
		switch existingQueue.Status {
		case queue.QueueStatusPausedByFailure, queue.QueueStatusCancelled:
			// Auto-archive the stale queue so re-dispatch is one command (hk-ly4w5).
			archivePath, archiveErr := queue.ArchiveFailedQueue(persistCtx, projectDir, queue.QueueNameMain, time.Now())
			if archiveErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik run: cannot archive stale queue: %v\n", archiveErr)
				return 1
			}
			fmt.Fprintf(os.Stderr, "harmonik run: archived stale queue to %s\n", archivePath)
		default:
			// For active (or other non-terminal) queues, probe the pidfile to
			// detect whether the daemon that owns the queue is still alive
			// (hk-i6hhn). If the pidfile is absent or stale, the daemon died
			// before it could finalise the queue file — auto-archive and proceed.
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

	// qs is created here so that run.go can inspect final queue status after
	// daemon.Start returns (Fix 2: exit code reflects bead outcome, hk-8jh26).
	qs := daemon.NewQueueStore()

	// --- Create .harmonik subdirectories and resolve tmux session ---

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

	// hk-15b83: resolve the spawn-target session DETERMINISTICALLY from the
	// project directory — NOT from the ambient $TMUX session.
	//
	// `tmux display-message -p '#{session_name}'` returns whatever session the
	// calling process inherits via $TMUX.  When the auto-revive supervisor (itself
	// running in its own `hk-daemon-supervise` session) starts `harmonik run`,
	// that resolves to the SUPERVISOR's session, so implementer windows spawn there
	// instead of the run-owned session (same root cause as hk-9vp51 #3 in main.go).
	//
	// DefaultSessionName(projectDir) == "harmonik-<hash>-default" — the same name
	// `hk tmux-start` creates by default — so launched via tmux-start this attaches
	// to the operator's existing session; launched by the supervisor it creates its own.
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

	// --- Build a context that cancels on SIGINT/SIGTERM or after queue exits ---

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// runCtx is cancelled only by SIGINT/SIGTERM (inherited from ctx) or after
	// all in-flight goroutines drain. It must NOT be cancelled by queue-drain /
	// queue-exit callbacks — those would kill in-flight reviewers (hk-2o2i9).
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	// stopDispatchCtx is a child of runCtx that is cancelled when the queue
	// reaches a terminal state (all-success OR paused-by-failure). The work
	// loop's outer poll checks this context to stop pulling new beads, while
	// in-flight goroutines continue running on runCtx.
	//
	// Bead ref: hk-2o2i9.
	stopDispatchCtx, cancelStopDispatch := context.WithCancel(runCtx)
	defer cancelStopDispatch()

	beadIDStrs := make([]string, len(beadIDs))
	for i, id := range beadIDs {
		beadIDStrs[i] = string(id)
	}
	fmt.Fprintf(os.Stderr, "harmonik run: starting run for [%s] (max-concurrent=%d) in %s\n",
		strings.Join(beadIDStrs, ", "), maxConcurrent, projectDir)

	// hk-xb5yi: resolve spawn cap (same logic as harmonik daemon).
	maxSessions := spawnCapFromEnv(maxConcurrent)

	cfg := daemon.Config{
		ProjectDir:               projectDir,
		BrPath:                   brPath,
		JSONLLogPath:             jsonlLogPath,
		MaxConcurrent:            maxConcurrent,                                                                                                                                             // hk-w3cp1: user-controlled concurrency
		Substrate:                daemon.NewTmuxSubstrate(tmuxAdapter, sessionName, daemon.WithSpawnCap(maxSessions), daemon.WithCrewProjectHash(lifecycle.ComputeProjectHash(projectDir))), // fleet-portability T2
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

	// hk-qd3f4: hard-exit watchdog — independent of dispatch state.
	//
	// Problem: when dispatch goroutines are wedged (blocking syscall, channel
	// deadlock, etc.), ctx.Done() fires on SIGINT/SIGTERM but runWorkLoop never
	// returns, so daemon.Start never returns, so the process never exits.  The
	// operator must use SIGKILL.
	//
	// Fix: spin a watchdog goroutine that is structurally independent of the
	// dispatch path. It waits for the signal context (ctx) to be cancelled, then
	// arms a 5-second grace timer.  If daemon.Start returns before the timer
	// fires, the watchdog is disarmed via daemonDone.  If the timer fires first,
	// the watchdog calls os.Exit(1) unconditionally — no path through wedged
	// goroutines can prevent it.
	//
	// Invariant: daemonDone must be closed BEFORE runBeadSubcommand returns so
	// the goroutine always exits and never leaks in tests.
	//
	// Spec ref: hk-qd3f4.
	daemonDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Signal received; arm the hard-exit timer.
			fmt.Fprintf(os.Stderr, "harmonik run: signal received — arming 5s hard-exit timer\n")
			select {
			case <-daemonDone:
				// daemon.Start returned within the grace window; normal exit.
			case <-time.After(signalGracePeriod):
				fmt.Fprintf(os.Stderr, "harmonik run: grace period expired — forcing os.Exit(1)\n")
				os.Exit(1)
			}
		case <-daemonDone:
			// daemon.Start returned normally before any signal; watchdog not needed.
		}
	}()

	startErr := daemon.Start(runCtx, cfg)
	close(daemonDone) // disarm watchdog
	if notifyFile != nil {
		_ = notifyFile.Close()
	}
	if startErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: %v\n", startErr)
		if errors.Is(startErr, lifecycle.ErrPidfileLocked) {
			return 5
		}
		return 1
	}

	// Fix 2: map final queue status to exit code (hk-8jh26).
	// After daemon.Start returns, qs reflects the terminal queue state:
	//
	//   nil + no signal    → CompleteAndUnlink ran → all-success → exit 0
	//   nil + signal       → drainCancelledQueue archived queue → operator cancel → exit 1
	//   paused-by-failure  → bead failed → exit 1
	//   other non-nil      → unexpected state → exit 2 with diagnostic
	//
	// hk-ppt32: ctx is the signal.NotifyContext (not runCtx). Its Err() is non-nil
	// only when a real SIGINT/SIGTERM was received; it stays nil when
	// cancelOnQueueDrain/cancelOnQueueExit fired (those cancel runCtx, not ctx).
	finalQueue := qs.Queue()
	if finalQueue == nil {
		if ctx.Err() != nil {
			// Operator cancelled via SIGINT/SIGTERM; drainCancelledQueue already
			// archived the queue file so the next run can start cleanly.
			fmt.Fprintf(os.Stderr, "harmonik run: cancelled by operator (signal)\n")
			return 1
		}
		// Queue was cleared via CompleteAndUnlink → all-success.
		return 0
	}
	if finalQueue.Status == queue.QueueStatusPausedByFailure {
		fmt.Fprintf(os.Stderr, "harmonik run: one or more beads failed (queue paused-by-failure)\n")
		archivePath, archiveErr := queue.ArchiveFailedQueue(context.Background(), projectDir, queue.NormaliseQueueName(finalQueue.Name), time.Now())
		if archiveErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: warning: could not archive queue file: %v\n", archiveErr)
		} else if archivePath != "" {
			fmt.Fprintf(os.Stderr, "harmonik run: archived failed queue → %s\n", archivePath)
		}
		return 1
	}
	// Unexpected terminal state — surface for debugging.
	fmt.Fprintf(os.Stderr, "harmonik run: unexpected queue state after exit: %s (queue_id=%s)\n",
		finalQueue.Status, finalQueue.QueueID)
	return 2
}

// printDryRunPlan writes the intended spawn plan to out and returns.
// No state is mutated; no queue is persisted; no daemon is started.
//
// Output format (hk-cebjc):
//
//	harmonik run --dry-run: plan for N beads (max-concurrent=M, queue=stream)
//
//	  hk-abc123  "title..."       workflow=review-loop  → 1 implementer + up to 3 reviewers
//	  hk-def456  "another title"  workflow=single       → 1 implementer
//
//	Total: N implementers + up to R reviewers across N beads (max-concurrent=M)
//	No changes written. Run without --dry-run to execute.
//
// Bead ref: hk-cebjc.
func printDryRunPlan(out io.Writer, beadRecords []core.BeadRecord, workflowMode string, workflowRef string, maxConcurrent int, groupKind queue.GroupKind) {
	// reviewLoopMaxReviewers mirrors the unexported reviewLoopIterationCap = 3
	// in internal/daemon/reviewloop.go (hk-cebjc).
	const reviewLoopMaxReviewers = 3

	n := len(beadRecords)
	fmt.Fprintf(out, "harmonik run --dry-run: plan for %d bead(s) (max-concurrent=%d, queue=%s)\n\n",
		n, maxConcurrent, groupKind)

	totalImplementers := 0
	totalMaxReviewers := 0

	for _, rec := range beadRecords {
		title := rec.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}

		var spawnDesc string
		switch core.WorkflowMode(workflowMode) {
		case core.WorkflowModeReviewLoop:
			spawnDesc = fmt.Sprintf("1 implementer + up to %d reviewers", reviewLoopMaxReviewers)
			totalImplementers++
			totalMaxReviewers += reviewLoopMaxReviewers
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

		fmt.Fprintf(out, "  %-12s  %-52s  workflow=%-12s  → %s\n",
			string(rec.BeadID), fmt.Sprintf("%q", title), workflowMode, spawnDesc)
	}

	fmt.Fprintln(out)
	switch core.WorkflowMode(workflowMode) {
	case core.WorkflowModeReviewLoop:
		fmt.Fprintf(out, "Total: %d implementer(s) + up to %d reviewer(s) across %d bead(s) (max-concurrent=%d)\n",
			totalImplementers, totalMaxReviewers, n, maxConcurrent)
	case core.WorkflowModeDot:
		fmt.Fprintf(out, "Total: %d+ agent(s) across %d bead(s) — exact count depends on graph (max-concurrent=%d)\n",
			totalImplementers, n, maxConcurrent)
	default:
		fmt.Fprintf(out, "Total: %d implementer(s) across %d bead(s) (max-concurrent=%d)\n",
			totalImplementers, n, maxConcurrent)
	}
	fmt.Fprintln(out, "No changes written. Run without --dry-run to execute.")
}

// runUsage prints help for `harmonik run --help` to w. Output goes to
// stdout so it can be captured by agents without stderr redirection (hk-vudz0).
func runUsage(w io.Writer) {
	fmt.Fprint(w, `harmonik run — legacy/solo-bootstrap bead execution

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
  --workflow-mode MODE          Workflow dispatch shape: builtin (default), single, review-loop, dot
  --workflow-ref PATH           Path to the .dot workflow file; required with --workflow-mode dot
  --no-review-loop              Opt out of review-loop workflow (default: on); beads run single-node
  --review-loop                 Deprecated: review-loop is now the default; this flag is a no-op
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
  1   At least one bead failed, or argument/validation error
  2   Unexpected queue state (diagnostic; inline-daemon path only)
  5   Another harmonik instance is already running (inline-daemon path only;
      when a daemon is detected via daemon.sock, beads are submitted to it
      instead, avoiding the collision — exit 5 is not returned in that case)

EXAMPLES
  harmonik run hk-abc123
  harmonik run --beads hk-abc123,hk-def456 --max-concurrent 2
  harmonik run hk-abc123 --context "Focus on the migration spec only"
  harmonik run hk-abc123 --context @/path/to/context.txt
  harmonik run hk-abc123 --no-review-loop
  harmonik run hk-abc123 --workflow-mode dot --workflow-ref ./review-loop.dot
  harmonik run hk-abc123 --workflow-mode dot --workflow-ref ./my.dot --param ISSUE_NUMBER=172
  harmonik run --beads hk-abc123,hk-def456 --project /path/to/project --max-concurrent 4
  harmonik run --beads hk-abc123,hk-def456 --notify-stream
  harmonik run --beads hk-abc123,hk-def456 --notify-stream=/tmp/hk-events.fifo
  harmonik run --beads hk-abc123,hk-def456 --dry-run
  harmonik run --beads hk-abc123,hk-def456 --plan-only --max-concurrent 4
`)
}
