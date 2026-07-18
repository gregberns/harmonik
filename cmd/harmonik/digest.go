package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/google/uuid"

	digestcmd "github.com/gregberns/harmonik/cmd/harmonik/digest"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/digest"
)

// runDigestSubcommand implements `harmonik digest` per CL-030..CL-033 and
// specs/process-lifecycle.md §PL-028d.
//
// Exit codes:
//
//	0  — success
//	1  — argument or flag error
//	7  — .harmonik/ directory absent
func runDigestSubcommand(args []string) int {
	var projectFlag string
	var sinceFlag string
	var jsonFlag bool
	var fullFlag bool
	var watchFlag bool

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Print(`harmonik digest — compute the cognition-loop status sheet (no daemon required)

USAGE
  harmonik digest [--project DIR] [--json] [--since EVENT_ID] [--full] [--watch]

FLAGS
  --project DIR     Project directory (default: current working directory)
  --json            Emit one schema-versioned NDJSON object to stdout
  --since EVENT_ID  Restrict events to those after this UUIDv7 (ScanAfter watermark)
  --full            Disable size caps (include all active runs, events, and notes)
  --watch           Live TUI loop: poll at 1s cadence; Ctrl-C to exit (CL-082)

OUTPUT
  Without --json, emits a human-readable status sheet to stdout.
  With --json, emits a single NDJSON line carrying schema_version + all fields.
  With --watch, renders a live updating view polling at 1s cadence.

EXIT CODES
  0  — success
  1  — argument error
  7  — .harmonik/ directory not found

EXAMPLES
  harmonik digest
  harmonik digest --json
  harmonik digest --since 01900000-0000-7000-0000-000000000000
  harmonik digest --full
  harmonik digest --watch
`)
			return 0
		case args[i] == "--json":
			jsonFlag = true
		case args[i] == "--full":
			fullFlag = true
		case args[i] == "--watch":
			watchFlag = true
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectFlag = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectFlag = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--since" && i+1 < len(args):
			i++
			sinceFlag = args[i]
		case strings.HasPrefix(args[i], "--since="):
			sinceFlag = strings.TrimPrefix(args[i], "--since=")
		}
	}

	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik digest: cannot determine working directory: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	projectDir, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik digest: cannot resolve project path %q: %v\n", projectFlag, err)
		return 1
	}

	var sinceID core.EventID
	if sinceFlag != "" {
		var u uuid.UUID
		if err := u.UnmarshalText([]byte(sinceFlag)); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik digest: --since %q is not a valid UUID: %v\n", sinceFlag, err)
			return 1
		}
		sinceID = core.EventID(u)
	}

	lim := digest.DefaultLimits()
	if fullFlag {
		lim = digest.FullLimits()
	}

	brPath, _ := exec.LookPath("br")
	kerfPath, _ := exec.LookPath("kerf")

	in := digest.BuildInput{
		ProjectDir:   projectDir,
		SinceEventID: sinceID,
		Limits:       lim,
		BrPath:       brPath,
		KerfPath:     kerfPath,
	}

	// --watch: live TUI polling loop per CL-082.
	if watchFlag {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		_ = digestcmd.RunWatch(ctx, digestcmd.WatchInput{Build: in}, os.Stdout)
		return 0
	}

	d, err := digest.Build(context.Background(), in)
	if err != nil {
		if err == digest.ErrNoHarmonikDir {
			fmt.Fprintf(os.Stderr, "harmonik digest: .harmonik/ not found in %q\n", projectDir)
			return 7
		}
		fmt.Fprintf(os.Stderr, "harmonik digest: %v\n", err)
		return 1
	}

	if jsonFlag {
		b, err := json.Marshal(d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik digest: marshal: %v\n", err)
			return 1
		}
		fmt.Printf("%s\n", b)
		return 0
	}

	// Human-readable output.
	printHumanDigest(d)
	return 0
}

// printHumanDigest renders d as a compact human-readable status sheet.
func printHumanDigest(d *digest.DigestJSON) {
	fmt.Printf("harmonik digest  schema_version=%d  generated_at=%s\n\n",
		d.SchemaVersion, d.GeneratedAt.Format("2006-01-02T15:04:05Z"))

	// Queue
	fmt.Println("=== Queue ===")
	if !d.Queue.Present {
		fmt.Println("  (no active queue)")
	} else {
		fmt.Printf("  status=%s  active=%d  pending=%d\n",
			d.Queue.Status, d.Queue.ActiveRunCount, d.Queue.PendingCount)
		for _, r := range d.Queue.ActiveRuns {
			runStr := r.RunID
			if runStr == "" {
				runStr = "(no run_id)"
			}
			fmt.Printf("    %s  run=%s\n", r.BeadID, runStr)
		}
		if d.Truncated != nil && d.Truncated.ActiveRunsOmitted > 0 {
			fmt.Printf("    [+%d more truncated]\n", d.Truncated.ActiveRunsOmitted)
		}
	}

	// Recent commits
	if len(d.RecentCommits) > 0 {
		fmt.Println("\n=== Recent commits (origin/main) ===")
		for _, c := range d.RecentCommits {
			fmt.Printf("  %s  %s\n", c.Hash, c.Subject)
		}
	}

	// Ready beads
	if len(d.ReadyBeads) > 0 {
		fmt.Printf("\n=== Ready beads (%d) ===\n", len(d.ReadyBeads))
		for _, b := range d.ReadyBeads {
			fmt.Printf("  %s  %s\n", b.BeadID, b.Title)
		}
	}

	// In-progress beads
	if len(d.InProgressBeads) > 0 {
		fmt.Printf("\n=== In-progress beads (%d) ===\n", len(d.InProgressBeads))
		for _, b := range d.InProgressBeads {
			fmt.Printf("  %s  %s\n", b.BeadID, b.Title)
		}
	}

	// Pending decisions (EV-044) — surfaced unconditionally so operators cannot
	// miss a sentinel trip or other blocking decision_required event.
	if len(d.PendingDecisions) > 0 {
		fmt.Printf("\n=== Pending decisions (%d — BLOCKING) ===\n", len(d.PendingDecisions))
		for _, pd := range d.PendingDecisions {
			fmt.Printf("  [%s/%s] %s\n", pd.SubjectKind, pd.SubjectID, pd.Reason)
			if pd.SuggestedAction != "" {
				fmt.Printf("    → %s\n", pd.SuggestedAction)
			}
			fmt.Printf("    ack_token=%s\n", pd.AckToken)
		}
	}

	// Open notes
	fmt.Printf("\n=== Open notes (%d) ===\n", len(d.OpenNotes))
	if len(d.OpenNotes) == 0 {
		fmt.Println("  (none)")
	}
	for _, n := range d.OpenNotes {
		fmt.Printf("  [%s]  %s\n", n.Kind, n.Text)
	}
	if d.Truncated != nil && d.Truncated.OpenNotesOmitted > 0 {
		fmt.Printf("  [+%d more truncated]\n", d.Truncated.OpenNotesOmitted)
	}

	// Recent events
	if d.Truncated != nil && d.Truncated.RecentEventsOmitted > 0 {
		fmt.Printf("\n=== Recent events (%d shown, +%d truncated) ===\n",
			len(d.RecentEvents), d.Truncated.RecentEventsOmitted)
	} else if len(d.RecentEvents) > 0 {
		fmt.Printf("\n=== Recent events (%d) ===\n", len(d.RecentEvents))
	}
	for _, ev := range d.RecentEvents {
		if ev.RunID != "" {
			fmt.Printf("  %s  type=%s  run=%s\n", digestShortID(ev.EventID), ev.Type, digestShortID(ev.RunID))
		} else {
			fmt.Printf("  %s  type=%s\n", digestShortID(ev.EventID), ev.Type)
		}
	}

	// Agents online (comms who)
	fmt.Printf("\n=== Agents online (%d) ===\n", len(d.CommsWho))
	if len(d.CommsWho) == 0 {
		fmt.Println("  (none)")
	}
	for _, w := range d.CommsWho {
		fmt.Printf("  %s  status=%s\n", w.Agent, w.Status)
	}

	// Registered crews
	fmt.Printf("\n=== Registered crews (%d) ===\n", len(d.Crews))
	if len(d.Crews) == 0 {
		fmt.Println("  (none)")
	}
	for _, c := range d.Crews {
		fmt.Printf("  %s  queue=%s  epic=%s\n", c.Name, c.Queue, c.Epic)
	}

	// tmux fleet
	fmt.Printf("\n=== tmux fleet (%d sessions) ===\n", len(d.TmuxFleet))
	if len(d.TmuxFleet) == 0 {
		fmt.Println("  (no tmux sessions)")
	}
	for _, s := range d.TmuxFleet {
		fmt.Printf("  %s  windows=%s\n", s.Session, strings.Join(s.Windows, ","))
	}

	// Paused / failed queues
	if len(d.PausedQueues) > 0 {
		fmt.Printf("\n=== PAUSED/FAILED QUEUES (%d) ===\n", len(d.PausedQueues))
		for _, q := range d.PausedQueues {
			fmt.Printf("  %s  status=%s\n", q.Name, q.Status)
		}
	} else {
		fmt.Println("\n=== Paused/failed queues ===")
		fmt.Println("  (none — all queues active or idle-healthy)")
	}

	// kerf map
	if d.KerfMap != "" {
		fmt.Println("\n=== Kerf map ===")
		fmt.Println(d.KerfMap)
	}

	// Non-fatal errors
	if len(d.Errors) > 0 {
		fmt.Println("\n=== Collection errors ===")
		for _, e := range d.Errors {
			fmt.Printf("  WARN: %s\n", e)
		}
	}
}

// digestShortID returns the first 8 characters of id, or id itself when it is
// shorter — a bare id[:8] panics on short IDs.
func digestShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
