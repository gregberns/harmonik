package cli

// cancel.go — `harmonik queue cancel` subcommand implementation.
//
// Semantics (hk-i6hhn):
//  1. Parse --project and --force flags.
//  2. Load .harmonik/queue.json; absent queue → exit 0 (nothing to cancel).
//  3. Refuse if the queue status is already terminal (completed) unless --force.
//  4. Archive queue.json to .harmonik/queue.json.cancelled-<timestamp> so the
//     next harmonik run invocation finds no active queue.
//  5. Emit a queue_cancel_operator event to events.jsonl (best-effort).
//
// This verb works entirely without a live daemon — it manipulates queue.json
// directly. It is intended for the "killed wedged daemon left status=active"
// recovery case (hk-i6hhn).
//
// Exit-code contract:
//
//	0  — queue archived (or was already absent)
//	1  — argument, I/O, or validation error
//
// Spec ref: specs/queue-model.md §2.2 (queue status lifecycle).
// Bead ref: hk-i6hhn.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
)

// RunQueueCancel implements `harmonik queue cancel [--project DIR] [--force]`.
//
// It does not require a live daemon — it archives queue.json directly.
// Pass --force to cancel even a completed queue (unusual but allowed for
// manual cleanup).
func RunQueueCancel(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	forceFlag := false

	projectDir, positional, _, ok := parseQueueFlagsExtra(subArgs, errOut, func(args []string, i int) (int, bool) {
		switch {
		case args[i] == "--force":
			forceFlag = true
			return i + 1, true
		case strings.HasPrefix(args[i], "--"):
			// Unknown flag — let parseQueueFlagsExtra handle as positional.
		}
		return i, false
	})
	if !ok {
		return 1
	}

	// If the caller supplied a queue name as a positional argument, use it.
	// Otherwise default to "main" so the no-arg form stays backward-compatible.
	queueName := queue.QueueNameMain
	if len(positional) > 0 {
		queueName = queue.NormaliseQueueName(positional[0])
	}

	existingQueue, loadErr := queue.Load(ctx, projectDir, queueName)
	if loadErr != nil {
		fmt.Fprintf(errOut, "harmonik queue cancel: cannot read queue file: %v\n", loadErr)
		return 1
	}
	if existingQueue == nil {
		fmt.Fprintln(out, "harmonik queue cancel: no active queue found (queue file absent)")
		return 0
	}

	if existingQueue.Status == queue.QueueStatusCompleted && !forceFlag {
		fmt.Fprintf(errOut, "harmonik queue cancel: queue %s is already completed; use --force to archive anyway\n", existingQueue.QueueID)
		return 1
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
