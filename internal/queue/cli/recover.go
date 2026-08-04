package cli

// recover.go — `harmonik queue recover <name>`.
//
// The verb is deliberately separate from `harmonik queue resume`. Resume
// releases a drain pause and touches only the queue status. Recover re-arms
// every failed item, resets attempt counts, clears failure reasons, and reopens
// the affected groups. One command that did both, chosen by the queue's current
// status, would silently rewrite item state when the operator asked only to
// un-pause.
//
// Two lanes built this verb at the same time and reached the same shape. This
// file is the union of the two. The flag surface and the re-armed listing come
// from the socket-registered path. The result-and-receipt contract comes from
// the receipt-bound recovery transaction: the daemon answers only after the
// recovery receipt is durable, so an answer with no receipt is not a success
// and this command refuses to print one as if it were.
//
// Spec ref: specs/queue-model.md §8.3b QM-052b and QM-058a;
// specs/process-lifecycle.md PL-032.

import (
	"context"
	"encoding/json"
	"io"
	"strings"
)

// RunQueueRecover sends a `queue-recover` request for one named queue.
//
// Flag args (subArgs is os.Args[3:]):
//
//	<name>             required positional: the queue name to recover
//	--queue <name>     the queue name (flag form, alternative to positional)
//	--queue=<name>     equals form
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//	--json             output raw JSON (shorthand for --format json)
//	--format json|text output format (default text)
func RunQueueRecover(ctx context.Context, subArgs []string, out, errOut io.Writer) int {
	diag := newPrinter(errOut)
	var queueName string
	projectDir, positional, outputJSON, ok := parseQueueFlagsExtra(subArgs, errOut, func(args []string, i int) (int, bool) {
		switch {
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

	if queueName == "" {
		if len(positional) < 1 {
			diag.println("harmonik queue recover: usage: harmonik queue recover <name>")
			return exitTransportError
		}
		queueName = positional[0]
	}

	msg := struct {
		Op    string `json:"op"`
		Queue string `json:"queue"`
	}{Op: "queue-recover", Queue: queueName}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		diag.printf("harmonik queue recover: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			diag.println("harmonik queue recover: daemon not running (no socket at " + harmonikDir + "/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, renderQueueRecoverText)
}

// renderQueueRecoverText prints the recovery result, its durable receipt, and
// the re-armed bead IDs.
//
// The receipt is required, not decorative. The daemon answers only after the
// recovery receipt is durable, so a payload that reports `accepted` or `no-op`
// with no receipt describes a recovery that cannot be proved to have happened.
// This function refuses that payload rather than print a success line for it.
//
// An empty re-armed list is printed as such rather than omitted: "recovered,
// nothing re-armed" is a different answer from "recovered 3 items", and the
// operator needs to tell them apart.
func renderQueueRecoverText(result json.RawMessage, out io.Writer) int {
	var response struct {
		Queue        string          `json:"queue"`
		QueueID      string          `json:"queue_id"`
		Result       string          `json:"result"`
		Receipt      json.RawMessage `json:"receipt"`
		Rearmed      []string        `json:"rearmed"`
		RearmedCount int             `json:"rearmed_count"`
	}
	p := newPrinter(out)
	if err := json.Unmarshal(result, &response); err != nil {
		p.printf("%s\n", result)
		return renderExit(p)
	}
	switch response.Result {
	case "accepted", "no-op":
		if len(response.Receipt) == 0 || string(response.Receipt) == "null" {
			return exitTransportError
		}
		if response.Queue != "" {
			p.printf("recovered: %s\n", response.Queue)
		}
		p.printf("recovery: %s\n", response.Result)
		p.printf("receipt: %s\n", response.Receipt)
		p.printf("re-armed: %d\n", response.RearmedCount)
		for _, id := range response.Rearmed {
			p.printf("  %s\n", id)
		}
		return renderExit(p)
	case "rejected":
		p.println("recovery: rejected")
		return renderExit(p)
	default:
		return exitTransportError
	}
}
