package cli

// cancel.go — `harmonik queue cancel` subcommand implementation.
//
// Semantics (hk-i6hhn, extended hk-fkpb7, hk-0mmy4, hk-r7y5g):
//  1. Parse --project, --force, --queue, and --queue-id flags.
//  2. Refuse a selector the caller GAVE but left EMPTY — `--queue-id=`,
//     `--queue-id ""`, `--queue=`, `--queue ""`, or an empty positional — with
//     exit 2, before any queue is resolved. Giving no selector and giving an
//     empty one are different acts: the first asks for the default, the second
//     reached for one specific queue and the value never arrived (an unexpanded
//     shell variable, usually). Treating the second as the first archived the
//     default queue, which the caller had never named, and reported success
//     (hk-r7y5g).
//  3. Resolve the target queue:
//     --queue-id <uuid> → enumerate per-queue files and find by UUID.
//     --queue <name>    → load the named per-queue file.
//     <positional>      → load the named per-queue file (backward-compat).
//     (no selector)     → default to "main".
//  4. Absent queue → exit 0 when the caller selected no queue (a bare cancel
//     asserts an end state, and an absent default queue satisfies it); exit 2
//     naming the selector when the caller DID give one, by --queue / positional
//     name or by --queue-id, the same refusal `queue pause` and `queue resume`
//     make (hk-wka5o).
//  5. Refuse if the queue status is already terminal (completed) unless --force.
//  6. Cancel the queue:
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
//  7. Emit a queue_cancel_operator event to events.jsonl (best-effort).
//
// This verb works without a live daemon — it archives the per-queue file
// directly when no daemon is reachable. Because a paused-by-failure queue has
// no active in-flight runs, the daemon will not re-persist the archived file;
// a subsequent `queue submit --queue <name>` replaces the daemon's in-memory
// slot via SetQueue (hk-fkpb7 problem (3)).
//
// Exit-code contract:
//
//	0  — the queue is not running: it was archived here, or a bare cancel
//	     found no default queue, or a queue this command had already loaded was
//	     gone by the time the daemon reached it
//	1  — I/O or validation error, and a completed queue refused without --force
//	2  — argument error: an unrecognized flag, a selector given with an empty
//	     value, or a queue name or a queue_id that matches no queue
//
// Every exit-0 path prints what it did. No path reports success for a selector
// that matched nothing, and no path reads an empty selector value as "no
// selector given" — those are the same defect, once without a destructive act
// attached (hk-wka5o) and once with one (hk-r7y5g). Leaving either behind on
// any one selector would repeat it.
//
// Spec ref: specs/queue-model.md §2.2 (queue status lifecycle).
// Bead ref: hk-i6hhn, hk-fkpb7, hk-0mmy4, hk-r7y5g.

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
	// GIVEN is not the same question as NON-EMPTY, and the resolution below can
	// only read the value. `--queue-id=` and `--queue-id ""` both leave
	// queueIDFlag == "", which is indistinguishable from a caller who never
	// typed the flag unless the parse records that they did.
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
		// Any other "--flag" is unrecognized: parseQueueFlagsExtra rejects it
		// loudly (exit 2) rather than swallowing it as a positional (hk-snjr).
		return i, false
	})
	if !ok {
		return exitTransportError
	}

	// An empty selector VALUE is refused here, before any queue is resolved.
	//
	// Every one of these used to fall through the `!= ""` guards below, leave
	// namedByCaller false, and take the bare-cancel path — whose default is
	// "main". The empty positional got there by a second route:
	// NormaliseQueueName("") also returns "main". So a caller whose shell
	// variable did not expand archived the default queue, which they had never
	// named, and read a success line saying so (hk-r7y5g).
	//
	// The order matches the resolution order below: --queue-id wins, so its
	// emptiness is the one to report when more than one selector is empty.
	//
	// This does NOT touch a bare `harmonik queue cancel`. That gives no selector
	// at all, none of these arms fire, and it keeps its exit 0 on an absent main.
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

	// Resolve the target queue. UUID lookup enumerates disk files; name-based
	// lookup loads the per-name slot directly.
	var queueName string
	var existingQueue *queue.Queue

	if queueIDFlag != "" {
		foundName, q, err := cancelFindByID(ctx, projectDir, queueIDFlag)
		if err != nil {
			diag.printf("harmonik queue cancel: --queue-id lookup: %v\n", err)
			return 1
		}
		if q == nil {
			// A queue_id is the MOST explicit way a caller can name a target,
			// so a uuid that matches nothing is refused exactly as a name that
			// matches nothing is. Same error value, same exit code; only the
			// selector wording differs, because a uuid is not a name.
			return refuseUnknownCancelQueue(diag, projectDir, "", queueIDFlag)
		}
		existingQueue = q
		// The archive target is the file the lookup FOUND, not the name that
		// file declares about itself. Nothing validates the `name` field
		// against the filename, and an empty one normalises to "main", so
		// trusting it archived the default queue while reporting the selected
		// queue's id and exiting 0 (hk-r7y5g).
		queueName = foundName
	} else {
		// namedByCaller records whether the operator actually typed a queue
		// name.
		//
		// A bare `harmonik queue cancel` asserts an END STATE — "main is not
		// running" — and that state is satisfied when main was never there, the
		// way `rm -f` is satisfied by an absent file. It stays a clean exit 0.
		// A name the operator typed is a different act: it asserts something
		// about a specific queue, and when nothing matches it, the assertion
		// was wrong. That is a typo, and a typo must be an error (hk-wka5o).
		//
		// The three sibling verbs do not contradict this. `queue pause`,
		// `queue resume` and `queue recover` all REQUIRE a name and reject a
		// bare invocation with a usage error, so none of them has a bare form
		// for this to disagree with.
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
			// Corrupt/zero-value stub (e.g. schema_version:0 left by a half-completed
			// session): archive by name even though we can't parse a queue_id (hk-9ztth).
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

	// hk-0mmy4: prefer routing through a live daemon so it reaps its
	// in-memory QueueStore slot alongside the on-disk archive. Falls back to
	// the disk-only archive below when no daemon is reachable.
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

// refuseUnknownCancelQueue refuses a cancel aimed at a queue that does not
// exist, and returns the exit code the command must end on.
//
// Exactly one of queueName and queueID is set: cancel accepts both selectors
// and either one can miss. The refusal is the same either way, because the
// caller's mistake is the same either way.
//
// This is the same refusal `queue pause` and `queue resume` already make, and
// it is deliberately the SAME error value (queue.UnknownQueueError) and the
// same exit code rather than a fourth spelling of the check. Those two verbs
// refuse in the daemon, because the name they aim at only exists in the
// daemon's in-memory queue set. Cancel resolves its target from the on-disk
// per-queue files and works with no daemon at all, so the check has to live
// here — on the same reader the resolution above already used.
//
// A cancel that reports success is the same wrong answer pause used to give:
// the operator asked for a specific queue to stop, was told nothing was wrong,
// and no queue was cancelled.
//
// Bead ref: hk-wka5o. Sibling: internal/daemon refuseUnknownQueueLocked.
func refuseUnknownCancelQueue(diag *printer, projectDir, queueName, queueID string) int {
	// The refusal names the queues that DO exist so a near-miss is visible
	// without a second command. When that listing cannot be read, say so and
	// refuse with the thinner message: an unreadable queue directory is not
	// evidence that the queue the caller named exists, so it must never
	// downgrade this back into a reported success.
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

// journalCancel appends the operator cancel event to events.jsonl and warns on
// stderr if that fails. The archive it records has already landed on disk, so a
// journalling failure is reported but never changes the exit code.
func journalCancel(diag *printer, projectDir, queueID, priorStatus string) {
	if err := emitQueueCancelEvent(projectDir, queueID, priorStatus); err != nil {
		diag.printf("harmonik queue cancel: warning: could not journal cancel event: %v\n", err)
	}
}

// cancelExitOK maps a completed cancel to its exit code, downgrading it to 1
// when the confirmation line never reached stdout: a caller that scripts
// `queue cancel` reads that line, so a silently truncated report must not look
// like a clean cancel.
func cancelExitOK(report *printer) int {
	if report.failed() {
		return 1
	}
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
		// exitDaemonDown (no live daemon) or any transport error: fall back
		// to the disk-only archive rather than failing the command outright.
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
	// An empty queue_id has two causes, and they are opposite answers.
	//
	// HandleQueueCancel returns a wholly empty response — no queue_id AND no
	// prior_status — when its own load found nothing. This path is reached only
	// after the resolution above ALREADY loaded the queue, so that means the
	// queue was there when this command read it and was gone by the time the
	// daemon looked: a race with whatever removed it. Nothing was archived, and
	// saying so by name is the honest report.
	//
	// It returns a queue_id of "" WITH a prior_status when it did archive a
	// queue whose file carries no queue_id. That is a cancel that really ran,
	// and reporting it as "no active queue found" told the operator the exact
	// opposite of what happened AND skipped the journal event for it. The
	// missing id is a property of the queue file, not evidence of an absent
	// queue.
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

// cancelFindByID enumerates all per-queue files under projectDir and returns
// the first one whose QueueID equals queueID, together with the ENUMERATED name
// it was found under. Returns ("", nil, nil) when no match is found.
//
// The name is returned because the caller needs it and must not take it from
// the loaded file. queue.Load reads .harmonik/queues/<name>.json literally and
// never checks that the file's own `name` field agrees with the file it came
// out of, and neither does UnmarshalQueue. So a file whose `name` is empty or
// simply wrong sends the caller's archive step at a DIFFERENT file — and an
// empty one sends it at "main", because NormaliseQueueName("") is "main". That
// archived the default queue nobody had named, at exit 0, while printing the
// selected queue's id (hk-r7y5g).
//
// The enumerated name is the honest source: it is the file this loop actually
// opened and the file the caller therefore selected. It is used raw, not
// normalised, for the same reason — normalisation would send the archive back
// to "main" for the degenerate name the caller did not select.
//
// Bead ref: hk-fkpb7, hk-r7y5g.
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

// queueCancelOperatorEvent is the JSONL event emitted when the operator
// cancels a queue via `harmonik queue cancel`.
type queueCancelOperatorEvent struct {
	QueueID     string `json:"queue_id"`
	PriorStatus string `json:"prior_status"`
	By          string `json:"by"`
}

// emitQueueCancelEvent appends a queue_cancelled_operator event to events.jsonl.
//
// Best-effort by design: the cancel itself has already succeeded on disk by the
// time this runs, so a failure to journal it must not fail the command. The
// error is nonetheless RETURNED rather than discarded, so a caller (and the
// tests) can tell a journalled cancel from an unjournalled one; RunQueueCancel
// deliberately ignores it and still exits 0.
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
	// 0o644 matches internal/eventbus's JSONL writer: events.jsonl is an
	// append-only journal written by BOTH the daemon and this CLI path, and a
	// tighter mode here would give the file different perms depending on which
	// writer happened to create it.
	f, openErr := os.OpenFile(eventsPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644) //nolint:gosec // G304: eventsPath is derived from the operator's --project dir, so it is a runtime value by construction and G304 fires however it is validated
	if openErr != nil {
		return fmt.Errorf("open %q: %w", eventsPath, openErr)
	}
	// A failed Close can mean the appended line never reached the disk, so it
	// joins the result rather than being dropped in favour of the write error.
	defer func() { err = errors.Join(err, f.Close()) }()

	if _, writeErr := f.Write(line); writeErr != nil {
		return fmt.Errorf("append to %q: %w", eventsPath, writeErr)
	}
	return nil
}
