package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
)

// RunQueueDryRun implements `hk queue dry-run [--queue <name>] [--beads hk-a,hk-b | <queue-file>]`.
//
// Accepts either:
//   - --beads hk-a,hk-b (or positional bead IDs): builds a minimal
//     stream-group QueueDryRunRequest from the given bead IDs.
//   - <queue-file>: reads a QueueDryRunRequest JSON document from the file.
//
// When --queue <name> is given, the request is validated against that named
// queue's per-name single-active slot (QM-027) rather than defaulting to
// "main". When absent, defaults to "main" (QueueNameMain).
//
// Sends the request to the daemon WITHOUT mutating state or emitting events
// (per QM-028), prints the QueueDryRunResponse to out (human-readable by
// default; JSON with --json or --format json), and returns the PL-028c exit
// code.
//
// Flag args (subArgs is os.Args[3:]):
//
//	--queue <name>               target queue name (default: main)
//	--queue=<name>               equals form
//	--beads hk-a,hk-b[,...]     comma-separated bead IDs (expands to stream group)
//	--beads hk-a --beads hk-b   repeated form; accumulates across flags
//	--workflow-mode <mode>       per-item workflow mode (default: review-loop); stamped on each
//	                             minted item so the queue.json record is self-describing (hk-tldws)
//	--workflow-mode=<mode>       equals form
//	--project <dir>              project directory (default: cwd)
//	--project=<dir>              equals form
//	--json                       output raw JSON (shorthand for --format json)
//	--format json|text           output format (default text)
//
// Spec refs:
//   - specs/process-lifecycle.md §4.4 PL-028 + PL-028c
//   - specs/queue-model.md §2.10 RECORD QueueDryRunRequest / QueueDryRunResponse
//   - specs/queue-model.md §6 QM-028 (validate-only, no persist, no events)
//
// Bead ref: hk-eblue, hk-m9a7g, hk-40r9b, hk-tldws.
func RunQueueDryRun(ctx context.Context, subArgs []string, out, errOut io.Writer) int {
	diag := newPrinter(errOut)
	var beadIDs []string
	var queueName string
	workflowMode := "review-loop" // default: review-loop per hk-g0ckv / hk-rssrg / hk-tldws
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
		case args[i] == "--workflow-mode" && i+1 < len(args):
			workflowMode = args[i+1]
			return i + 2, true
		case strings.HasPrefix(args[i], "--workflow-mode="):
			workflowMode = strings.TrimPrefix(args[i], "--workflow-mode=")
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
		var buildErr error
		queueDoc, buildErr = beadsToQueueDoc(beadIDs, queueName, workflowMode)
		if buildErr != nil {
			diag.printf("harmonik queue dry-run: cannot build queue doc: %v\n", buildErr)
			return exitTransportError
		}

	case len(positional) > 0:
		// Positional argument: treat as a queue-file path.
		var loaded bool
		queueDoc, loaded = loadQueueDocFromFile("dry-run", positional[0], queueName, diag)
		if !loaded {
			return exitTransportError
		}

	default:
		diag.println("harmonik queue dry-run: missing argument; use --beads hk-a,hk-b or provide a <queue-file>")
		return exitTransportError
	}

	// Embed the queue document in a socket request envelope.
	// The server's HandlerAdapter.HandleQueueDryRun unmarshals the params
	// (the entire SocketRequest JSON) into a QueueDryRunRequest.
	payload, marshalErr := encodeEnvelope("queue-dry-run", queueDoc)
	if marshalErr != nil {
		diag.printf("harmonik queue dry-run: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			diag.println("harmonik queue dry-run: daemon not running (no socket at " + harmonikDir + "/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, renderQueueDryRunText)
}
