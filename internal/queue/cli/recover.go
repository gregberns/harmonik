package cli

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
//
// A selector given with an EMPTY value is refused (exit 2) rather than read
// as the daemon's default queue. Only the absence of every selector reaches
// the usage error (hk-wki4e).
func RunQueueRecover(ctx context.Context, subArgs []string, out, errOut io.Writer) int {
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
		diag.println("harmonik queue recover: --queue was given an empty value; pass a queue name or drop the flag. Nothing was recovered.")
		return exitTransportError
	case len(positional) > 0 && positional[0] == "":
		diag.println("harmonik queue recover: the queue name argument was empty; pass a queue name or drop the argument. Nothing was recovered.")
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
