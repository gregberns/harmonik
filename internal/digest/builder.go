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

	// addErr records a non-fatal collection error per DC-007. Every individual
	// source failure is surfaced in out.Errors; only a missing .harmonik/ (above)
	// is a hard failure (DC-002).
	addErr := func(source string, err error) {
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", source, err))
		}
	}

	// --- Queue summary ---
	var queueErr error
	out.Queue, queueErr = buildQueueSummary(ctx, in.ProjectDir, lim)
	addErr("queue", queueErr)
	// DC-005: surface the count of active runs hidden by the cap so the
	// operator can tell how many runs were omitted (breaks DC-005 otherwise).
	if out.Queue.ActiveRunsOmitted > 0 {
		out.Truncated = ensureTruncation(out.Truncated)
		out.Truncated.ActiveRunsOmitted = out.Queue.ActiveRunsOmitted
	}

	// --- Recent commits on origin/main ---
	var commitsErr error
	out.RecentCommits, commitsErr = recentCommits(ctx, in.ProjectDir, in.GitPath, 10)
	addErr("recent_commits", commitsErr)

	// --- Events via ScanAfter ---
	eventsPath := filepath.Join(harmonikDir, "events", "events.jsonl")
	var eventsTrunc *TruncationReport
	out.RecentEvents, eventsTrunc = buildRecentEvents(eventsPath, in.SinceEventID, lim)
	if eventsTrunc != nil && eventsTrunc.RecentEventsOmitted > 0 {
		out.Truncated = ensureTruncation(out.Truncated)
		out.Truncated.RecentEventsOmitted = eventsTrunc.RecentEventsOmitted
	}

	// --- br ready + br list --status in_progress ---
	if in.BrPath != "" {
		var readyErr, inProgErr error
		out.ReadyBeads, readyErr = brReady(ctx, in.BrPath, in.ProjectDir)
		addErr("br_ready", readyErr)
		out.InProgressBeads, inProgErr = brListByStatus(ctx, in.BrPath, in.ProjectDir, "in_progress")
		addErr("br_list", inProgErr)
	}

	// --- notes.jsonl ---
	notesPath := filepath.Join(harmonikDir, "cognition", "notes.jsonl")
	notes, notesErr := readOpenNotes(notesPath)
	addErr("notes", notesErr)
	out.OpenNotes, out.Truncated = applyNoteTruncation(notes, lim, out.Truncated)

	// --- kerf next --format=json ---
	if in.KerfPath != "" {
		var kerfErr error
		out.KerfNext, kerfErr = kerfNext(ctx, in.KerfPath, in.ProjectDir)
		addErr("kerf_next", kerfErr)
	}

	// --- pending decision_required events (EV-044) ---
	// Scanned from the beginning of events.jsonl, ignoring SinceEventID, so
	// that unacknowledged decisions surface even during quiet periods where the
	// watermark has advanced past the event.
	out.PendingDecisions = buildPendingDecisions(eventsPath)

	// --- suppression resolver (flywheel-motion.md §3) ---
	// Deterministic, LLM-free. EXECUTE-BACKLOG is the default (Suppressed=false).
	// Reads .harmonik/config.yaml sentinel: block; config errors are non-fatal
	// (fail-open: the invalid source is treated as inactive per §3.2).
	sentinelCfg, sentinelErr := LoadSentinelConfig(in.ProjectDir)
	if sentinelErr != nil {
		// Non-fatal: record as a collection error and use zero config (all defaults).
		addErr("sentinel_config", sentinelErr)
	}
	out.SuppressionState = ResolveSuppressionState(eventsPath, now, sentinelCfg)

	// --- HasUndeployedTail (flywheel-motion.md §5.2, §5.3) ---
	// If Phase-2 classes are configured and br is available, check whether any
	// closed bead carries a Phase-2 class label. Until the verify step lands a
	// closed Phase-2 bead counts as merged-but-undeployed (actionable work for
	// the opportunity gate).
	if in.BrPath != "" && len(sentinelCfg.Phase2Classes()) > 0 {
		var undeployedErr error
		out.HasUndeployedTail, undeployedErr = buildHasUndeployedTail(ctx, in.BrPath, in.ProjectDir, sentinelCfg.Phase2Classes())
		addErr("undeployed_tail", undeployedErr)
	}

	return out, nil
}

// buildPendingDecisions scans the full events.jsonl (from the beginning, ignoring
// SinceEventID) and returns every decision_required event whose ack_token has no
// matching decision_acknowledged event (EV-044).
//
// "Quiet" suppression — where a watermark-advancing consumer would skip old events
// — MUST NOT apply to decision_required: they must appear in every digest until
// explicitly acknowledged.
func buildPendingDecisions(eventsPath string) []DecisionRequiredSummary {
	type decisionRequiredPayload struct {
		Subject struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"subject"`
		Reason          string `json:"reason"`
		SuggestedAction string `json:"suggested_action"`
		AckToken        string `json:"ack_token"`
	}
	type decisionAcknowledgedPayload struct {
		AckToken string `json:"ack_token"`
	}

	// First pass: collect all decision_required and decision_acknowledged events.
	var decisions []struct {
		eventID string
		payload decisionRequiredPayload
	}
	ackedTokens := make(map[string]struct{})

	for ev := range eventbus.ScanAfter(eventsPath, ZeroEventID) {
		switch ev.Type {
		case "decision_required":
			var p decisionRequiredPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.AckToken == "" {
				continue
			}
			decisions = append(decisions, struct {
				eventID string
				payload decisionRequiredPayload
			}{eventID: ev.EventID.String(), payload: p})
		case "decision_acknowledged":
			var p decisionAcknowledgedPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.AckToken != "" {
				ackedTokens[p.AckToken] = struct{}{}
			}
		}
	}

	// Second pass: filter to unacknowledged decisions.
	var out []DecisionRequiredSummary
	for _, d := range decisions {
		if _, acked := ackedTokens[d.payload.AckToken]; acked {
			continue
		}
		out = append(out, DecisionRequiredSummary{
			EventID:         d.eventID,
			AckToken:        d.payload.AckToken,
			SubjectKind:     d.payload.Subject.Kind,
			SubjectID:       d.payload.Subject.ID,
			Reason:          d.payload.Reason,
			SuggestedAction: d.payload.SuggestedAction,
		})
	}
	return out
}

// ensureTruncation returns tr, allocating a fresh TruncationReport when tr is nil.
func ensureTruncation(tr *TruncationReport) *TruncationReport {
	if tr == nil {
		return &TruncationReport{}
	}
	return tr
}

// buildQueueSummary reads queue.json and returns a QueueSummary. The caller's
// ctx is threaded through to queue.Load. A nil error with Present=false means
// queue.json is absent (no active queue, not an error); a non-nil error is a
// genuine load failure to be surfaced per DC-007.
func buildQueueSummary(ctx context.Context, projectDir string, lim Limits) (QueueSummary, error) {
	q, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		return QueueSummary{Present: false}, err
	}
	if q == nil {
		return QueueSummary{Present: false}, nil
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
		// DC-005: record the omission count so it can flow into out.Truncated.
		sum.ActiveRunsOmitted = len(dispatched) - cap
		sum.ActiveRuns = dispatched[:cap]
	} else {
		sum.ActiveRuns = dispatched
	}
	return sum, nil
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

// brBeadLabels holds the minimal fields needed to check Phase-2 class labels.
type brBeadLabels struct {
	Labels []string `json:"labels"`
}

// brBeadLabelsEnvelope is the JSON shape of `br list --status closed --json`.
type brBeadLabelsEnvelope struct {
	Issues []brBeadLabels `json:"issues"`
}

// brClosedBeadsWithLabels runs `br list --status closed --json` and returns a
// slice of label sets for every closed bead. Errors are returned to the caller
// for non-fatal surfacing per DC-007.
func brClosedBeadsWithLabels(ctx context.Context, brPath string) ([][]string, error) {
	out, err := runCmd(ctx, brPath, "list", "--status", "closed", "--json")
	if err != nil {
		return nil, err
	}
	var env brBeadLabelsEnvelope
	if jsonErr := json.Unmarshal(out, &env); jsonErr != nil {
		return nil, jsonErr
	}
	result := make([][]string, 0, len(env.Issues))
	for _, item := range env.Issues {
		result = append(result, item.Labels)
	}
	return result, nil
}

// buildHasUndeployedTail returns true when at least one closed bead carries a
// Phase-2 class label (flywheel-motion.md §5.2, §5.3). Phase-2 classes are
// provided by the caller (SentinelConfig.Phase2Classes()). Any error querying
// br is returned for non-fatal surfacing; the boolean is false on error.
func buildHasUndeployedTail(ctx context.Context, brPath, _ string, phase2Classes []string) (bool, error) {
	if len(phase2Classes) == 0 {
		return false, nil
	}
	classSet := make(map[string]struct{}, len(phase2Classes))
	for _, c := range phase2Classes {
		classSet[c] = struct{}{}
	}
	labelSets, err := brClosedBeadsWithLabels(ctx, brPath)
	if err != nil {
		return false, err
	}
	for _, labels := range labelSets {
		for _, l := range labels {
			if _, ok := classSet[l]; ok {
				return true, nil
			}
		}
	}
	return false, nil
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
