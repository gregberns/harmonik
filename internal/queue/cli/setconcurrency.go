package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// RunQueueSetConcurrency implements `harmonik queue set-concurrency <n>`.
//
// Sends a queue-set-concurrency request to the daemon via the Unix socket.
// The daemon's ConcurrencyController updates the dispatch ceiling atomically;
// no daemon restart is required. Raising n lets the gate admit up to n
// concurrent runs immediately; lowering n below the running count lets
// in-flight runs complete naturally and stops new dispatch until running < n.
// n < 1 is rejected by the daemon.
//
// Flag args (subArgs is os.Args[3:]):
//
//	<n>                required positional: new max_concurrent ceiling (>= 1)
//	--project <dir>    project directory (default: cwd)
//	--project=<dir>    equals form
//	--json             output raw JSON (shorthand for --format json)
//	--format json|text output format (default text)
//
// Bead ref: hk-ohiaf.
func RunQueueSetConcurrency(ctx context.Context, subArgs []string, out io.Writer, errOut io.Writer) int {
	projectDir, positional, outputJSON, ok := parseQueueFlags(subArgs, errOut)
	if !ok {
		return exitTransportError
	}

	if len(positional) < 1 {
		fmt.Fprintln(errOut, "harmonik queue set-concurrency: usage: harmonik queue set-concurrency <n>")
		return exitTransportError
	}
	n, convErr := strconv.Atoi(positional[0])
	if convErr != nil || n < 1 {
		fmt.Fprintf(errOut, "harmonik queue set-concurrency: n must be an integer >= 1, got %q\n", positional[0])
		return exitTransportError
	}

	msg := struct {
		Op string `json:"op"`
		N  int    `json:"n"`
	}{Op: "queue-set-concurrency", N: n}

	payload, marshalErr := marshalJSON(msg)
	if marshalErr != nil {
		fmt.Fprintf(errOut, "harmonik queue set-concurrency: cannot marshal request: %v\n", marshalErr)
		return exitTransportError
	}

	harmonikDir := harmonikDirFromProject(projectDir, errOut)
	if harmonikDir == "" {
		return exitTransportError
	}

	resp, earlyExit := sendRequest(ctx, harmonikDir, payload)
	if earlyExit != -1 {
		if earlyExit == exitDaemonDown {
			fmt.Fprintln(errOut, "harmonik queue set-concurrency: daemon not running (no socket at "+harmonikDir+"/daemon.sock)")
		}
		return earlyExit
	}

	return handleResponse(resp, out, outputJSON, func(result json.RawMessage, w io.Writer) int {
		var r struct {
			OldN     int `json:"old_n"`
			NewN     int `json:"new_n"`
			SpawnCap int `json:"spawn_cap"`
		}
		if jsonErr := json.Unmarshal(result, &r); jsonErr != nil {
			fmt.Fprintf(w, "set-concurrency: ok\n") //nolint:errcheck
			return exitSuccess
		}
		fmt.Fprintf(w, "max_concurrent: %d → %d\n", r.OldN, r.NewN) //nolint:errcheck
		if r.SpawnCap > 0 {
			fmt.Fprintf(w, "spawn_cap: %d non-terminal sessions (safe max_concurrent = %d; restart to raise)\n", r.SpawnCap, r.SpawnCap/2) //nolint:errcheck
		}
		return exitSuccess
	})
}
