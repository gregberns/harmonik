package supervisecmd

// pause.go — `harmonik supervise pause` (hk-ry8q1).
//
// Sends an operator-pause request to the running daemon via its Unix socket.
// The daemon responds by emitting operator_pause_status events and
// transitioning the active queue to paused-by-drain.
//
// Exit codes:
//
//	0  — daemon acknowledged the pause (or was already paused)
//	1  — argument or I/O error
//	17 — daemon not running (socket absent or ECONNREFUSED)
//
// Spec ref: specs/operator-nfr.md §4.3 ON-007–ON-010.
// Bead ref: hk-ry8q1.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// RunPause implements `harmonik supervise pause`.
func RunPause(args []string, stdout, stderr io.Writer) int {
	var projectDir string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			if pauseWritef(stdout, "%s", pauseUsage) != nil {
				return 1
			}
			return 0
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		default:
			if pauseWritef(stderr, "harmonik supervise pause: unknown argument %q\n", args[i]) != nil {
				return 1
			}
			return 1
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			if pauseWritef(stderr, "harmonik supervise pause: cannot determine working directory: %v\n", err) != nil {
				return 1
			}
			return 1
		}
		projectDir = wd
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sockPath := lifecycle.SocketPath(projectDir)
	code := sendOperatorOp(ctx, sockPath, "operator-pause", stderr)
	if code == 0 {
		if pauseWritef(stdout, "harmonik supervise pause: daemon paused\n") != nil {
			return 1
		}
	}
	return code
}

const pauseUsage = `harmonik supervise pause — pause the daemon dispatch loop

USAGE
  harmonik supervise pause [--project DIR]

FLAGS
  --project DIR  Project directory (default: current working directory)

EXIT CODES
  0   Success (daemon paused or already paused)
  1   Argument or I/O error
  17  Daemon not running

NOTES
  The daemon completes any in-flight runs before entering the fully-paused
  state. New dispatches are blocked immediately.
  Use 'harmonik supervise resume' to resume.
`

// ---------------------------------------------------------------------------
// sendOperatorOp — shared socket client for pause and resume
// ---------------------------------------------------------------------------

// sendOperatorOp dials sockPath, sends {"op": op}, and interprets the response.
// Returns 0 on success, 1 on protocol/I/O error, 17 when the daemon is down.
func sendOperatorOp(ctx context.Context, sockPath, op string, stderr io.Writer) (code int) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if err != nil {
		if isSocketAbsentOrRefused(err) {
			if pauseWritef(stderr, "harmonik supervise %s: daemon not running (exit 17)\n", opVerb(op)) != nil {
				return 1
			}
			return 17
		}
		if pauseWritef(stderr, "harmonik supervise %s: dial: %v\n", opVerb(op), err) != nil {
			return 1
		}
		return 1
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck

	payload, err := json.Marshal(map[string]string{"op": op})
	if err != nil {
		if pauseWritef(stderr, "harmonik supervise %s: marshal: %v\n", opVerb(op), err) != nil {
			return 1
		}
		return 1
	}

	if _, writeErr := conn.Write(payload); writeErr != nil {
		if pauseWritef(stderr, "harmonik supervise %s: write: %v\n", opVerb(op), writeErr) != nil {
			return 1
		}
		return 1
	}

	// Half-close write side so the daemon's json.Decoder sees EOF.
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck // the daemon can decode, respond and close before this statement runs, at which point CloseWrite returns ENOTCONN for an operation that already succeeded
	}

	var resp struct {
		Ok    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if decErr := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); decErr != nil {
		if pauseWritef(stderr, "harmonik supervise %s: decode response: %v\n", opVerb(op), decErr) != nil {
			return 1
		}
		return 1
	}

	if !resp.Ok {
		if pauseWritef(stderr, "harmonik supervise %s: daemon error: %s\n", opVerb(op), resp.Error) != nil {
			return 1
		}
		return 1
	}

	return 0
}

func pauseWritef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

// opVerb returns the human-readable verb for an op string ("operator-pause" → "pause").
func opVerb(op string) string {
	switch op {
	case "operator-pause":
		return "pause"
	case "operator-resume":
		return "resume"
	default:
		return op
	}
}

// isSocketAbsentOrRefused reports whether the error indicates the daemon's
// socket is absent or not accepting connections.
func isSocketAbsentOrRefused(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "no such file") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connect: no such file or directory")
}
