package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
)

// RunQueueRecover sends a `queue-recover` (or, with --drop, a `queue-drop`)
// request for one named queue.
//
// Flag args (subArgs is os.Args[3:]):
//
//	<name>             required positional: the queue name to recover
//	--queue <name>     the queue name (flag form, alternative to positional)
//	--queue=<name>     equals form
//	--drop             dispose of the failed entries instead of re-arming
//	                   them — see `harmonik queue --help` for how this
//	                   differs from a plain recover and from cancel
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
	queueName, dropFlag, projectDir, outputJSON, ok := parseQueueRecoverArgs(subArgs, errOut, diag)
	if !ok {
		return exitTransportError
	}

	verb, op, renderFn := "recover", "queue-recover", renderQueueRecoverText
	if dropFlag {
		verb, op, renderFn = "recover --drop", "queue-drop", renderQueueDropText
	}

	msg := struct {
		Op    string `json:"op"`
		Queue string `json:"queue"`
	}{Op: op, Queue: queueName}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		diag.printf("harmonik queue %s: cannot marshal request: %v\n", verb, marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			diag.printf("harmonik queue %s: daemon not running (no socket at %s/daemon.sock)\n", verb, harmonikDir)
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, renderFn)
}

// parseQueueRecoverArgs parses and validates the `queue recover` flag set,
// including --drop. ok is false when subArgs failed validation and a
// diagnostic has already been printed to diag.
func parseQueueRecoverArgs(subArgs []string, errOut io.Writer, diag *printer) (queueName string, dropFlag bool, projectDir string, outputJSON, ok bool) {
	var queueNameGiven bool
	projectDir, positional, outputJSON, parsedOK := parseQueueFlagsExtra(subArgs, errOut, func(args []string, i int) (int, bool) {
		switch {
		case args[i] == "--queue" && i+1 < len(args):
			queueName = args[i+1]
			queueNameGiven = true
			return i + 2, true
		case strings.HasPrefix(args[i], "--queue="):
			queueName = strings.TrimPrefix(args[i], "--queue=")
			queueNameGiven = true
			return i + 1, true
		case args[i] == "--drop":
			dropFlag = true
			return i + 1, true
		}
		return i, false
	})
	if !parsedOK {
		return "", false, "", false, false
	}

	verb := "recover"
	if dropFlag {
		verb = "recover --drop"
	}

	switch {
	case queueNameGiven && queueName == "":
		diag.printf("harmonik queue %s: --queue was given an empty value; pass a queue name or drop the flag. Nothing was done.\n", verb)
		return "", false, "", false, false
	case len(positional) > 0 && positional[0] == "":
		diag.printf("harmonik queue %s: the queue name argument was empty; pass a queue name or drop the argument. Nothing was done.\n", verb)
		return "", false, "", false, false
	}

	if queueName == "" {
		if len(positional) < 1 {
			diag.printf("harmonik queue %s: usage: harmonik queue %s <name>\n", verb, verb)
			return "", false, "", false, false
		}
		queueName = positional[0]
	}

	return queueName, dropFlag, projectDir, outputJSON, true
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

func renderQueueDropText(result json.RawMessage, out io.Writer) int {
	var response struct {
		Queue        string   `json:"queue"`
		QueueID      string   `json:"queue_id"`
		Dropped      []string `json:"dropped"`
		DroppedCount int      `json:"dropped_count"`
		ArchivePath  string   `json:"archive_path"`
	}
	p := newPrinter(out)
	if err := json.Unmarshal(result, &response); err != nil {
		p.printf("%s\n", result)
		return renderExit(p)
	}
	if response.Queue != "" {
		p.printf("dropped: %s\n", response.Queue)
	}
	p.printf("removed: %d\n", response.DroppedCount)
	for _, id := range response.Dropped {
		p.printf("  %s\n", id)
	}
	if response.ArchivePath != "" {
		p.printf("archived to: %s\n", response.ArchivePath)
	}
	return renderExit(p)
}
