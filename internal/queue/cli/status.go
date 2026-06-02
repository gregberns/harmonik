package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// RunQueueStatus implements `hk queue status`.
//
// Sends a queue-status request to the daemon, prints the QueueStatusResponse
// to out (human-readable by default; JSON with --json or --format json), and
// returns the PL-028c exit code.
//
// queue-status always exits 0 if the daemon is reachable (the response body
// contains {queue: null} when no queue is active, per QM-057).
//
// Flag args (subArgs is os.Args[3:]):
//
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//	--queue <name>     show status of the named queue (default: main)
//	--queue=<name>     equals form
//	--queue-id <uuid>  find and show the queue with this queue_id
//	--queue-id=<uuid>  equals form
//	--json             output raw JSON (shorthand for --format json)
//	--format json|text output format (default text)
//
// Spec refs:
//   - specs/process-lifecycle.md §4.4 PL-028 + PL-028c
//   - specs/queue-model.md §2.10 RECORD QueueStatusResponse, QM-057
//
// Bead ref: hk-eblue, hk-1k5as.
func RunQueueStatus(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	var queueID string
	var queueName string
	projectDir, _, outputJSON, ok := parseQueueFlagsExtra(subArgs, errOut, func(args []string, i int) (int, bool) {
		switch {
		case args[i] == "--queue-id" && i+1 < len(args):
			queueID = args[i+1]
			return i + 2, true
		case len(args[i]) > len("--queue-id=") && args[i][:len("--queue-id=")] == "--queue-id=":
			queueID = args[i][len("--queue-id="):]
			return i + 1, true
		case args[i] == "--queue" && i+1 < len(args):
			queueName = args[i+1]
			return i + 2, true
		case strings.HasPrefix(args[i], "--queue="):
			queueName = strings.TrimPrefix(args[i], "--queue=")
			return i + 1, true
		}
		return i, false
	})
	if !ok {
		return exitTransportError
	}

	// Build the request payload. Include name/queue_id when provided so the
	// daemon routes to the correct named queue (hk-1k5as).
	type statusPayload struct {
		Op      string `json:"op"`
		Name    string `json:"name,omitempty"`
		QueueID string `json:"queue_id,omitempty"`
	}
	msg := statusPayload{
		Op:      "queue-status",
		Name:    queueName,
		QueueID: queueID,
	}

	payload, marshalErr := json.Marshal(msg)
	if marshalErr != nil {
		fmt.Fprintf(errOut, "harmonik queue status: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			fmt.Fprintln(errOut, "harmonik queue status: daemon not running (no socket at "+harmonikDir+"/daemon.sock)")
		}
		return earlyExit
	}

	// queue-status always exits 0 when the daemon is reachable, even when the
	// queue is null. The response carries {queue: null} in that case per QM-057.
	return handleResponse(resp, out, outputJSON, renderQueueStatusText)
}
