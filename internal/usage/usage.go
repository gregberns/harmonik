// Package usage implements the harmonik token-usage join analysis.
//
// It joins Claude-Code session transcripts (~/.claude/projects/…/*.jsonl)
// against harmonik events.jsonl on run_id, producing per-run, per-bead, and
// per-model cost rollups. Daemon worktree runs are attributed via the
// gitBranch="run/<run_id>" field in each transcript turn; always-on
// orchestrator sessions (captain/crew) are surfaced as the idle-burn bucket.
//
// No daemon connection required; this package reads files only.
//
// Bead ref: hk-b89kk (Phase-0 harmonik usage verb).
package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Pricing table (per 1M tokens, USD) — Anthropic pricing as of June 2026.
// ──────────────────────────────────────────────────────────────────────────────

type modelPrice struct {
	Input         float64
	Output        float64
	CacheCreation float64
	CacheRead     float64
}

var pricingTable = map[string]modelPrice{
	"claude-opus-4-8":   {15.0, 75.0, 18.75, 1.50},
	"claude-opus-4":     {15.0, 75.0, 18.75, 1.50},
	"claude-sonnet-4-6": {3.0, 15.0, 3.75, 0.30},
	"claude-sonnet-4-5": {3.0, 15.0, 3.75, 0.30},
	"claude-sonnet-4":   {3.0, 15.0, 3.75, 0.30},
	"claude-haiku-4-8":  {0.80, 4.00, 1.00, 0.08},
	"claude-haiku-3-5":  {0.80, 4.00, 1.00, 0.08},
	"claude-haiku-3":    {0.25, 1.25, 0.30, 0.03},
}

var defaultPrice = modelPrice{3.0, 15.0, 3.75, 0.30}

func priceFor(model string) modelPrice {
	// Try exact match, then prefix/suffix scan for known families.
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

// ──────────────────────────────────────────────────────────────────────────────
// Core data structures
// ──────────────────────────────────────────────────────────────────────────────

// TokenUsage holds the four token categories from a Claude turn or rollup.
type TokenUsage struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	CacheCreation int64 `json:"cache_creation"`
	CacheRead     int64 `json:"cache_read"`
}

func (u TokenUsage) Total() int64 {
	return u.Input + u.Output + u.CacheCreation + u.CacheRead
}

func (u TokenUsage) CacheReadPct() float64 {
	t := u.Total()
	if t == 0 {
		return 0
	}
	return 100.0 * float64(u.CacheRead) / float64(t)
}

func (u *TokenUsage) Add(other TokenUsage) {
	u.Input += other.Input
	u.Output += other.Output
	u.CacheCreation += other.CacheCreation
	u.CacheRead += other.CacheRead
}

func computeCost(u TokenUsage, model string) float64 {
	p := priceFor(model)
	return float64(u.Input)*p.Input/1_000_000 +
		float64(u.Output)*p.Output/1_000_000 +
		float64(u.CacheCreation)*p.CacheCreation/1_000_000 +
		float64(u.CacheRead)*p.CacheRead/1_000_000
}

func modelTier(model string) string {
	ml := strings.ToLower(model)
	switch {
	case strings.Contains(ml, "opus"):
		return "opus"
	case strings.Contains(ml, "sonnet"):
		return "sonnet"
	case strings.Contains(ml, "haiku"):
		return "haiku"
	default:
		return "other"
	}
}

// RunRecord is the per-daemon-run result after joining events + transcripts.
type RunRecord struct {
	RunID         string          `json:"run_id"`
	BeadID        string          `json:"bead_id"`
	NodeID        string          `json:"node_id,omitempty"`
	QueueID       string          `json:"queue_id,omitempty"`
	StartedAt     string          `json:"started_at,omitempty"`
	EndedAt       string          `json:"ended_at,omitempty"`
	Success       bool            `json:"success"`
	TurnCount     int             `json:"turn_count"`
	Models        map[string]int  `json:"models"`
	DominantModel string          `json:"dominant_model"`
	Usage         TokenUsage      `json:"usage"`
	CostUSD       float64         `json:"cost_usd"`
	HourBuckets   map[string]bool `json:"-"`
}

// BeadRecord aggregates all runs for one bead.
type BeadRecord struct {
	BeadID        string         `json:"bead_id"`
	RunCount      int            `json:"run_count"`
	Usage         TokenUsage     `json:"usage"`
	CostUSD       float64        `json:"cost_usd"`
	Models        map[string]int `json:"models"`
	DominantModel string         `json:"dominant_model"`
	NodeIDs       []string       `json:"node_ids,omitempty"`
	CacheReadPct  float64        `json:"cache_read_pct"`
}

// OrchestratorSession describes a long-lived non-daemon session (captain/crew).
type OrchestratorSession struct {
	SessionID     string         `json:"session_id"`
	SessionFile   string         `json:"session_file"`
	Type          string         `json:"type"`
	FirstTS       string         `json:"first_ts,omitempty"`
	LastTS        string         `json:"last_ts,omitempty"`
	TurnCount     int            `json:"turn_count"`
	Models        map[string]int `json:"models"`
	DominantModel string         `json:"dominant_model"`
	Usage         TokenUsage     `json:"usage"`
	CostUSD       float64        `json:"cost_usd"`
	Branches      []string       `json:"branches,omitempty"`
}

// ModelStat holds aggregated cost + tokens for one model across the window.
type ModelStat struct {
	Cost    float64    `json:"cost_usd"`
	CostPct float64    `json:"cost_pct"`
	Tokens  TokenUsage `json:"tokens"`
}

// TierStat holds aggregated cost for one model tier (opus/sonnet/haiku/other).
type TierStat struct {
	Cost float64 `json:"cost_usd"`
	Pct  float64 `json:"pct"`
}

// HourStat holds cost + tokens for one UTC hour bucket.
type HourStat struct {
	Cost   float64    `json:"cost_usd"`
	Tokens TokenUsage `json:"tokens"`
}

// AnalysisResult is the complete output of RunAnalysis.
type AnalysisResult struct {
	Window              struct{ Since, Until string } `json:"window"`
	TotalCostUSD        float64                       `json:"total_cost_usd"`
	ProductiveCostUSD   float64                       `json:"productive_cost_usd"`
	OrchestratorCostUSD float64                       `json:"orchestrator_cost_usd"`
	ProductivePct       float64                       `json:"productive_pct"`
	IdlePct             float64                       `json:"idle_pct"`
	GlobalUsage         TokenUsage                    `json:"global_usage"`
	CacheReadPct        float64                       `json:"cache_read_pct"`
	BeadCount           int                           `json:"bead_count"`
	RunCount            int                           `json:"run_count"`
	OrchSessionCount    int                           `json:"orch_session_count"`
	ByModel             map[string]ModelStat          `json:"by_model"`
	ByTier              map[string]TierStat           `json:"by_tier"`
	ByHour              map[string]HourStat           `json:"by_hour"`
	TopBeads            []BeadRecord                  `json:"top_beads"`
	TopRuns             []RunRecord                   `json:"top_runs"`
	TopOrchestrators    []OrchestratorSession         `json:"top_orchestrators"`
	Warnings            []string                      `json:"warnings"`
}

// ──────────────────────────────────────────────────────────────────────────────
// Config
// ──────────────────────────────────────────────────────────────────────────────

// Config controls the analysis window and file locations.
type Config struct {
	// Since / Until are normalized ISO UTC timestamps ("YYYY-MM-DDTHH:MM:SSZ").
	Since string
	Until string
	// EventsFile is the absolute path to events.jsonl.
	EventsFile string
	// ClaudeProjectsDir is ~/.claude/projects.
	ClaudeProjectsDir string
}

// DefaultConfig returns a Config with paths set to the standard harmonik
// locations. Since defaults to 24 hours ago, Until to now.
func DefaultConfig(projectDir string) Config {
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)
	return Config{
		Since:             normTS(since.Format(time.RFC3339)),
		Until:             normTS(now.Format(time.RFC3339)),
		EventsFile:        filepath.Join(projectDir, ".harmonik", "events", "events.jsonl"),
		ClaudeProjectsDir: filepath.Join(os.Getenv("HOME"), ".claude", "projects"),
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Timestamp helpers
// ──────────────────────────────────────────────────────────────────────────────

var tsRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})`)

// NormTS normalizes any ISO timestamp to a comparable UTC string (strip tz offset).
func NormTS(ts string) string { return normTS(ts) }

func normTS(ts string) string {
	if ts == "" {
		return ""
	}
	if m := tsRe.FindStringSubmatch(ts); m != nil {
		return m[1] + "Z"
	}
	if len(ts) >= 20 {
		return strings.TrimRight(ts[:20], "T") + "Z"
	}
	return ts
}

// ParseSince parses a --since value: ISO timestamp or duration shorthand (24h, 7d, …).
func ParseSince(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty --since value")
	}
	// Try duration shorthand first: ends with h, d, m, s.
	if d, err := parseDurationShorthand(s); err == nil {
		return normTS(time.Now().UTC().Add(-d).Format(time.RFC3339)), nil
	}
	// Try ISO parse: must match the YYYY-MM-DDTHH:MM:SS prefix.
	if m := tsRe.FindStringSubmatch(s); m != nil {
		return m[1] + "Z", nil
	}
	return "", fmt.Errorf("cannot parse --since %q: expected ISO timestamp or duration (e.g. 24h, 7d)", s)
}

func parseDurationShorthand(s string) (time.Duration, error) {
	// Support Nd (N days) as well as Go duration strings.
	if strings.HasSuffix(s, "d") {
		n := 0
		if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}

// ──────────────────────────────────────────────────────────────────────────────
// Phase 1 — Build event index from events.jsonl
// ──────────────────────────────────────────────────────────────────────────────

type eventRun struct {
	RunID      string
	BeadID     string
	NodeID     string
	QueueID    string
	LogPaths   []string
	SessionIDs []string
	StartedAt  string
	EndedAt    string
	Success    bool
	SuccessSet bool
}

type eventIndex struct {
	Runs     map[string]*eventRun
	Warnings []string
}

func buildEventIndex(eventsFile, since, until string) (*eventIndex, error) {
	//nolint:gosec // G304: eventsFile derived from operator-provided projectDir; not user input.
	f, err := os.Open(eventsFile)
	if err != nil {
		return &eventIndex{Runs: map[string]*eventRun{}}, nil // events file absent = no runs
	}
	defer f.Close()

	idx := &eventIndex{Runs: map[string]*eventRun{}}
	sessionIDSeen := map[string]map[string]bool{} // run_id → set of session_ids

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

		ts := jsonStr(ev["timestamp_wall"])
		tsNorm := normTS(ts)
		if tsNorm < since || tsNorm > until {
			continue
		}

		runID := jsonStr(ev["run_id"])
		evType := jsonStr(ev["type"])

		r, ok := idx.Runs[runID]
		if !ok {
			r = &eventRun{RunID: runID}
			idx.Runs[runID] = r
			sessionIDSeen[runID] = map[string]bool{}
		}

		var payload map[string]json.RawMessage
		if ev["payload"] != nil {
			_ = json.Unmarshal(ev["payload"], &payload)
		}

		switch evType {
		case "run_started":
			if r.BeadID == "" {
				r.BeadID = jsonStr(payload["bead_id"])
			}
			if r.QueueID == "" {
				r.QueueID = jsonStr(payload["queue_id"])
			}
			if r.StartedAt == "" {
				r.StartedAt = jsonStr(payload["started_at"])
			}
		case "run_completed", "run_failed":
			if r.BeadID == "" {
				r.BeadID = jsonStr(payload["bead_id"])
			}
			if r.QueueID == "" {
				r.QueueID = jsonStr(payload["queue_id"])
			}
			if r.EndedAt == "" {
				r.EndedAt = jsonStr(payload["ended_at"])
			}
			if !r.SuccessSet {
				if evType == "run_completed" {
					r.Success = true
				} else {
					// check payload.success
					if s := jsonStr(payload["success"]); s == "true" {
						r.Success = true
					}
				}
				r.SuccessSet = true
			}
		case "launch_initiated", "handler_capabilities":
			csid := jsonStr(payload["claude_session_id"])
			if csid != "" && !sessionIDSeen[runID][csid] {
				sessionIDSeen[runID][csid] = true
				r.SessionIDs = append(r.SessionIDs, csid)
			}
		case "session_log_location":
			if r.NodeID == "" {
				r.NodeID = jsonStr(payload["node_id"])
			}
			lp := jsonStr(payload["log_path"])
			if lp != "" {
				dup := false
				for _, existing := range r.LogPaths {
					if existing == lp {
						dup = true
						break
					}
				}
				if !dup {
					r.LogPaths = append(r.LogPaths, lp)
				}
			}
		}
	}
	return idx, sc.Err()
}

func jsonStr(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return strings.Trim(string(raw), `"`)
	}
	return s
}

// ──────────────────────────────────────────────────────────────────────────────
// Phase 2 — Read Claude transcripts
// ──────────────────────────────────────────────────────────────────────────────

type transcriptTurn struct {
	Timestamp string
	Model     string
	Usage     TokenUsage
	GitBranch string
}

func readTranscript(path, since, until string) ([]transcriptTurn, error) {
	//nolint:gosec // G304: path is operator-controlled (eventsFile log_path or ClaudeProjectsDir scan).
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

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
		if jsonStr(entry["type"]) != "assistant" {
			continue
		}
		ts := jsonStr(entry["timestamp"])
		tsNorm := normTS(ts)
		if tsNorm < since || tsNorm > until {
			continue
		}
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(entry["message"], &msg); err != nil {
			continue
		}
		var rawUsage map[string]json.RawMessage
		if msg["usage"] == nil {
			continue
		}
		if err := json.Unmarshal(msg["usage"], &rawUsage); err != nil {
			continue
		}
		u := TokenUsage{
			Input:         jsonInt64(rawUsage["input_tokens"]),
			Output:        jsonInt64(rawUsage["output_tokens"]),
			CacheCreation: jsonInt64(rawUsage["cache_creation_input_tokens"]),
			CacheRead:     jsonInt64(rawUsage["cache_read_input_tokens"]),
		}
		turns = append(turns, transcriptTurn{
			Timestamp: ts,
			Model:     jsonStr(msg["model"]),
			Usage:     u,
			GitBranch: jsonStr(entry["gitBranch"]),
		})
	}
	return turns, sc.Err()
}

func jsonInt64(raw json.RawMessage) int64 {
	if raw == nil {
		return 0
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	return n
}

// resolveTranscriptPath translates a log_path from events.jsonl (which may use
// a different slug encoding than actual disk paths) to the real file path.
func resolveTranscriptPath(logPath, claudeProjectsDir string) string {
	if logPath == "" {
		return ""
	}
	if _, err := os.Stat(logPath); err == nil {
		return logPath
	}
	fname := filepath.Base(logPath)

	// Pattern: .../worktrees-<run_id>/<session.jsonl>
	if m := regexp.MustCompile(`worktrees-([0-9a-f-]{36})/([^/]+\.jsonl)$`).FindStringSubmatch(logPath); m != nil {
		runID := m[1]
		candidate := filepath.Join(claudeProjectsDir,
			fmt.Sprintf("-Users-%s-github-harmonik--harmonik-worktrees-%s",
				os.Getenv("USER"), runID), fname)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		// reviewer variants
		for _, suffix := range []string{"-reviewer-1", "-reviewer-2", "-reviewer-3"} {
			c2 := filepath.Join(claudeProjectsDir,
				fmt.Sprintf("-Users-%s-github-harmonik--harmonik-worktrees-%s%s",
					os.Getenv("USER"), runID, suffix), fname)
			if _, err := os.Stat(c2); err == nil {
				return c2
			}
		}
	}

	// Generic fallback: scan all project dirs for the session file name.
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

// ──────────────────────────────────────────────────────────────────────────────
// Phase 3 — Find long-lived orchestrator sessions
// ──────────────────────────────────────────────────────────────────────────────

func findOrchestratorSessions(claudeProjectsDir, since, until string, knownSessionIDs map[string]bool) ([]OrchestratorSession, error) {
	// Main harmonik project dir: -Users-<user>-github-harmonik
	user := os.Getenv("USER")
	mainProjectDir := filepath.Join(claudeProjectsDir, fmt.Sprintf("-Users-%s-github-harmonik", user))
	if _, err := os.Stat(mainProjectDir); err != nil {
		return nil, nil
	}

	//nolint:gosec // G304: mainProjectDir derived from ClaudeProjectsDir (operator config) + USER env.
	entries, err := os.ReadDir(mainProjectDir)
	if err != nil {
		return nil, err
	}

	var sessions []OrchestratorSession
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
		if knownSessionIDs[sessionID] {
			continue
		}

		fpath := filepath.Join(mainProjectDir, e.Name())
		sess, err := analyzeOrchSession(fpath, sessionID, since, until)
		if err != nil || sess.TurnCount == 0 {
			continue
		}
		// Only non-run branches qualify as orchestrators.
		allRunBranches := true
		for _, b := range sess.Branches {
			if !strings.HasPrefix(b, "run/") {
				allRunBranches = false
				break
			}
		}
		if allRunBranches && len(sess.Branches) > 0 {
			continue // all run/ branches → daemon run, not an orchestrator
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

func analyzeOrchSession(fpath, sessionID, since, until string) (OrchestratorSession, error) {
	sess := OrchestratorSession{
		SessionID:   sessionID,
		SessionFile: fpath,
		Type:        "orchestrator",
		Models:      map[string]int{},
	}
	branchSet := map[string]bool{}

	turns, err := readTranscript(fpath, since, until)
	if err != nil {
		return sess, err
	}
	for _, t := range turns {
		sess.TurnCount++
		sess.Models[t.Model]++
		sess.Usage.Add(t.Usage)
		if t.GitBranch != "" {
			branchSet[t.GitBranch] = true
		}
		if sess.FirstTS == "" {
			sess.FirstTS = t.Timestamp
		}
		sess.LastTS = t.Timestamp
	}

	for b := range branchSet {
		sess.Branches = append(sess.Branches, b)
	}
	sort.Strings(sess.Branches)

	if len(sess.Models) > 0 {
		sess.DominantModel = dominantKey(sess.Models)
	}

	// Compute cost weighted by model share.
	totalTurns := 0
	for _, c := range sess.Models {
		totalTurns += c
	}
	for m, cnt := range sess.Models {
		frac := float64(cnt) / math.Max(1, float64(totalTurns))
		sess.CostUSD += computeCost(sess.Usage, m) * frac
	}

	return sess, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Main analysis
// ──────────────────────────────────────────────────────────────────────────────

// RunAnalysis performs the full transcript × events join for the given window.
func RunAnalysis(cfg Config) (*AnalysisResult, error) {
	result := &AnalysisResult{
		ByModel: map[string]ModelStat{},
		ByTier:  map[string]TierStat{},
		ByHour:  map[string]HourStat{},
	}
	result.Window.Since = cfg.Since
	result.Window.Until = cfg.Until

	// Phase 1: event index.
	idx, err := buildEventIndex(cfg.EventsFile, cfg.Since, cfg.Until)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	result.Warnings = append(result.Warnings, idx.Warnings...)

	// Phase 2: join transcripts.
	runRecords := map[string]*RunRecord{}
	beadRecords := map[string]*BeadRecord{}
	knownSessionIDs := map[string]bool{}

	for runID, r := range idx.Runs {
		if r.BeadID == "" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("run %s: no bead_id in events", runID))
			continue
		}

		var allTurns []transcriptTurn

		// Primary: session_log_location paths.
		for _, lp := range r.LogPaths {
			resolved := resolveTranscriptPath(lp, cfg.ClaudeProjectsDir)
			if resolved == "" {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("run %s bead %s: transcript not found: %s", runID, r.BeadID, lp))
				continue
			}
			sid := strings.TrimSuffix(filepath.Base(resolved), ".jsonl")
			knownSessionIDs[sid] = true
			turns, _ := readTranscript(resolved, cfg.Since, cfg.Until)
			allTurns = append(allTurns, turns...)
		}

		// Fallback: claude_session_id scan.
		if len(allTurns) == 0 && len(r.SessionIDs) > 0 {
			//nolint:gosec // G304: ClaudeProjectsDir is operator-supplied config, not user input.
			projectEntries, _ := os.ReadDir(cfg.ClaudeProjectsDir)
			for _, csid := range r.SessionIDs {
				knownSessionIDs[csid] = true
				for _, e := range projectEntries {
					if !e.IsDir() {
						continue
					}
					fp := filepath.Join(cfg.ClaudeProjectsDir, e.Name(), csid+".jsonl")
					if _, statErr := os.Stat(fp); statErr == nil {
						turns, _ := readTranscript(fp, cfg.Since, cfg.Until)
						allTurns = append(allTurns, turns...)
						break
					}
				}
			}
		}

		if len(allTurns) == 0 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("run %s bead %s: no transcript data found", runID, r.BeadID))
		}

		// Aggregate per run.
		var usageTotal TokenUsage
		models := map[string]int{}
		var costTotal float64
		for _, t := range allTurns {
			usageTotal.Add(t.Usage)
			models[t.Model]++
			costTotal += computeCost(t.Usage, t.Model)
		}
		dominantModel := dominantKey(models)

		rr := &RunRecord{
			RunID:         runID,
			BeadID:        r.BeadID,
			NodeID:        r.NodeID,
			QueueID:       r.QueueID,
			StartedAt:     r.StartedAt,
			EndedAt:       r.EndedAt,
			Success:       r.Success,
			TurnCount:     len(allTurns),
			Models:        models,
			DominantModel: dominantModel,
			Usage:         usageTotal,
			CostUSD:       costTotal,
		}
		runRecords[runID] = rr

		// Roll up per bead.
		br, ok := beadRecords[r.BeadID]
		if !ok {
			br = &BeadRecord{BeadID: r.BeadID, Models: map[string]int{}}
			beadRecords[r.BeadID] = br
		}
		br.RunCount++
		br.Usage.Add(usageTotal)
		br.CostUSD += costTotal
		for m, c := range models {
			br.Models[m] += c
		}
		if r.NodeID != "" {
			found := false
			for _, nid := range br.NodeIDs {
				if nid == r.NodeID {
					found = true
					break
				}
			}
			if !found {
				br.NodeIDs = append(br.NodeIDs, r.NodeID)
			}
		}
	}

	// Finalize bead records.
	for _, br := range beadRecords {
		br.DominantModel = dominantKey(br.Models)
		br.CacheReadPct = br.Usage.CacheReadPct()
	}

	// Phase 3: orchestrator sessions.
	orchSessions, _ := findOrchestratorSessions(cfg.ClaudeProjectsDir, cfg.Since, cfg.Until, knownSessionIDs)

	// Global rollups.
	var productiveCost, orchCost float64
	var globalUsage TokenUsage
	for _, rr := range runRecords {
		productiveCost += rr.CostUSD
		globalUsage.Add(rr.Usage)
	}
	for _, s := range orchSessions {
		orchCost += s.CostUSD
		globalUsage.Add(s.Usage)
	}
	totalCost := productiveCost + orchCost

	result.TotalCostUSD = totalCost
	result.ProductiveCostUSD = productiveCost
	result.OrchestratorCostUSD = orchCost
	if totalCost > 0 {
		result.ProductivePct = 100.0 * productiveCost / totalCost
		result.IdlePct = 100.0 * orchCost / totalCost
	}
	result.GlobalUsage = globalUsage
	result.CacheReadPct = globalUsage.CacheReadPct()
	result.BeadCount = len(beadRecords)
	result.RunCount = len(runRecords)
	result.OrchSessionCount = len(orchSessions)

	// By-model accumulation.
	byModel := map[string]*ModelStat{}
	accumModel := func(models map[string]int, usage TokenUsage, cost float64) {
		total := 0
		for _, c := range models {
			total += c
		}
		for m, cnt := range models {
			frac := float64(cnt) / math.Max(1, float64(total))
			s, ok := byModel[m]
			if !ok {
				s = &ModelStat{}
				byModel[m] = s
			}
			s.Cost += cost * frac
			u := TokenUsage{
				Input:         int64(float64(usage.Input) * frac),
				Output:        int64(float64(usage.Output) * frac),
				CacheCreation: int64(float64(usage.CacheCreation) * frac),
				CacheRead:     int64(float64(usage.CacheRead) * frac),
			}
			s.Tokens.Add(u)
		}
	}
	for _, rr := range runRecords {
		accumModel(rr.Models, rr.Usage, rr.CostUSD)
	}
	for _, s := range orchSessions {
		accumModel(s.Models, s.Usage, s.CostUSD)
	}
	for m, s := range byModel {
		if totalCost > 0 {
			s.CostPct = 100.0 * s.Cost / totalCost
		}
		result.ByModel[m] = *s
	}

	// By-tier.
	tierCost := map[string]float64{}
	for m, s := range byModel {
		tierCost[modelTier(m)] += s.Cost
	}
	totalTier := 0.0
	for _, c := range tierCost {
		totalTier += c
	}
	for tier, c := range tierCost {
		pct := 0.0
		if totalTier > 0 {
			pct = 100.0 * c / totalTier
		}
		result.ByTier[tier] = TierStat{Cost: c, Pct: pct}
	}

	// By-hour (approximate: use run start_at or orch first_ts).
	byHour := map[string]*HourStat{}
	accumHour := func(ts string, usage TokenUsage, cost float64) {
		hour := "unknown"
		n := normTS(ts)
		if len(n) >= 14 {
			hour = n[:14]
		}
		s, ok := byHour[hour]
		if !ok {
			s = &HourStat{}
			byHour[hour] = s
		}
		s.Cost += cost
		s.Tokens.Add(usage)
	}
	for _, rr := range runRecords {
		accumHour(rr.StartedAt, rr.Usage, rr.CostUSD)
	}
	for _, s := range orchSessions {
		accumHour(s.FirstTS, s.Usage, s.CostUSD)
	}
	for h, s := range byHour {
		result.ByHour[h] = *s
	}

	// Top N.
	runSlice := make([]RunRecord, 0, len(runRecords))
	for _, rr := range runRecords {
		runSlice = append(runSlice, *rr)
	}
	sort.Slice(runSlice, func(i, j int) bool { return runSlice[i].CostUSD > runSlice[j].CostUSD })
	if len(runSlice) > 10 {
		runSlice = runSlice[:10]
	}
	result.TopRuns = runSlice

	beadSlice := make([]BeadRecord, 0, len(beadRecords))
	for _, br := range beadRecords {
		beadSlice = append(beadSlice, *br)
	}
	sort.Slice(beadSlice, func(i, j int) bool { return beadSlice[i].CostUSD > beadSlice[j].CostUSD })
	if len(beadSlice) > 10 {
		beadSlice = beadSlice[:10]
	}
	result.TopBeads = beadSlice

	sort.Slice(orchSessions, func(i, j int) bool { return orchSessions[i].CostUSD > orchSessions[j].CostUSD })
	if len(orchSessions) > 10 {
		orchSessions = orchSessions[:10]
	}
	result.TopOrchestrators = orchSessions

	return result, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Formatters
// ──────────────────────────────────────────────────────────────────────────────

func fmtDollars(v float64) string { return fmt.Sprintf("$%.4f", v) }

func fmtTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// PrintSummary writes a one-screen human-readable summary to w.
func PrintSummary(r *AnalysisResult, w io.Writer) {
	p := func(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }
	gu := r.GlobalUsage

	p("======================================================================")
	p("  HARMONIK TOKEN USAGE ANALYSIS")
	p("  Window: %s  →  %s", r.Window.Since, r.Window.Until)
	p("======================================================================")
	p("")
	p("  TOTAL COST:        %s", fmtDollars(r.TotalCostUSD))
	p("  Productive (bead): %s  (%.1f%%)", fmtDollars(r.ProductiveCostUSD), r.ProductivePct)
	p("  Idle/Orchestrator: %s  (%.1f%%)", fmtDollars(r.OrchestratorCostUSD), r.IdlePct)
	p("")
	p("  Beads attributed:  %d", r.BeadCount)
	p("  Daemon runs:       %d", r.RunCount)
	p("  Orch sessions:     %d", r.OrchSessionCount)
	p("")
	tokTotal := gu.Total()
	p("  TOKEN TOTALS:      %s total", fmtTokens(tokTotal))
	p("    input:           %s", fmtTokens(gu.Input))
	p("    output:          %s", fmtTokens(gu.Output))
	p("    cache_creation:  %s", fmtTokens(gu.CacheCreation))
	p("    cache_read:      %s  (%.1f%% of total)", fmtTokens(gu.CacheRead), r.CacheReadPct)
	p("")

	p("  BY MODEL TIER (share of total spend):")
	tiers := make([]string, 0, len(r.ByTier))
	for t := range r.ByTier {
		tiers = append(tiers, t)
	}
	sort.Slice(tiers, func(i, j int) bool { return r.ByTier[tiers[i]].Cost > r.ByTier[tiers[j]].Cost })
	for _, tier := range tiers {
		d := r.ByTier[tier]
		p("    %-10s  %s  (%.1f%%)", tier, fmtDollars(d.Cost), d.Pct)
	}
	p("")

	p("  BY MODEL (detailed):")
	models := make([]string, 0, len(r.ByModel))
	for m := range r.ByModel {
		models = append(models, m)
	}
	sort.Slice(models, func(i, j int) bool { return r.ByModel[models[i]].Cost > r.ByModel[models[j]].Cost })
	for _, m := range models {
		d := r.ByModel[m]
		p("    %-28s  %s  (%.1f%%)", m, fmtDollars(d.Cost), d.CostPct)
	}
	p("")

	if len(r.TopBeads) > 0 {
		p("  TOP 10 BEADS BY COST:")
		for i, b := range r.TopBeads {
			p("    %2d. %-12s  %s  runs=%d  model=%s  cache_read=%.0f%%",
				i+1, b.BeadID, fmtDollars(b.CostUSD), b.RunCount, b.DominantModel, b.CacheReadPct)
		}
		p("")
	}

	if len(r.TopRuns) > 0 {
		p("  TOP 10 DAEMON RUNS BY COST:")
		for i, rr := range r.TopRuns {
			ok := "FAIL"
			if rr.Success {
				ok = "OK"
			}
			p("    %2d. bead=%-12s  %s  %s  model=%s  turns=%d",
				i+1, rr.BeadID, fmtDollars(rr.CostUSD), ok, rr.DominantModel, rr.TurnCount)
		}
		p("")
	}

	if len(r.TopOrchestrators) > 0 {
		p("  TOP ORCHESTRATOR SESSIONS (always-on burn):")
		for i, s := range r.TopOrchestrators {
			shortID := s.SessionID
			if len(shortID) > 8 {
				shortID = shortID[:8] + "…"
			}
			p("    %d. session=%s  %s  model=%s  turns=%d",
				i+1, shortID, fmtDollars(s.CostUSD), s.DominantModel, s.TurnCount)
			if s.FirstTS != "" && s.LastTS != "" {
				first := s.FirstTS
				if len(first) > 19 {
					first = first[:19]
				}
				last := s.LastTS
				if len(last) > 19 {
					last = last[:19]
				}
				p("       %s → %s", first, last)
			}
		}
		p("")
	}

	if len(r.ByHour) > 0 {
		p("  HOURLY SHAPE (cost by hour, UTC):")
		hours := make([]string, 0, len(r.ByHour))
		for h := range r.ByHour {
			hours = append(hours, h)
		}
		sort.Strings(hours)
		maxCost := 0.0
		for _, h := range hours {
			if r.ByHour[h].Cost > maxCost {
				maxCost = r.ByHour[h].Cost
			}
		}
		for _, h := range hours {
			d := r.ByHour[h]
			barLen := 0
			if maxCost > 0 {
				barLen = int(d.Cost / maxCost * 30)
			}
			if barLen < 1 && d.Cost > 0 {
				barLen = 1
			}
			bar := strings.Repeat("█", barLen)
			p("    %s  %s  %s", h, fmtDollars(d.Cost), bar)
		}
		p("")
	}

	if len(r.Warnings) > 0 {
		p("  WARNINGS (%d coverage gaps):", len(r.Warnings))
		limit := 15
		if len(r.Warnings) < limit {
			limit = len(r.Warnings)
		}
		for _, w := range r.Warnings[:limit] {
			p("    ⚠  %s", w)
		}
		if len(r.Warnings) > 15 {
			p("    … and %d more", len(r.Warnings)-15)
		}
		p("")
	}

	p("======================================================================")
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func dominantKey(m map[string]int) string {
	best, bestCount := "unknown", -1
	for k, c := range m {
		if c > bestCount {
			bestCount = c
			best = k
		}
	}
	return best
}
