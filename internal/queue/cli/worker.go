package cli

import (
	"context"
	"encoding/json"
	"io"
)

// RunWorkerEnable implements `harmonik worker enable <name>`.
// RunWorkerDisable implements `harmonik worker disable <name>`.
//
// Both send a worker-set-enabled request to the daemon via the Unix socket,
// flipping the named worker's enabled flag in the LIVE worker registry so remote
// dispatch can be turned on/off WITHOUT a daemon restart. This mirrors the
// `queue set-concurrency` live-setter pattern (hk-ohiaf): same socket transport,
// same JSON-response convention, same exit-code contract.
//
// Flag args (subArgs is os.Args[3:]):
//
//	<name>             required positional: the worker name from .harmonik/workers.yaml
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//	--json             output raw JSON (shorthand for --format json)
//	--format json|text output format (default text)
//
// Bead ref: hk-xjbvi.
func RunWorkerEnable(ctx context.Context, subArgs []string, out, errOut io.Writer) int {
	return runWorkerSetEnabled(ctx, subArgs, true, out, errOut)
}

// RunWorkerDisable implements `harmonik worker disable <name>` (see RunWorkerEnable).
//
// Bead ref: hk-xjbvi.
func RunWorkerDisable(ctx context.Context, subArgs []string, out, errOut io.Writer) int {
	return runWorkerSetEnabled(ctx, subArgs, false, out, errOut)
}

func runWorkerSetEnabled(ctx context.Context, subArgs []string, enabled bool, out, errOut io.Writer) int {
	diag := newPrinter(errOut)
	verb := "enable"
	if !enabled {
		verb = "disable"
	}

	projectDir, positional, outputJSON, ok := parseQueueFlags(subArgs, errOut)
	if !ok {
		return exitTransportError
	}

	if len(positional) < 1 {
		diag.printf("harmonik worker %s: usage: harmonik worker %s <name>\n", verb, verb)
		return exitTransportError
	}
	name := positional[0]

	msg := struct {
		Op      string `json:"op"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}{Op: "worker-set-enabled", Name: name, Enabled: enabled}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		diag.printf("harmonik worker %s: cannot marshal request: %v\n", verb, marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			diag.println("harmonik worker " + verb + ": daemon not running (no socket at " + harmonikDir + "/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, func(result json.RawMessage, w io.Writer) int {
		var r struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		p := newPrinter(w)
		if jsonErr := json.Unmarshal(result, &r); jsonErr != nil {
			p.printf("worker %s: ok\n", verb)
			return renderExit(p)
		}
		state := "disabled"
		if r.Enabled {
			state = "enabled"
		}
		p.printf("worker %s: %s\n", r.Name, state)
		return renderExit(p)
	})
}
