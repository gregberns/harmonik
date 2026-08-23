package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/presence"
	"github.com/gregberns/harmonik/internal/sessiondata"
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

	stateSnap := b.state.Build(ctx)

	activeRunIDs := make(map[string]bool, len(stateSnap.Runs))
	for _, r := range stateSnap.Runs {
		activeRunIDs[r.RunID] = true
	}

	snap := DashboardSnapshot{
		SchemaVersion: 1,
		CapturedAt:    now.Format(time.RFC3339),
		State:         stateSnap,
	}

	snap.Config = b.readDashboardConfig()
	snap.Lanes = b.readLanes()

	snap.OpenDecisions = b.readOpenDecisions()

	snap.ActiveStalls = b.readActiveStalls(now, activeRunIDs)

	snap.Throughput = b.readThroughput(now)

	return snap
}

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
			Topic:          d.Topic,
			Urgency:        string(d.Urgency),
		})
	}
	return out
}

func (b *DashboardBuilder) readActiveStalls(now time.Time, activeRunIDs map[string]bool) []DashStall {
	if b.eventsPath == "" {
		return nil
	}

	cutoff := now.Add(-time.Duration(dashStallWindowH) * time.Hour)
	var zeroID core.EventID
	var out []DashStall

	for ev := range eventbus.ScanAfter(b.eventsPath, zeroID) {
		if ev.Type != core.EventTypeStallDetected {
			continue
		}
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

func (b *DashboardBuilder) readThroughput(now time.Time) *DashThroughput {
	if b.projectDir == "" {
		return &DashThroughput{Available: false}
	}
	path := filepath.Join(b.projectDir, sessionDataJSONL)
	if _, err := os.Stat(path); err != nil {
		return &DashThroughput{Available: false}
	}

	since := now.Add(-time.Duration(throughputWindowH) * time.Hour).UTC().Format(time.RFC3339)
	records, err := sessiondata.ReadAll(b.projectDir, since, "")
	if err != nil {
		return &DashThroughput{Available: false}
	}
	if len(records) == 0 {
		return &DashThroughput{Available: true, WindowH: throughputWindowH}
	}

	queueToLane := make(map[string]string)
	queueToCrew := make(map[string]string)
	for _, l := range b.readLanes() {
		if l.Queue == "" {
			continue
		}
		if l.Lane != "" {
			queueToLane[l.Queue] = l.Lane
		}
		if l.Crew != "" {
			queueToCrew[l.Queue] = l.Crew
		}
	}

	type group struct {
		key         groupKey
		wallSecs    []float64
		beadsClosed int
		byOutcome   map[string]*outcomeAgg
	}
	type laneAgg struct {
		runCount    int
		beadsClosed int
		wallSecs    float64
	}

	groups := make(map[groupKey]*group)
	lanes := make(map[string]*laneAgg)

	for _, rec := range records {
		gk := groupKey{Crew: queueToCrew[rec.QueueID], Queue: rec.QueueID, Harness: rec.Harness, Model: rec.Model}
		g := groups[gk]
		if g == nil {
			g = &group{key: gk, byOutcome: make(map[string]*outcomeAgg)}
			groups[gk] = g
		}
		g.wallSecs = append(g.wallSecs, rec.WallTimeS)
		if rec.Success {
			g.beadsClosed++
		}

		outcome := "failure"
		if rec.Success {
			outcome = "success"
		}
		oa := g.byOutcome[outcome]
		if oa == nil {
			oa = &outcomeAgg{}
			g.byOutcome[outcome] = oa
		}
		oa.count++
		oa.tokens.Add(rec.TokensTotal)
		if rec.CostUSD != nil {
			if oa.costUSD == nil {
				c := 0.0
				oa.costUSD = &c
			}
			*oa.costUSD += *rec.CostUSD
		}

		if lane := queueToLane[rec.QueueID]; lane != "" {
			la := lanes[lane]
			if la == nil {
				la = &laneAgg{}
				lanes[lane] = la
			}
			la.runCount++
			if rec.Success {
				la.beadsClosed++
			}
			la.wallSecs += rec.WallTimeS
		}
	}

	groupList := make([]DashGroupThroughput, 0, len(groups))
	for _, g := range groups {
		dg := DashGroupThroughput{
			Crew:        g.key.Crew,
			Queue:       g.key.Queue,
			Harness:     g.key.Harness,
			Model:       g.key.Model,
			RunCount:    len(g.wallSecs),
			BeadsClosed: g.beadsClosed,
		}
		dg.MeanWallSecs = meanF(g.wallSecs)
		dg.P50WallSecs = medianF(g.wallSecs)
		for _, outcome := range []string{"success", "failure"} {
			oa := g.byOutcome[outcome]
			if oa == nil {
				continue
			}
			dg.ByOutcome = append(dg.ByOutcome, DashOutcomeStats{
				Outcome:             outcome,
				Count:               oa.count,
				TokensInput:         oa.tokens.Input,
				TokensOutput:        oa.tokens.Output,
				TokensCacheCreation: oa.tokens.CacheCreation,
				TokensCacheRead:     oa.tokens.CacheRead,
				CostUSD:             oa.costUSD,
			})
		}
		groupList = append(groupList, dg)
	}
	sort.Slice(groupList, func(i, j int) bool {
		if groupList[i].Queue != groupList[j].Queue {
			return groupList[i].Queue < groupList[j].Queue
		}
		if groupList[i].Harness != groupList[j].Harness {
			return groupList[i].Harness < groupList[j].Harness
		}
		return groupList[i].Model < groupList[j].Model
	})

	laneList := make([]DashLaneThroughput, 0, len(lanes))
	for lane, agg := range lanes {
		lt := DashLaneThroughput{Lane: lane, BeadsClosed: agg.beadsClosed}
		if agg.runCount > 0 {
			lt.MeanWallSecs = int64(agg.wallSecs / float64(agg.runCount))
		}
		laneList = append(laneList, lt)
	}
	sort.Slice(laneList, func(i, j int) bool { return laneList[i].Lane < laneList[j].Lane })

	return &DashThroughput{
		Available: true,
		WindowH:   throughputWindowH,
		ByLane:    laneList,
		ByGroup:   groupList,
	}
}

type groupKey struct {
	Crew    string
	Queue   string
	Harness string
	Model   string
}

type outcomeAgg struct {
	count   int
	tokens  sessiondata.TokenUsage
	costUSD *float64
}

func meanF(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func medianF(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
