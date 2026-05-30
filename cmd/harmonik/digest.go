package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/google/uuid"
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

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Print(`harmonik digest — compute the cognition-loop status sheet (no daemon required)

USAGE
  harmonik digest [--project DIR] [--json] [--since EVENT_ID] [--full]

FLAGS
  --project DIR     Project directory (default: current working directory)
  --json            Emit one schema-versioned NDJSON object to stdout
  --since EVENT_ID  Restrict events to those after this UUIDv7 (ScanAfter watermark)
  --full            Disable size caps (include all active runs, events, and notes)

OUTPUT
  Without --json, emits a human-readable status sheet to stdout.
  With --json, emits a single NDJSON line carrying schema_version + all fields.

EXIT CODES
  0  — success
  1  — argument error
  7  — .harmonik/ directory not found

EXAMPLES
  harmonik digest
  harmonik digest --json
  harmonik digest --since 01900000-0000-7000-0000-000000000000
  harmonik digest --full
`)
			return 0
		case args[i] == "--json":
			jsonFlag = true
		case args[i] == "--full":
			fullFlag = true
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
			fmt.Printf("  %s  type=%s  run=%s\n", ev.EventID[:8], ev.Type, ev.RunID[:8])
		} else {
			fmt.Printf("  %s  type=%s\n", ev.EventID[:8], ev.Type)
		}
	}

	// Non-fatal errors
	if len(d.Errors) > 0 {
		fmt.Println("\n=== Collection errors ===")
		for _, e := range d.Errors {
			fmt.Printf("  WARN: %s\n", e)
		}
	}
}
