package supervisecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/supervise"
)

// RunReap implements `harmonik supervise reap`: enumerate flywheel-family tmux
// sessions (harmonik-<12hex>-flywheel), kill those whose pane is dead AND that
// predate the live daemon's start time, and emit a tmux_orphan_reaped event per
// kill. Safe when no tmux server is running (clean no-op, exit 0).
//
// This is the on-demand counterpart to the boot auto-reap wired into RunStart.
// It targets ONLY -flywheel sessions (CONTRACT.md invariant I3); it can never
// touch a -default, -captain, -crew-*, or -supervise session.
//
// Exit codes:
//
//	0  — reap pass completed (zero or more sessions reaped)
//	1  — argument or operational error
//
// Spec ref: docs/retro/2026-06-10/A3-embed-inventory.md gap #2 (Tmux orphan reap).
func RunReap(args []string, stdout, stderr io.Writer) int {
	var projectDir string
	var asJSON bool

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, reapUsage)
			return 0
		case args[i] == "--json":
			asJSON = true
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik supervise reap: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	opts := supervise.ReapOptions{
		DaemonStartTime: resolveDaemonStartTime(projectDir),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := supervise.ReapOrphanFlywheelSessions(ctx, supervise.OSReapAdapter(), opts)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik supervise reap: %v\n", err)
		return 1
	}

	// Emit one tmux_orphan_reaped event per kill (newline-delimited JSON on
	// stdout so it is greppable / pipeable to the daemon event stream).
	for _, ev := range result.Events {
		b, mErr := json.Marshal(ev)
		if mErr != nil {
			continue
		}
		fmt.Fprintln(stdout, string(b))
	}

	if asJSON {
		summary := struct {
			Scanned int      `json:"scanned"`
			Reaped  []string `json:"reaped"`
			Skipped int      `json:"skipped"`
		}{
			Scanned: result.Scanned,
			Reaped:  result.Reaped,
			Skipped: result.Skipped,
		}
		if summary.Reaped == nil {
			summary.Reaped = []string{}
		}
		b, _ := json.Marshal(summary)
		fmt.Fprintln(stdout, string(b))
		return 0
	}

	fmt.Fprintf(stdout,
		"harmonik supervise reap: scanned %d flywheel session(s), reaped %d, skipped %d\n",
		result.Scanned, len(result.Reaped), result.Skipped)
	return 0
}

// bootReapOrphanFlywheels is the boot-path entry point: it runs the host-wide
// flywheel orphan reaper while protecting protectSession (the flywheel session
// the supervisor just created). Called from RunStart after session creation.
// Best-effort: it owns its own bounded context and swallows errors — a reap
// failure must never fail an otherwise-successful supervisor start.
func bootReapOrphanFlywheels(projectDir, protectSession string) supervise.ReapResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := supervise.ReapOptions{
		DaemonStartTime: resolveDaemonStartTime(projectDir),
		ProtectSession:  protectSession,
	}
	result, _ := supervise.ReapOrphanFlywheelSessions(ctx, supervise.OSReapAdapter(), opts)
	return result
}

// resolveDaemonStartTime returns a proxy for the live daemon's start time: the
// mtime of the daemon pidfile (.harmonik/daemon.pid), which the daemon writes
// and locks at boot. A flywheel session created before this time predates the
// live daemon and is a true orphan. When the pidfile is absent (no live daemon),
// returns the zero time — the reaper then applies no predate gate, treating
// every dead-pane flywheel as an orphan (correct: with no live daemon there is
// nothing to preserve).
func resolveDaemonStartTime(projectDir string) time.Time {
	info, err := os.Stat(lifecycle.PidfilePath(projectDir))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

const reapUsage = `harmonik supervise reap — reap dead flywheel tmux orphan sessions

USAGE
  harmonik supervise reap [--project DIR] [--json]

DESCRIPTION
  Enumerates tmux sessions matching harmonik-<12hex>-flywheel and kills those
  whose pane is dead (pane_dead=1) AND that predate the live daemon's start
  time, emitting a tmux_orphan_reaped event per kill. Safe when no tmux server
  is running (clean no-op). Targets ONLY -flywheel sessions — never a -default,
  -captain, -crew-*, or -supervise session.

FLAGS
  --project DIR   Project directory (default: current working directory)
  --json          Emit a machine-readable summary line after the per-kill events

EXIT CODES
   0  Reap pass completed
   1  Argument or operational error

EXAMPLES
  harmonik supervise reap
  harmonik supervise reap --json
`
