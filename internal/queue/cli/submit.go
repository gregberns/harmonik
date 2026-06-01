package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// RunQueueSubmit implements `hk queue submit [--queue <name>] [--beads hk-a,hk-b | <queue-file>]`.
//
// Accepts either:
//   - --beads hk-a,hk-b (or positional bead IDs): builds a minimal
//     stream-group QueueSubmitRequest from the given bead IDs.
//   - <queue-file>: reads a QueueSubmitRequest JSON document from the file.
//
// When --queue <name> is given, the request is routed to that named queue
// (auto-created on first submit). When absent, routes to "main" (QueueNameMain).
//
// Sends the request to the daemon via the Unix socket, prints the
// QueueSubmitResponse to out (human-readable by default; JSON with --json or
// --format json), and returns the PL-028c exit code.
//
// Flag args (subArgs is os.Args[3:]):
//
//	--queue <name>           target queue name (default: main)
//	--queue=<name>           equals form
//	--beads hk-a,hk-b[,...] comma-separated bead IDs (expands to stream group)
//	--beads hk-a --beads hk-b repeated form; accumulates across flags
//	--project <dir>          project directory (default: cwd)
//	--project=<dir>          equals form
//	--json                   output raw JSON (shorthand for --format json)
//	--format json|text       output format (default text)
//
// Spec refs:
//   - specs/process-lifecycle.md §4.4 PL-028 + PL-028c
//   - specs/queue-model.md §2.10 RECORD QueueSubmitRequest / QueueSubmitResponse
//
// Bead ref: hk-eblue, hk-m9a7g, hk-tigaf.8.
func RunQueueSubmit(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	var beadIDs []string
	var queueName string
	projectDir, positional, outputJSON, ok := parseQueueFlagsExtra(subArgs, errOut, func(args []string, i int) (int, bool) {
		switch {
		case args[i] == "--beads" && i+1 < len(args):
			beadIDs = append(beadIDs, parseBeadsFlag(args[i+1])...)
			return i + 2, true
		case strings.HasPrefix(args[i], "--beads="):
			beadIDs = append(beadIDs, parseBeadsFlag(strings.TrimPrefix(args[i], "--beads="))...)
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

	var queueDoc map[string]json.RawMessage

	switch {
	case len(beadIDs) > 0:
		// --beads flag: synthesise a minimal stream-group request.
		var buildErr error
		queueDoc, buildErr = beadsToQueueDoc(beadIDs, queueName)
		if buildErr != nil {
			fmt.Fprintf(errOut, "harmonik queue submit: cannot build queue doc: %v\n", buildErr)
			return exitTransportError
		}

	case len(positional) > 0:
		// Positional argument: treat as a queue-file path.
		queueFile := positional[0]
		//nolint:gosec // G304: path comes from operator CLI argument
		data, err := os.ReadFile(queueFile)
		if err != nil {
			fmt.Fprintf(errOut, "harmonik queue submit: cannot read %q: %v\n", queueFile, err)
			return exitTransportError
		}
		if jsonErr := json.Unmarshal(data, &queueDoc); jsonErr != nil {
			fmt.Fprintf(errOut, "harmonik queue submit: invalid JSON in %q: %v\n", queueFile, jsonErr)
			return exitTransportError
		}
		// Inject --queue name into file-based doc when provided.
		if queueName != "" {
			nameBytes, _ := json.Marshal(queueName) //nolint:errcheck // string; cannot fail
			queueDoc["name"] = nameBytes
		}

	default:
		fmt.Fprintln(errOut, "harmonik queue submit: missing argument; use --beads hk-a,hk-b or provide a <queue-file>")
		return exitTransportError
	}

	// Embed the queue document in a socket request envelope.
	// The server's HandlerAdapter.HandleQueueSubmit unmarshals the params
	// (the entire SocketRequest JSON) into a QueueSubmitRequest, so we merge
	// the queue document fields with the "op" field at the top level.
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
