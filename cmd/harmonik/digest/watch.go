// Package digestcmd implements the --watch live loop for `harmonik digest`.
//
// Spec: specs/cognition-loop.md §CL-082.
// Bead: hk-e3bnw.
package digestcmd

import (
	"context"
	"errors"
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

	if err := renderWatchFrame(ctx, w, in.Build); err != nil {
		return err
	}

	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			if _, err := fmt.Fprintln(w, "\033[H\033[2J"); err != nil {
				return fmt.Errorf("clear watch display: %w", err)
			}
			if _, err := fmt.Fprintln(w, "harmonik digest --watch: stopped."); err != nil {
				return fmt.Errorf("write watch shutdown message: %w", err)
			}
			return nil
		case <-tick.C:
			if err := renderWatchFrame(ctx, w, in.Build); err != nil {
				return err
			}
		}
	}
}

func renderWatchFrame(ctx context.Context, w io.Writer, in digest.BuildInput) error {
	now := time.Now()
	d, buildErr := digest.Build(ctx, in)
	frame := watchFrame{}

	frame.print("\033[H\033[2J")

	frame.printf("harmonik digest --watch   %s   [file-poll]   Ctrl-C to exit\n",
		now.Format("2006-01-02 15:04:05"))
	frame.println(strings.Repeat("─", 68))

	if buildErr != nil {
		if errors.Is(buildErr, digest.ErrNoHarmonikDir) {
			frame.println("ERROR: .harmonik/ directory not found — is this a harmonik project?")
		} else {
			frame.printf("ERROR: %v\n", buildErr)
		}
		return frame.writeTo(w)
	}

	refreshLag := now.Sub(d.GeneratedAt).Truncate(time.Millisecond)
	frame.printf("schema_version: %d   refresh lag: %s\n", d.SchemaVersion, refreshLag)

	watermarkAge := "(no events)"
	if len(d.RecentEvents) > 0 {
		watermarkAge = uuidv7Age(d.RecentEvents[0].EventID, now)
	}
	frame.printf("watermark age:  %s\n", watermarkAge)
	frame.println()

	activeCount := d.Queue.ActiveRunCount
	pendingCount := d.Queue.PendingCount
	if !d.Queue.Present {
		frame.println("=== In-flight runs === (no active queue)")
	} else {
		frame.printf("=== In-flight runs (%d active, %d pending) ===\n",
			activeCount, pendingCount)
		for _, r := range d.Queue.ActiveRuns {
			runID := r.RunID
			if runID == "" {
				runID = "(no run_id)"
			} else if len(runID) > 8 {
				runID = runID[:8]
			}
			frame.printf("  %-14s  run=%-8s  %s\n", r.BeadID, runID, r.Status)
		}
		if d.Truncated != nil && d.Truncated.ActiveRunsOmitted > 0 {
			frame.printf("  [+%d more omitted]\n", d.Truncated.ActiveRunsOmitted)
		}
		if pendingCount > 0 {
			frame.printf("  [%d pending in queue]\n", pendingCount)
		}
	}
	frame.println()

	completions := filterEventsByType(d.RecentEvents, "run_completed", "run_failed")
	frame.printf("=== Recent completions (%d) ===\n", len(completions))
	if len(completions) == 0 {
		frame.println("  (none)")
	}
	for _, ev := range completions {
		evAge := uuidv7Age(ev.EventID, now)
		runID := ev.RunID
		if len(runID) > 8 {
			runID = runID[:8]
		}
		if runID != "" {
			frame.printf("  %-16s  run=%-8s  %s ago\n", ev.Type, runID, evAge)
		} else {
			frame.printf("  %-16s  %s ago\n", ev.Type, evAge)
		}
	}
	frame.println()

	frame.printf("=== Open notes (%d) ===\n", len(d.OpenNotes))
	if len(d.OpenNotes) == 0 {
		frame.println("  (none)")
	}
	for _, n := range d.OpenNotes {
		noteAge := formatDuration(now.Sub(n.Ts))
		text := n.Text
		if len(text) > 60 {
			text = text[:57] + "..."
		}
		frame.printf("  [%-10s]  %s  (%s ago)\n", n.Kind, text, noteAge)
	}
	if d.Truncated != nil && d.Truncated.OpenNotesOmitted > 0 {
		frame.printf("  [+%d more omitted]\n", d.Truncated.OpenNotesOmitted)
	}
	frame.println()

	if len(d.Errors) > 0 {
		frame.println("=== Collection errors ===")
		for _, e := range d.Errors {
			frame.printf("  WARN: %s\n", e)
		}
	}

	return frame.writeTo(w)
}

type watchFrame struct {
	strings.Builder
	err error
}

func (f *watchFrame) print(args ...any) {
	if f.err != nil {
		return
	}
	_, f.err = fmt.Fprint(&f.Builder, args...)
}

func (f *watchFrame) println(args ...any) {
	if f.err != nil {
		return
	}
	_, f.err = fmt.Fprintln(&f.Builder, args...)
}

func (f *watchFrame) printf(format string, args ...any) {
	if f.err != nil {
		return
	}
	_, f.err = fmt.Fprintf(&f.Builder, format, args...)
}

func (f *watchFrame) writeTo(w io.Writer) error {
	if f.err != nil {
		return fmt.Errorf("format watch frame: %w", f.err)
	}
	if _, err := io.WriteString(w, f.String()); err != nil {
		return fmt.Errorf("write watch frame: %w", err)
	}
	return nil
}

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

func uuidv7Age(eventID string, now time.Time) string {
	if len(eventID) < 8 {
		return "?"
	}
	u, err := uuid.Parse(eventID)
	if err != nil {
		return "?"
	}
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
