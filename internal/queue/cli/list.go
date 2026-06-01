package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// RunQueueList implements `hk queue list`.
//
// Sends a queue-list request to the daemon, prints the QueueListResponse to
// out (human-readable by default; JSON with --json or --format json), and
// returns the PL-028c exit code.
//
// queue-list exits 0 when the daemon is reachable, even when no queues are
// active (the response carries an empty Queues slice).
//
// Flag args (subArgs is os.Args[3:]):
//
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//	--json             output raw JSON (shorthand for --format json)
//	--format json|text output format (default text)
//
// Bead ref: hk-tigaf.8.
func RunQueueList(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	projectDir, _, outputJSON, ok := parseQueueFlags(subArgs, errOut)
	if !ok {
		return exitTransportError
	}

	msg := struct {
		Op string `json:"op"`
	}{Op: "queue-list"}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		fmt.Fprintf(errOut, "harmonik queue list: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			fmt.Fprintln(errOut, "harmonik queue list: daemon not running (no socket at "+harmonikDir+"/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, renderQueueListText)
}

// renderQueueListText prints a human-readable summary of a QueueListResponse.
func renderQueueListText(result json.RawMessage, out io.Writer) int {
	var resp struct {
		Queues []struct {
			Name           string `json:"name"`
			QueueID        string `json:"queue_id"`
			Status         string `json:"status"`
			PendingItems   int    `json:"pending_items"`
			Workers        int    `json:"workers"`
			CompletedItems int    `json:"completed_items"`
			FailedItems    int    `json:"failed_items"`
		} `json:"queues"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		_, _ = fmt.Fprintf(out, "%s\n", result) //nolint:errcheck
		return exitSuccess
	}

	if len(resp.Queues) == 0 {
		_, _ = fmt.Fprintln(out, "(no active queues)") //nolint:errcheck
		return exitSuccess
	}

	for _, q := range resp.Queues {
		_, _ = fmt.Fprintf(out, "%-20s  %-20s  status=%-18s  pending=%d  workers=%d  completed=%d  failed=%d\n",
			q.Name, q.QueueID, q.Status,
			q.PendingItems, q.Workers, q.CompletedItems, q.FailedItems,
		) //nolint:errcheck
	}
	return exitSuccess
}
