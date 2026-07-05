package daemon

// dashboardgather.go — DashboardBuilder for `harmonik dashboard` (hk-2exz9).
//
// DashboardBuilder joins LiveStateBuilder.Build() with captain-curated files
// (dashboard.json, lanes.json), a windowed session-data.jsonl aggregation,
// open decisions, and active stall signals from events.jsonl.
// Read-only; no new persisted store.
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §2.
// Bead ref: hk-2exz9.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/presence"
)

const (
	dashboardJSONPath = ".harmonik/context/dashboard.json"
	lanesJSONPath     = ".harmonik/context/lanes.json"
	sessionDataJSONL  = ".harmonik/session-data.jsonl"
	dashStallWindowH  = 4 // hours back to look for stall events
	throughputWindowH = 24
)

// DashboardBuilder assembles a DashboardSnapshot from in-daemon memory + disk.
type DashboardBuilder struct {
	state      *LiveStateBuilder
	projectDir string
	eventsPath string
}

// NewDashboardBuilder constructs a DashboardBuilder.
// eventsPath is the absolute path to events.jsonl (cfg.JSONLLogPath).
func NewDashboardBuilder(stateBuilder *LiveStateBuilder, projectDir, eventsPath string) *DashboardBuilder {
	return &DashboardBuilder{
		state:      stateBuilder,
		projectDir: projectDir,
		eventsPath: eventsPath,
	}
}

// Build assembles and returns a DashboardSnapshot.
func (b *DashboardBuilder) Build(ctx context.Context) DashboardSnapshot {
	now := time.Now().UTC()

	// Tier A: live state (reuse verbatim).
	stateSnap := b.state.Build(ctx)

	// Collect active run IDs for stall cross-reference.
	activeRunIDs := make(map[string]bool, len(stateSnap.Runs))
	for _, r := range stateSnap.Runs {
		activeRunIDs[r.RunID] = true
	}

	snap := DashboardSnapshot{
		SchemaVersion: 1,
		CapturedAt:    now.Format(time.RFC3339),
		State:         stateSnap,
	}

	// Tier B: captain-curated planning layer.
	snap.Config = b.readDashboardConfig()
	snap.Lanes = b.readLanes()

	// Open decisions (hitl-decisions K3 projection).
	snap.OpenDecisions = b.readOpenDecisions()

	// Active stalls from events.jsonl.
	snap.ActiveStalls = b.readActiveStalls(now, activeRunIDs)

	// Windowed throughput from session-data.jsonl.
	snap.Throughput = b.readThroughput(now)

	return snap
}

// readDashboardConfig reads .harmonik/context/dashboard.json.
// Returns nil when the file is absent or malformed (the file is optional —
// it starts empty until the captain creates it).
func (b *DashboardBuilder) readDashboardConfig() *DashboardConfig {
	if b.projectDir == "" {
		return nil
	}
	path := filepath.Join(b.projectDir, dashboardJSONPath)
	data, err := os.ReadFile(path) //nolint:gosec // G304: operator-controlled projectDir
	if err != nil {
		return nil
	}
	var cfg DashboardConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// readLanes reads .harmonik/context/lanes.json.
// Returns nil slice when the file is absent or malformed.
func (b *DashboardBuilder) readLanes() []DashLane {
	if b.projectDir == "" {
		return nil
	}
	path := filepath.Join(b.projectDir, lanesJSONPath)
	data, err := os.ReadFile(path) //nolint:gosec // G304: operator-controlled projectDir
	if err != nil {
		return nil
	}
	var lf lanesFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil
	}
	return lf.Lanes
}

// readOpenDecisions projects the hitl-decisions open set from events.jsonl.
func (b *DashboardBuilder) readOpenDecisions() []DashDecision {
	if b.eventsPath == "" {
		return nil
	}
	open := presence.OpenDecisions(b.eventsPath)
	if len(open) == 0 {
		return nil
	}
	out := make([]DashDecision, 0, len(open))
	for _, d := range open {
		out = append(out, DashDecision{
			DecisionID:     d.DecisionID,
			Question:       d.Question,
			Options:        d.Options,
			BlockedAgent:   d.BlockedAgent,
			ContextLink:    d.ContextLink,
			ValueRequested: d.ValueRequested,
		})
	}
	return out
}

// readActiveStalls scans events.jsonl for stall_detected events within the
// last dashStallWindowH hours whose run_id is still in activeRunIDs.
func (b *DashboardBuilder) readActiveStalls(now time.Time, activeRunIDs map[string]bool) []DashStall {
	if b.eventsPath == "" {
		return nil
	}

	cutoff := now.Add(-time.Duration(dashStallWindowH) * time.Hour)
	var zeroID core.EventID
	var out []DashStall

	for ev := range eventbus.ScanAfter(b.eventsPath, zeroID) {
		if ev.Type != string(core.EventTypeStallDetected) {
			continue
		}
		// Filter by window using the wall timestamp.
		if !ev.TimestampWall.IsZero() && ev.TimestampWall.Before(cutoff) {
			continue
		}
		var p core.StallDetectedPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			continue
		}
		if !p.Valid() {
			continue
		}
		// Only report stalls for currently-active runs.
		if !activeRunIDs[p.RunID] {
			continue
		}
		out = append(out, DashStall{
			RunID:     p.RunID,
			BeadID:    p.BeadID,
			Signature: string(p.Signature),
			ElapsedMs: p.ElapsedMs,
		})
	}
	return out
}

// readThroughput reads a windowed aggregate from session-data.jsonl.
// Returns {available: false} when the file does not exist.
func (b *DashboardBuilder) readThroughput(now time.Time) *DashThroughput {
	if b.projectDir == "" {
		return &DashThroughput{Available: false}
	}
	path := filepath.Join(b.projectDir, sessionDataJSONL)
	f, err := os.Open(path) //nolint:gosec // G304: operator-controlled projectDir
	if err != nil {
		return &DashThroughput{Available: false}
	}
	defer f.Close() //nolint:errcheck

	cutoff := now.Add(-time.Duration(throughputWindowH) * time.Hour)

	// Session-data records: one JSON object per line. Schema is WS1-defined.
	// Fields we need: lane (string), closed_at (RFC3339), wall_secs (int64).
	type sessionRecord struct {
		Lane     string `json:"lane"`
		ClosedAt string `json:"closed_at"`
		WallSecs int64  `json:"wall_secs"`
	}

	type laneAgg struct {
		count    int
		wallSecs int64
	}
	byLane := make(map[string]*laneAgg)
	totalCount := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec sessionRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.ClosedAt != "" {
			ts, parseErr := time.Parse(time.RFC3339, rec.ClosedAt)
			if parseErr != nil || ts.Before(cutoff) {
				continue
			}
		}
		agg := byLane[rec.Lane]
		if agg == nil {
			agg = &laneAgg{}
			byLane[rec.Lane] = agg
		}
		agg.count++
		agg.wallSecs += rec.WallSecs
		totalCount++
	}

	if totalCount == 0 {
		return &DashThroughput{Available: true, WindowH: throughputWindowH}
	}

	laneList := make([]DashLaneThroughput, 0, len(byLane))
	for lane, agg := range byLane {
		lt := DashLaneThroughput{
			Lane:        lane,
			BeadsClosed: agg.count,
		}
		if agg.count > 0 {
			lt.MeanWallSecs = agg.wallSecs / int64(agg.count)
		}
		laneList = append(laneList, lt)
	}

	return &DashThroughput{
		Available: true,
		WindowH:   throughputWindowH,
		ByLane:    laneList,
	}
}
