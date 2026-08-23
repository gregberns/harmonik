package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
)

// RunQueueResume implements `hk queue resume <name>`.
//
// Sends an operator-resume request scoped to the named queue to the daemon via
// the Unix socket. The daemon's OperatorPauseController emits
// operator_resuming events; the QueueOperatorEventConsumer transitions the
// named queue from paused-by-drain back to active.
//
// Flag args (subArgs is os.Args[3:]):
//
//	<name>             required positional: the queue name to resume
//	--queue <name>     the queue name to resume (flag form, alternative to positional)
//	--queue=<name>     equals form
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//	--json             output raw JSON (shorthand for --format json)
//	--format json|text output format (default text)
//
// A selector given with an EMPTY value is refused (exit 2) rather than read
// as the daemon's global scope. Only the absence of every selector reaches
// the usage error. See the refusal block below (hk-wki4e).
//
// Bead ref: hk-tigaf.8.
func RunQueueResume(ctx context.Context, subArgs []string, out, errOut io.Writer) int {
	diag := newPrinter(errOut)
	var queueName string
	queueNameGiven := false
	projectDir, positional, outputJSON, ok := parseQueueFlagsExtra(subArgs, errOut, func(args []string, i int) (int, bool) {
		switch {
		case args[i] == "--queue" && i+1 < len(args):
			queueName = args[i+1]
			queueNameGiven = true
			return i + 2, true
		case strings.HasPrefix(args[i], "--queue="):
			queueName = strings.TrimPrefix(args[i], "--queue=")
			queueNameGiven = true
			return i + 1, true
		}
		return i, false
	})
	if !ok {
		return exitTransportError
	}

	switch {
	case queueNameGiven && queueName == "":
		diag.println("harmonik queue resume: --queue was given an empty value; pass a queue name or drop the flag. Nothing was resumed.")
		return exitTransportError
	case len(positional) > 0 && positional[0] == "":
		diag.println("harmonik queue resume: the queue name argument was empty; pass a queue name or drop the argument. Nothing was resumed.")
		return exitTransportError
	}

	if queueName == "" {
		if len(positional) < 1 {
			diag.println("harmonik queue resume: usage: hk queue resume <name>")
			return exitTransportError
		}
		queueName = positional[0]
	}

	msg := struct {
		Op    string `json:"op"`
		Queue string `json:"queue"`
	}{Op: "operator-resume", Queue: queueName}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		diag.printf("harmonik queue resume: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			diag.println("harmonik queue resume: daemon not running (no socket at " + harmonikDir + "/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, func(_ json.RawMessage, w io.Writer) int {
		p := newPrinter(w)
		p.printf("resumed: %s\n", queueName)
		return renderExit(p)
	})
}
