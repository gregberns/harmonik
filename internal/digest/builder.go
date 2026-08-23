package digest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/presence"
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

	addErr := func(source string, err error) {
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", source, err))
		}
	}

	var queueErr error
	out.Queue, queueErr = buildQueueSummary(ctx, in.ProjectDir, lim)
	addErr("queue", queueErr)
	if out.Queue.ActiveRunsOmitted > 0 {
		out.Truncated = ensureTruncation(out.Truncated)
		out.Truncated.ActiveRunsOmitted = out.Queue.ActiveRunsOmitted
	}

	var commitsErr error
	out.RecentCommits, commitsErr = recentCommits(ctx, in.ProjectDir, in.GitPath, 10)
	addErr("recent_commits", commitsErr)

	eventsPath := filepath.Join(harmonikDir, "events", "events.jsonl")
	var eventsTrunc *TruncationReport
	out.RecentEvents, eventsTrunc = buildRecentEvents(eventsPath, in.SinceEventID, lim)
	if eventsTrunc != nil && eventsTrunc.RecentEventsOmitted > 0 {
		out.Truncated = ensureTruncation(out.Truncated)
		out.Truncated.RecentEventsOmitted = eventsTrunc.RecentEventsOmitted
	}

	if in.BrPath != "" {
		var readyErr, inProgErr error
		out.ReadyBeads, readyErr = brReady(ctx, in.BrPath, in.ProjectDir)
		addErr("br_ready", readyErr)
		out.InProgressBeads, inProgErr = brListByStatus(ctx, in.BrPath, in.ProjectDir, "in_progress")
		addErr("br_list", inProgErr)
	}

	notesPath := filepath.Join(harmonikDir, "cognition", "notes.jsonl")
	notes, notesErr := readOpenNotes(notesPath)
	addErr("notes", notesErr)
	out.OpenNotes, out.Truncated = applyNoteTruncation(notes, lim, out.Truncated)

	if in.KerfPath != "" {
		var kerfErr error
		out.KerfNext, kerfErr = kerfNext(ctx, in.KerfPath, in.ProjectDir)
		addErr("kerf_next", kerfErr)
	}

	acksDir := filepath.Join(in.ProjectDir, ".harmonik", "decision_acks")
	out.PendingDecisions = buildPendingDecisions(eventsPath, acksDir)

	sentinelCfg, sentinelErr := LoadSentinelConfig(in.ProjectDir)
	if sentinelErr != nil {
		addErr("sentinel_config", sentinelErr)
	}
	out.SuppressionState = ResolveSuppressionState(eventsPath, now, sentinelCfg)

	if in.BrPath != "" && len(sentinelCfg.Phase2Classes()) > 0 {
		var undeployedErr error
		out.HasUndeployedTail, undeployedErr = buildHasUndeployedTail(ctx, in.BrPath, in.ProjectDir, sentinelCfg.Phase2Classes())
		addErr("undeployed_tail", undeployedErr)
	}

	out.CommsWho = buildCommsWho(eventsPath, now)

	var crewErr error
	out.Crews, crewErr = buildCrewList(in.ProjectDir)
	addErr("crew_list", crewErr)

	var tmuxErr error
	out.TmuxFleet, tmuxErr = buildTmuxFleet(ctx)
	addErr("tmux_fleet", tmuxErr)

	var pausedErr error
	out.PausedQueues, pausedErr = buildPausedQueues(ctx, in.ProjectDir)
	addErr("paused_queues", pausedErr)

	if in.KerfPath != "" {
		var kerfMapErr error
		out.KerfMap, kerfMapErr = kerfMapText(ctx, in.KerfPath)
		addErr("kerf_map", kerfMapErr)
	}

	return out, nil
}

func buildCommsWho(eventsPath string, now time.Time) []CommsWhoEntry {
	registry := presence.ComputeRegistry(eventsPath)
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]CommsWhoEntry, 0, len(names))
	for _, name := range names {
		rec := registry[name]
		var status string
		switch presence.GetStateAt(rec, now) {
		case presence.StateOnline:
			status = "online"
		case presence.StateStale:
			status = "stale"
		default:
			continue // offline: omitted, matching `harmonik comms who`
		}
		out = append(out, CommsWhoEntry{
			Agent:    name,
			Status:   status,
			LastSeen: rec.EffectiveLastSeen,
		})
	}
	return out
}

func buildCrewList(projectDir string) ([]CrewSummary, error) {
	records, err := crew.List(projectDir)
	if err != nil {
		return nil, err
	}
	out := make([]CrewSummary, 0, len(records))
	for _, r := range records {
		out = append(out, CrewSummary{
			Name:      r.Name,
			Type:      r.EffectiveType(),
			Queue:     r.Queue,
			Epic:      r.Epic,
			SessionID: r.SessionID,
			StartedAt: r.StartedAt,
		})
	}
	return out, nil
}

func buildTmuxFleet(ctx context.Context) ([]TmuxSessionSummary, error) {
	adapter := tmux.OSAdapter{}
	sessions, err := adapter.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TmuxSessionSummary, 0, len(sessions))
	var errs []error
	for _, s := range sessions {
		windows, wErr := adapter.ListWindows(ctx, s)
		if wErr != nil {
			if !errors.Is(wErr, tmux.ErrNoSession) {
				errs = append(errs, fmt.Errorf("list-windows %s: %w", s, wErr))
			}
			out = append(out, TmuxSessionSummary{Session: s})
			continue
		}
		out = append(out, TmuxSessionSummary{Session: s, Windows: windows})
	}
	return out, errors.Join(errs...)
}

var pausedQueueStatusRe = regexp.MustCompile(`paused|complete-with-failures`)

func buildPausedQueues(ctx context.Context, projectDir string) ([]PausedQueueSummary, error) {
	names, err := queue.EnumerateQueueNames(projectDir)
	if err != nil {
		return nil, err
	}

	var out []PausedQueueSummary
	var errs []error
	for _, name := range names {
		q, loadErr := queue.Load(ctx, projectDir, name)
		if loadErr != nil {
			errs = append(errs, fmt.Errorf("load queue %s: %w", name, loadErr))
			continue
		}
		if q == nil {
			continue
		}
		status := string(q.Status)
		if pausedQueueStatusRe.MatchString(status) {
			out = append(out, PausedQueueSummary{Name: name, Status: status})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errors.Join(errs...)
}

func kerfMapText(ctx context.Context, kerfPath string) (string, error) {
	out, err := runCmd(ctx, kerfPath, "map")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func buildPendingDecisions(eventsPath, acksDir string) []DecisionRequiredSummary {
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
		default:
		}
	}

	seen := make(map[string]struct{}) // ack_token → already in out
	out := make([]DecisionRequiredSummary, 0, len(decisions))
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
		seen[d.payload.AckToken] = struct{}{}
	}

	if entries, err := os.ReadDir(acksDir); err == nil {
		type ackRecord struct {
			SchemaVersion int    `json:"schema_version"`
			AckToken      string `json:"ack_token"`
			Status        string `json:"status"`
			SubjectKind   string `json:"subject_kind"`
			SubjectID     string `json:"subject_id"`
			Reason        string `json:"reason"`
			EmittedAt     string `json:"emitted_at"`
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, readErr := os.ReadFile(filepath.Join(acksDir, entry.Name())) //nolint:gosec // G304: operator-controlled dir
			if readErr != nil {
				continue
			}
			var rec ackRecord
			if jsonErr := json.Unmarshal(data, &rec); jsonErr != nil {
				continue
			}
			if rec.Status != "pending" || rec.AckToken == "" {
				continue
			}
			if _, already := seen[rec.AckToken]; already {
				continue // already surfaced via events.jsonl
			}
			out = append(out, DecisionRequiredSummary{
				AckToken:    rec.AckToken,
				SubjectKind: rec.SubjectKind,
				SubjectID:   rec.SubjectID,
				Reason:      rec.Reason,
			})
		}
	}

	return out
}

func ensureTruncation(tr *TruncationReport) *TruncationReport {
	if tr == nil {
		return &TruncationReport{}
	}
	return tr
}

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
			case queue.ItemStatusCompleted, queue.ItemStatusFailed, queue.ItemStatusDeferredForLedgerDep:
			}
		}
	}

	limit := lim.maxActiveRuns()
	if limit > 0 && len(dispatched) > limit {
		sum.ActiveRunsOmitted = len(dispatched) - limit
		sum.ActiveRuns = dispatched[:limit]
	} else {
		sum.ActiveRuns = dispatched
	}
	return sum, nil
}

func buildRecentEvents(eventsPath string, sinceID core.EventID, lim Limits) ([]EventSummary, *TruncationReport) {
	all := make([]EventSummary, 0)
	for ev := range eventbus.ScanAfter(eventsPath, sinceID) {
		s := EventSummary{
			EventID: ev.EventID.String(),
			Type:    string(ev.Type),
		}
		if ev.RunID != nil {
			s.RunID = ev.RunID.String()
		}
		all = append(all, s)
	}

	limit := lim.maxRecentEvents()
	if limit > 0 && len(all) > limit {
		omitted := len(all) - limit
		tr := &TruncationReport{RecentEventsOmitted: omitted}
		return all[len(all)-limit:], tr
	}
	return all, nil
}

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

	limit := lim.maxOpenNotes()
	if limit > 0 && len(summaries) > limit {
		omitted := len(summaries) - limit
		if existing == nil {
			existing = &TruncationReport{}
		}
		existing.OpenNotesOmitted = omitted
		return summaries[:limit], existing
	}
	return summaries, existing
}

func recentCommits(ctx context.Context, projectDir, gitPath string, n int) ([]CommitSummary, error) {
	if gitPath == "" {
		gitPath = "git"
	}
	args := []string{"-C", projectDir, "log", "origin/main", "--oneline", fmt.Sprintf("-%d", n)}
	out, err := runCmd(ctx, gitPath, args...)
	if err != nil {
		return nil, err
	}
	commits := make([]CommitSummary, 0)
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

func brReady(ctx context.Context, brPath, projectDir string) ([]BeadSummary, error) {
	out, err := runCmd(ctx, brPath, "ready", "--limit", "0", "--json")
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

func brListByStatus(ctx context.Context, brPath, _, status string) ([]BeadSummary, error) {
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

type brBeadLabels struct {
	Labels []string `json:"labels"`
}

type brBeadLabelsEnvelope struct {
	Issues []brBeadLabels `json:"issues"`
}

func brClosedBeadsWithLabels(ctx context.Context, brPath string) ([][]string, error) {
	out, err := runCmd(ctx, brPath, "list", "--status", "closed", "--limit", "0", "--json")
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

// BuildHasUndeployedTail returns true when at least one closed bead carries a
// Phase-2 class label (flywheel-motion.md §5.2, §5.3). Used by the sentinel
// governor (FW2 hk-z1lr) to populate GovernorInput.HasUndeployedTail without
// building a full digest. Returns false (not an error) when phase2Classes is empty.
func BuildHasUndeployedTail(ctx context.Context, brPath string, phase2Classes []string) (bool, error) {
	return buildHasUndeployedTail(ctx, brPath, "", phase2Classes)
}

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

func runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
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
