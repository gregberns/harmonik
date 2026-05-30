package digest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
)

// ErrNoHarmonikDir is returned by Build when .harmonik/ is absent (exit 7 per PL-028d).
var ErrNoHarmonikDir = fmt.Errorf("digest: .harmonik/ directory not found")

// BuildInput holds the configuration for a single Build call.
type BuildInput struct {
	// ProjectDir is the root of the project (parent of .harmonik/).
	ProjectDir string

	// SinceEventID restricts events to those strictly after this ID.
	// Zero EventID means "include all events" (ScanAfter returns all).
	SinceEventID core.EventID

	// Limits controls truncation per CL-032.
	Limits Limits

	// BrPath is the absolute path to the br binary, or empty to skip br queries.
	BrPath string

	// KerfPath is the absolute path to the kerf binary, or empty to skip.
	KerfPath string

	// GitPath is the absolute path to git, or empty to use "git" on PATH.
	GitPath string

	// Now overrides time.Now() for testing.
	Now time.Time
}

// Build collects the status sheet from durable file surfaces and returns a
// schema-versioned DigestJSON (CL-030..CL-033). No LLM is consulted.
//
// Returns ErrNoHarmonikDir when .harmonik/ is absent (caller maps to exit 7).
func Build(ctx context.Context, in BuildInput) (*DigestJSON, error) {
	harmonikDir := filepath.Join(in.ProjectDir, ".harmonik")
	if _, err := os.Stat(harmonikDir); err != nil {
		return nil, ErrNoHarmonikDir
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	lim := in.Limits
	out := &DigestJSON{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   now,
	}

	// --- Queue summary ---
	out.Queue = buildQueueSummary(ctx, in.ProjectDir, lim)

	// --- Recent commits on origin/main ---
	out.RecentCommits, _ = recentCommits(ctx, in.ProjectDir, in.GitPath, 10)

	// --- Events via ScanAfter ---
	eventsPath := filepath.Join(harmonikDir, "events", "events.jsonl")
	out.RecentEvents, out.Truncated = buildRecentEvents(eventsPath, in.SinceEventID, lim)

	// --- br ready + br list --status in_progress ---
	if in.BrPath != "" {
		out.ReadyBeads, _ = brReady(ctx, in.BrPath, in.ProjectDir)
		out.InProgressBeads, _ = brListByStatus(ctx, in.BrPath, in.ProjectDir, "in_progress")
	}

	// --- notes.jsonl ---
	notesPath := filepath.Join(harmonikDir, "cognition", "notes.jsonl")
	notes, _ := readOpenNotes(notesPath)
	out.OpenNotes, out.Truncated = applyNoteTruncation(notes, lim, out.Truncated)

	// --- kerf next --format=json ---
	if in.KerfPath != "" {
		out.KerfNext, _ = kerfNext(ctx, in.KerfPath, in.ProjectDir)
	}

	return out, nil
}

// buildQueueSummary reads queue.json and returns a QueueSummary.
func buildQueueSummary(_ context.Context, projectDir string, lim Limits) QueueSummary {
	q, err := queue.Load(context.Background(), projectDir)
	if err != nil || q == nil {
		return QueueSummary{Present: false}
	}
	sum := QueueSummary{
		Present: true,
		Status:  string(q.Status),
	}
	var dispatched []QueueItemSummary
	for _, g := range q.Groups {
		for _, item := range g.Items {
			switch item.Status {
			case queue.ItemStatusDispatched:
				sum.ActiveRunCount++
				entry := QueueItemSummary{
					BeadID: string(item.BeadID),
					Status: string(item.Status),
				}
				if item.RunID != nil {
					entry.RunID = *item.RunID
				}
				dispatched = append(dispatched, entry)
			case queue.ItemStatusPending:
				sum.PendingCount++
			}
		}
	}

	cap := lim.maxActiveRuns()
	if cap > 0 && len(dispatched) > cap {
		sum.ActiveRuns = dispatched[:cap]
	} else {
		sum.ActiveRuns = dispatched
	}
	return sum
}

// buildRecentEvents collects events via ScanAfter and applies truncation.
func buildRecentEvents(eventsPath string, sinceID core.EventID, lim Limits) ([]EventSummary, *TruncationReport) {
	var all []EventSummary
	for ev := range eventbus.ScanAfter(eventsPath, sinceID) {
		s := EventSummary{
			EventID: ev.EventID.String(),
			Type:    ev.Type,
		}
		if ev.RunID != nil {
			s.RunID = ev.RunID.String()
		}
		all = append(all, s)
	}

	cap := lim.maxRecentEvents()
	if cap > 0 && len(all) > cap {
		omitted := len(all) - cap
		tr := &TruncationReport{RecentEventsOmitted: omitted}
		return all[len(all)-cap:], tr
	}
	return all, nil
}

// applyNoteTruncation applies the note cap and merges into an existing TruncationReport.
func applyNoteTruncation(notes []noteEntry, lim Limits, existing *TruncationReport) ([]NoteSummary, *TruncationReport) {
	summaries := make([]NoteSummary, 0, len(notes))
	for _, n := range notes {
		summaries = append(summaries, NoteSummary{
			Kind:       n.Kind,
			Text:       n.Text,
			Ts:         n.Ts,
			ToolCallID: n.ToolCallID,
			SessionID:  n.SessionID,
			Refs:       n.Refs,
		})
	}

	cap := lim.maxOpenNotes()
	if cap > 0 && len(summaries) > cap {
		omitted := len(summaries) - cap
		if existing == nil {
			existing = &TruncationReport{}
		}
		existing.OpenNotesOmitted = omitted
		return summaries[:cap], existing
	}
	return summaries, existing
}

// recentCommits runs `git log origin/main --oneline -<n>` and parses results.
func recentCommits(ctx context.Context, projectDir, gitPath string, n int) ([]CommitSummary, error) {
	if gitPath == "" {
		gitPath = "git"
	}
	args := []string{"-C", projectDir, "log", "origin/main", "--oneline", fmt.Sprintf("-%d", n)}
	out, err := runCmd(ctx, gitPath, args...)
	if err != nil {
		return nil, err
	}
	var commits []CommitSummary
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			commits = append(commits, CommitSummary{Hash: parts[0]})
			continue
		}
		commits = append(commits, CommitSummary{Hash: parts[0], Subject: parts[1]})
	}
	return commits, nil
}

// brReady runs `br ready --format json` and returns BeadSummary slice.
func brReady(ctx context.Context, brPath, projectDir string) ([]BeadSummary, error) {
	out, err := runCmd(ctx, brPath, "--format", "json", "ready")
	if err != nil {
		return nil, err
	}
	return parseBrReadyJSON(out, projectDir)
}

type brReadySummaryItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Priority int    `json:"priority"`
	Status   string `json:"status"`
}

func parseBrReadyJSON(data []byte, _ string) ([]BeadSummary, error) {
	var items []brReadySummaryItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	out := make([]BeadSummary, 0, len(items))
	for _, it := range items {
		out = append(out, BeadSummary{
			BeadID:   it.ID,
			Title:    it.Title,
			Priority: it.Priority,
			Status:   it.Status,
		})
	}
	return out, nil
}

// brListByStatus runs `br list --status <status> --json` and returns BeadSummary.
func brListByStatus(ctx context.Context, brPath, _ string, status string) ([]BeadSummary, error) {
	out, err := runCmd(ctx, brPath, "list", "--status", status, "--json")
	if err != nil {
		return nil, err
	}
	return parseBrListJSON(out)
}

type brListEnvelope struct {
	Issues []brListItem `json:"issues"`
}

type brListItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Priority int    `json:"priority"`
	Status   string `json:"status"`
}

func parseBrListJSON(data []byte) ([]BeadSummary, error) {
	var env brListEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	out := make([]BeadSummary, 0, len(env.Issues))
	for _, it := range env.Issues {
		out = append(out, BeadSummary{
			BeadID:   it.ID,
			Title:    it.Title,
			Priority: it.Priority,
			Status:   it.Status,
		})
	}
	return out, nil
}

// kerfNext runs `kerf next --format=json` and returns the parsed output.
func kerfNext(ctx context.Context, kerfPath, _ string) (interface{}, error) {
	out, err := runCmd(ctx, kerfPath, "next", "--format=json")
	if err != nil {
		return nil, err
	}
	var v interface{}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// runCmd executes a command and returns its stdout. Stderr is discarded.
func runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: controlled inputs only
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// ZeroEventID is the nil EventID used to indicate "scan from beginning".
var ZeroEventID = core.EventID(uuid.Nil)
