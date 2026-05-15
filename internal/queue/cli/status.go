package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// RunQueueStatus implements `hk queue status`.
//
// Sends a queue-status request to the daemon, prints the QueueStatusResponse
// JSON to out, and returns the PL-028c exit code.
//
// queue-status always exits 0 if the daemon is reachable (the response body
// contains {queue: null} when no queue is active, per QM-057).
//
// Flag args (subArgs is os.Args[3:]):
//
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//	--queue-id <uuid>  optional; for future filtering (currently informational)
//	--queue-id=<uuid>  equals form
//
// Spec refs:
//   - specs/process-lifecycle.md §4.4 PL-028 + PL-028c
//   - specs/queue-model.md §2.10 RECORD QueueStatusResponse, QM-057
//
// Bead ref: hk-eblue.
func RunQueueStatus(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	projectDir, _, ok := parseQueueFlags(subArgs, errOut)
	if !ok {
		return exitTransportError
	}

	// queue-status has no required positional arguments. The handler does not
	// need a params payload; we send only the "op" discriminator.
	msg := struct {
		Op string `json:"op"`
	}{Op: "queue-status"}

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
	return handleResponse(resp, out)
}
