package cli

// cancel.go — `harmonik queue cancel` subcommand implementation.
//
// Semantics (hk-i6hhn, extended hk-fkpb7, hk-0mmy4):
//  1. Parse --project, --force, --queue, and --queue-id flags.
//  2. Resolve the target queue:
//     --queue-id <uuid> → enumerate per-queue files and find by UUID.
//     --queue <name>    → load the named per-queue file.
//     <positional>      → load the named per-queue file (backward-compat).
//     (no arg)          → default to "main".
//  3. Absent queue → exit 0 (nothing to cancel).
//  4. Refuse if the queue status is already terminal (completed) unless --force.
//  5. Cancel the queue:
//     - If a daemon is live (socket reachable), route the cancel through it
//       via the "queue-cancel" op (HandlerAdapter.HandleQueueCancel). The
//       daemon archives .harmonik/queues/<name>.json to
//       <name>.json.failed-<timestamp> AND reaps its in-memory QueueStore
//       slot for that name — the reap step is what a purely disk-based
//       archive cannot do, and its absence is exactly what let a cancelled
//       queue's still-in-memory Dispatched item hard-block re-dispatch of the
//       same bead from another queue via cross_queue_duplicate (hk-0mmy4).
//     - Otherwise (no live daemon), fall back to archiving the file directly
//       — there is no in-memory registry to reap in that case.
//  6. Emit a queue_cancel_operator event to events.jsonl (best-effort).
//
// This verb works without a live daemon — it archives the per-queue file
// directly when no daemon is reachable. Because a paused-by-failure queue has
// no active in-flight runs, the daemon will not re-persist the archived file;
// a subsequent `queue submit --queue <name>` replaces the daemon's in-memory
// slot via SetQueue (hk-fkpb7 problem (3)).
//
// Exit-code contract:
//
//	0  — queue archived (or was already absent)
//	1  — argument, I/O, or validation error
//
// Spec ref: specs/queue-model.md §2.2 (queue status lifecycle).
// Bead ref: hk-i6hhn, hk-fkpb7, hk-0mmy4.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
)

// RunQueueCancel implements `harmonik queue cancel [--project DIR] [--force]
// [--queue <name>|--queue-id <uuid>|<name>]`.
//
// It does not require a live daemon — it archives the per-queue file directly.
// Pass --force to cancel even a completed queue (unusual but allowed for
// manual cleanup).
//
// Queue resolution priority: --queue-id > --queue flag > positional arg > "main".
//
// Bead ref: hk-i6hhn (original), hk-fkpb7 (--queue / --queue-id flags).
func RunQueueCancel(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	forceFlag := false
	var queueIDFlag string
	var queueNameFlag string

	projectDir, positional, _, ok := parseQueueFlagsExtra(subArgs, errOut, func(args []string, i int) (int, bool) {
		switch {
		case args[i] == "--force":
			forceFlag = true
			return i + 1, true
		case args[i] == "--queue" && i+1 < len(args):
			queueNameFlag = args[i+1]
			return i + 2, true
		case strings.HasPrefix(args[i], "--queue="):
			queueNameFlag = strings.TrimPrefix(args[i], "--queue=")
			return i + 1, true
		case args[i] == "--queue-id" && i+1 < len(args):
			queueIDFlag = args[i+1]
			return i + 2, true
		case strings.HasPrefix(args[i], "--queue-id="):
			queueIDFlag = strings.TrimPrefix(args[i], "--queue-id=")
			return i + 1, true
		}
		// Any other "--flag" is unrecognized: parseQueueFlagsExtra rejects it
		// loudly (exit 2) rather than swallowing it as a positional (hk-snjr).
		return i, false
	})
	if !ok {
		return exitTransportError
	}

	// Resolve the target queue. UUID lookup enumerates disk files; name-based
	// lookup loads the per-name slot directly.
	var queueName string
	var existingQueue *queue.Queue

	if queueIDFlag != "" {
		q, err := cancelFindByID(ctx, projectDir, queueIDFlag)
		if err != nil {
			fmt.Fprintf(errOut, "harmonik queue cancel: --queue-id lookup: %v\n", err)
			return 1
		}
		if q == nil {
			fmt.Fprintln(out, "harmonik queue cancel: no active queue found (queue_id not found)")
			return 0
		}
		existingQueue = q
		queueName = queue.NormaliseQueueName(q.Name)
	} else {
		switch {
		case queueNameFlag != "":
			queueName = queue.NormaliseQueueName(queueNameFlag)
		case len(positional) > 0:
			queueName = queue.NormaliseQueueName(positional[0])
		default:
			queueName = queue.QueueNameMain
		}

		var loadErr error
		existingQueue, loadErr = queue.Load(ctx, projectDir, queueName)
		if loadErr != nil {
			if !errors.Is(loadErr, queue.ErrCorrupt) {
				fmt.Fprintf(errOut, "harmonik queue cancel: cannot read queue file: %v\n", loadErr)
				return 1
			}
			// Corrupt/zero-value stub (e.g. schema_version:0 left by a half-completed
			// session): archive by name even though we can't parse a queue_id (hk-9ztth).
			archivePath, archiveErr := queue.ArchiveFailedQueue(ctx, projectDir, queueName, time.Now())
			if archiveErr != nil {
				fmt.Fprintf(errOut, "harmonik queue cancel: cannot archive corrupt queue file: %v\n", archiveErr)
				return 1
			}
			fmt.Fprintf(out, "corrupt queue stub for %q archived to %s\n", queueName, archivePath)
			return 0
		}
		if existingQueue == nil {
			fmt.Fprintln(out, "harmonik queue cancel: no active queue found (queue file absent)")
			return 0
		}
	}

	if existingQueue.Status == queue.QueueStatusCompleted && !forceFlag {
		fmt.Fprintf(errOut, "harmonik queue cancel: queue %s is already completed; use --force to archive anyway\n", existingQueue.QueueID)
		return 1
	}

	// hk-0mmy4: prefer routing through a live daemon so it reaps its
	// in-memory QueueStore slot alongside the on-disk archive. Falls back to
	// the disk-only archive below when no daemon is reachable.
	if handled, exitCode := tryDaemonQueueCancel(ctx, projectDir, queueName, forceFlag, out, errOut); handled {
		return exitCode
	}

	archivePath, archiveErr := queue.ArchiveFailedQueue(ctx, projectDir, queueName, time.Now())
	if archiveErr != nil {
		fmt.Fprintf(errOut, "harmonik queue cancel: cannot archive queue.json: %v\n", archiveErr)
		return 1
	}

	fmt.Fprintf(out, "queue %s (status=%s) archived to %s\n", existingQueue.QueueID, existingQueue.Status, archivePath)

	// Best-effort: emit a queue_cancelled_operator event to events.jsonl.
	emitQueueCancelEvent(projectDir, existingQueue.QueueID, string(existingQueue.Status))

	return 0
}

// tryDaemonQueueCancel attempts to route the cancel through a live daemon's
// "queue-cancel" socket op (HandlerAdapter.HandleQueueCancel) so the daemon's
// in-memory QueueStore slot for queueName is reaped alongside the on-disk
// archive (hk-0mmy4). See the file-level doc comment for why the reap step
// matters: without it, a live daemon's in-memory copy of a cancelled queue
// keeps its dispatched item's Status at ItemStatusDispatched, hard-blocking
// re-dispatch of the same bead from another queue with cross_queue_duplicate.
//
// Returns handled=false whenever the daemon could not be reached or the
// exchange failed for any transport reason — the caller falls back to the
// disk-only archive path in that case, unchanged from pre-hk-0mmy4 behaviour.
// Returns handled=true once the daemon has authoritatively processed the
// request (success or a typed rejection such as queue_already_completed);
// exitCode is the process exit code the caller should return immediately.
//
// Bead ref: hk-0mmy4.
func tryDaemonQueueCancel(ctx context.Context, projectDir, queueName string, force bool, out, errOut io.Writer) (handled bool, exitCode int) {
	msg := struct {
		Op    string `json:"op"`
		Queue string `json:"queue"`
		Force bool   `json:"force,omitempty"`
	}{Op: "queue-cancel", Queue: queueName, Force: force}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		return false, 0
	}

	harmonikDir := harmonikDirFromProject(projectDir, io.Discard)
	if harmonikDir == "" {
		return false, 0
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		// exitDaemonDown (no live daemon) or any transport error: fall back
		// to the disk-only archive rather than failing the command outright.
		return false, 0
	}

	if !resp.Ok {
		fmt.Fprintf(errOut, "harmonik queue cancel: %s\n", resp.Error) //nolint:errcheck
		return true, 1
	}

	var result struct {
		QueueID     string `json:"queue_id"`
		PriorStatus string `json:"prior_status"`
	}
	if jsonErr := json.Unmarshal(resp.Result, &result); jsonErr != nil {
		return false, 0
	}
	if result.QueueID == "" {
		fmt.Fprintln(out, "harmonik queue cancel: no active queue found (queue file absent)") //nolint:errcheck
		return true, 0
	}

	fmt.Fprintf(out, "queue %s (status=%s) archived (daemon-reaped)\n", result.QueueID, result.PriorStatus) //nolint:errcheck
	emitQueueCancelEvent(projectDir, result.QueueID, result.PriorStatus)
	return true, 0
}

// cancelFindByID enumerates all per-queue files under projectDir and returns
// the first one whose QueueID equals queueID. Returns (nil, nil) when no match
// is found. The returned *Queue is the loaded file contents — callers use
// q.Name to derive the archive target.
//
// Bead ref: hk-fkpb7.
func cancelFindByID(ctx context.Context, projectDir, queueID string) (*queue.Queue, error) {
	names, err := queue.EnumerateQueueNames(projectDir)
	if err != nil {
		return nil, fmt.Errorf("enumerate queues: %w", err)
	}
	for _, name := range names {
		q, loadErr := queue.Load(ctx, projectDir, name)
		if loadErr != nil || q == nil {
			continue
		}
		if q.QueueID == queueID {
			return q, nil
		}
	}
	return nil, nil
}

// queueCancelOperatorEvent is the JSONL event emitted when the operator
// cancels a queue via `harmonik queue cancel`.
type queueCancelOperatorEvent struct {
	EventType   string `json:"event_type"`
	EmittedAt   string `json:"emitted_at"`
	QueueID     string `json:"queue_id"`
	PriorStatus string `json:"prior_status"`
	By          string `json:"by"`
}

// emitQueueCancelEvent appends a queue_cancelled_operator event to events.jsonl.
// Best-effort: errors are silently discarded.
func emitQueueCancelEvent(projectDir, queueID, priorStatus string) {
	evt := queueCancelOperatorEvent{
		EventType:   "queue_cancelled_operator",
		EmittedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		QueueID:     queueID,
		PriorStatus: priorStatus,
		By:          "operator",
	}
	line, err := json.Marshal(evt)
	if err != nil {
		return
	}
	line = append(line, '\n')

	eventsDir := projectDir + "/.harmonik/events"
	_ = os.MkdirAll(eventsDir, 0o755) //nolint:errcheck // best-effort
	eventsPath := eventsDir + "/events.jsonl"
	f, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644) //nolint:gosec // G304: operator-controlled project dir
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // best-effort
	_, _ = f.Write(line)             //nolint:errcheck // best-effort
}
