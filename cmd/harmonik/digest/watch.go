// Package digestcmd implements the --watch live loop for `harmonik digest`.
//
// Spec: specs/cognition-loop.md §CL-082.
// Bead: hk-e3bnw.
package digestcmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/digest"
)

// WatchInput holds parameters for the --watch loop.
type WatchInput struct {
	// Build is forwarded to digest.Build on every tick.
	Build digest.BuildInput
	// Interval is the poll cadence; 0 defaults to 1 second.
	Interval time.Duration
}

// RunWatch polls the digest producer at the configured cadence and renders a
// live status sheet to w. It blocks until ctx is cancelled (e.g. SIGINT).
//
// Graceful degrade: digest.Build reads durable file surfaces only (DC-001);
// no daemon socket is required. When the daemon is offline the file-poll path
// continues uninterrupted.
//
// Spec: specs/cognition-loop.md §CL-082.
func RunWatch(ctx context.Context, in WatchInput, w io.Writer) error {
	interval := in.Interval
	if interval <= 0 {
		interval = time.Second
	}

	// Render immediately before the first tick so the screen is never blank.
	renderWatchFrame(ctx, w, in.Build)

	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			// Clear the status line and exit without leaving stale output.
			fmt.Fprintln(w, "\033[H\033[2J")
			fmt.Fprintln(w, "harmonik digest --watch: stopped.")
			return nil
		case <-tick.C:
			renderWatchFrame(ctx, w, in.Build)
		}
	}
}

// renderWatchFrame clears the terminal and emits one full digest snapshot.
// ctx is propagated to digest.Build so that br/kerf subprocesses are
// cancelled when the user hits Ctrl-C (CTX_PROPAGATION fix).
func renderWatchFrame(ctx context.Context, w io.Writer, in digest.BuildInput) {
	now := time.Now()
	d, buildErr := digest.Build(ctx, in)

	// Move cursor to top-left, then clear to end of screen.
	fmt.Fprint(w, "\033[H\033[2J")

	// Header ─────────────────────────────────────────────────────────────────
	fmt.Fprintf(w, "harmonik digest --watch   %s   [file-poll]   Ctrl-C to exit\n",
		now.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(w, strings.Repeat("─", 68))

	if buildErr != nil {
		if buildErr == digest.ErrNoHarmonikDir {
			fmt.Fprintln(w, "ERROR: .harmonik/ directory not found — is this a harmonik project?")
		} else {
			fmt.Fprintf(w, "ERROR: %v\n", buildErr)
		}
		return
	}

	// Metadata row.
	refreshLag := now.Sub(d.GeneratedAt).Truncate(time.Millisecond)
	fmt.Fprintf(w, "schema_version: %d   refresh lag: %s\n", d.SchemaVersion, refreshLag)

	// Watermark age: age of the most recent event (UUIDv7 timestamp).
	watermarkAge := "(no events)"
	if len(d.RecentEvents) > 0 {
		watermarkAge = uuidv7Age(d.RecentEvents[0].EventID, now)
	}
	fmt.Fprintf(w, "watermark age:  %s\n", watermarkAge)
	fmt.Fprintln(w)

	// In-flight runs ─────────────────────────────────────────────────────────
	activeCount := d.Queue.ActiveRunCount
	pendingCount := d.Queue.PendingCount
	if !d.Queue.Present {
		fmt.Fprintln(w, "=== In-flight runs === (no active queue)")
	} else {
		fmt.Fprintf(w, "=== In-flight runs (%d active, %d pending) ===\n",
			activeCount, pendingCount)
		for _, r := range d.Queue.ActiveRuns {
			runID := r.RunID
			if runID == "" {
				runID = "(no run_id)"
			} else if len(runID) > 8 {
				runID = runID[:8]
			}
			fmt.Fprintf(w, "  %-14s  run=%-8s  %s\n", r.BeadID, runID, r.Status)
		}
		if d.Truncated != nil && d.Truncated.ActiveRunsOmitted > 0 {
			fmt.Fprintf(w, "  [+%d more omitted]\n", d.Truncated.ActiveRunsOmitted)
		}
		if pendingCount > 0 {
			fmt.Fprintf(w, "  [%d pending in queue]\n", pendingCount)
		}
	}
	fmt.Fprintln(w)

	// Recent completions ──────────────────────────────────────────────────────
	completions := filterEventsByType(d.RecentEvents, "run_completed", "run_failed")
	fmt.Fprintf(w, "=== Recent completions (%d) ===\n", len(completions))
	if len(completions) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, ev := range completions {
		evAge := uuidv7Age(ev.EventID, now)
		runID := ev.RunID
		if len(runID) > 8 {
			runID = runID[:8]
		}
		if runID != "" {
			fmt.Fprintf(w, "  %-16s  run=%-8s  %s ago\n", ev.Type, runID, evAge)
		} else {
			fmt.Fprintf(w, "  %-16s  %s ago\n", ev.Type, evAge)
		}
	}
	fmt.Fprintln(w)

	// Open notes ──────────────────────────────────────────────────────────────
	fmt.Fprintf(w, "=== Open notes (%d) ===\n", len(d.OpenNotes))
	if len(d.OpenNotes) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, n := range d.OpenNotes {
		noteAge := formatDuration(now.Sub(n.Ts))
		text := n.Text
		if len(text) > 60 {
			text = text[:57] + "..."
		}
		fmt.Fprintf(w, "  [%-10s]  %s  (%s ago)\n", n.Kind, text, noteAge)
	}
	if d.Truncated != nil && d.Truncated.OpenNotesOmitted > 0 {
		fmt.Fprintf(w, "  [+%d more omitted]\n", d.Truncated.OpenNotesOmitted)
	}
	fmt.Fprintln(w)

	// Non-fatal collection errors ─────────────────────────────────────────────
	if len(d.Errors) > 0 {
		fmt.Fprintln(w, "=== Collection errors ===")
		for _, e := range d.Errors {
			fmt.Fprintf(w, "  WARN: %s\n", e)
		}
	}
}

// filterEventsByType returns events whose Type is one of the supplied types.
func filterEventsByType(events []digest.EventSummary, types ...string) []digest.EventSummary {
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}
	out := make([]digest.EventSummary, 0, len(events))
	for _, ev := range events {
		if typeSet[ev.Type] {
			out = append(out, ev)
		}
	}
	return out
}

// uuidv7Age extracts the embedded Unix-millisecond timestamp from a UUIDv7
// string and returns the age relative to now as a human-readable string.
// Returns "?" on any parse or version error.
func uuidv7Age(eventID string, now time.Time) string {
	if len(eventID) < 8 {
		return "?"
	}
	u, err := uuid.Parse(eventID)
	if err != nil {
		return "?"
	}
	// UUIDv7 layout: first 48 bits are Unix epoch milliseconds.
	msec := int64(u[0])<<40 | int64(u[1])<<32 | int64(u[2])<<24 |
		int64(u[3])<<16 | int64(u[4])<<8 | int64(u[5])
	if msec <= 0 {
		return "?"
	}
	t := time.Unix(msec/1000, (msec%1000)*int64(time.Millisecond))
	age := now.Sub(t)
	if age < 0 {
		return "0s"
	}
	return formatDuration(age)
}

// formatDuration formats d as a compact human-readable age string.
// Examples: "3s", "2m05s", "1h15m".
func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%02dm", h, m)
}
