package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
)

// RunQueueRecover implements `harmonik queue recover <name>`.
//
// It requests failed-item recovery. It is separate from queue resume, which
// resumes only queues paused by drain. The daemon returns only after the
// receipt-bound recovery transaction is durable.
//
// Spec ref: specs/process-lifecycle.md PL-032.
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
	payload, err := marshalJSON(msg)
	if err != nil {
		diag.printf("harmonik queue recover: cannot marshal request: %v\n", err)
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
		Result  string          `json:"result"`
		Receipt json.RawMessage `json:"receipt"`
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
		p.printf("recovery: %s\n", response.Result)
		p.printf("receipt: %s\n", response.Receipt)
		return renderExit(p)
	case "rejected":
		p.println("recovery: rejected")
		return renderExit(p)
	default:
		return exitTransportError
	}
}
