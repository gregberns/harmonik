package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
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
//	--queue <name>     the queue name to pause (flag form, alternative to positional)
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
func RunQueuePause(ctx context.Context, subArgs []string, out, errOut io.Writer) int {
	diag := newPrinter(errOut)
	var queueName string
	// GIVEN is not the same question as NON-EMPTY. `--queue=` and
	// `--queue ""` both leave queueName == "", which the resolution below
	// cannot tell apart from a caller who never typed the flag unless the
	// parse records that they did.
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

	// An empty selector VALUE is refused here, before the request is built.
	//
	// An empty queue name is not a name the daemon rejects — its
	// `refuseUnknownQueueLocked` returns early on "" without looking it up.
	// It is the GLOBAL scope: `HandleOperatorPause` branches on `queueName == ""`
	// and drives EVERY queue. So
	// `harmonik queue pause "$q"` with $q unset paused every active queue,
	// and said "paused: " with an empty name at exit 0. The caller aimed at one
	// queue and the value did not arrive (hk-wki4e).
	//
	// A BARE `harmonik queue pause` is a different act — it supplies no
	// selector at all — and keeps the usage error below.
	//
	// Global scope is not lost: `harmonik supervise pause` sends the op with
	// no `queue` key at all, and it is the only other spelling of it. There
	// is no --all flag and no literal keyword, so refusing "" here costs
	// the caller nothing they cannot say another way.
	//
	// The order matches the resolution order below: the flag wins, so its
	// emptiness is the one to report when both are empty.
	switch {
	case queueNameGiven && queueName == "":
		diag.println("harmonik queue pause: --queue was given an empty value; pass a queue name or drop the flag. Nothing was paused.")
		return exitTransportError
	case len(positional) > 0 && positional[0] == "":
		diag.println("harmonik queue pause: the queue name argument was empty; pass a queue name or drop the argument. Nothing was paused.")
		return exitTransportError
	}

	// Queue name: prefer --queue flag, fall back to positional argument.
	if queueName == "" {
		if len(positional) < 1 {
			diag.println("harmonik queue pause: usage: hk queue pause <name>")
			return exitTransportError
		}
		queueName = positional[0]
	}

	msg := struct {
		Op    string `json:"op"`
		Queue string `json:"queue"`
	}{Op: "operator-pause", Queue: queueName}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		diag.printf("harmonik queue pause: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			diag.println("harmonik queue pause: daemon not running (no socket at " + harmonikDir + "/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, func(_ json.RawMessage, w io.Writer) int {
		p := newPrinter(w)
		p.printf("paused: %s\n", queueName)
		return renderExit(p)
	})
}
