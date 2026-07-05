// Package sessiondata implements the always-on session-data collector fired from
// emitDone at the end of every daemon run. It reads session logs (Claude
// transcripts via session_log_location events) and appends one normalized record
// per run to <project>/.harmonik/session-data.jsonl.
//
// harmonik usage reads this file as a pre-computed VIEW instead of re-deriving
// token counts from raw transcripts on every invocation.
//
// Bead: hk-eval-prog-sessiondata-hook-vmxrk (WS1b).
package sessiondata

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ── Token types ───────────────────────────────────────────────────────────────

// TokenUsage holds the four token categories from a Claude turn or rollup.
type TokenUsage struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	CacheCreation int64 `json:"cache_creation"`
	CacheRead     int64 `json:"cache_read"`
}

// Total returns the sum of all four token categories.
func (u TokenUsage) Total() int64 {
	return u.Input + u.Output + u.CacheCreation + u.CacheRead
}

// Add accumulates other into u in-place.
func (u *TokenUsage) Add(other TokenUsage) {
	u.Input += other.Input
	u.Output += other.Output
	u.CacheCreation += other.CacheCreation
	u.CacheRead += other.CacheRead
}

// CacheReadPct returns the fraction of total tokens that were cache reads.
func (u TokenUsage) CacheReadPct() float64 {
	t := u.Total()
	if t == 0 {
		return 0
	}
	return 100.0 * float64(u.CacheRead) / float64(t)
}

// ── Pricing ──────────────────────────────────────────────────────────────────

// ModelPrice holds per-million-token prices (USD) for a model.
type ModelPrice struct {
	Input         float64
	Output        float64
	CacheCreation float64
	CacheRead     float64
}

var pricingTable = map[string]ModelPrice{
	"claude-opus-4-8":   {15.0, 75.0, 18.75, 1.50},
	"claude-opus-4":     {15.0, 75.0, 18.75, 1.50},
	"claude-sonnet-4-6": {3.0, 15.0, 3.75, 0.30},
	"claude-sonnet-4-5": {3.0, 15.0, 3.75, 0.30},
	"claude-sonnet-4":   {3.0, 15.0, 3.75, 0.30},
	"claude-haiku-4-8":  {0.80, 4.00, 1.00, 0.08},
	"claude-haiku-3-5":  {0.80, 4.00, 1.00, 0.08},
	"claude-haiku-3":    {0.25, 1.25, 0.30, 0.03},
}

var defaultPrice = ModelPrice{3.0, 15.0, 3.75, 0.30}

// PriceFor returns the per-million-token price for the given model.
func PriceFor(model string) ModelPrice {
	if p, ok := pricingTable[model]; ok {
		return p
	}
	ml := strings.ToLower(model)
	for k, p := range pricingTable {
		if strings.Contains(ml, strings.ToLower(k)) {
			return p
		}
	}
	return defaultPrice
}

// ComputeCost returns the USD cost for the given token usage and model.
func ComputeCost(u TokenUsage, model string) float64 {
	p := PriceFor(model)
	return float64(u.Input)*p.Input/1_000_000 +
		float64(u.Output)*p.Output/1_000_000 +
		float64(u.CacheCreation)*p.CacheCreation/1_000_000 +
		float64(u.CacheRead)*p.CacheRead/1_000_000
}

// ── Record schema (schema_version 1) ─────────────────────────────────────────

// NodeRecord is the per-node breakdown element in Record.Nodes.
type NodeRecord struct {
	NodeID    string      `json:"node_id"`
	WallTimeS float64     `json:"wall_time_s,omitempty"`
	Tokens    *TokenUsage `json:"tokens,omitempty"`
}

// Record is one row in session-data.jsonl. Schema version 1.
// See plans/2026-07-03-eval-program/01-run-matrix-and-metrics.md §2.2.
type Record struct {
	SchemaVersion int          `json:"schema_version"`
	RunID         string       `json:"run_id"`
	BeadID        string       `json:"bead_id"`
	QueueID       string       `json:"queue_id,omitempty"`
	Harness       string       `json:"harness,omitempty"`
	Model         string       `json:"model,omitempty"`
	Success       bool         `json:"success"`
	StartedAt     string       `json:"started_at,omitempty"`
	EndedAt       string       `json:"ended_at,omitempty"`
	WallTimeS     float64      `json:"wall_time_s,omitempty"`
	Nodes         []NodeRecord `json:"nodes,omitempty"`
	TokensTotal   TokenUsage   `json:"tokens_total"`
	CostUSD       *float64     `json:"cost_usd,omitempty"`
	TurnCount     int          `json:"turn_count"`
	CommitSHA     string       `json:"commit_sha,omitempty"`
}

// SessionDataPath returns the path to session-data.jsonl for the given project directory.
func SessionDataPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "session-data.jsonl")
}

// ── Collect ───────────────────────────────────────────────────────────────────

// CollectParams is the input to Collect.
type CollectParams struct {
	RunID             string
	BeadID            string
	QueueID           string
	Harness           string // resolved agent type (e.g. "claude-code", "pi", "codex")
	Model             string // effective model string (empty for codex)
	Success           bool
	CommitSHA         string // HEAD SHA of the run branch after a successful commit (empty on failure)
	StartedAt         time.Time
	EndedAt           time.Time
	ProjectDir        string
	ClaudeProjectsDir string
}

// Collect reads session logs for the given run, builds a Record, and appends it
// to <projectDir>/.harmonik/session-data.jsonl. It is designed to be called in
// a goroutine from emitDone — best-effort, off the hot path.
func Collect(p CollectParams) error {
	eventsFile := filepath.Join(p.ProjectDir, ".harmonik", "events", "events.jsonl")
	runData, err := buildRunEventData(eventsFile, p.RunID)
	if err != nil {
		return fmt.Errorf("sessiondata: buildRunEventData: %w", err)
	}

	// Prefer started_at from the run_started event if caller didn't pass a precise value.
	startedAt := p.StartedAt
	if runData.StartedAt != "" && startedAt.IsZero() {
		if t, parseErr := time.Parse(time.RFC3339, runData.StartedAt); parseErr == nil {
			startedAt = t
		}
	}
	endedAt := p.EndedAt
	wallTimeS := math.Round(endedAt.Sub(startedAt).Seconds()*10) / 10

	// Prefer values passed from emitDone over those derived from events.
	beadID := p.BeadID
	if beadID == "" {
		beadID = runData.BeadID
	}
	queueID := p.QueueID
	if queueID == "" {
		queueID = runData.QueueID
	}

	// Build per-node time windows from node_dispatch_requested events.
	// Each window spans from the node's dispatch time to the next node's dispatch
	// time (or the run end time for the last node).
	type nodeWindow struct {
		NodeID  string
		StartAt time.Time
		EndAt   time.Time
	}
	var windows []nodeWindow
	if len(runData.NodeDispatchEvents) > 0 {
		for i, ev := range runData.NodeDispatchEvents {
			w := nodeWindow{NodeID: ev.NodeID, StartAt: ev.RequestedAt}
			if i+1 < len(runData.NodeDispatchEvents) {
				w.EndAt = runData.NodeDispatchEvents[i+1].RequestedAt
			} else {
				w.EndAt = endedAt
			}
			windows = append(windows, w)
		}
	}

	// Build node_id → log path from session_log_location events.
	logByNodeID := make(map[string]string, len(runData.LogPaths))
	for i, lp := range runData.LogPaths {
		nid := runData.NodeIDs[i]
		if nid == "" {
			nid = "implement"
		}
		if _, exists := logByNodeID[nid]; !exists {
			logByNodeID[nid] = lp
		}
	}

	// Read Claude-style transcript logs; attribute turns to nodes by time window
	// when dispatch events are available (DOT multi-node runs), or by transcript
	// file (single-node / non-DOT runs). Pi/codex token extraction is P2.
	var total TokenUsage
	var turnCount int
	var nodes []NodeRecord

	if len(windows) > 0 {
		// DOT multi-node path: process in dispatch order with window-filtered attribution.
		handledNodeIDs := make(map[string]bool, len(windows))
		for _, w := range windows {
			handledNodeIDs[w.NodeID] = true
			wallTimeS := math.Round(w.EndAt.Sub(w.StartAt).Seconds()*10) / 10

			lp := logByNodeID[w.NodeID]
			if lp == "" {
				// Non-agentic node (e.g. shell gate): WallTimeS only, no tokens.
				nodes = append(nodes, NodeRecord{NodeID: w.NodeID, WallTimeS: wallTimeS})
				continue
			}
			resolved := ResolveTranscriptPath(lp, p.ClaudeProjectsDir)
			if resolved == "" {
				nodes = append(nodes, NodeRecord{NodeID: w.NodeID, WallTimeS: wallTimeS})
				continue
			}
			turns, _ := readTranscript(resolved)
			var nodeTok TokenUsage
			for _, t := range turns {
				// Filter by window when the turn carries a timestamp.
				if !t.Timestamp.IsZero() {
					if t.Timestamp.Before(w.StartAt) || !t.Timestamp.Before(w.EndAt) {
						continue
					}
				}
				total.Add(t.Usage)
				nodeTok.Add(t.Usage)
				turnCount++
			}
			nr := NodeRecord{NodeID: w.NodeID, WallTimeS: wallTimeS}
			if nodeTok.Total() > 0 {
				nt := nodeTok
				nr.Tokens = &nt
			}
			nodes = append(nodes, nr)
		}
		// Include any session_log nodes not covered by dispatch windows (edge case).
		for i, lp := range runData.LogPaths {
			nid := runData.NodeIDs[i]
			if nid == "" {
				nid = "implement"
			}
			if handledNodeIDs[nid] {
				continue
			}
			resolved := ResolveTranscriptPath(lp, p.ClaudeProjectsDir)
			if resolved == "" {
				continue
			}
			turns, _ := readTranscript(resolved)
			var nodeTok TokenUsage
			for _, t := range turns {
				total.Add(t.Usage)
				nodeTok.Add(t.Usage)
				turnCount++
			}
			if len(turns) > 0 {
				nt := nodeTok
				nodes = append(nodes, NodeRecord{NodeID: nid, Tokens: &nt})
			}
		}
	} else {
		// Non-DOT path: attribute all turns from each transcript to its node.
		for i, lp := range runData.LogPaths {
			resolved := ResolveTranscriptPath(lp, p.ClaudeProjectsDir)
			if resolved == "" {
				continue
			}
			turns, _ := readTranscript(resolved)
			var nodeTok TokenUsage
			for _, t := range turns {
				total.Add(t.Usage)
				nodeTok.Add(t.Usage)
				turnCount++
			}
			nodeID := runData.NodeIDs[i]
			if nodeID == "" {
				nodeID = "implement"
			}
			if len(turns) > 0 {
				nt := nodeTok
				nodes = append(nodes, NodeRecord{NodeID: nodeID, Tokens: &nt})
			}
		}
	}

	// Cost is nil when no price table entry exists (Pi/ornith with no known pricing).
	var costUSD *float64
	if p.Model != "" {
		c := ComputeCost(total, p.Model)
		costUSD = &c
	}

	rec := Record{
		SchemaVersion: 1,
		RunID:         p.RunID,
		BeadID:        beadID,
		QueueID:       queueID,
		Harness:       p.Harness,
		Model:         p.Model,
		Success:       p.Success,
		CommitSHA:     p.CommitSHA,
		StartedAt:     startedAt.UTC().Format(time.RFC3339),
		EndedAt:       endedAt.UTC().Format(time.RFC3339),
		WallTimeS:     wallTimeS,
		Nodes:         nodes,
		TokensTotal:   total,
		CostUSD:       costUSD,
		TurnCount:     turnCount,
	}
	return Append(p.ProjectDir, rec)
}

// Append appends rec as a JSONL line to <projectDir>/.harmonik/session-data.jsonl.
func Append(projectDir string, rec Record) error {
	path := SessionDataPath(projectDir)
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions; path is projectDir-derived.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sessiondata: MkdirAll: %w", err)
	}
	//nolint:gosec // G304: path is projectDir-derived (operator config, not user input).
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // G306: world-readable session metrics, not a secret.
	if err != nil {
		return fmt.Errorf("sessiondata: OpenFile: %w", err)
	}
	defer f.Close() //nolint:errcheck // best-effort; error returned by Write below takes priority.
	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("sessiondata: json.Marshal: %w", err)
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadAll reads all records from <projectDir>/.harmonik/session-data.jsonl within
// the given normalized ISO time window. Returns nil, nil when the file is absent.
func ReadAll(projectDir, since, until string) ([]Record, error) {
	path := SessionDataPath(projectDir)
	//nolint:gosec // G304: path is projectDir-derived (operator config, not user input).
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only file; close error is not actionable.

	var records []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if since != "" && rec.EndedAt != "" && normTS(rec.EndedAt) < since {
			continue
		}
		if until != "" && rec.StartedAt != "" && normTS(rec.StartedAt) > until {
			continue
		}
		records = append(records, rec)
	}
	return records, sc.Err()
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// nodeDispatchEvent holds one node_dispatch_requested event's key fields.
type nodeDispatchEvent struct {
	NodeID      string
	RequestedAt time.Time
}

type runEventData struct {
	BeadID    string
	QueueID   string
	NodeIDs   []string
	StartedAt string
	LogPaths  []string
	// NodeDispatchEvents is the ordered list of dispatched nodes (from
	// node_dispatch_requested events), used for per-node time windows.
	// Deduplicated by NodeID: only the first dispatch for a given node is kept
	// so retried nodes do not produce duplicate windows.
	NodeDispatchEvents []nodeDispatchEvent
}

// buildRunEventData scans events.jsonl for events belonging to runID.
// Only envelope run_id is checked (session_log_location and run_started use
// bus.EmitWithRunID). model_selected / harness_selected are read from the caller.
func buildRunEventData(eventsFile, runID string) (*runEventData, error) {
	//nolint:gosec // G304: eventsFile is projectDir-derived (operator config).
	f, err := os.Open(eventsFile)
	if err != nil {
		return &runEventData{}, nil // absent = treat as no events
	}
	defer f.Close() //nolint:errcheck // read-only file.

	d := &runEventData{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if sdJsonStr(ev["run_id"]) != runID {
			continue
		}
		evType := sdJsonStr(ev["type"])
		var payload map[string]json.RawMessage
		if ev["payload"] != nil {
			_ = json.Unmarshal(ev["payload"], &payload)
		}
		switch evType {
		case "run_started":
			if d.BeadID == "" {
				d.BeadID = sdJsonStr(payload["bead_id"])
			}
			if d.QueueID == "" {
				d.QueueID = sdJsonStr(payload["queue_id"])
			}
			if d.StartedAt == "" {
				d.StartedAt = sdJsonStr(payload["started_at"])
			}
		case "session_log_location":
			lp := sdJsonStr(payload["log_path"])
			if lp == "" {
				continue
			}
			dup := false
			for _, e := range d.LogPaths {
				if e == lp {
					dup = true
					break
				}
			}
			if !dup {
				d.LogPaths = append(d.LogPaths, lp)
				nodeID := sdJsonStr(payload["node_id"])
				d.NodeIDs = append(d.NodeIDs, nodeID)
			}
		case "node_dispatch_requested":
			nodeID := sdJsonStr(payload["node_id"])
			requestedAtStr := sdJsonStr(payload["requested_at"])
			if nodeID == "" || requestedAtStr == "" {
				continue
			}
			// Deduplicate by NodeID: skip retried dispatches for the same node.
			alreadySeen := false
			for _, ev := range d.NodeDispatchEvents {
				if ev.NodeID == nodeID {
					alreadySeen = true
					break
				}
			}
			if alreadySeen {
				continue
			}
			if t, parseErr := time.Parse(time.RFC3339, requestedAtStr); parseErr == nil {
				d.NodeDispatchEvents = append(d.NodeDispatchEvents, nodeDispatchEvent{
					NodeID:      nodeID,
					RequestedAt: t,
				})
			}
		}
	}
	return d, sc.Err()
}

type transcriptTurn struct {
	Model     string
	Usage     TokenUsage
	Timestamp time.Time // zero when the transcript entry carries no timestamp
}

// readTranscript reads ALL assistant turns from the given Claude transcript file.
func readTranscript(path string) ([]transcriptTurn, error) {
	//nolint:gosec // G304: path comes from session_log_location payload (operator-controlled).
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only file.

	var turns []transcriptTurn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var entry map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if sdJsonStr(entry["type"]) != "assistant" {
			continue
		}
		var msg map[string]json.RawMessage
		if entry["message"] == nil {
			continue
		}
		if err := json.Unmarshal(entry["message"], &msg); err != nil {
			continue
		}
		if msg["usage"] == nil {
			continue
		}
		var rawUsage map[string]json.RawMessage
		if err := json.Unmarshal(msg["usage"], &rawUsage); err != nil {
			continue
		}
		u := TokenUsage{
			Input:         sdJsonInt64(rawUsage["input_tokens"]),
			Output:        sdJsonInt64(rawUsage["output_tokens"]),
			CacheCreation: sdJsonInt64(rawUsage["cache_creation_input_tokens"]),
			CacheRead:     sdJsonInt64(rawUsage["cache_read_input_tokens"]),
		}
		turn := transcriptTurn{
			Model: sdJsonStr(msg["model"]),
			Usage: u,
		}
		// Parse the top-level "timestamp" field (RFC3339 with optional sub-seconds).
		if tsStr := sdJsonStr(entry["timestamp"]); tsStr != "" {
			if t, parseErr := time.Parse(time.RFC3339Nano, tsStr); parseErr == nil {
				turn.Timestamp = t
			} else if t, parseErr = time.Parse(time.RFC3339, tsStr); parseErr == nil {
				turn.Timestamp = t
			}
		}
		turns = append(turns, turn)
	}
	return turns, sc.Err()
}

var sdTsRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})`)

func normTS(ts string) string {
	if ts == "" {
		return ""
	}
	if m := sdTsRe.FindStringSubmatch(ts); m != nil {
		return m[1] + "Z"
	}
	if len(ts) >= 20 {
		return strings.TrimRight(ts[:20], "T") + "Z"
	}
	return ts
}

func sdJsonStr(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return strings.Trim(string(raw), `"`)
	}
	return s
}

func sdJsonInt64(raw json.RawMessage) int64 {
	if raw == nil {
		return 0
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	return n
}

// ResolveTranscriptPath translates a session_log_location log_path to the
// actual on-disk transcript file path. Exported for use by internal/usage.
func ResolveTranscriptPath(logPath, claudeProjectsDir string) string {
	if logPath == "" {
		return ""
	}
	if _, err := os.Stat(logPath); err == nil {
		return logPath
	}
	fname := filepath.Base(logPath)

	if m := regexp.MustCompile(`worktrees-([0-9a-f-]{36})/([^/]+\.jsonl)$`).FindStringSubmatch(logPath); m != nil {
		runID := m[1]
		user := os.Getenv("USER")
		candidate := filepath.Join(claudeProjectsDir,
			fmt.Sprintf("-Users-%s-github-harmonik--harmonik-worktrees-%s", user, runID), fname)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		for _, suffix := range []string{"-reviewer-1", "-reviewer-2", "-reviewer-3"} {
			c2 := filepath.Join(claudeProjectsDir,
				fmt.Sprintf("-Users-%s-github-harmonik--harmonik-worktrees-%s%s", user, runID, suffix), fname)
			if _, err := os.Stat(c2); err == nil {
				return c2
			}
		}
	}

	entries, _ := os.ReadDir(claudeProjectsDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fp := filepath.Join(claudeProjectsDir, e.Name(), fname)
		if _, err := os.Stat(fp); err == nil {
			return fp
		}
	}
	return ""
}
