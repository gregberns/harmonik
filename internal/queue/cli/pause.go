package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// RunQueuePause implements `hk queue pause <name>`.
//
// Sends an operator-pause request scoped to the named queue to the daemon via
// the Unix socket. The daemon's OperatorPauseController emits
// operator_pause_status events; the QueueOperatorEventConsumer transitions
// the named queue to paused-by-drain.
//
// Flag args (subArgs is os.Args[3:]):
//
//	<name>             required positional: the queue name to pause
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//	--json             output raw JSON (shorthand for --format json)
//	--format json|text output format (default text)
//
// Bead ref: hk-tigaf.8.
func RunQueuePause(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	projectDir, positional, outputJSON, ok := parseQueueFlags(subArgs, errOut)
	if !ok {
		return exitTransportError
	}

	if len(positional) < 1 {
		fmt.Fprintln(errOut, "harmonik queue pause: usage: hk queue pause <name>")
		return exitTransportError
	}
	queueName := positional[0]

	msg := struct {
		Op    string `json:"op"`
		Queue string `json:"queue"`
	}{Op: "operator-pause", Queue: queueName}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		fmt.Fprintf(errOut, "harmonik queue pause: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			fmt.Fprintln(errOut, "harmonik queue pause: daemon not running (no socket at "+harmonikDir+"/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, func(_ json.RawMessage, w io.Writer) int {
		_, _ = fmt.Fprintf(w, "paused: %s\n", queueName) //nolint:errcheck
		return exitSuccess
	})
}
