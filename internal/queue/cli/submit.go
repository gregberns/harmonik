package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// RunQueueSubmit implements `hk queue submit <queue-file>`.
//
// Reads a QueueSubmitRequest JSON document from queueFile, sends it to the
// daemon via the Unix socket, prints the QueueSubmitResponse to out (human-
// readable by default; JSON with --json or --format json), and returns the
// PL-028c exit code.
//
// Flag args (subArgs is os.Args[3:]):
//
//	--project <dir>         project directory (default: cwd)
//	--project=<dir>         equals form
//	--json                  output raw JSON (shorthand for --format json)
//	--format json|text      output format (default text)
//
// Spec refs:
//   - specs/process-lifecycle.md §4.4 PL-028 + PL-028c
//   - specs/queue-model.md §2.10 RECORD QueueSubmitRequest / QueueSubmitResponse
//
// Bead ref: hk-eblue.
func RunQueueSubmit(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	projectDir, positional, outputJSON, ok := parseQueueFlags(subArgs, errOut)
	if !ok {
		return exitTransportError
	}

	if len(positional) == 0 {
		fmt.Fprintln(errOut, "harmonik queue submit: missing <queue-file> argument")
		return exitTransportError
	}
	queueFile := positional[0]

	// Read and validate the queue file.
	//nolint:gosec // G304: path comes from operator CLI argument
	data, err := os.ReadFile(queueFile)
	if err != nil {
		fmt.Fprintf(errOut, "harmonik queue submit: cannot read %q: %v\n", queueFile, err)
		return exitTransportError
	}

	// Embed the queue file payload in a socket request envelope.
	// The server's HandlerAdapter.HandleQueueSubmit unmarshals the params
	// (the entire SocketRequest JSON) into a QueueSubmitRequest, so we merge
	// the queue document fields with the "op" field at the top level.
	var queueDoc map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &queueDoc); jsonErr != nil {
		fmt.Fprintf(errOut, "harmonik queue submit: invalid JSON in %q: %v\n", queueFile, jsonErr)
		return exitTransportError
	}

	envelope := buildEnvelope("queue-submit", queueDoc)
	payload, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		fmt.Fprintf(errOut, "harmonik queue submit: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			fmt.Fprintln(errOut, "harmonik queue submit: daemon not running (no socket at "+harmonikDir+"/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, renderQueueSubmitText)
}
