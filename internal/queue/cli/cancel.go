package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
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
// Only the ABSENCE of every selector reaches that "main" default — a selector
// given with an empty value is refused first (exit 2), because the caller aimed
// at one specific queue and the value did not arrive (hk-r7y5g).
//
// Bead ref: hk-i6hhn (original), hk-fkpb7 (--queue / --queue-id flags),
// hk-r7y5g (empty selector value).
func RunQueueCancel(ctx context.Context, subArgs []string, out, errOut io.Writer) int {
	diag := newPrinter(errOut)
	report := newPrinter(out)
	forceFlag := false
	var queueIDFlag string
	var queueNameFlag string
	queueIDGiven := false
	queueNameGiven := false

	projectDir, positional, _, ok := parseQueueFlagsExtra(subArgs, errOut, func(args []string, i int) (int, bool) {
		switch {
		case args[i] == "--force":
			forceFlag = true
			return i + 1, true
		case args[i] == "--queue" && i+1 < len(args):
			queueNameFlag = args[i+1]
			queueNameGiven = true
			return i + 2, true
		case strings.HasPrefix(args[i], "--queue="):
			queueNameFlag = strings.TrimPrefix(args[i], "--queue=")
			queueNameGiven = true
			return i + 1, true
		case args[i] == "--queue-id" && i+1 < len(args):
			queueIDFlag = args[i+1]
			queueIDGiven = true
			return i + 2, true
		case strings.HasPrefix(args[i], "--queue-id="):
			queueIDFlag = strings.TrimPrefix(args[i], "--queue-id=")
			queueIDGiven = true
			return i + 1, true
		}
		return i, false
	})
	if !ok {
		return exitTransportError
	}

	switch {
	case queueIDGiven && queueIDFlag == "":
		diag.println("harmonik queue cancel: --queue-id was given an empty value; pass a queue_id or drop the flag. Nothing was cancelled.")
		return exitTransportError
	case queueNameGiven && queueNameFlag == "":
		diag.println("harmonik queue cancel: --queue was given an empty value; pass a queue name or drop the flag. Nothing was cancelled.")
		return exitTransportError
	case len(positional) > 0 && positional[0] == "":
		diag.println("harmonik queue cancel: the queue name argument was empty; pass a queue name or drop the argument. Nothing was cancelled.")
		return exitTransportError
	}

	var queueName string
	var existingQueue *queue.Queue

	if queueIDFlag != "" {
		foundName, q, err := cancelFindByID(ctx, projectDir, queueIDFlag)
		if err != nil {
			diag.printf("harmonik queue cancel: --queue-id lookup: %v\n", err)
			return 1
		}
		if q == nil {
			return refuseUnknownCancelQueue(diag, projectDir, "", queueIDFlag)
		}
		existingQueue = q
		queueName = foundName
	} else {
		namedByCaller := true
		switch {
		case queueNameFlag != "":
			queueName = queue.NormaliseQueueName(queueNameFlag)
		case len(positional) > 0:
			queueName = queue.NormaliseQueueName(positional[0])
		default:
			queueName = queue.QueueNameMain
			namedByCaller = false
		}

		var loadErr error
		existingQueue, loadErr = queue.Load(ctx, projectDir, queueName)
		if loadErr != nil {
			if !errors.Is(loadErr, queue.ErrCorrupt) {
				diag.printf("harmonik queue cancel: cannot read queue file: %v\n", loadErr)
				return 1
			}
			archivePath, archiveErr := queue.ArchiveFailedQueue(ctx, projectDir, queueName, time.Now())
			if archiveErr != nil {
				diag.printf("harmonik queue cancel: cannot archive corrupt queue file: %v\n", archiveErr)
				return 1
			}
			report.printf("corrupt queue stub for %q archived to %s\n", queueName, archivePath)
			return cancelExitOK(report)
		}
		if existingQueue == nil {
			if namedByCaller {
				return refuseUnknownCancelQueue(diag, projectDir, queueName, "")
			}
			report.println("harmonik queue cancel: no active queue found (queue file absent)")
			return cancelExitOK(report)
		}
	}

	if existingQueue.Status == queue.QueueStatusCompleted && !forceFlag {
		diag.printf("harmonik queue cancel: queue %s is already completed; use --force to archive anyway\n", existingQueue.QueueID)
		return 1
	}

	if handled, exitCode := tryDaemonQueueCancel(ctx, projectDir, queueName, forceFlag, out, errOut); handled {
		return exitCode
	}

	archivePath, archiveErr := queue.ArchiveFailedQueue(ctx, projectDir, queueName, time.Now())
	if archiveErr != nil {
		diag.printf("harmonik queue cancel: cannot archive queue.json: %v\n", archiveErr)
		return 1
	}

	report.printf("queue %s (status=%s) archived to %s\n", existingQueue.QueueID, existingQueue.Status, archivePath)

	journalCancel(diag, projectDir, existingQueue.QueueID, string(existingQueue.Status))

	return cancelExitOK(report)
}

func refuseUnknownCancelQueue(diag *printer, projectDir, queueName, queueID string) int {
	known, enumErr := queue.EnumerateQueueNames(projectDir)
	if enumErr != nil {
		diag.printf("harmonik queue cancel: cannot list the queues that exist: %v\n", enumErr)
		known = nil
	}
	err := &queue.UnknownQueueError{
		Verb:           "cancel",
		PastTense:      "cancelled",
		NormalizedName: queueName,
		QueueID:        queueID,
		KnownNames:     known,
	}
	diag.printf("harmonik queue cancel: %s\n", err)
	return exitTransportError
}

func journalCancel(diag *printer, projectDir, queueID, priorStatus string) {
	if err := emitQueueCancelEvent(projectDir, queueID, priorStatus); err != nil {
		diag.printf("harmonik queue cancel: warning: could not journal cancel event: %v\n", err)
	}
}

func cancelExitOK(report *printer) int {
	if report.failed() {
		return 1
	}
	return 0
}

func tryDaemonQueueCancel(ctx context.Context, projectDir, queueName string, force bool, out, errOut io.Writer) (handled bool, exitCode int) {
	diag := newPrinter(errOut)
	report := newPrinter(out)
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
		return false, 0
	}

	if !resp.Ok {
		diag.printf("harmonik queue cancel: %s\n", resp.Error)
		return true, 1
	}

	var result struct {
		QueueID     string `json:"queue_id"`
		PriorStatus string `json:"prior_status"`
	}
	if jsonErr := json.Unmarshal(resp.Result, &result); jsonErr != nil {
		return false, 0
	}
	if result.QueueID == "" && result.PriorStatus == "" {
		report.printf("harmonik queue cancel: queue %q was gone before the daemon reached it; nothing was archived\n", queueName)
		return true, cancelExitOK(report)
	}

	archivedID := result.QueueID
	if archivedID == "" {
		archivedID = "(the queue file carries no queue_id)"
	}
	report.printf("queue %s (status=%s) archived (daemon-reaped)\n", archivedID, result.PriorStatus)
	journalCancel(diag, projectDir, result.QueueID, result.PriorStatus)
	return true, cancelExitOK(report)
}

func cancelFindByID(ctx context.Context, projectDir, queueID string) (string, *queue.Queue, error) {
	names, err := queue.EnumerateQueueNames(projectDir)
	if err != nil {
		return "", nil, fmt.Errorf("enumerate queues: %w", err)
	}
	for _, name := range names {
		q, loadErr := queue.Load(ctx, projectDir, name)
		if loadErr != nil || q == nil {
			continue
		}
		if q.QueueID == queueID {
			return name, q, nil
		}
	}
	return "", nil, nil
}

type queueCancelOperatorEvent struct {
	QueueID     string `json:"queue_id"`
	PriorStatus string `json:"prior_status"`
	By          string `json:"by"`
}

func emitQueueCancelEvent(projectDir, queueID, priorStatus string) (err error) {
	payload, err := json.Marshal(queueCancelOperatorEvent{
		QueueID:     queueID,
		PriorStatus: priorStatus,
		By:          "operator",
	})
	if err != nil {
		return fmt.Errorf("marshal queue_cancelled_operator payload: %w", err)
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("new queue_cancelled_operator event ID: %w", err)
	}
	event := core.Event{
		EventID:       core.EventID(eventID),
		SchemaVersion: 1,
		Type:          "queue_cancelled_operator",
		TimestampWall: time.Now().UTC(),
		// Queue owns this CLI fallback writer and registers its subsystem ID.
		SourceSubsystem: "github.com/gregberns/harmonik/internal/queue",
		Payload:         payload,
	}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal queue_cancelled_operator event: %w", err)
	}
	line = append(line, '\n')

	eventsDir := projectDir + "/.harmonik/events"
	if mkErr := os.MkdirAll(eventsDir, core.HarmonikDirMode); mkErr != nil {
		return fmt.Errorf("mkdir %q: %w", eventsDir, mkErr)
	}
	eventsPath := eventsDir + "/events.jsonl"
	f, openErr := os.OpenFile(eventsPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644) //nolint:gosec // G304: eventsPath is derived from the operator's --project dir, so it is a runtime value by construction and G304 fires however it is validated
	if openErr != nil {
		return fmt.Errorf("open %q: %w", eventsPath, openErr)
	}
	defer func() { err = errors.Join(err, f.Close()) }()

	if _, writeErr := f.Write(line); writeErr != nil {
		return fmt.Errorf("append to %q: %w", eventsPath, writeErr)
	}
	return nil
}
