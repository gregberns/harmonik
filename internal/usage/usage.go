// Package usage implements the harmonik token-usage report.
//
// harmonik usage is a VIEW over <project>/.harmonik/session-data.jsonl, which
// the daemon populates at the end of every run via sessiondata.Collect. Per-run
// token counts and cost are pre-computed by the collector; this package only
// aggregates and formats them. Orchestrator sessions (captain/crew) are still
// derived live by scanning Claude transcripts because they are not run-dispatched.
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

	"github.com/gregberns/harmonik/internal/sessiondata"
)

// TokenUsage is the four-category token count; type is owned by sessiondata.
type TokenUsage = sessiondata.TokenUsage

// ──────────────────────────────────────────────────────────────────────────────
// Core data structures
// ──────────────────────────────────────────────────────────────────────────────

// RunRecord is the per-daemon-run result in AnalysisResult.
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
	// EventsFile is the absolute path to events.jsonl (kept for compat; not
	// used by RunAnalysis when session-data.jsonl is present).
	EventsFile string
	// ClaudeProjectsDir is ~/.claude/projects.
	ClaudeProjectsDir string
	// ProjectDir is the harmonik project root (for session-data.jsonl).
	// Defaults to the directory containing .harmonik/ when derived from EventsFile.
	ProjectDir string
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
		ProjectDir:        projectDir,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Timestamp helpers
// ──────────────────────────────────────────────────────────────────────────────

var tsRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})`)

// NormTS normalizes any ISO timestamp to a comparable UTC string.
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
	if d, err := parseDurationShorthand(s); err == nil {
		return normTS(time.Now().UTC().Add(-d).Format(time.RFC3339)), nil
	}
	if m := tsRe.FindStringSubmatch(s); m != nil {
		return m[1] + "Z", nil
	}
	return "", fmt.Errorf("cannot parse --since %q: expected ISO timestamp or duration (e.g. 24h, 7d)", s)
}

func parseDurationShorthand(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n := 0
		if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}

// ──────────────────────────────────────────────────────────────────────────────
// Main analysis — VIEW over session-data.jsonl
// ──────────────────────────────────────────────────────────────────────────────

// RunAnalysis performs the token-usage analysis for the given window.
// Primary source: <ProjectDir>/.harmonik/session-data.jsonl (pre-computed by the daemon).
// Orchestrator sessions (captain/crew) are always derived live from transcripts.
func RunAnalysis(cfg Config) (*AnalysisResult, error) {
	result := &AnalysisResult{
		ByModel: map[string]ModelStat{},
		ByTier:  map[string]TierStat{},
		ByHour:  map[string]HourStat{},
	}
	result.Window.Since = cfg.Since
	result.Window.Until = cfg.Until

	// Resolve project dir from EventsFile when ProjectDir is not set.
	projectDir := cfg.ProjectDir
	if projectDir == "" && cfg.EventsFile != "" {
		// EventsFile is <projectDir>/.harmonik/events/events.jsonl
		projectDir = filepath.Dir(filepath.Dir(filepath.Dir(cfg.EventsFile)))
	}

	// Phase 1: read pre-computed run records from session-data.jsonl.
	sdRecords, sdErr := sessiondata.ReadAll(projectDir, cfg.Since, cfg.Until)
	if sdErr != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("session-data.jsonl read error: %v", sdErr))
	}

	runRecords := map[string]*RunRecord{}
	beadRecords := map[string]*BeadRecord{}

	for i := range sdRecords {
		r := &sdRecords[i]
		model := r.Model
		if model == "" {
			model = "unknown"
		}
		costUSD := 0.0
		if r.CostUSD != nil {
			costUSD = *r.CostUSD
		}
		rr := &RunRecord{
			RunID:         r.RunID,
			BeadID:        r.BeadID,
			QueueID:       r.QueueID,
			StartedAt:     r.StartedAt,
			EndedAt:       r.EndedAt,
			Success:       r.Success,
			TurnCount:     r.TurnCount,
			Models:        map[string]int{model: r.TurnCount},
			DominantModel: model,
			Usage:         r.TokensTotal,
			CostUSD:       costUSD,
		}
		runRecords[r.RunID] = rr

		br, ok := beadRecords[r.BeadID]
		if !ok {
			br = &BeadRecord{BeadID: r.BeadID, Models: map[string]int{}}
			beadRecords[r.BeadID] = br
		}
		br.RunCount++
		br.Usage.Add(r.TokensTotal)
		br.CostUSD += costUSD
		br.Models[model] += r.TurnCount
	}

	for _, br := range beadRecords {
		br.DominantModel = dominantKey(br.Models)
		br.CacheReadPct = br.Usage.CacheReadPct()
	}

	// Collect session IDs attributed to daemon runs (to exclude from orchestrator scan).
	knownSessionIDs := map[string]bool{}

	// Phase 2: orchestrator sessions — live transcript scan (not in session-data.jsonl).
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

	// By-hour (use run start_at for daemon runs; orch first_ts for sessions).
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
// Phase 2 — Find long-lived orchestrator sessions
// ──────────────────────────────────────────────────────────────────────────────

func findOrchestratorSessions(claudeProjectsDir, since, until string, knownSessionIDs map[string]bool) ([]OrchestratorSession, error) {
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
		allRunBranches := true
		for _, b := range sess.Branches {
			if !strings.HasPrefix(b, "run/") {
				allRunBranches = false
				break
			}
		}
		if allRunBranches && len(sess.Branches) > 0 {
			continue
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

	totalTurns := 0
	for _, c := range sess.Models {
		totalTurns += c
	}
	for m, cnt := range sess.Models {
		frac := float64(cnt) / math.Max(1, float64(totalTurns))
		sess.CostUSD += sessiondata.ComputeCost(sess.Usage, m) * frac
	}

	return sess, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Transcript reader (for orchestrator sessions only)
// ──────────────────────────────────────────────────────────────────────────────

type transcriptTurn struct {
	Timestamp string
	Model     string
	Usage     TokenUsage
	GitBranch string
}

func readTranscript(path, since, until string) ([]transcriptTurn, error) {
	//nolint:gosec // G304: path is operator-controlled (ClaudeProjectsDir scan).
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
		if msg["usage"] == nil {
			continue
		}
		var rawUsage map[string]json.RawMessage
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
