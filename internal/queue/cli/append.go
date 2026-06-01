package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RunQueueAppend implements `hk queue append <group-index> <bead-id ...>`.
//
// Parses the positional arguments (group-index followed by one or more bead
// IDs), sends a queue-append request to the daemon via the Unix socket, and
// prints the QueueAppendResponse to out (human-readable by default; JSON with
// --json or --format json).
//
// Flag args (subArgs is os.Args[3:]):
//
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//	--queue-id <uuid>  active queue identity guard (mutually exclusive with --queue)
//	--queue-id=<uuid>  equals form
//	--queue <name>     resolve append target by queue name (default: main)
//	--queue=<name>     equals form
//	--json             output raw JSON (shorthand for --format json)
//	--format json|text output format (default text)
//
// Spec refs:
//   - specs/process-lifecycle.md §4.4 PL-028 + PL-028c
//   - specs/queue-model.md §2.10 RECORD QueueAppendRequest / QueueAppendResponse
//   - specs/queue-model.md §7 (append validation: QM-024)
//
// Bead ref: hk-eblue, hk-tigaf.8.
func RunQueueAppend(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	var queueID string
	var queueName string
	projectDir, positional, outputJSON, ok := parseQueueFlagsExtra(subArgs, errOut, func(args []string, i int) (int, bool) {
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

	if len(positional) < 2 {
		fmt.Fprintln(errOut, "harmonik queue append: usage: hk queue append [--queue-id <uuid>|--queue <name>] <group-index> <bead-id ...>")
		return exitTransportError
	}

	groupIdx, parseErr := strconv.Atoi(positional[0])
	if parseErr != nil {
		fmt.Fprintf(errOut, "harmonik queue append: invalid group-index %q: %v\n", positional[0], parseErr)
		return exitTransportError
	}
	beadIDs := positional[1:]

	// Build the socket request envelope. The HandlerAdapter.HandleQueueAppend
	// unmarshals the entire SocketRequest into QueueAppendRequest, so we merge
	// queue_id, name, group_index, and bead_ids with the "op" field.
	type appendPayload struct {
		Op         string   `json:"op"`
		QueueID    string   `json:"queue_id,omitempty"`
		Name       string   `json:"name,omitempty"`
		GroupIndex int      `json:"group_index"`
		BeadIDs    []string `json:"bead_ids"`
	}
	msg := appendPayload{
		Op:         "queue-append",
		QueueID:    queueID,
		Name:       queueName,
		GroupIndex: groupIdx,
		BeadIDs:    beadIDs,
	}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		fmt.Fprintf(errOut, "harmonik queue append: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			fmt.Fprintln(errOut, "harmonik queue append: daemon not running (no socket at "+harmonikDir+"/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, renderQueueAppendText)
}
