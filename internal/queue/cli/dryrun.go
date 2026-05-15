package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// RunQueueDryRun implements `hk queue dry-run <queue-file>`.
//
// Reads a QueueDryRunRequest JSON document from queueFile, sends it to the
// daemon via the Unix socket WITHOUT mutating state or emitting events (per
// QM-028), prints the QueueDryRunResponse JSON to out, and returns the
// PL-028c exit code.
//
// Flag args (subArgs is os.Args[3:]):
//
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//
// Spec refs:
//   - specs/process-lifecycle.md §4.4 PL-028 + PL-028c
//   - specs/queue-model.md §2.10 RECORD QueueDryRunRequest / QueueDryRunResponse
//   - specs/queue-model.md §6 QM-028 (validate-only, no persist, no events)
//
// Bead ref: hk-eblue.
func RunQueueDryRun(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	projectDir, positional, ok := parseQueueFlags(subArgs, errOut)
	if !ok {
		return exitTransportError
	}

	if len(positional) == 0 {
		fmt.Fprintln(errOut, "harmonik queue dry-run: missing <queue-file> argument")
		return exitTransportError
	}
	queueFile := positional[0]

	// Read and validate the queue file.
	//nolint:gosec // G304: path comes from operator CLI argument
	data, err := os.ReadFile(queueFile)
	if err != nil {
		fmt.Fprintf(errOut, "harmonik queue dry-run: cannot read %q: %v\n", queueFile, err)
		return exitTransportError
	}

	// Embed the queue file payload in a socket request envelope.
	// The server's HandlerAdapter.HandleQueueDryRun unmarshals the params
	// (the entire SocketRequest JSON) into a QueueDryRunRequest.
	var queueDoc map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &queueDoc); jsonErr != nil {
		fmt.Fprintf(errOut, "harmonik queue dry-run: invalid JSON in %q: %v\n", queueFile, jsonErr)
		return exitTransportError
	}

	envelope := buildEnvelope("queue-dry-run", queueDoc)
	payload, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		fmt.Fprintf(errOut, "harmonik queue dry-run: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			fmt.Fprintln(errOut, "harmonik queue dry-run: daemon not running (no socket at "+harmonikDir+"/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out)
}
